package xapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/youyo/x/internal/config"
)

// 既定値定数群。NewClient のオプションで上書きされない場合に使われる。
const (
	defaultBaseURL     = "https://api.x.com"
	defaultMaxRetries  = 3
	defaultBaseBackoff = 2 * time.Second
	defaultMaxBackoff  = 30 * time.Second
	// rateLimitMaxWait は 429 受信時に x-rate-limit-reset まで待つ際の安全上限である
	// (§10 ページング: 最大 15 分)。
	rateLimitMaxWait = 15 * time.Minute
)

// Client は X API v2 に対する HTTP リクエストを送出するための高レベルクライアントである。
//
// 内包する http.Client (NewHTTPClient 由来) が OAuth 1.0a 署名を担当し、
// Client 自身は次の責務を持つ:
//   - 429 / 5xx の自動リトライ (exponential backoff, 最大 maxRetries 回)
//   - x-rate-limit-* ヘッダのパースと Response への構造化
//   - 401/403/404/429 を番兵エラー (ErrAuthentication 等) にマッピング
//   - context.Cancel による sleep 中断
//
// ゼロ値は利用不可。必ず NewClient を介して構築する。
type Client struct {
	httpClient *http.Client
	baseURL    string

	sleep func(context.Context, time.Duration) error
	now   func() time.Time

	maxRetries  int
	baseBackoff time.Duration
	maxBackoff  time.Duration
}

// Response は xapi.Client.Do が返す HTTP レスポンスである。
// *http.Response を embed しつつ、X API のレートリミット情報を構造化して保持する。
// 呼び出し側は通常通り Body の Close 責務を持つ。
type Response struct {
	*http.Response
	// RateLimit は x-rate-limit-* ヘッダの構造化済み値である。
	RateLimit RateLimitInfo
}

// RateLimitInfo は X API レスポンスヘッダ x-rate-limit-* の構造化された値である。
//
// フィールドの解釈:
//   - Raw=false: ヘッダがレスポンスに含まれていなかった (= 値は未取得 / 信頼不可)
//   - Raw=true かつ Remaining=0: X API が「枠を使い切った」と明示的に返した
//
// Remaining=-1 はヘッダ未取得時のゼロ値であり、Raw=false と組で用いる。
type RateLimitInfo struct {
	// Limit は x-rate-limit-limit の値 (ウィンドウ全体の上限)。未取得時は 0。
	Limit int
	// Remaining は x-rate-limit-remaining の値 (残り呼び出し可能回数)。未取得時は -1。
	Remaining int
	// Reset は x-rate-limit-reset (UNIX 秒) を time.Time に変換した値。未取得時はゼロ値。
	Reset time.Time
	// Raw はヘッダのいずれか 1 つでも実際に存在した場合 true。
	Raw bool
}

// Option は NewClient の挙動を変更するための関数オプションである。
type Option func(*Client)

// WithBaseURL は X API ベース URL を上書きする (テスト用 httptest server URL 等)。
// 末尾スラッシュの正規化は行わない (呼び出し側責務)。
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = u }
}

// WithMaxRetries は 429/5xx 時の再試行回数上限を設定する。
// 0 を渡すとリトライ無効 (1 回試行のみ)。
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = n }
}

// WithBackoff は exponential backoff の base / max を設定する。
// base <= 0 または max <= 0 の場合は既定値を維持する。
func WithBackoff(base, maxDur time.Duration) Option {
	return func(c *Client) {
		if base > 0 {
			c.baseBackoff = base
		}
		if maxDur > 0 {
			c.maxBackoff = maxDur
		}
	}
}

// withSleep はテスト専用に sleep 実装を差し替える未公開オプション。
func withSleep(fn func(context.Context, time.Duration) error) Option {
	return func(c *Client) { c.sleep = fn }
}

// withNow はテスト専用に現在時刻関数を差し替える未公開オプション。
func withNow(fn func() time.Time) Option {
	return func(c *Client) { c.now = fn }
}

// withHTTPClient はテスト専用に内部 http.Client を差し替える未公開オプション。
// httptest 経由のテストで OAuth 1.0a 署名検証を経由せず Client.Do の挙動だけを試したい場合に用いる。
func withHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// NewClient は ctx と config.Credentials から retry/rate-limit aware な xapi.Client を生成する。
//
// 内部で NewHTTPClient(ctx, creds) を呼んで OAuth 1.0a 署名済みの *http.Client を組み立てる。
// creds == nil の場合の挙動は NewHTTPClient と同方針 (panic せず、X API 側で 401 を発生させる)。
//
// オプションを渡さない場合の既定:
//   - baseURL    = "https://api.x.com"
//   - maxRetries = 3
//   - backoff    = base 2s / max 30s
func NewClient(ctx context.Context, creds *config.Credentials, opts ...Option) *Client {
	c := &Client{
		httpClient:  NewHTTPClient(ctx, creds),
		baseURL:     defaultBaseURL,
		sleep:       defaultSleep,
		now:         time.Now,
		maxRetries:  defaultMaxRetries,
		baseBackoff: defaultBaseBackoff,
		maxBackoff:  defaultMaxBackoff,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// BaseURL は構成済みの X API ベース URL を返す。
// M7+ の高レベル API (GetUserMe 等) が完全 URL を組み立てる際に用いる。
func (c *Client) BaseURL() string { return c.baseURL }

// Do は req を送信し、リトライポリシーに従って成功レスポンスまたは分類済みエラーを返す。
//
// 振る舞い:
//   - 2xx/3xx → *Response (RateLimit 含む) を返す
//   - 401/403/404 → *APIError (番兵エラーを Unwrap) を即返却 (リトライしない)
//   - 429/5xx   → exponential backoff で最大 maxRetries 回リトライ
//     429 で x-rate-limit-reset が未来であればその時刻まで待機 (最大 15 分)
//   - context が cancel されると sleep 途中でも即返却し ctx.Err() を返す
//
// 呼び出し側は成功時のみ Response.Body の Close 責務を持つ
// (リトライ前後の中間レスポンスは Client が内部で Close する)。
func (c *Client) Do(req *http.Request) (*Response, error) {
	ctx := req.Context()
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// 2 回目以降の試行で body を巻き戻す (GetBody が設定されていれば)。
		// http.NewRequestWithContext は body=nil/bytes/strings.Reader の場合に自動設定される。
		// それ以外の Body (例: 任意の io.Reader を直接渡した場合) は本 M6 では未対応。
		if attempt > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("xapi: rewind request body: %w", err)
			}
			req.Body = body
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			// ネットワークエラーは現状リトライしない (context cancel 等の判別は呼び出し側責務)。
			return nil, err
		}
		rateInfo := parseRateLimit(resp.Header)

		switch {
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError:
			if attempt == c.maxRetries {
				return nil, c.toAPIError(resp, exhaustedSentinel(resp.StatusCode))
			}
			wait := c.computeBackoff(attempt, resp.StatusCode, rateInfo)
			_ = resp.Body.Close()
			if err := c.sleep(ctx, wait); err != nil {
				return nil, err
			}
			continue
		case resp.StatusCode >= http.StatusBadRequest:
			return nil, c.toAPIError(resp, mapClientErr(resp.StatusCode))
		default:
			return &Response{Response: resp, RateLimit: rateInfo}, nil
		}
	}
	// for ループ内で必ず return するため到達不能。安全側のフォールバックを残す。
	return nil, errors.New("xapi: unreachable retry loop")
}

// defaultSleep は context-aware な sleep の標準実装である。
// d 経過前に ctx が cancel されたら ctx.Err() を返す。
func defaultSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// parseRateLimit は HTTP ヘッダから x-rate-limit-* を取り出し RateLimitInfo に詰める。
// http.Header.Get はケース非依存なので任意の表記揺れを許容する。
func parseRateLimit(h http.Header) RateLimitInfo {
	info := RateLimitInfo{Remaining: -1}
	if h == nil {
		return info
	}
	if v := h.Get("x-rate-limit-limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.Limit = n
			info.Raw = true
		}
	}
	if v := h.Get("x-rate-limit-remaining"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			info.Remaining = n
			info.Raw = true
		}
	}
	if v := h.Get("x-rate-limit-reset"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			info.Reset = time.Unix(n, 0)
			info.Raw = true
		}
	}
	return info
}

// computeBackoff は次回リトライまでの待機時間を計算する。
//
// 規則:
//   - status == 429 かつ rateInfo.Reset が未来 → reset までの時間を rateLimitMaxWait と maxBackoff で頭打ち
//   - それ以外 → base * 2^attempt を maxBackoff で頭打ちにした exp backoff
//   - 結果が 0 以下になったら baseBackoff を返す (即時リトライ抑止)
func (c *Client) computeBackoff(attempt, status int, info RateLimitInfo) time.Duration {
	if status == http.StatusTooManyRequests && !info.Reset.IsZero() {
		wait := info.Reset.Sub(c.now())
		if wait > 0 {
			if wait > rateLimitMaxWait {
				wait = rateLimitMaxWait
			}
			if wait > c.maxBackoff {
				wait = c.maxBackoff
			}
			return wait
		}
	}
	// exponential backoff: base * 2^attempt
	d := c.baseBackoff << attempt
	if d <= 0 || d > c.maxBackoff {
		d = c.maxBackoff
	}
	if d <= 0 {
		d = c.baseBackoff
	}
	return d
}

// mapClientErr は 4xx のステータスを対応する番兵エラーに写像する。
// 該当しない 4xx (例: 400) は nil を返し、APIError のみとして扱う。
func mapClientErr(status int) error {
	switch status {
	case http.StatusUnauthorized:
		return ErrAuthentication
	case http.StatusForbidden:
		return ErrPermission
	case http.StatusNotFound:
		return ErrNotFound
	default:
		return nil
	}
}

// exhaustedSentinel はリトライ枯渇時に付与する番兵を返す。
// 429 のみ ErrRateLimit を返し、5xx 枯渇は番兵なし (APIError として扱う)。
func exhaustedSentinel(status int) error {
	if status == http.StatusTooManyRequests {
		return ErrRateLimit
	}
	return nil
}

// toAPIError は resp を APIError に変換する。Body は読み切って Close まで完結させる。
// レスポンスを返さない (エラー終了) パスから呼ばれる前提。
func (c *Client) toAPIError(resp *http.Response, sentinel error) error {
	var body []byte
	if resp.Body != nil {
		b, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr == nil {
			body = b
		}
	}
	return &APIError{
		StatusCode: resp.StatusCode,
		Body:       body,
		Header:     resp.Header.Clone(),
		sentinel:   sentinel,
	}
}
