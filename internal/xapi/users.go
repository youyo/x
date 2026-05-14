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
)

// UserFieldsOption は GetUserMe のクエリパラメータを設定する関数オプションである。
//
// 同じキーで複数回呼ばれた場合は最後の呼び出しが勝つ (url.Values.Set 挙動)。
//
// 将来的 (M8 likes) には RequestOption へ統合し本型はエイリアスとして残す予定。
type UserFieldsOption func(*url.Values)

// WithUserFields は X API v2 の `user.fields` クエリパラメータを設定する。
//
// 例: `WithUserFields("username", "name", "verified", "public_metrics")` は
// URL に `?user.fields=username,name,verified,public_metrics` を付与する。
//
// 空引数で呼ばれた場合はクエリパラメータを変更しない (no-op)。
// 既に同名のフィールドが設定されている場合は上書きする (last-wins)。
//
// 利用可能なフィールド名は X API v2 ドキュメントを参照すること。
// 代表例: id / username / name / verified / description / public_metrics /
// created_at / profile_image_url / protected。
func WithUserFields(fields ...string) UserFieldsOption {
	return func(v *url.Values) {
		if len(fields) == 0 {
			return
		}
		v.Set("user.fields", strings.Join(fields, ","))
	}
}

// GetUserMe は X API v2 `GET /2/users/me` を呼び出し、認証ユーザーの User 情報を返す。
//
// 認証は NewClient 時に渡した *config.Credentials の OAuth 1.0a 署名で行われる。
// opts でクエリパラメータをカスタマイズできる (例: WithUserFields)。
//
// エラーの分類は M6 Client.Do と同じ:
//   - errors.Is(err, ErrAuthentication) → 401
//   - errors.Is(err, ErrPermission)     → 403
//   - errors.Is(err, ErrNotFound)       → 404
//   - errors.Is(err, ErrRateLimit)      → 429 リトライ枯渇
//   - errors.As(err, &apiErr)           → APIError から Body/Header/StatusCode 取得
//   - errors.Is(err, context.Canceled)  → context cancel
//
// レスポンスの JSON 形式: `{"data": {"id": "...", "username": "...", ...}}`。
// `data` フィールドが文字列等の型不一致だった場合は decode エラー
// (xapi: decode GetUserMe response) を返し、リトライはしない。
func (c *Client) GetUserMe(ctx context.Context, opts ...UserFieldsOption) (*User, error) {
	values := url.Values{}
	for _, opt := range opts {
		opt(&values)
	}
	endpoint := c.BaseURL() + "/2/users/me"
	if q := values.Encode(); q != "" {
		endpoint += "?" + q
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build GetUserMe request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read GetUserMe response: %w", err)
	}
	var env struct {
		Data User `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("xapi: decode GetUserMe response: %w", err)
	}
	return &env.Data, nil
}

// =============================================================================
// M32: Users Extended — lookup / search / graph / blocking / muting
// =============================================================================
//
// 9 endpoint をラップする (5 endpoint がページング iterator 付き):
//   - GET /2/users/:id                            → GetUser
//   - GET /2/users                                → GetUsers (?ids=, 1..100)
//   - GET /2/users/by/username/:username          → GetUserByUsername
//   - GET /2/users/by                             → GetUsersByUsernames (?usernames=, 1..100)
//   - GET /2/users/search                         → SearchUsers / EachSearchUsersPage (next_token)
//   - GET /2/users/:id/following                  → GetFollowing / EachFollowingPage (pagination_token)
//   - GET /2/users/:id/followers                  → GetFollowers / EachFollowersPage (pagination_token)
//   - GET /2/users/:id/blocking                   → GetBlocking / EachBlockingPage (self only)
//   - GET /2/users/:id/muting                     → GetMuting / EachMutingPage (self only)
//
// 設計判断:
//   - Option 型を **3 種類に分離** (UserLookupOption / UserSearchOption / UserGraphOption) — M32 D-1
//   - graph 4 endpoint は eachUserGraphPage(suffix, ...) で DRY (M31 timeline と同形)
//   - search のページネーション key は next_token (graph は pagination_token、別実装) — M32 D-3
//   - partial error は UserLookupError (TweetLookupError と同形だが別型) — M32 D-15

const (
	// userGraphDefaultMaxPages は WithUserGraphMaxPages 未指定時の上限ページ数。
	userGraphDefaultMaxPages = 50
	// userGraphRateLimitThreshold は Each*Page が rate-limit aware sleep を発動する閾値 (M31 timeline と同値)。
	userGraphRateLimitThreshold = 2
	// userSearchDefaultMaxPages は WithUserSearchMaxPages 未指定時の上限。
	userSearchDefaultMaxPages = 50
	// userSearchRateLimitThreshold は SearchUsers の iterator 用 threshold。
	userSearchRateLimitThreshold = 2

	// userLookupBatchMaxIDs は GetUsers / GetUsersByUsernames の per-call 件数上限 (X API 仕様)。
	userLookupBatchMaxIDs = 100

	// graph endpoint の path suffix。
	userGraphSuffixFollowing = "following"
	userGraphSuffixFollowers = "followers"
	userGraphSuffixBlocking  = "blocking"
	userGraphSuffixMuting    = "muting"
)

// UserLookupError は GetUsers / GetUsersByUsernames の partial error を表す DTO である (M32 D-15)。
//
// X API の partial error スキーマは TweetLookupError と同形だが、User 用に別型として定義することで
// 責務 (どのリソースの error か) を型レベルで区別する。
type UserLookupError struct {
	Value        string `json:"value,omitempty"`
	Detail       string `json:"detail,omitempty"`
	Title        string `json:"title,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	Parameter    string `json:"parameter,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	Type         string `json:"type,omitempty"`
}

// UserResponse は GetUser / GetUserByUsername の単一ユーザーレスポンス本体である (M32)。
type UserResponse struct {
	Data     *User             `json:"data,omitempty"`
	Includes Includes          `json:"includes,omitempty"`
	Errors   []UserLookupError `json:"errors,omitempty"`
}

// UsersResponse は GetUsers / GetUsersByUsernames / SearchUsers / GetFollowing / GetFollowers /
// GetBlocking / GetMuting が返す配列レスポンス本体である (M32)。
//
// Meta は graph/search 系のみ非空。lookup batch では空。
type UsersResponse struct {
	Data     []User            `json:"data,omitempty"`
	Includes Includes          `json:"includes,omitempty"`
	Meta     Meta              `json:"meta,omitempty"`
	Errors   []UserLookupError `json:"errors,omitempty"`
}

// -----------------------------------------------------------------------------
// Option types — 3 種類に分離 (M32 D-1)
// -----------------------------------------------------------------------------

// UserLookupOption は GetUser / GetUsers / GetUserByUsername / GetUsersByUsernames で共通の
// クエリオプションである。max_results / pagination は持たない。
type UserLookupOption func(*userLookupConfig)

type userLookupConfig struct {
	userFields  []string
	expansions  []string
	tweetFields []string
}

// WithUserLookupUserFields は X API の user.fields を設定する。空引数は no-op。
func WithUserLookupUserFields(fields ...string) UserLookupOption {
	return func(c *userLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithUserLookupExpansions は X API の expansions を設定する。空引数は no-op。
func WithUserLookupExpansions(exp ...string) UserLookupOption {
	return func(c *userLookupConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithUserLookupTweetFields は X API の tweet.fields を設定する (pinned_tweet_id expansion 用)。空引数は no-op。
func WithUserLookupTweetFields(fields ...string) UserLookupOption {
	return func(c *userLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// UserSearchOption は SearchUsers / EachSearchUsersPage で使用するオプションである。
// X API のページネーションキーは next_token (pagination_token ではない、M32 D-3)。
type UserSearchOption func(*userSearchConfig)

type userSearchConfig struct {
	maxResults  int
	nextToken   string
	userFields  []string
	expansions  []string
	tweetFields []string
	maxPages    int
}

// WithUserSearchMaxResults は X API の max_results を設定する (1..1000、default 100)。
// 0 は no-op (X API デフォルトに任せる)。
func WithUserSearchMaxResults(n int) UserSearchOption {
	return func(c *userSearchConfig) { c.maxResults = n }
}

// WithUserSearchNextToken は X API の next_token を設定する。
// EachSearchUsersPage は内部で next_token を都度上書きするため、初回ページのみ有効。
func WithUserSearchNextToken(token string) UserSearchOption {
	return func(c *userSearchConfig) { c.nextToken = token }
}

// WithUserSearchUserFields は X API の user.fields を設定する。空引数は no-op。
func WithUserSearchUserFields(fields ...string) UserSearchOption {
	return func(c *userSearchConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithUserSearchExpansions は X API の expansions を設定する。空引数は no-op。
func WithUserSearchExpansions(exp ...string) UserSearchOption {
	return func(c *userSearchConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithUserSearchTweetFields は X API の tweet.fields を設定する。空引数は no-op。
func WithUserSearchTweetFields(fields ...string) UserSearchOption {
	return func(c *userSearchConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// WithUserSearchMaxPages は EachSearchUsersPage の上限ページ数を設定する。
// 未指定 (or 0 以下) の場合は default `userSearchDefaultMaxPages = 50`。
func WithUserSearchMaxPages(n int) UserSearchOption {
	return func(c *userSearchConfig) { c.maxPages = n }
}

// UserGraphOption は GetFollowing / GetFollowers / GetBlocking / GetMuting で共通の
// クエリオプションである。X API のページネーションキーは pagination_token。
type UserGraphOption func(*userGraphConfig)

type userGraphConfig struct {
	maxResults      int
	paginationToken string
	userFields      []string
	expansions      []string
	tweetFields     []string
	maxPages        int
}

// WithUserGraphMaxResults は X API の max_results を設定する (1..1000)。
// 0 は no-op (X API デフォルトに任せる)。CLI 層で範囲チェックを担う。
func WithUserGraphMaxResults(n int) UserGraphOption {
	return func(c *userGraphConfig) { c.maxResults = n }
}

// WithUserGraphPaginationToken は X API の pagination_token を設定する。
// Each*Page は内部で pagination_token を都度上書きするため、初回ページのみ有効。
func WithUserGraphPaginationToken(token string) UserGraphOption {
	return func(c *userGraphConfig) { c.paginationToken = token }
}

// WithUserGraphUserFields は X API の user.fields を設定する。空引数は no-op。
func WithUserGraphUserFields(fields ...string) UserGraphOption {
	return func(c *userGraphConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithUserGraphExpansions は X API の expansions を設定する。空引数は no-op。
func WithUserGraphExpansions(exp ...string) UserGraphOption {
	return func(c *userGraphConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithUserGraphTweetFields は X API の tweet.fields を設定する。空引数は no-op。
func WithUserGraphTweetFields(fields ...string) UserGraphOption {
	return func(c *userGraphConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// WithUserGraphMaxPages は Each*Page の上限ページ数を設定する (default 50)。
func WithUserGraphMaxPages(n int) UserGraphOption {
	return func(c *userGraphConfig) { c.maxPages = n }
}

// -----------------------------------------------------------------------------
// Lookup endpoints (4 関数、iterator なし)
// -----------------------------------------------------------------------------

// GetUser は X API v2 `GET /2/users/:id` を呼び出し、単一ユーザーを返す (M32)。
//
// userID は url.PathEscape されパスに埋め込まれる。
// エラー分類は M6 Client.Do と同じ (ErrAuthentication / ErrPermission / ErrNotFound / ErrRateLimit)。
func (c *Client) GetUser(ctx context.Context, userID string, opts ...UserLookupOption) (*UserResponse, error) {
	if userID == "" {
		return nil, fmt.Errorf("xapi: GetUser: userID must be non-empty")
	}
	cfg := newUserLookupConfig(opts)
	endpoint := buildUserLookupURL(c.BaseURL(), "/2/users/"+url.PathEscape(userID), nil, "", &cfg)
	body, err := c.fetchJSON(ctx, endpoint, "GetUser")
	if err != nil {
		return nil, err
	}
	out := &UserResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetUser response: %w", err)
	}
	return out, nil
}

// GetUsers は X API v2 `GET /2/users?ids=ID1,ID2,...` を呼び出し、複数ユーザーをバッチ取得する (M32)。
//
// ids は 1..100 件の範囲で渡す必要がある。範囲外はエラー。
// partial error (一部 ID が見つからない等) は UsersResponse.Errors に入る (HTTP 200 OK)。
func (c *Client) GetUsers(ctx context.Context, ids []string, opts ...UserLookupOption) (*UsersResponse, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("xapi: GetUsers: ids must be non-empty")
	}
	if len(ids) > userLookupBatchMaxIDs {
		return nil, fmt.Errorf("xapi: GetUsers: ids must be at most %d, got %d", userLookupBatchMaxIDs, len(ids))
	}
	cfg := newUserLookupConfig(opts)
	endpoint := buildUserLookupURL(c.BaseURL(), "/2/users", ids, "ids", &cfg)
	body, err := c.fetchJSON(ctx, endpoint, "GetUsers")
	if err != nil {
		return nil, err
	}
	out := &UsersResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetUsers response: %w", err)
	}
	return out, nil
}

// GetUserByUsername は X API v2 `GET /2/users/by/username/:username` を呼び出す (M32)。
func (c *Client) GetUserByUsername(ctx context.Context, username string, opts ...UserLookupOption) (*UserResponse, error) {
	if username == "" {
		return nil, fmt.Errorf("xapi: GetUserByUsername: username must be non-empty")
	}
	cfg := newUserLookupConfig(opts)
	endpoint := buildUserLookupURL(c.BaseURL(), "/2/users/by/username/"+url.PathEscape(username), nil, "", &cfg)
	body, err := c.fetchJSON(ctx, endpoint, "GetUserByUsername")
	if err != nil {
		return nil, err
	}
	out := &UserResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetUserByUsername response: %w", err)
	}
	return out, nil
}

// GetUsersByUsernames は X API v2 `GET /2/users/by?usernames=alice,bob,...` を呼び出す (M32)。
//
// usernames は 1..100 件。範囲外はエラー。partial error は UsersResponse.Errors に入る。
func (c *Client) GetUsersByUsernames(ctx context.Context, usernames []string, opts ...UserLookupOption) (*UsersResponse, error) {
	if len(usernames) == 0 {
		return nil, fmt.Errorf("xapi: GetUsersByUsernames: usernames must be non-empty")
	}
	if len(usernames) > userLookupBatchMaxIDs {
		return nil, fmt.Errorf("xapi: GetUsersByUsernames: usernames must be at most %d, got %d", userLookupBatchMaxIDs, len(usernames))
	}
	cfg := newUserLookupConfig(opts)
	endpoint := buildUserLookupURL(c.BaseURL(), "/2/users/by", usernames, "usernames", &cfg)
	body, err := c.fetchJSON(ctx, endpoint, "GetUsersByUsernames")
	if err != nil {
		return nil, err
	}
	out := &UsersResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetUsersByUsernames response: %w", err)
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// SearchUsers + EachSearchUsersPage
// -----------------------------------------------------------------------------

// SearchUsers は X API v2 `GET /2/users/search?query=...` を呼び出し、ユーザー検索結果 (単一ページ) を返す (M32)。
//
// max_results は X API 仕様で 1..1000 (default 100)。pagination は next_token (graph 系の pagination_token と異なる)。
func (c *Client) SearchUsers(ctx context.Context, query string, opts ...UserSearchOption) (*UsersResponse, error) {
	if query == "" {
		return nil, fmt.Errorf("xapi: SearchUsers: query must be non-empty")
	}
	cfg := newUserSearchConfig(opts)
	endpoint := buildUserSearchURL(c.BaseURL(), query, &cfg)
	fetched, err := c.fetchUserSearchPage(ctx, endpoint, "SearchUsers")
	if err != nil {
		return nil, err
	}
	return fetched.body, nil
}

// EachSearchUsersPage は SearchUsers を next_token で自動辿りする rate-limit aware iterator (M32)。
func (c *Client) EachSearchUsersPage(
	ctx context.Context,
	query string,
	fn func(*UsersResponse) error,
	opts ...UserSearchOption,
) error {
	if query == "" {
		return fmt.Errorf("xapi: EachSearchUsersPage: query must be non-empty")
	}
	cfg := newUserSearchConfig(opts)
	maxPages := cfg.maxPages
	if maxPages <= 0 {
		maxPages = userSearchDefaultMaxPages
	}
	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		endpoint := buildUserSearchURL(c.BaseURL(), query, &cfg)
		fetched, err := c.fetchUserSearchPage(ctx, endpoint, "EachSearchUsersPage")
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
		wait := c.computeInterPageWait(fetched.rateLimit, userSearchRateLimitThreshold)
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
		cfg.nextToken = next
	}
	return nil
}

// -----------------------------------------------------------------------------
// Graph endpoints (4 関数 + 4 iterator、DRY 共通実装)
// -----------------------------------------------------------------------------

// GetFollowing は X API v2 `GET /2/users/:id/following` を呼び出す (M32)。
func (c *Client) GetFollowing(ctx context.Context, userID string, opts ...UserGraphOption) (*UsersResponse, error) {
	return c.getUserGraph(ctx, "GetFollowing", userGraphSuffixFollowing, userID, opts)
}

// GetFollowers は X API v2 `GET /2/users/:id/followers` を呼び出す (M32)。
func (c *Client) GetFollowers(ctx context.Context, userID string, opts ...UserGraphOption) (*UsersResponse, error) {
	return c.getUserGraph(ctx, "GetFollowers", userGraphSuffixFollowers, userID, opts)
}

// GetBlocking は X API v2 `GET /2/users/:id/blocking` を呼び出す (self only、M32)。
//
// X API 仕様で userID = 認証ユーザー必須 (他人の blocking は取得不可)。CLI 層 (M32) で GetUserMe で自動解決する設計 (M32 D-5)。
func (c *Client) GetBlocking(ctx context.Context, userID string, opts ...UserGraphOption) (*UsersResponse, error) {
	return c.getUserGraph(ctx, "GetBlocking", userGraphSuffixBlocking, userID, opts)
}

// GetMuting は X API v2 `GET /2/users/:id/muting` を呼び出す (self only、M32)。
func (c *Client) GetMuting(ctx context.Context, userID string, opts ...UserGraphOption) (*UsersResponse, error) {
	return c.getUserGraph(ctx, "GetMuting", userGraphSuffixMuting, userID, opts)
}

// EachFollowingPage は GetFollowing を pagination_token で自動辿りする iterator (M32)。
func (c *Client) EachFollowingPage(
	ctx context.Context, userID string, fn func(*UsersResponse) error, opts ...UserGraphOption,
) error {
	return c.eachUserGraphPage(ctx, "EachFollowingPage", userGraphSuffixFollowing, userID, fn, opts)
}

// EachFollowersPage は GetFollowers を pagination_token で自動辿りする iterator (M32)。
func (c *Client) EachFollowersPage(
	ctx context.Context, userID string, fn func(*UsersResponse) error, opts ...UserGraphOption,
) error {
	return c.eachUserGraphPage(ctx, "EachFollowersPage", userGraphSuffixFollowers, userID, fn, opts)
}

// EachBlockingPage は GetBlocking を pagination_token で自動辿りする iterator (self only、M32)。
func (c *Client) EachBlockingPage(
	ctx context.Context, userID string, fn func(*UsersResponse) error, opts ...UserGraphOption,
) error {
	return c.eachUserGraphPage(ctx, "EachBlockingPage", userGraphSuffixBlocking, userID, fn, opts)
}

// EachMutingPage は GetMuting を pagination_token で自動辿りする iterator (self only、M32)。
func (c *Client) EachMutingPage(
	ctx context.Context, userID string, fn func(*UsersResponse) error, opts ...UserGraphOption,
) error {
	return c.eachUserGraphPage(ctx, "EachMutingPage", userGraphSuffixMuting, userID, fn, opts)
}

// getUserGraph は 4 graph endpoint の DRY 共通実装である。
func (c *Client) getUserGraph(
	ctx context.Context, funcName, suffix, userID string, opts []UserGraphOption,
) (*UsersResponse, error) {
	if userID == "" {
		return nil, fmt.Errorf("xapi: %s: userID must be non-empty", funcName)
	}
	cfg := newUserGraphConfig(opts)
	fetched, err := c.fetchUserGraphPage(ctx, suffix, userID, &cfg)
	if err != nil {
		return nil, err
	}
	return fetched.body, nil
}

// eachUserGraphPage は 4 graph iterator の DRY 共通実装である。
//
// 終了条件 (いずれか早い方):
//   - meta.next_token が空文字列 (= 最終ページ)
//   - 取得済みページ数が WithUserGraphMaxPages (default 50) に到達 → 正常終了
//   - fn が non-nil error を返す → そのまま返却
//   - ctx が cancel された → ctx.Err() を返却
//
// ページ間 sleep は computeInterPageWait + userGraphRateLimitThreshold (= 2) を再利用。
func (c *Client) eachUserGraphPage(
	ctx context.Context, funcName, suffix, userID string,
	fn func(*UsersResponse) error, opts []UserGraphOption,
) error {
	if userID == "" {
		return fmt.Errorf("xapi: %s: userID must be non-empty", funcName)
	}
	cfg := newUserGraphConfig(opts)
	maxPages := cfg.maxPages
	if maxPages <= 0 {
		maxPages = userGraphDefaultMaxPages
	}
	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fetched, err := c.fetchUserGraphPage(ctx, suffix, userID, &cfg)
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
		wait := c.computeInterPageWait(fetched.rateLimit, userGraphRateLimitThreshold)
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
		cfg.paginationToken = next
	}
	return nil
}

// -----------------------------------------------------------------------------
// 内部 fetch / URL build / config 関数
// -----------------------------------------------------------------------------

// userGraphFetched は graph endpoint single page の取得結果である。
type userGraphFetched struct {
	body      *UsersResponse
	rateLimit RateLimitInfo
}

// fetchUserGraphPage は graph endpoint single page を取得する DRY 内部関数。
func (c *Client) fetchUserGraphPage(
	ctx context.Context, suffix, userID string, cfg *userGraphConfig,
) (*userGraphFetched, error) {
	endpoint := buildUserGraphURL(c.BaseURL(), suffix, userID, cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build graph request (%s): %w", suffix, err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read graph response (%s): %w", suffix, err)
	}
	out := &UsersResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode graph response (%s): %w", suffix, err)
	}
	return &userGraphFetched{body: out, rateLimit: resp.RateLimit}, nil
}

// userSearchFetched は search endpoint single page の取得結果である。
type userSearchFetched struct {
	body      *UsersResponse
	rateLimit RateLimitInfo
}

// fetchUserSearchPage は SearchUsers single page を取得する DRY 内部関数。
func (c *Client) fetchUserSearchPage(
	ctx context.Context, endpoint, funcName string,
) (*userSearchFetched, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build %s request: %w", funcName, err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read %s response: %w", funcName, err)
	}
	out := &UsersResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode %s response: %w", funcName, err)
	}
	return &userSearchFetched{body: out, rateLimit: resp.RateLimit}, nil
}

// fetchJSON は lookup endpoint single page (ページング無し) を取得する DRY 内部関数。
//
// 戻り値はレスポンスボディの []byte で、呼び出し側で UserResponse / UsersResponse 等にデコードする。
func (c *Client) fetchJSON(ctx context.Context, endpoint, funcName string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build %s request: %w", funcName, err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read %s response: %w", funcName, err)
	}
	return body, nil
}

// buildUserLookupURL は lookup endpoint の完全 URL を組み立てる。
// path は "/2/users/<id>" 等の固定パス、batchKey は "ids" / "usernames" / "" (batch 不要)、
// batchValues は batch 用の値配列。
func buildUserLookupURL(baseURL, path string, batchValues []string, batchKey string, cfg *userLookupConfig) string {
	values := url.Values{}
	if batchKey != "" && len(batchValues) > 0 {
		values.Set(batchKey, strings.Join(batchValues, ","))
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

// buildUserSearchURL は SearchUsers の完全 URL を組み立てる。
// X API クエリパラメータ: query (必須), max_results, next_token, user.fields, expansions, tweet.fields
func buildUserSearchURL(baseURL, query string, cfg *userSearchConfig) string {
	values := url.Values{}
	values.Set("query", query)
	if cfg.maxResults > 0 {
		values.Set("max_results", strconv.Itoa(cfg.maxResults))
	}
	if cfg.nextToken != "" {
		values.Set("next_token", cfg.nextToken)
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
	return baseURL + "/2/users/search?" + values.Encode()
}

// buildUserGraphURL は graph endpoint (`/2/users/:id/<suffix>`) の完全 URL を組み立てる。
// userID は url.PathEscape されパスに埋め込まれる。
func buildUserGraphURL(baseURL, suffix, userID string, cfg *userGraphConfig) string {
	path := "/2/users/" + url.PathEscape(userID) + "/" + suffix
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

func newUserLookupConfig(opts []UserLookupOption) userLookupConfig {
	cfg := userLookupConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func newUserSearchConfig(opts []UserSearchOption) userSearchConfig {
	cfg := userSearchConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func newUserGraphConfig(opts []UserGraphOption) userGraphConfig {
	cfg := userGraphConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
