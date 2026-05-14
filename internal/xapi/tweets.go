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

// tweetLookupConfig は GetTweet / GetTweets のオプション集約構造体である。
type tweetLookupConfig struct {
	tweetFields []string
	expansions  []string
	userFields  []string
	mediaFields []string
}

// TweetLookupOption は GetTweet / GetTweets の挙動を変更する関数オプションである。
// 同じキーで複数回呼ばれた場合は最後の呼び出しが勝つ (last-wins)。
type TweetLookupOption func(*tweetLookupConfig)

// WithGetTweetFields は X API の `tweet.fields` クエリパラメータを設定する。
// 空引数は no-op。
func WithGetTweetFields(fields ...string) TweetLookupOption {
	return func(c *tweetLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// WithGetTweetExpansions は X API の `expansions` クエリパラメータを設定する。
// 空引数は no-op。
func WithGetTweetExpansions(exp ...string) TweetLookupOption {
	return func(c *tweetLookupConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithGetTweetUserFields は X API の `user.fields` クエリパラメータを設定する。
// 空引数は no-op。
func WithGetTweetUserFields(fields ...string) TweetLookupOption {
	return func(c *tweetLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithGetTweetMediaFields は X API の `media.fields` クエリパラメータを設定する。
// 空引数は no-op。
func WithGetTweetMediaFields(fields ...string) TweetLookupOption {
	return func(c *tweetLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.mediaFields = append([]string(nil), fields...)
	}
}

// TweetResponse は GetTweet が返す単一ツイートレスポンス本体である。
//
// X API v2 `GET /2/tweets/:id` のレスポンス `{ data, includes }` を構造化する。
type TweetResponse struct {
	// Data は取得したツイート本体。X API がツイートを返さなかった場合は nil。
	Data *Tweet `json:"data,omitempty"`
	// Includes は expansions で取得された関連リソース (users / tweets)。
	Includes Includes `json:"includes,omitempty"`
}

// TweetsResponse は GetTweets が返すバッチ取得レスポンス本体である。
//
// X API v2 `GET /2/tweets?ids=...` のレスポンス `{ data, includes, errors }` を構造化する。
// `errors` は一部 ID が見つからない等の partial error が入る (D-9, TweetLookupError 参照)。
type TweetsResponse struct {
	// Data は取得できたツイートの配列 (0 件以上)。
	Data []Tweet `json:"data,omitempty"`
	// Includes は expansions で取得された関連リソース (users / tweets)。
	Includes Includes `json:"includes,omitempty"`
	// Errors はバッチ取得時の partial error (一部 ID が見つからなかった等)。
	// HTTP ステータスは 200 OK のまま、応答 JSON にこれが入ることがある。
	Errors []TweetLookupError `json:"errors,omitempty"`
}

// TweetLookupError は GetTweets の partial error を表す DTO である (M29 D-9)。
//
// X API v2 のバッチ partial error スキーマは既存 ErrorResponse / APIErrorPayload と
// フィールドが異なるため別型を用意する。
//
// 代表例 (`Could not find tweet with ids: [<id>].`):
//
//	{
//	  "value":         "<id>",
//	  "detail":        "Could not find tweet with ids: [<id>].",
//	  "title":         "Not Found Error",
//	  "resource_type": "tweet",
//	  "parameter":     "ids",
//	  "resource_id":   "<id>",
//	  "type":          "https://api.twitter.com/2/problems/resource-not-found"
//	}
type TweetLookupError struct {
	Value        string `json:"value,omitempty"`
	Detail       string `json:"detail,omitempty"`
	Title        string `json:"title,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	Parameter    string `json:"parameter,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	Type         string `json:"type,omitempty"`
}

// GetTweet は X API v2 `GET /2/tweets/:id` を呼び出し、単一ツイートを返す。
//
// 認証は NewClient 時に渡した *config.Credentials の OAuth 1.0a 署名で行われる。
// opts で `tweet.fields` / `expansions` / `user.fields` / `media.fields` をカスタマイズ可能。
//
// エラーの分類は M6 Client.Do と同じ:
//   - errors.Is(err, ErrAuthentication) → 401
//   - errors.Is(err, ErrPermission)     → 403
//   - errors.Is(err, ErrNotFound)       → 404
//   - errors.Is(err, ErrRateLimit)      → 429 リトライ枯渇
//
// レスポンスの JSON 形式: `{"data": {...}, "includes": {...}}`。
// `data` フィールドが配列等の型不一致だった場合は decode エラーを返し、リトライしない。
func (c *Client) GetTweet(
	ctx context.Context,
	tweetID string,
	opts ...TweetLookupOption,
) (*TweetResponse, error) {
	cfg := applyTweetLookupOpts(opts)
	endpoint := buildGetTweetURL(c.BaseURL(), tweetID, &cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build GetTweet request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read GetTweet response: %w", err)
	}
	out := &TweetResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetTweet response: %w", err)
	}
	return out, nil
}

// GetTweets は X API v2 `GET /2/tweets?ids=ID1,ID2,...` を呼び出し、複数ツイートを返す。
//
// ids は 1〜100 件の範囲で X API に渡される。
//   - 0 件 → "ids must be non-empty" エラー (CLI 層でラップ)
//   - 101 件以上 → "ids must be at most 100" エラー
//
// partial error (一部 ID が見つからない等) は TweetsResponse.Errors に入る。HTTP は 200 OK。
func (c *Client) GetTweets(
	ctx context.Context,
	ids []string,
	opts ...TweetLookupOption,
) (*TweetsResponse, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("xapi: GetTweets: ids must be non-empty")
	}
	if len(ids) > 100 {
		return nil, fmt.Errorf("xapi: GetTweets: ids must be at most 100, got %d", len(ids))
	}
	cfg := applyTweetLookupOpts(opts)
	endpoint := buildGetTweetsURL(c.BaseURL(), ids, &cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build GetTweets request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read GetTweets response: %w", err)
	}
	out := &TweetsResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetTweets response: %w", err)
	}
	return out, nil
}

// applyTweetLookupOpts は opts を tweetLookupConfig に適用して返す。
func applyTweetLookupOpts(opts []TweetLookupOption) tweetLookupConfig {
	cfg := tweetLookupConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// buildGetTweetURL は `GET /2/tweets/:id` の完全 URL を組み立てる (path escape あり)。
func buildGetTweetURL(baseURL, tweetID string, cfg *tweetLookupConfig) string {
	path := "/2/tweets/" + url.PathEscape(tweetID)
	values := tweetLookupValues(cfg)
	if q := values.Encode(); q != "" {
		return baseURL + path + "?" + q
	}
	return baseURL + path
}

// buildGetTweetsURL は `GET /2/tweets?ids=...` の完全 URL を組み立てる。
func buildGetTweetsURL(baseURL string, ids []string, cfg *tweetLookupConfig) string {
	values := tweetLookupValues(cfg)
	values.Set("ids", strings.Join(ids, ","))
	return baseURL + "/2/tweets?" + values.Encode()
}

// -- Social Signals -------------------------------------------------------
//
// `GET /2/tweets/:id/liking_users`     → GetLikingUsers
// `GET /2/tweets/:id/retweeted_by`     → GetRetweetedBy
// `GET /2/tweets/:id/quote_tweets`     → GetQuoteTweets

// UsersByTweetResponse は liking_users / retweeted_by が返すレスポンス本体である。
type UsersByTweetResponse struct {
	// Data はいいねまたはリツイートしたユーザー配列。
	Data []User `json:"data,omitempty"`
	// Includes は expansions で取得された関連リソース。
	Includes Includes `json:"includes,omitempty"`
	// Meta は result_count / next_token を含むページネーション情報。
	Meta Meta `json:"meta,omitempty"`
}

// QuoteTweetsResponse は quote_tweets が返すレスポンス本体である。
type QuoteTweetsResponse struct {
	// Data は引用ツイートの配列。
	Data []Tweet `json:"data,omitempty"`
	// Includes は expansions で取得された関連リソース。
	Includes Includes `json:"includes,omitempty"`
	// Meta は result_count / next_token を含むページネーション情報。
	Meta Meta `json:"meta,omitempty"`
}

// usersByTweetConfig は GetLikingUsers / GetRetweetedBy のオプション集約構造体である。
type usersByTweetConfig struct {
	maxResults      int // 0 は no-op
	paginationToken string
	userFields      []string
	expansions      []string
	tweetFields     []string // expansions=pinned_tweet_id 時に Includes.Tweets 用
}

// UsersByTweetOption は GetLikingUsers / GetRetweetedBy の挙動を変更する関数オプションである。
type UsersByTweetOption func(*usersByTweetConfig)

// WithUsersByTweetMaxResults は X API の max_results (1-100) を設定する。0 は no-op。
func WithUsersByTweetMaxResults(n int) UsersByTweetOption {
	return func(c *usersByTweetConfig) { c.maxResults = n }
}

// WithUsersByTweetPaginationToken は X API の pagination_token を設定する。
func WithUsersByTweetPaginationToken(token string) UsersByTweetOption {
	return func(c *usersByTweetConfig) { c.paginationToken = token }
}

// WithUsersByTweetUserFields は X API の user.fields を設定する。空引数は no-op。
func WithUsersByTweetUserFields(fields ...string) UsersByTweetOption {
	return func(c *usersByTweetConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithUsersByTweetExpansions は X API の expansions を設定する。空引数は no-op。
func WithUsersByTweetExpansions(exp ...string) UsersByTweetOption {
	return func(c *usersByTweetConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithUsersByTweetTweetFields は X API の tweet.fields を設定する (expansions=pinned_tweet_id 用)。
// 空引数は no-op。
func WithUsersByTweetTweetFields(fields ...string) UsersByTweetOption {
	return func(c *usersByTweetConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// GetLikingUsers は X API v2 `GET /2/tweets/:id/liking_users` を呼び出し、
// 指定ツイートに「いいね」したユーザー一覧を返す。
func (c *Client) GetLikingUsers(
	ctx context.Context,
	tweetID string,
	opts ...UsersByTweetOption,
) (*UsersByTweetResponse, error) {
	return c.fetchUsersByTweet(ctx, tweetID, "liking_users", opts)
}

// GetRetweetedBy は X API v2 `GET /2/tweets/:id/retweeted_by` を呼び出し、
// 指定ツイートをリツイートしたユーザー一覧を返す。
func (c *Client) GetRetweetedBy(
	ctx context.Context,
	tweetID string,
	opts ...UsersByTweetOption,
) (*UsersByTweetResponse, error) {
	return c.fetchUsersByTweet(ctx, tweetID, "retweeted_by", opts)
}

// fetchUsersByTweet は liking_users / retweeted_by 共通の取得処理。
// suffix は "liking_users" or "retweeted_by"。
func (c *Client) fetchUsersByTweet(
	ctx context.Context,
	tweetID, suffix string,
	opts []UsersByTweetOption,
) (*UsersByTweetResponse, error) {
	cfg := usersByTweetConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	endpoint := buildUsersByTweetURL(c.BaseURL(), tweetID, suffix, &cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build %s request: %w", suffix, err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read %s response: %w", suffix, err)
	}
	out := &UsersByTweetResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode %s response: %w", suffix, err)
	}
	return out, nil
}

// buildUsersByTweetURL は `/2/tweets/:id/{liking_users|retweeted_by}` の完全 URL を組み立てる。
func buildUsersByTweetURL(baseURL, tweetID, suffix string, cfg *usersByTweetConfig) string {
	path := "/2/tweets/" + url.PathEscape(tweetID) + "/" + suffix
	values := url.Values{}
	if cfg.maxResults > 0 {
		values.Set("max_results", strconv.Itoa(cfg.maxResults))
	}
	if cfg.paginationToken != "" {
		values.Set("pagination_token", cfg.paginationToken)
	}
	if len(cfg.userFields) > 0 {
		values.Set("user.fields", strings.Join(cfg.userFields, ","))
	}
	if len(cfg.expansions) > 0 {
		values.Set("expansions", strings.Join(cfg.expansions, ","))
	}
	if len(cfg.tweetFields) > 0 {
		values.Set("tweet.fields", strings.Join(cfg.tweetFields, ","))
	}
	if q := values.Encode(); q != "" {
		return baseURL + path + "?" + q
	}
	return baseURL + path
}

// quoteTweetsConfig は GetQuoteTweets のオプション集約構造体である。
type quoteTweetsConfig struct {
	maxResults      int // 0 は no-op
	paginationToken string
	exclude         []string // "retweets" / "replies"
	tweetFields     []string
	expansions      []string
	userFields      []string
	mediaFields     []string
}

// QuoteTweetsOption は GetQuoteTweets の挙動を変更する関数オプションである。
type QuoteTweetsOption func(*quoteTweetsConfig)

// WithQuoteTweetsMaxResults は X API の max_results (1-100) を設定する。0 は no-op。
func WithQuoteTweetsMaxResults(n int) QuoteTweetsOption {
	return func(c *quoteTweetsConfig) { c.maxResults = n }
}

// WithQuoteTweetsPaginationToken は X API の pagination_token を設定する。
func WithQuoteTweetsPaginationToken(token string) QuoteTweetsOption {
	return func(c *quoteTweetsConfig) { c.paginationToken = token }
}

// WithQuoteTweetsExclude は X API の exclude クエリパラメータ ("retweets" / "replies") を設定する。
// 空引数は no-op。
func WithQuoteTweetsExclude(values ...string) QuoteTweetsOption {
	return func(c *quoteTweetsConfig) {
		if len(values) == 0 {
			return
		}
		c.exclude = append([]string(nil), values...)
	}
}

// WithQuoteTweetsTweetFields は X API の tweet.fields を設定する。空引数は no-op。
func WithQuoteTweetsTweetFields(fields ...string) QuoteTweetsOption {
	return func(c *quoteTweetsConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// WithQuoteTweetsExpansions は X API の expansions を設定する。空引数は no-op。
func WithQuoteTweetsExpansions(exp ...string) QuoteTweetsOption {
	return func(c *quoteTweetsConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithQuoteTweetsUserFields は X API の user.fields を設定する。空引数は no-op。
func WithQuoteTweetsUserFields(fields ...string) QuoteTweetsOption {
	return func(c *quoteTweetsConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithQuoteTweetsMediaFields は X API の media.fields を設定する。空引数は no-op。
func WithQuoteTweetsMediaFields(fields ...string) QuoteTweetsOption {
	return func(c *quoteTweetsConfig) {
		if len(fields) == 0 {
			return
		}
		c.mediaFields = append([]string(nil), fields...)
	}
}

// GetQuoteTweets は X API v2 `GET /2/tweets/:id/quote_tweets` を呼び出し、
// 指定ツイートを引用したツイート一覧を返す。
func (c *Client) GetQuoteTweets(
	ctx context.Context,
	tweetID string,
	opts ...QuoteTweetsOption,
) (*QuoteTweetsResponse, error) {
	cfg := quoteTweetsConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	endpoint := buildQuoteTweetsURL(c.BaseURL(), tweetID, &cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build GetQuoteTweets request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read GetQuoteTweets response: %w", err)
	}
	out := &QuoteTweetsResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetQuoteTweets response: %w", err)
	}
	return out, nil
}

// buildQuoteTweetsURL は `/2/tweets/:id/quote_tweets` の完全 URL を組み立てる。
func buildQuoteTweetsURL(baseURL, tweetID string, cfg *quoteTweetsConfig) string {
	path := "/2/tweets/" + url.PathEscape(tweetID) + "/quote_tweets"
	values := url.Values{}
	if cfg.maxResults > 0 {
		values.Set("max_results", strconv.Itoa(cfg.maxResults))
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

// tweetLookupValues は cfg の各 fields/expansions を url.Values に詰める。
func tweetLookupValues(cfg *tweetLookupConfig) url.Values {
	values := url.Values{}
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
	return values
}

// -- SearchRecent / EachSearchPage (M30) -----------------------------------
//
// `GET /2/tweets/search/recent` を呼び出す (X API v2 Basic 以上必須、Free tier は 403)。
// `EachSearchPage` は next_token を辿る rate-limit aware iterator で、M29 で抽出した
// `(c *Client).computeInterPageWait(rl, threshold)` (pagination.go) を再利用する。

// search.go 固有の定数群 (likes.go と並び)。
const (
	// searchDefaultMaxPages は WithSearchMaxPages 未指定時の上限ページ数である
	// (spec §10 `--max-pages` (default: 50))。
	searchDefaultMaxPages = 50
	// searchRateLimitThreshold は EachSearchPage が rate-limit aware sleep を発動する
	// remaining の閾値である (likes と同値 2、M30 D-6)。
	searchRateLimitThreshold = 2
)

// SearchResponse は SearchRecent / EachSearchPage が返すレスポンス本体である。
//
// X API v2 `GET /2/tweets/search/recent` のレスポンス `{ data, includes, meta }` を構造化する。
// Errors は M29 D-9 の TweetLookupError を再利用 (実発生はほぼ無いが将来互換、M30 D-7)。
type SearchResponse struct {
	// Data は検索でマッチしたツイートの配列。
	Data []Tweet `json:"data,omitempty"`
	// Includes は expansions で取得された関連リソース (users / tweets)。
	Includes Includes `json:"includes,omitempty"`
	// Meta は result_count / next_token を含むページネーション情報。
	Meta Meta `json:"meta,omitempty"`
	// Errors はバッチ/検索系の partial error (X API v2 仕様、実 search/recent では稀)。
	Errors []TweetLookupError `json:"errors,omitempty"`
}

// SearchOption は SearchRecent / EachSearchPage の挙動を変更する関数オプションである。
//
// 同じキーで複数回呼ばれた場合は最後の呼び出しが勝つ (last-wins)。
// 中間構造体 searchConfig に集約する設計は M29 D-8 と同パターン。
type SearchOption func(*searchConfig)

// searchConfig はオプション設定を集約する未公開構造体である。
type searchConfig struct {
	startTime       *time.Time
	endTime         *time.Time
	maxResults      int // 0 は no-op (X API デフォルトに任せる)
	paginationToken string
	tweetFields     []string
	expansions      []string
	userFields      []string
	mediaFields     []string

	// EachSearchPage 専用 (SearchRecent では無視される)。
	maxPages int // 0 ならデフォルト searchDefaultMaxPages を使う
}

// WithSearchMaxResults は X API の max_results を設定する。
//
// X API v2 `search/recent` は 10..100 を要求する (10 未満は 400)。
// 0 を渡すと no-op (X API デフォルト 10)。CLI 層 (M30) が下限補正を担う。
func WithSearchMaxResults(n int) SearchOption {
	return func(c *searchConfig) { c.maxResults = n }
}

// WithSearchStartTime は X API の start_time を RFC3339 (UTC, ナノ秒なし) で設定する。
// search/recent は過去 7 日まで遡れる (それ以上前は X API が 400)。
func WithSearchStartTime(t time.Time) SearchOption {
	return func(c *searchConfig) {
		tt := t
		c.startTime = &tt
	}
}

// WithSearchEndTime は X API の end_time を RFC3339 (UTC, ナノ秒なし) で設定する。
func WithSearchEndTime(t time.Time) SearchOption {
	return func(c *searchConfig) {
		tt := t
		c.endTime = &tt
	}
}

// WithSearchPaginationToken は X API の pagination_token を設定する。
// EachSearchPage は内部で next_token を都度上書きするため、初回ページのみ有効。
func WithSearchPaginationToken(token string) SearchOption {
	return func(c *searchConfig) { c.paginationToken = token }
}

// WithSearchTweetFields は X API の tweet.fields を設定する。空引数は no-op。
func WithSearchTweetFields(fields ...string) SearchOption {
	return func(c *searchConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// WithSearchExpansions は X API の expansions を設定する。空引数は no-op。
func WithSearchExpansions(exp ...string) SearchOption {
	return func(c *searchConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithSearchUserFields は X API の user.fields を設定する。空引数は no-op。
func WithSearchUserFields(fields ...string) SearchOption {
	return func(c *searchConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithSearchMediaFields は X API の media.fields を設定する。空引数は no-op。
func WithSearchMediaFields(fields ...string) SearchOption {
	return func(c *searchConfig) {
		if len(fields) == 0 {
			return
		}
		c.mediaFields = append([]string(nil), fields...)
	}
}

// WithSearchMaxPages は EachSearchPage の上限ページ数を設定する。
// 未指定 (or 0 以下) の場合 default `searchDefaultMaxPages = 50`。
// SearchRecent では本オプションは無視される。
func WithSearchMaxPages(n int) SearchOption {
	return func(c *searchConfig) { c.maxPages = n }
}

// SearchRecent は X API v2 `GET /2/tweets/search/recent` を呼び出し、
// 過去 7 日のキーワード検索結果 (単一ページ) を返す。
//
// 認証は NewClient 時に渡した *config.Credentials の OAuth 1.0a 署名で行われる。
// **Tier 要件**: X API v2 Basic 以上 (Free tier では 403 → ErrPermission → exit 4)。
//
// query は事前 trim 不要の生クエリ (X API 演算子 `from:` / `lang:` / `conversation_id:` 等)。
// 空文字列は "query must be non-empty" エラーで拒否する (X API は 400 を返すが、
// ネットワーク往復を避けるため事前バリデーション)。
//
// エラー分類は M6 Client.Do と同じ:
//   - errors.Is(err, ErrAuthentication) → 401
//   - errors.Is(err, ErrPermission)     → 403 (Free tier or scope 不足)
//   - errors.Is(err, ErrNotFound)       → 404
//   - errors.Is(err, ErrRateLimit)      → 429 リトライ枯渇
//
// レスポンスの JSON 形式: `{"data":[...],"includes":{...},"meta":{...}}`。
// `data` 配列が型不一致 → decode エラー (リトライしない)。
func (c *Client) SearchRecent(
	ctx context.Context,
	query string,
	opts ...SearchOption,
) (*SearchResponse, error) {
	if query == "" {
		return nil, fmt.Errorf("xapi: SearchRecent: query must be non-empty")
	}
	cfg := newSearchConfig(opts)
	resp, err := c.fetchSearchPage(ctx, query, &cfg)
	if err != nil {
		return nil, err
	}
	return resp.body, nil
}

// EachSearchPage は X API v2 `GET /2/tweets/search/recent` を next_token が
// 無くなるまで自動辿りし、各ページを fn に渡す (rate-limit aware ページネーション)。
//
// 終了条件 (いずれか早い方):
//   - meta.next_token が空文字列 (= 最終ページ)
//   - 取得済みページ数が WithSearchMaxPages (default 50) に到達 → 正常終了
//   - fn が non-nil error を返す → そのエラーをそのまま返却
//   - ctx が cancel された → ctx.Err() を返却
//
// ページ間 sleep:
//   - レスポンスの x-rate-limit-remaining が searchRateLimitThreshold (= 2) 以下のとき
//     x-rate-limit-reset まで sleep (最大 rateLimitMaxWait = 15 分)
//   - reset が過去 (clock skew or stale) の場合は最小 200ms にフォールバック
//   - それ以外は最小 200ms 待機 (バースト抑止)
//
// 共通ロジックは M29 の `(c *Client).computeInterPageWait` を再利用 (pagination.go)。
func (c *Client) EachSearchPage(
	ctx context.Context,
	query string,
	fn func(*SearchResponse) error,
	opts ...SearchOption,
) error {
	if query == "" {
		return fmt.Errorf("xapi: EachSearchPage: query must be non-empty")
	}
	cfg := newSearchConfig(opts)
	maxPages := cfg.maxPages
	if maxPages <= 0 {
		maxPages = searchDefaultMaxPages
	}

	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fetched, err := c.fetchSearchPage(ctx, query, &cfg)
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
		wait := c.computeInterPageWait(fetched.rateLimit, searchRateLimitThreshold)
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
		cfg.paginationToken = next
	}
	return nil
}

// searchFetched は単一ページの取得結果である (本体 + rate-limit ヘッダ情報)。
type searchFetched struct {
	body      *SearchResponse
	rateLimit RateLimitInfo
}

// fetchSearchPage は単一ページのリクエスト送出 + JSON デコードを行う。
// SearchRecent と EachSearchPage の共通実装。
func (c *Client) fetchSearchPage(
	ctx context.Context,
	query string,
	cfg *searchConfig,
) (*searchFetched, error) {
	endpoint := buildSearchURL(c.BaseURL(), query, cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build SearchRecent request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read SearchRecent response: %w", err)
	}
	out := &SearchResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode SearchRecent response: %w", err)
	}
	return &searchFetched{body: out, rateLimit: resp.RateLimit}, nil
}

// buildSearchURL は `GET /2/tweets/search/recent` の完全 URL を組み立てる。
//
// D-3: query は生のまま url.Values.Set し、url.Values.Encode() に任せる。
// X API 演算子 (`from:` / `conversation_id:` / `lang:`) のコロンは `%3A` にエスケープ
// されるが、X API は両表現を受け付ける想定 (v0.5.0 リリース前に実機 smoke test)。
func buildSearchURL(baseURL, query string, cfg *searchConfig) string {
	values := url.Values{}
	values.Set("query", query)
	if cfg.maxResults > 0 {
		values.Set("max_results", strconv.Itoa(cfg.maxResults))
	}
	if cfg.startTime != nil {
		values.Set("start_time", cfg.startTime.UTC().Format(time.RFC3339))
	}
	if cfg.endTime != nil {
		values.Set("end_time", cfg.endTime.UTC().Format(time.RFC3339))
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
	if len(cfg.mediaFields) > 0 {
		values.Set("media.fields", strings.Join(cfg.mediaFields, ","))
	}
	return baseURL + "/2/tweets/search/recent?" + values.Encode()
}

// newSearchConfig は opts を適用した searchConfig を返す。
func newSearchConfig(opts []SearchOption) searchConfig {
	cfg := searchConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
