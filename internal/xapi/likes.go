package xapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// likes.go 固有の定数群。
// rateLimitMaxWait (= 15 * time.Minute) は client.go 側、defaultInterPageDelay (= 200ms)
// および computeInterPageWait は pagination.go 側で定義済みのものを再利用する (M29 T7)。
const (
	// likesDefaultMaxPages は WithMaxPages 未指定時の上限ページ数である
	// (spec §10 `--max-pages` (default: 50))。
	likesDefaultMaxPages = 50
	// likesRateLimitThreshold は EachLikedPage が rate-limit aware sleep を発動する
	// remaining の閾値である (spec §10 「remaining ≤ 2 になったら reset 時刻まで sleep」)。
	likesRateLimitThreshold = 2
)

// LikedTweetsResponse は ListLikedTweets / EachLikedPage が返すレスポンス本体である。
//
// X API v2 GET /2/users/:id/liked_tweets のレスポンス JSON `{ data, includes, meta }` を
// そのまま構造化したものである。EachLikedPage の callback には本構造体のポインタが渡される。
type LikedTweetsResponse struct {
	// Data は Like したツイートの配列。X API が `data` を返さない場合 (0 件) は nil/空。
	Data []Tweet `json:"data,omitempty"`
	// Includes は expansions で取得された関連リソース (users / tweets)。
	Includes Includes `json:"includes,omitempty"`
	// Meta は result_count / next_token を含むページネーション情報。
	Meta Meta `json:"meta,omitempty"`
}

// LikedTweetsOption は ListLikedTweets / EachLikedPage の挙動を変更する関数オプションである。
//
// 同じキーで複数回呼ばれた場合は最後の呼び出しが勝つ (last-wins)。
// `func(*url.Values)` ではなく中間構造体を介する設計理由は plans/x-m08-xapi-likes.md D-1 を参照。
type LikedTweetsOption func(*likedTweetsConfig)

// likedTweetsConfig はオプション設定を集約する未公開構造体である。
type likedTweetsConfig struct {
	startTime       *time.Time
	endTime         *time.Time
	maxResults      int // 0 は no-op (X API デフォルトに任せる)
	paginationToken string
	tweetFields     []string
	expansions      []string
	userFields      []string

	// EachLikedPage 専用 (ListLikedTweets では無視される)。
	maxPages int // 0 ならデフォルト likesDefaultMaxPages を使う
}

// WithStartTime は X API の `start_time` クエリパラメータを RFC3339 形式 (UTC, ナノ秒なし)
// で設定する。time.Time が UTC でない場合は `t.UTC()` で正規化される。
//
// 例: `WithStartTime(time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC))`
//
//	→ `?start_time=2026-05-12T00:00:00Z`
func WithStartTime(t time.Time) LikedTweetsOption {
	return func(c *likedTweetsConfig) {
		tt := t
		c.startTime = &tt
	}
}

// WithEndTime は X API の `end_time` クエリパラメータを RFC3339 形式 (UTC, ナノ秒なし)
// で設定する。WithStartTime と同じ正規化規則。
func WithEndTime(t time.Time) LikedTweetsOption {
	return func(c *likedTweetsConfig) {
		tt := t
		c.endTime = &tt
	}
}

// WithMaxResults は X API の `max_results` クエリパラメータ (1-100) を設定する。
//
// 0 を渡すと no-op (X API のデフォルト = 10 が適用される) になる。
// CLI 層 (M10/M11) で `default_max_results = 100` (spec §11) を必ずセットする責務を負うこと。
// 1-100 のレンジ検証は本関数では行わず X API 側にエラー応答を委ねる。
func WithMaxResults(n int) LikedTweetsOption {
	return func(c *likedTweetsConfig) { c.maxResults = n }
}

// WithPaginationToken は X API の `pagination_token` クエリパラメータを設定する。
// 主に CLI `--pagination-token` フラグから直接呼ばれる。
// EachLikedPage は内部で next_token を都度上書きするため、呼び出し時の値は初回ページのみ有効。
func WithPaginationToken(token string) LikedTweetsOption {
	return func(c *likedTweetsConfig) { c.paginationToken = token }
}

// WithTweetFields は X API の `tweet.fields` クエリパラメータを設定する。
//
// 例: `WithTweetFields("id","text","public_metrics")` は
// `?tweet.fields=id,text,public_metrics` を付与する。
// 空引数で呼ばれた場合は no-op。重複呼び出しは last-wins。
func WithTweetFields(fields ...string) LikedTweetsOption {
	return func(c *likedTweetsConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// WithExpansions は X API の `expansions` クエリパラメータを設定する。
//
// 例: `WithExpansions("author_id","referenced_tweets.id")` は
// `?expansions=author_id,referenced_tweets.id` を付与する。
// 空引数で呼ばれた場合は no-op。重複呼び出しは last-wins。
func WithExpansions(exp ...string) LikedTweetsOption {
	return func(c *likedTweetsConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithLikedUserFields は X API の `user.fields` クエリパラメータを設定する。
//
// 注意: M7 で導入した `WithUserFields` は GetUserMe 専用の `UserFieldsOption` 型であり、
// 本関数は LikedTweetsOption 型として独立して公開している (関数オプション型を統一しないため)。
// CLI 層では用途に応じて両方の関数を使い分ける。
//
// 空引数で呼ばれた場合は no-op。重複呼び出しは last-wins。
func WithLikedUserFields(fields ...string) LikedTweetsOption {
	return func(c *likedTweetsConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithMaxPages は EachLikedPage の上限ページ数を設定する。
//
// 未指定 (or 0 以下) の場合は default `likesDefaultMaxPages = 50` (spec §10) が適用される。
// 上限に達した場合は error 返却ではなく正常終了し、`next_token` の有無は呼び出し側で
// 直接判断できない (将来 (pagesFetched, truncated, err) シグネチャに拡張する余地あり、
// plans/x-m08-xapi-likes.md D-4 参照)。
//
// ListLikedTweets では本オプションは無視される。
func WithMaxPages(n int) LikedTweetsOption {
	return func(c *likedTweetsConfig) { c.maxPages = n }
}

// ListLikedTweets は X API v2 `GET /2/users/:id/liked_tweets` を呼び出し、
// 単一ページの Like ツイート一覧を返す。
//
// 認証は NewClient 時に渡した *config.Credentials の OAuth 1.0a 署名で行われる。
// opts で `start_time` / `end_time` / `max_results` / `pagination_token` /
// `tweet.fields` / `expansions` / `user.fields` をカスタマイズできる。
// WithMaxPages は ListLikedTweets では無視される (EachLikedPage 専用)。
//
// userID は url.PathEscape されパスに埋め込まれる (パスインジェクション防止)。
//
// エラーの分類は M6 Client.Do と同じ:
//   - errors.Is(err, ErrAuthentication) → 401
//   - errors.Is(err, ErrPermission)     → 403
//   - errors.Is(err, ErrNotFound)       → 404
//   - errors.Is(err, ErrRateLimit)      → 429 リトライ枯渇
//   - errors.As(err, &apiErr)           → APIError から Body/Header/StatusCode 取得
//   - errors.Is(err, context.Canceled)  → context cancel
//
// レスポンスの JSON 形式: `{"data":[...],"includes":{...},"meta":{...}}`。
// `data` フィールドが型不一致だった場合は decode エラー
// (xapi: decode ListLikedTweets response) を返し、リトライはしない。
func (c *Client) ListLikedTweets(
	ctx context.Context,
	userID string,
	opts ...LikedTweetsOption,
) (*LikedTweetsResponse, error) {
	cfg := newLikedTweetsConfig(opts)
	resp, err := c.fetchLikedTweetsPage(ctx, userID, &cfg)
	if err != nil {
		return nil, err
	}
	return resp.body, nil
}

// EachLikedPage は X API v2 `GET /2/users/:id/liked_tweets` を next_token が
// 無くなるまで自動辿りし、各ページを fn に渡す (rate-limit aware ページネーション)。
//
// 終了条件 (いずれか早い方):
//   - meta.next_token が空文字列 (= 最終ページ)
//   - 取得済みページ数が WithMaxPages (default 50) に到達 → 正常終了 (error 返さない)
//   - fn が non-nil error を返す → そのエラーをそのまま返却 (errors.Is で参照可能)
//   - ctx が cancel された → ctx.Err() を返却
//
// ページ間 sleep (spec §10):
//   - レスポンスの x-rate-limit-remaining が likesRateLimitThreshold (= 2) 以下のとき
//     x-rate-limit-reset 時刻まで sleep (最大 rateLimitMaxWait = 15 分)
//   - reset が過去 (clock skew or stale ヘッダ) の場合は最小 200ms にフォールバック
//   - それ以外は最小 200ms 待機 (バースト抑止)
//
// 429 / 5xx の単一ページリトライは Client.Do (M6) が exponential backoff で吸収するため
// EachLikedPage 自身では追加のリトライは行わない。
//
// 呼び出し例:
//
//	var all []xapi.Tweet
//	err := c.EachLikedPage(ctx, userID, func(p *xapi.LikedTweetsResponse) error {
//	    all = append(all, p.Data...)
//	    return nil
//	}, xapi.WithMaxResults(100), xapi.WithMaxPages(50))
func (c *Client) EachLikedPage(
	ctx context.Context,
	userID string,
	fn func(*LikedTweetsResponse) error,
	opts ...LikedTweetsOption,
) error {
	cfg := newLikedTweetsConfig(opts)
	maxPages := cfg.maxPages
	if maxPages <= 0 {
		maxPages = likesDefaultMaxPages
	}

	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fetched, err := c.fetchLikedTweetsPage(ctx, userID, &cfg)
		if err != nil {
			return err
		}
		if err := fn(fetched.body); err != nil {
			return err
		}
		next := fetched.body.Meta.NextToken
		if next == "" {
			return nil
		}
		if page+1 >= maxPages {
			// max_pages 到達。fn は呼び終わっているのでここで正常終了 (sleep 不要)。
			return nil
		}
		wait := c.computeInterPageWait(fetched.rateLimit, likesRateLimitThreshold)
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
		// 次ページの pagination_token を設定 (WithPaginationToken 初期値より優先)
		cfg.paginationToken = next
	}
	return nil
}

// likedTweetsFetched は単一ページの取得結果である (本体 + rate-limit ヘッダ情報)。
type likedTweetsFetched struct {
	body      *LikedTweetsResponse
	rateLimit RateLimitInfo
}

// fetchLikedTweetsPage は単一ページのリクエスト送出 + JSON デコードを行う。
// ListLikedTweets と EachLikedPage の共通実装。
func (c *Client) fetchLikedTweetsPage(
	ctx context.Context,
	userID string,
	cfg *likedTweetsConfig,
) (*likedTweetsFetched, error) {
	endpoint := buildLikedTweetsURL(c.BaseURL(), userID, cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build ListLikedTweets request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read ListLikedTweets response: %w", err)
	}
	out := &LikedTweetsResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode ListLikedTweets response: %w", err)
	}
	return &likedTweetsFetched{body: out, rateLimit: resp.RateLimit}, nil
}

// buildLikedTweetsURL は GET /2/users/:id/liked_tweets の完全 URL を組み立てる。
// userID は url.PathEscape されパスに埋め込まれる (パスインジェクション防止)。
func buildLikedTweetsURL(baseURL, userID string, cfg *likedTweetsConfig) string {
	path := "/2/users/" + url.PathEscape(userID) + "/liked_tweets"
	values := url.Values{}
	if cfg.startTime != nil {
		values.Set("start_time", cfg.startTime.UTC().Format(time.RFC3339))
	}
	if cfg.endTime != nil {
		values.Set("end_time", cfg.endTime.UTC().Format(time.RFC3339))
	}
	if cfg.maxResults > 0 {
		values.Set("max_results", strconv.Itoa(cfg.maxResults))
	}
	if cfg.paginationToken != "" {
		values.Set("pagination_token", cfg.paginationToken)
	}
	if len(cfg.tweetFields) > 0 {
		values.Set("tweet.fields", strings.Join(cfg.tweetFields, ","))
	}
	if len(cfg.expansions) > 0 {
		values.Set("expansions", strings.Join(cfg.expansions, ","))
	}
	if len(cfg.userFields) > 0 {
		values.Set("user.fields", strings.Join(cfg.userFields, ","))
	}
	if q := values.Encode(); q != "" {
		return baseURL + path + "?" + q
	}
	return baseURL + path
}

// newLikedTweetsConfig は opts を適用した likedTweetsConfig を返す。
func newLikedTweetsConfig(opts []LikedTweetsOption) likedTweetsConfig {
	cfg := likedTweetsConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
