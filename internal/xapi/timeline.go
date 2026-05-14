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

// timeline.go は X API v2 のユーザータイムライン系 3 エンドポイントをラップする (M31)。
//
//   - GET /2/users/:id/tweets                            → GetUserTweets / EachUserTweetsPage
//   - GET /2/users/:id/mentions                          → GetUserMentions / EachUserMentionsPage
//   - GET /2/users/:id/timelines/reverse_chronological   → GetHomeTimeline / EachHomeTimelinePage
//
// 3 関数分離 (M31 D-3) は kind enum よりも型安全。内部実装は fetchTimelinePage / eachTimelinePage で DRY。
// ページネーション iterator は M29 で抽出した (c *Client).computeInterPageWait(rl, threshold) を再利用する。

const (
	// timelineDefaultMaxPages は WithTimelineMaxPages 未指定時の上限ページ数である
	// (spec §10 `--max-pages` (default: 50))。
	timelineDefaultMaxPages = 50
	// timelineRateLimitThreshold は Each*TimelinePage が rate-limit aware sleep を発動する
	// remaining の閾値である (likes/search と同値 2、M31 D-6)。
	timelineRateLimitThreshold = 2

	// timeline endpoint の path suffix (URL 組み立て用)。
	timelineSuffixTweets   = "tweets"
	timelineSuffixMentions = "mentions"
	timelineSuffixHome     = "timelines/reverse_chronological"
)

// TimelineResponse は Get*Timeline / Each*TimelinePage が返すレスポンス本体である。
//
// X API v2 タイムライン系 3 エンドポイントの `{ data, includes, meta }` を構造化する。
// timeline 系はバッチ/検索系のような top-level `errors` フィールドを返さないため、
// SearchResponse とは違い Errors フィールドは搭載しない (M31 D-7)。
type TimelineResponse struct {
	// Data は取得したツイートの配列。
	Data []Tweet `json:"data,omitempty"`
	// Includes は expansions で取得された関連リソース (users / tweets)。
	Includes Includes `json:"includes,omitempty"`
	// Meta は result_count / next_token を含むページネーション情報。
	Meta Meta `json:"meta,omitempty"`
}

// TimelineOption は Get*Timeline / Each*TimelinePage の挙動を変更する関数オプションである。
//
// 同じキーで複数回呼ばれた場合は最後の呼び出しが勝つ (last-wins)。
// 中間構造体 timelineConfig に集約する設計は M30 SearchOption (D-8) と同パターン。
type TimelineOption func(*timelineConfig)

// timelineConfig はオプション設定を集約する未公開構造体である。
type timelineConfig struct {
	startTime       *time.Time
	endTime         *time.Time
	sinceID         string
	untilID         string
	maxResults      int // 0 は no-op (X API デフォルトに任せる)
	paginationToken string
	exclude         []string // "retweets" / "replies"
	tweetFields     []string
	expansions      []string
	userFields      []string
	mediaFields     []string

	// Each*TimelinePage 専用 (Get*Timeline では無視される)。
	maxPages int // 0 ならデフォルト timelineDefaultMaxPages を使う
}

// WithTimelineMaxResults は X API の max_results を設定する。
//
// X API v2 の per-page 仕様:
//   - GET /2/users/:id/tweets:                            5..100
//   - GET /2/users/:id/mentions:                          5..100
//   - GET /2/users/:id/timelines/reverse_chronological:   1..100
//
// 0 を渡すと no-op (X API デフォルト)。CLI 層 (M31) が endpoint 別の下限補正を担う。
func WithTimelineMaxResults(n int) TimelineOption {
	return func(c *timelineConfig) { c.maxResults = n }
}

// WithTimelineStartTime は X API の start_time を RFC3339 (UTC, ナノ秒なし) で設定する。
func WithTimelineStartTime(t time.Time) TimelineOption {
	return func(c *timelineConfig) {
		tt := t
		c.startTime = &tt
	}
}

// WithTimelineEndTime は X API の end_time を RFC3339 (UTC, ナノ秒なし) で設定する。
func WithTimelineEndTime(t time.Time) TimelineOption {
	return func(c *timelineConfig) {
		tt := t
		c.endTime = &tt
	}
}

// WithTimelineSinceID は X API の since_id (最小投稿 ID、下限) を設定する。
//
// `--start-time` / `--end-time` と独立に併用可能 (M31 D-14、X API 仕様)。
// 空文字列は no-op。
func WithTimelineSinceID(id string) TimelineOption {
	return func(c *timelineConfig) { c.sinceID = id }
}

// WithTimelineUntilID は X API の until_id (最大投稿 ID、上限) を設定する。
//
// `--start-time` / `--end-time` と独立に併用可能 (M31 D-14、X API 仕様)。
// 空文字列は no-op。
func WithTimelineUntilID(id string) TimelineOption {
	return func(c *timelineConfig) { c.untilID = id }
}

// WithTimelinePaginationToken は X API の pagination_token を設定する。
// Each*TimelinePage は内部で next_token を都度上書きするため、初回ページのみ有効。
func WithTimelinePaginationToken(token string) TimelineOption {
	return func(c *timelineConfig) { c.paginationToken = token }
}

// WithTimelineExclude は X API の exclude クエリパラメータを設定する。
//
// 有効値は "retweets" / "replies"。空引数は no-op。
//
// 注意: 本オプションは `/2/users/:id/tweets` (GetUserTweets) と
// `/2/users/:id/timelines/reverse_chronological` (GetHomeTimeline) のみで有効。
// `/2/users/:id/mentions` (GetUserMentions) は X API 仕様で exclude 非サポート
// であり、本オプションを渡すと X API が 400 を返す可能性がある (CLI 層 M31 では
// mentions サブコマンドに --exclude フラグ自体を登録しない M31 D-9)。
func WithTimelineExclude(values ...string) TimelineOption {
	return func(c *timelineConfig) {
		if len(values) == 0 {
			return
		}
		c.exclude = append([]string(nil), values...)
	}
}

// WithTimelineTweetFields は X API の tweet.fields を設定する。空引数は no-op。
func WithTimelineTweetFields(fields ...string) TimelineOption {
	return func(c *timelineConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// WithTimelineExpansions は X API の expansions を設定する。空引数は no-op。
func WithTimelineExpansions(exp ...string) TimelineOption {
	return func(c *timelineConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithTimelineUserFields は X API の user.fields を設定する。空引数は no-op。
func WithTimelineUserFields(fields ...string) TimelineOption {
	return func(c *timelineConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithTimelineMediaFields は X API の media.fields を設定する。空引数は no-op。
func WithTimelineMediaFields(fields ...string) TimelineOption {
	return func(c *timelineConfig) {
		if len(fields) == 0 {
			return
		}
		c.mediaFields = append([]string(nil), fields...)
	}
}

// WithTimelineMaxPages は Each*TimelinePage の上限ページ数を設定する。
// 未指定 (or 0 以下) の場合 default `timelineDefaultMaxPages = 50`。
// Get*Timeline では本オプションは無視される。
func WithTimelineMaxPages(n int) TimelineOption {
	return func(c *timelineConfig) { c.maxPages = n }
}

// GetUserTweets は X API v2 `GET /2/users/:id/tweets` を呼び出し、
// 指定ユーザーの Post タイムライン (単一ページ) を返す。
//
// max_results は X API 仕様で 5..100。CLI 層 (M31) で下限補正を担う。
// userID は url.PathEscape されパスに埋め込まれる。
//
// エラー分類は M6 Client.Do と同じ:
//   - errors.Is(err, ErrAuthentication) → 401
//   - errors.Is(err, ErrPermission)     → 403
//   - errors.Is(err, ErrNotFound)       → 404
//   - errors.Is(err, ErrRateLimit)      → 429 リトライ枯渇
func (c *Client) GetUserTweets(
	ctx context.Context,
	userID string,
	opts ...TimelineOption,
) (*TimelineResponse, error) {
	return c.getTimeline(ctx, "GetUserTweets", timelineSuffixTweets, userID, opts)
}

// GetUserMentions は X API v2 `GET /2/users/:id/mentions` を呼び出し、
// 指定ユーザーへのメンション一覧 (単一ページ) を返す。
//
// max_results は X API 仕様で 5..100。X API 仕様で exclude 非サポート
// (本オプションを渡すと X API が 400 の可能性、M31 D-9)。
func (c *Client) GetUserMentions(
	ctx context.Context,
	userID string,
	opts ...TimelineOption,
) (*TimelineResponse, error) {
	return c.getTimeline(ctx, "GetUserMentions", timelineSuffixMentions, userID, opts)
}

// GetHomeTimeline は X API v2 `GET /2/users/:id/timelines/reverse_chronological` を呼び出し、
// 認証ユーザーのホームタイムライン (単一ページ) を返す。
//
// X API 仕様で userID = 認証ユーザー必須 (他人の home は取得不可)。CLI 層 (M31) で GetUserMe で
// 自動解決する設計 (M31 D-4)。max_results は 1..100 (下限 1、tweets/mentions と異なる、M31 D-1)。
//
// 認証要件: **OAuth 1.0a 必須** (Bearer Token 不可)。本プロジェクトは OAuth 1.0a 専用のため
// 制約に問題なし。
func (c *Client) GetHomeTimeline(
	ctx context.Context,
	userID string,
	opts ...TimelineOption,
) (*TimelineResponse, error) {
	return c.getTimeline(ctx, "GetHomeTimeline", timelineSuffixHome, userID, opts)
}

// getTimeline は 3 公開関数 (GetUserTweets / GetUserMentions / GetHomeTimeline) の DRY 共通実装。
// funcName はエラーメッセージ用、suffix は path 末尾 (例: "tweets" / "mentions" / "timelines/reverse_chronological")。
func (c *Client) getTimeline(
	ctx context.Context,
	funcName, suffix, userID string,
	opts []TimelineOption,
) (*TimelineResponse, error) {
	if userID == "" {
		return nil, fmt.Errorf("xapi: %s: userID must be non-empty", funcName)
	}
	cfg := newTimelineConfig(opts)
	resp, err := c.fetchTimelinePage(ctx, suffix, userID, &cfg)
	if err != nil {
		return nil, err
	}
	return resp.body, nil
}

// EachUserTweetsPage は GET /2/users/:id/tweets を next_token で自動辿りする
// rate-limit aware ページネーション iterator である。詳細は eachTimelinePage を参照。
func (c *Client) EachUserTweetsPage(
	ctx context.Context,
	userID string,
	fn func(*TimelineResponse) error,
	opts ...TimelineOption,
) error {
	return c.eachTimelinePage(ctx, "EachUserTweetsPage", timelineSuffixTweets, userID, fn, opts)
}

// EachUserMentionsPage は GET /2/users/:id/mentions を next_token で自動辿りする
// rate-limit aware ページネーション iterator である。
func (c *Client) EachUserMentionsPage(
	ctx context.Context,
	userID string,
	fn func(*TimelineResponse) error,
	opts ...TimelineOption,
) error {
	return c.eachTimelinePage(ctx, "EachUserMentionsPage", timelineSuffixMentions, userID, fn, opts)
}

// EachHomeTimelinePage は GET /2/users/:id/timelines/reverse_chronological を next_token で自動辿りする
// rate-limit aware ページネーション iterator である。
func (c *Client) EachHomeTimelinePage(
	ctx context.Context,
	userID string,
	fn func(*TimelineResponse) error,
	opts ...TimelineOption,
) error {
	return c.eachTimelinePage(ctx, "EachHomeTimelinePage", timelineSuffixHome, userID, fn, opts)
}

// eachTimelinePage は 3 公開 iterator の DRY 共通実装。
//
// 終了条件 (いずれか早い方):
//   - meta.next_token が空文字列 (= 最終ページ)
//   - 取得済みページ数が WithTimelineMaxPages (default 50) に到達 → 正常終了
//   - fn が non-nil error を返す → そのエラーをそのまま返却
//   - ctx が cancel された → ctx.Err() を返却
//
// ページ間 sleep:
//   - レスポンスの x-rate-limit-remaining が timelineRateLimitThreshold (= 2) 以下のとき
//     x-rate-limit-reset まで sleep (最大 rateLimitMaxWait = 15 分)
//   - それ以外は 200ms 待機 (バースト抑止)
//
// 共通ロジックは M29 の `(c *Client).computeInterPageWait` を再利用 (pagination.go)。
func (c *Client) eachTimelinePage(
	ctx context.Context,
	funcName, suffix, userID string,
	fn func(*TimelineResponse) error,
	opts []TimelineOption,
) error {
	if userID == "" {
		return fmt.Errorf("xapi: %s: userID must be non-empty", funcName)
	}
	cfg := newTimelineConfig(opts)
	maxPages := cfg.maxPages
	if maxPages <= 0 {
		maxPages = timelineDefaultMaxPages
	}

	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fetched, err := c.fetchTimelinePage(ctx, suffix, userID, &cfg)
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
			return nil
		}
		wait := c.computeInterPageWait(fetched.rateLimit, timelineRateLimitThreshold)
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
		cfg.paginationToken = next
	}
	return nil
}

// timelineFetched は単一ページの取得結果である (本体 + rate-limit ヘッダ情報)。
type timelineFetched struct {
	body      *TimelineResponse
	rateLimit RateLimitInfo
}

// fetchTimelinePage は単一ページのリクエスト送出 + JSON デコードを行う。
// Get*Timeline と Each*TimelinePage の共通実装。
func (c *Client) fetchTimelinePage(
	ctx context.Context,
	suffix, userID string,
	cfg *timelineConfig,
) (*timelineFetched, error) {
	endpoint := buildTimelineURL(c.BaseURL(), suffix, userID, cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build timeline request (%s): %w", suffix, err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read timeline response (%s): %w", suffix, err)
	}
	out := &TimelineResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode timeline response (%s): %w", suffix, err)
	}
	return &timelineFetched{body: out, rateLimit: resp.RateLimit}, nil
}

// buildTimelineURL は `GET /2/users/:id/<suffix>` の完全 URL を組み立てる。
// userID は url.PathEscape されパスに埋め込まれる (パスインジェクション防止)。
func buildTimelineURL(baseURL, suffix, userID string, cfg *timelineConfig) string {
	path := "/2/users/" + url.PathEscape(userID) + "/" + suffix
	values := url.Values{}
	if cfg.maxResults > 0 {
		values.Set("max_results", strconv.Itoa(cfg.maxResults))
	}
	if cfg.startTime != nil {
		values.Set("start_time", cfg.startTime.UTC().Format(time.RFC3339))
	}
	if cfg.endTime != nil {
		values.Set("end_time", cfg.endTime.UTC().Format(time.RFC3339))
	}
	if cfg.sinceID != "" {
		values.Set("since_id", cfg.sinceID)
	}
	if cfg.untilID != "" {
		values.Set("until_id", cfg.untilID)
	}
	if cfg.paginationToken != "" {
		values.Set("pagination_token", cfg.paginationToken)
	}
	if len(cfg.exclude) > 0 {
		values.Set("exclude", strings.Join(cfg.exclude, ","))
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
	if len(cfg.mediaFields) > 0 {
		values.Set("media.fields", strings.Join(cfg.mediaFields, ","))
	}
	if q := values.Encode(); q != "" {
		return baseURL + path + "?" + q
	}
	return baseURL + path
}

// newTimelineConfig は opts を適用した timelineConfig を返す。
func newTimelineConfig(opts []TimelineOption) timelineConfig {
	cfg := timelineConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
