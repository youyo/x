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

// lists.go は X API v2 の List 系 7 エンドポイントをラップする (M33)。
//
//   - GET /2/lists/:id                           → GetList                   (lookup, no iterator)
//   - GET /2/lists/:id/tweets                    → GetListTweets / EachListTweetsPage
//   - GET /2/lists/:id/members                   → GetListMembers / EachListMembersPage
//   - GET /2/users/:id/owned_lists               → GetOwnedLists / EachOwnedListsPage
//   - GET /2/users/:id/list_memberships          → GetListMemberships / EachListMembershipsPage
//   - GET /2/users/:id/followed_lists            → GetFollowedLists / EachFollowedListsPage
//   - GET /2/users/:id/pinned_lists              → GetPinnedLists             (lookup, no iterator, self only)
//
// 設計判断 (詳細は plans/x-m33-lists.md):
//   - Option 型を **2 種類に分離** (ListLookupOption / ListPagedOption) — M33 D-1
//   - 5 paged endpoint は全て pagination_token (X API 仕様で統一、M33 D-2 確認済)
//   - GetListMembers のレスポンス型は既存 UsersResponse を再利用 — M33 D-4
//   - GetPinnedLists の Option は ListLookupOption を再利用 (paginate なし) — M33 D-5
//   - レスポンス型ごとに 3 種類の fetch/each ヘルパを分離 (generics 化見送り) — M33 D-3
//
// パッケージ doc は他ファイル (oauth1.go) に集約済のため、本ファイルには書かない。

const (
	// listPagedDefaultMaxPages は WithListPagedMaxPages 未指定時の上限ページ数。
	listPagedDefaultMaxPages = 50
	// listPagedRateLimitThreshold は Each*Page が rate-limit aware sleep を発動する閾値。
	listPagedRateLimitThreshold = 2

	// path suffix。
	listPagedSuffixTweets      = "tweets"
	listPagedSuffixMembers     = "members"
	userListsSuffixOwned       = "owned_lists"
	userListsSuffixMemberships = "list_memberships"
	userListsSuffixFollowed    = "followed_lists"
	userListsSuffixPinned      = "pinned_lists"
)

// =============================================================================
// DTOs
// =============================================================================

// List は X API v2 の List オブジェクトを表す DTO である (M33)。
//
// 必須フィールド (id / name) は X API がデフォルトで返却する。
// それ以外 (Private / Description / OwnerID / MemberCount / FollowerCount / CreatedAt) は
// `list.fields` クエリパラメータで明示的に要求した場合のみ返却される。
type List struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Private       bool   `json:"private,omitempty"`
	Description   string `json:"description,omitempty"`
	OwnerID       string `json:"owner_id,omitempty"`
	MemberCount   int    `json:"member_count,omitempty"`
	FollowerCount int    `json:"follower_count,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
}

// ListResponse は GetList の単一 List レスポンス本体である (M33)。
type ListResponse struct {
	Data     *List    `json:"data,omitempty"`
	Includes Includes `json:"includes,omitempty"`
}

// ListsResponse は GetOwnedLists / GetListMemberships / GetFollowedLists /
// GetPinnedLists が返す List 配列レスポンス本体である (M33)。
//
// GetListMembers (User 配列) は UsersResponse、GetListTweets (Tweet 配列) は
// ListTweetsResponse をそれぞれ用いる。
type ListsResponse struct {
	Data     []List   `json:"data,omitempty"`
	Includes Includes `json:"includes,omitempty"`
	Meta     Meta     `json:"meta,omitempty"`
}

// ListTweetsResponse は GetListTweets が返す Tweet 配列レスポンス本体である (M33)。
//
// 型としては既存 TimelineResponse / LikedTweetsResponse とフィールド構成は同一だが、
// 責務分離 (どの endpoint のレスポンスか) を明示するため別型として定義する。
type ListTweetsResponse struct {
	Data     []Tweet  `json:"data,omitempty"`
	Includes Includes `json:"includes,omitempty"`
	Meta     Meta     `json:"meta,omitempty"`
}

// =============================================================================
// Option types — 2 種類に分離 (M33 D-1)
// =============================================================================

// ListLookupOption は GetList / GetPinnedLists 用の関数オプションである。
// max_results / pagination は持たない (両 endpoint とも paginate なし)。
type ListLookupOption func(*listLookupConfig)

type listLookupConfig struct {
	listFields []string
	expansions []string
	userFields []string
}

// WithListLookupListFields は X API の list.fields を設定する。空引数は no-op。
func WithListLookupListFields(fields ...string) ListLookupOption {
	return func(c *listLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.listFields = append([]string(nil), fields...)
	}
}

// WithListLookupExpansions は X API の expansions を設定する (主に "owner_id")。空引数は no-op。
func WithListLookupExpansions(exp ...string) ListLookupOption {
	return func(c *listLookupConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithListLookupUserFields は X API の user.fields を設定する (owner_id expansion 用)。空引数は no-op。
func WithListLookupUserFields(fields ...string) ListLookupOption {
	return func(c *listLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// ListPagedOption は paged 5 endpoint (tweets / members / owned / memberships / followed) で
// 共通の関数オプションである。pagination_token + max_results に加え、各種 fields をサポートする。
type ListPagedOption func(*listPagedConfig)

type listPagedConfig struct {
	maxResults      int // 0 は no-op (X API デフォルト)
	paginationToken string
	listFields      []string
	userFields      []string
	tweetFields     []string
	expansions      []string
	mediaFields     []string

	maxPages int // Each 専用 (0 はデフォルト 50)
}

// WithListPagedMaxResults は X API の max_results を設定する (1..100、default 100)。
// 0 は no-op (X API デフォルトに任せる)。
func WithListPagedMaxResults(n int) ListPagedOption {
	return func(c *listPagedConfig) { c.maxResults = n }
}

// WithListPagedPaginationToken は X API の pagination_token を設定する。
// Each*Page は内部で都度上書きするため、初回ページのみ有効。
func WithListPagedPaginationToken(token string) ListPagedOption {
	return func(c *listPagedConfig) { c.paginationToken = token }
}

// WithListPagedListFields は X API の list.fields を設定する。空引数は no-op。
func WithListPagedListFields(fields ...string) ListPagedOption {
	return func(c *listPagedConfig) {
		if len(fields) == 0 {
			return
		}
		c.listFields = append([]string(nil), fields...)
	}
}

// WithListPagedUserFields は X API の user.fields を設定する。空引数は no-op。
func WithListPagedUserFields(fields ...string) ListPagedOption {
	return func(c *listPagedConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithListPagedTweetFields は X API の tweet.fields を設定する。空引数は no-op。
func WithListPagedTweetFields(fields ...string) ListPagedOption {
	return func(c *listPagedConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// WithListPagedExpansions は X API の expansions を設定する。空引数は no-op。
func WithListPagedExpansions(exp ...string) ListPagedOption {
	return func(c *listPagedConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithListPagedMediaFields は X API の media.fields を設定する (主に list tweets で利用)。空引数は no-op。
func WithListPagedMediaFields(fields ...string) ListPagedOption {
	return func(c *listPagedConfig) {
		if len(fields) == 0 {
			return
		}
		c.mediaFields = append([]string(nil), fields...)
	}
}

// WithListPagedMaxPages は Each*Page の上限ページ数を設定する (default 50)。
func WithListPagedMaxPages(n int) ListPagedOption {
	return func(c *listPagedConfig) { c.maxPages = n }
}

// =============================================================================
// GetList / GetPinnedLists (Lookup endpoints, paginate なし)
// =============================================================================

// GetList は X API v2 `GET /2/lists/:id` を呼び出し、単一 List を返す (M33)。
//
// listID は url.PathEscape されパスに埋め込まれる。
// エラー分類は M6 Client.Do と同じ (ErrAuthentication / ErrPermission / ErrNotFound / ErrRateLimit)。
func (c *Client) GetList(ctx context.Context, listID string, opts ...ListLookupOption) (*ListResponse, error) {
	if listID == "" {
		return nil, fmt.Errorf("xapi: GetList: listID must be non-empty")
	}
	cfg := newListLookupConfig(opts)
	endpoint := buildListLookupURL(c.BaseURL(), "/2/lists/"+url.PathEscape(listID), &cfg)
	body, err := c.fetchJSON(ctx, endpoint, "GetList")
	if err != nil {
		return nil, err
	}
	out := &ListResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetList response: %w", err)
	}
	return out, nil
}

// GetPinnedLists は X API v2 `GET /2/users/:id/pinned_lists` を呼び出す (M33)。
//
// X API 仕様で userID は **認証ユーザー必須** (self only)。CLI 層 (M33) で GetUserMe で
// 自動解決する設計 (M33 D-7)。本関数自体は素直に渡された userID を使う。
//
// この endpoint はページネーションをサポートしないため、ListLookupOption を再利用する (M33 D-5)。
func (c *Client) GetPinnedLists(ctx context.Context, userID string, opts ...ListLookupOption) (*ListsResponse, error) {
	if userID == "" {
		return nil, fmt.Errorf("xapi: GetPinnedLists: userID must be non-empty")
	}
	cfg := newListLookupConfig(opts)
	endpoint := buildListLookupURL(
		c.BaseURL(),
		"/2/users/"+url.PathEscape(userID)+"/"+userListsSuffixPinned,
		&cfg,
	)
	body, err := c.fetchJSON(ctx, endpoint, "GetPinnedLists")
	if err != nil {
		return nil, err
	}
	out := &ListsResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetPinnedLists response: %w", err)
	}
	return out, nil
}

// =============================================================================
// GetListTweets / EachListTweetsPage (paged, Tweet 配列)
// =============================================================================

// GetListTweets は X API v2 `GET /2/lists/:id/tweets` を呼び出し、List ツイート単一ページを返す (M33)。
//
// max_results は X API 仕様で 1..100 (default 100、CLI 層で範囲チェック)。
// pagination_token で次ページを取得する (M33 D-2)。
func (c *Client) GetListTweets(ctx context.Context, listID string, opts ...ListPagedOption) (*ListTweetsResponse, error) {
	if listID == "" {
		return nil, fmt.Errorf("xapi: GetListTweets: listID must be non-empty")
	}
	cfg := newListPagedConfig(opts)
	fetched, err := c.fetchListTweetsPage(
		ctx,
		"/2/lists/"+url.PathEscape(listID)+"/"+listPagedSuffixTweets,
		&cfg,
		"GetListTweets",
	)
	if err != nil {
		return nil, err
	}
	return fetched.body, nil
}

// EachListTweetsPage は GetListTweets を pagination_token で自動辿りする iterator (M33)。
func (c *Client) EachListTweetsPage(
	ctx context.Context,
	listID string,
	fn func(*ListTweetsResponse) error,
	opts ...ListPagedOption,
) error {
	if listID == "" {
		return fmt.Errorf("xapi: EachListTweetsPage: listID must be non-empty")
	}
	cfg := newListPagedConfig(opts)
	maxPages := cfg.maxPages
	if maxPages <= 0 {
		maxPages = listPagedDefaultMaxPages
	}
	path := "/2/lists/" + url.PathEscape(listID) + "/" + listPagedSuffixTweets
	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fetched, err := c.fetchListTweetsPage(ctx, path, &cfg, "EachListTweetsPage")
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
		wait := c.computeInterPageWait(fetched.rateLimit, listPagedRateLimitThreshold)
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
		cfg.paginationToken = next
	}
	return nil
}

// =============================================================================
// GetListMembers / EachListMembersPage (paged, User 配列。UsersResponse 再利用 M33 D-4)
// =============================================================================

// GetListMembers は X API v2 `GET /2/lists/:id/members` を呼び出し、List メンバー単一ページを返す (M33)。
//
// レスポンス型は既存 UsersResponse を再利用する (M33 D-4)。
func (c *Client) GetListMembers(ctx context.Context, listID string, opts ...ListPagedOption) (*UsersResponse, error) {
	if listID == "" {
		return nil, fmt.Errorf("xapi: GetListMembers: listID must be non-empty")
	}
	cfg := newListPagedConfig(opts)
	fetched, err := c.fetchListMembersPage(
		ctx,
		"/2/lists/"+url.PathEscape(listID)+"/"+listPagedSuffixMembers,
		&cfg,
		"GetListMembers",
	)
	if err != nil {
		return nil, err
	}
	return fetched.body, nil
}

// EachListMembersPage は GetListMembers を pagination_token で自動辿りする iterator (M33)。
//
// X API ドキュメント上は `next_token` 表記が混在するが、本実装では他 paged endpoint と
// 統一して **pagination_token** を送信する (M33 D-2、テスト pin)。
func (c *Client) EachListMembersPage(
	ctx context.Context,
	listID string,
	fn func(*UsersResponse) error,
	opts ...ListPagedOption,
) error {
	if listID == "" {
		return fmt.Errorf("xapi: EachListMembersPage: listID must be non-empty")
	}
	cfg := newListPagedConfig(opts)
	maxPages := cfg.maxPages
	if maxPages <= 0 {
		maxPages = listPagedDefaultMaxPages
	}
	path := "/2/lists/" + url.PathEscape(listID) + "/" + listPagedSuffixMembers
	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fetched, err := c.fetchListMembersPage(ctx, path, &cfg, "EachListMembersPage")
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
		wait := c.computeInterPageWait(fetched.rateLimit, listPagedRateLimitThreshold)
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
		cfg.paginationToken = next
	}
	return nil
}

// =============================================================================
// GetOwnedLists / GetListMemberships / GetFollowedLists + Each*Page (paged, List 配列)
// =============================================================================

// GetOwnedLists は X API v2 `GET /2/users/:id/owned_lists` を呼び出す (M33)。
func (c *Client) GetOwnedLists(ctx context.Context, userID string, opts ...ListPagedOption) (*ListsResponse, error) {
	return c.getUserLists(ctx, "GetOwnedLists", userListsSuffixOwned, userID, opts)
}

// GetListMemberships は X API v2 `GET /2/users/:id/list_memberships` を呼び出す (M33)。
func (c *Client) GetListMemberships(ctx context.Context, userID string, opts ...ListPagedOption) (*ListsResponse, error) {
	return c.getUserLists(ctx, "GetListMemberships", userListsSuffixMemberships, userID, opts)
}

// GetFollowedLists は X API v2 `GET /2/users/:id/followed_lists` を呼び出す (M33)。
func (c *Client) GetFollowedLists(ctx context.Context, userID string, opts ...ListPagedOption) (*ListsResponse, error) {
	return c.getUserLists(ctx, "GetFollowedLists", userListsSuffixFollowed, userID, opts)
}

// EachOwnedListsPage は GetOwnedLists を pagination_token で自動辿りする iterator (M33)。
func (c *Client) EachOwnedListsPage(
	ctx context.Context, userID string, fn func(*ListsResponse) error, opts ...ListPagedOption,
) error {
	return c.eachUserListsPage(ctx, "EachOwnedListsPage", userListsSuffixOwned, userID, fn, opts)
}

// EachListMembershipsPage は GetListMemberships を pagination_token で自動辿りする iterator (M33)。
func (c *Client) EachListMembershipsPage(
	ctx context.Context, userID string, fn func(*ListsResponse) error, opts ...ListPagedOption,
) error {
	return c.eachUserListsPage(ctx, "EachListMembershipsPage", userListsSuffixMemberships, userID, fn, opts)
}

// EachFollowedListsPage は GetFollowedLists を pagination_token で自動辿りする iterator (M33)。
func (c *Client) EachFollowedListsPage(
	ctx context.Context, userID string, fn func(*ListsResponse) error, opts ...ListPagedOption,
) error {
	return c.eachUserListsPage(ctx, "EachFollowedListsPage", userListsSuffixFollowed, userID, fn, opts)
}

// getUserLists は owned/memberships/followed 3 endpoint の DRY 共通実装。
func (c *Client) getUserLists(
	ctx context.Context, funcName, suffix, userID string, opts []ListPagedOption,
) (*ListsResponse, error) {
	if userID == "" {
		return nil, fmt.Errorf("xapi: %s: userID must be non-empty", funcName)
	}
	cfg := newListPagedConfig(opts)
	path := "/2/users/" + url.PathEscape(userID) + "/" + suffix
	fetched, err := c.fetchListsPage(ctx, path, &cfg, funcName)
	if err != nil {
		return nil, err
	}
	return fetched.body, nil
}

// eachUserListsPage は owned/memberships/followed 3 iterator の DRY 共通実装。
//
// 終了条件 (いずれか早い方):
//   - meta.next_token が空文字列 (= 最終ページ)
//   - 取得済みページ数が WithListPagedMaxPages (default 50) に到達
//   - fn が non-nil error を返す
//   - ctx が cancel された
//
// ページ間 sleep は computeInterPageWait + listPagedRateLimitThreshold (= 2) を再利用。
func (c *Client) eachUserListsPage(
	ctx context.Context, funcName, suffix, userID string,
	fn func(*ListsResponse) error, opts []ListPagedOption,
) error {
	if userID == "" {
		return fmt.Errorf("xapi: %s: userID must be non-empty", funcName)
	}
	cfg := newListPagedConfig(opts)
	maxPages := cfg.maxPages
	if maxPages <= 0 {
		maxPages = listPagedDefaultMaxPages
	}
	path := "/2/users/" + url.PathEscape(userID) + "/" + suffix
	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fetched, err := c.fetchListsPage(ctx, path, &cfg, funcName)
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
		wait := c.computeInterPageWait(fetched.rateLimit, listPagedRateLimitThreshold)
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
		cfg.paginationToken = next
	}
	return nil
}

// =============================================================================
// 内部 fetch ヘルパ (3 種類、レスポンス型ごとに分離、M33 D-3)
// =============================================================================

// listsPageFetched は List 配列ページの取得結果。
type listsPageFetched struct {
	body      *ListsResponse
	rateLimit RateLimitInfo
}

// fetchListsPage は List 配列ページ (owned/memberships/followed) の single page を取得する。
func (c *Client) fetchListsPage(
	ctx context.Context, path string, cfg *listPagedConfig, funcName string,
) (*listsPageFetched, error) {
	endpoint := buildListPagedURL(c.BaseURL(), path, cfg)
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
	out := &ListsResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode %s response: %w", funcName, err)
	}
	return &listsPageFetched{body: out, rateLimit: resp.RateLimit}, nil
}

// listTweetsPageFetched は List Tweets ページの取得結果。
type listTweetsPageFetched struct {
	body      *ListTweetsResponse
	rateLimit RateLimitInfo
}

// fetchListTweetsPage は List Tweets single page を取得する。
func (c *Client) fetchListTweetsPage(
	ctx context.Context, path string, cfg *listPagedConfig, funcName string,
) (*listTweetsPageFetched, error) {
	endpoint := buildListPagedURL(c.BaseURL(), path, cfg)
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
	out := &ListTweetsResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode %s response: %w", funcName, err)
	}
	return &listTweetsPageFetched{body: out, rateLimit: resp.RateLimit}, nil
}

// listMembersPageFetched は List Members ページの取得結果 (User 配列、UsersResponse 再利用 M33 D-4)。
type listMembersPageFetched struct {
	body      *UsersResponse
	rateLimit RateLimitInfo
}

// fetchListMembersPage は List Members single page を取得する。
func (c *Client) fetchListMembersPage(
	ctx context.Context, path string, cfg *listPagedConfig, funcName string,
) (*listMembersPageFetched, error) {
	endpoint := buildListPagedURL(c.BaseURL(), path, cfg)
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
	return &listMembersPageFetched{body: out, rateLimit: resp.RateLimit}, nil
}

// =============================================================================
// URL ビルダ (2 種類)
// =============================================================================

// buildListLookupURL は GetList / GetPinnedLists の完全 URL を組み立てる。
// クエリパラメータは list.fields / expansions / user.fields のみ。
func buildListLookupURL(baseURL, path string, cfg *listLookupConfig) string {
	values := url.Values{}
	if len(cfg.listFields) > 0 {
		values.Set("list.fields", strings.Join(cfg.listFields, ","))
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

// buildListPagedURL は paged 5 endpoint の完全 URL を組み立てる。
// max_results / pagination_token + 5 種類の fields/expansions を全て載せる。
func buildListPagedURL(baseURL, path string, cfg *listPagedConfig) string {
	values := url.Values{}
	if cfg.maxResults > 0 {
		values.Set("max_results", strconv.Itoa(cfg.maxResults))
	}
	if cfg.paginationToken != "" {
		values.Set("pagination_token", cfg.paginationToken)
	}
	if len(cfg.listFields) > 0 {
		values.Set("list.fields", strings.Join(cfg.listFields, ","))
	}
	if len(cfg.userFields) > 0 {
		values.Set("user.fields", strings.Join(cfg.userFields, ","))
	}
	if len(cfg.tweetFields) > 0 {
		values.Set("tweet.fields", strings.Join(cfg.tweetFields, ","))
	}
	if len(cfg.expansions) > 0 {
		values.Set("expansions", strings.Join(cfg.expansions, ","))
	}
	if len(cfg.mediaFields) > 0 {
		values.Set("media.fields", strings.Join(cfg.mediaFields, ","))
	}
	if q := values.Encode(); q != "" {
		return baseURL + path + "?" + q
	}
	return baseURL + path
}

// =============================================================================
// config builders
// =============================================================================

func newListLookupConfig(opts []ListLookupOption) listLookupConfig {
	cfg := listLookupConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func newListPagedConfig(opts []ListPagedOption) listPagedConfig {
	cfg := listPagedConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
