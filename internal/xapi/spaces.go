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

// spaces.go は X API v2 の Spaces 系 5 エンドポイントをラップする (M34)。
//
//   - GET /2/spaces/:id                       → GetSpace                  (lookup, no iterator)
//   - GET /2/spaces                           → GetSpaces (?ids=, 1..100, no iterator)
//   - GET /2/spaces/search                    → SearchSpaces              (non-paginated, X API 仕様で next_token 不在)
//   - GET /2/spaces/by/creator_ids            → GetSpacesByCreatorIDs (?user_ids=, 1..100, no iterator)
//   - GET /2/spaces/:id/tweets                → GetSpaceTweets / EachSpaceTweetsPage
//
// 設計判断 (詳細は plans/x-m34-spaces-trends.md):
//   - Option 型を **3 種類に分離** (SpaceLookupOption / SpaceSearchOption / SpaceTweetsOption) — M34 D-1
//   - SearchSpaces はページネーション非対応 (docs.x.com で検証済) — M34 D-2
//   - aggregator generics 化は見送り (M33 D-2 継続) — M34 D-8
//   - パッケージ doc は既存 oauth1.go に集約のため本ファイルには書かない — M34 D-9

const (
	// spaceTweetsDefaultMaxPages は EachSpaceTweetsPage の未指定時の上限ページ数。
	spaceTweetsDefaultMaxPages = 50
	// spaceTweetsRateLimitThreshold は Each*Page が rate-limit aware sleep を発動する閾値。
	spaceTweetsRateLimitThreshold = 2
	// spaceLookupBatchMaxIDs は GetSpaces / GetSpacesByCreatorIDs の per-call 件数上限 (X API 仕様)。
	spaceLookupBatchMaxIDs = 100
)

// =============================================================================
// DTOs
// =============================================================================

// Space は X API v2 の Space オブジェクトを表す DTO である (M34)。
//
// 必須フィールド (id) は X API がデフォルト返却する。それ以外 (State / Title / HostIDs ...) は
// `space.fields` クエリパラメータで明示的に要求した場合のみ返却される。
type Space struct {
	ID               string   `json:"id"`
	State            string   `json:"state,omitempty"`
	Title            string   `json:"title,omitempty"`
	HostIDs          []string `json:"host_ids,omitempty"`
	CreatorID        string   `json:"creator_id,omitempty"`
	CreatedAt        string   `json:"created_at,omitempty"`
	StartedAt        string   `json:"started_at,omitempty"`
	EndedAt          string   `json:"ended_at,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
	ScheduledStart   string   `json:"scheduled_start,omitempty"`
	InvitedUserIDs   []string `json:"invited_user_ids,omitempty"`
	SpeakerIDs       []string `json:"speaker_ids,omitempty"`
	TopicIDs         []string `json:"topic_ids,omitempty"`
	Lang             string   `json:"lang,omitempty"`
	IsTicketed       bool     `json:"is_ticketed,omitempty"`
	ParticipantCount int      `json:"participant_count,omitempty"`
	SubscriberCount  int      `json:"subscriber_count,omitempty"`
}

// SpaceResponse は GetSpace の単一 Space レスポンス本体である (M34)。
type SpaceResponse struct {
	Data     *Space   `json:"data,omitempty"`
	Includes Includes `json:"includes,omitempty"`
}

// SpacesResponse は GetSpaces / SearchSpaces / GetSpacesByCreatorIDs が返す配列レスポンス本体である (M34)。
type SpacesResponse struct {
	Data     []Space  `json:"data,omitempty"`
	Includes Includes `json:"includes,omitempty"`
	Meta     Meta     `json:"meta,omitempty"`
}

// SpaceTweetsResponse は GetSpaceTweets が返す Tweet 配列レスポンス本体である (M34)。
//
// 既存 TimelineResponse / ListTweetsResponse 等と同形だが、責務分離 (どの endpoint のレスポンスか)
// を明示するため別型として定義する。
type SpaceTweetsResponse struct {
	Data     []Tweet  `json:"data,omitempty"`
	Includes Includes `json:"includes,omitempty"`
	Meta     Meta     `json:"meta,omitempty"`
}

// =============================================================================
// Option types — 3 種類に分離 (M34 D-1)
// =============================================================================

// SpaceLookupOption は GetSpace / GetSpaces / GetSpacesByCreatorIDs 用の関数オプションである。
// max_results / state / pagination は持たない (誤用防止)。
type SpaceLookupOption func(*spaceLookupConfig)

type spaceLookupConfig struct {
	spaceFields []string
	expansions  []string
	userFields  []string
	topicFields []string
}

// WithSpaceLookupSpaceFields は X API の space.fields を設定する。空引数は no-op。
func WithSpaceLookupSpaceFields(fields ...string) SpaceLookupOption {
	return func(c *spaceLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.spaceFields = append([]string(nil), fields...)
	}
}

// WithSpaceLookupExpansions は X API の expansions を設定する。空引数は no-op。
func WithSpaceLookupExpansions(exp ...string) SpaceLookupOption {
	return func(c *spaceLookupConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithSpaceLookupUserFields は X API の user.fields を設定する。空引数は no-op。
func WithSpaceLookupUserFields(fields ...string) SpaceLookupOption {
	return func(c *spaceLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithSpaceLookupTopicFields は X API の topic.fields を設定する。空引数は no-op。
func WithSpaceLookupTopicFields(fields ...string) SpaceLookupOption {
	return func(c *spaceLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.topicFields = append([]string(nil), fields...)
	}
}

// SpaceSearchOption は SearchSpaces 用の関数オプションである (M34 D-1)。
// X API は本 endpoint で next_token を返さないため pagination は持たない (M34 D-2)。
// 将来 X API が next_token サポートを追加した場合に備え、本型は独立して定義してある。
type SpaceSearchOption func(*spaceSearchConfig)

type spaceSearchConfig struct {
	maxResults  int // 0 は no-op (X API デフォルト 100 に任せる)
	state       string
	spaceFields []string
	expansions  []string
	userFields  []string
	topicFields []string
}

// WithSpaceSearchMaxResults は X API の max_results を設定する (1..100、default 100)。
// 0 は no-op。CLI 層で範囲チェックを担う。
func WithSpaceSearchMaxResults(n int) SpaceSearchOption {
	return func(c *spaceSearchConfig) { c.maxResults = n }
}

// WithSpaceSearchState は X API の state を設定する ("live" / "scheduled" / "all")。空文字は no-op。
func WithSpaceSearchState(state string) SpaceSearchOption {
	return func(c *spaceSearchConfig) {
		if state == "" {
			return
		}
		c.state = state
	}
}

// WithSpaceSearchSpaceFields は SearchSpaces の space.fields を設定する。空引数は no-op。
func WithSpaceSearchSpaceFields(fields ...string) SpaceSearchOption {
	return func(c *spaceSearchConfig) {
		if len(fields) == 0 {
			return
		}
		c.spaceFields = append([]string(nil), fields...)
	}
}

// WithSpaceSearchExpansions は SearchSpaces の expansions を設定する。空引数は no-op。
func WithSpaceSearchExpansions(exp ...string) SpaceSearchOption {
	return func(c *spaceSearchConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithSpaceSearchUserFields は SearchSpaces の user.fields を設定する。空引数は no-op。
func WithSpaceSearchUserFields(fields ...string) SpaceSearchOption {
	return func(c *spaceSearchConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithSpaceSearchTopicFields は SearchSpaces の topic.fields を設定する。空引数は no-op。
func WithSpaceSearchTopicFields(fields ...string) SpaceSearchOption {
	return func(c *spaceSearchConfig) {
		if len(fields) == 0 {
			return
		}
		c.topicFields = append([]string(nil), fields...)
	}
}

// SpaceTweetsOption は GetSpaceTweets / EachSpaceTweetsPage 用の関数オプションである (M34)。
type SpaceTweetsOption func(*spaceTweetsConfig)

type spaceTweetsConfig struct {
	maxResults      int
	paginationToken string
	tweetFields     []string
	userFields      []string
	expansions      []string
	mediaFields     []string
	maxPages        int // Each 専用 (0 はデフォルト 50)
}

// WithSpaceTweetsMaxResults は X API の max_results を設定する (1..100)。0 は no-op。
func WithSpaceTweetsMaxResults(n int) SpaceTweetsOption {
	return func(c *spaceTweetsConfig) { c.maxResults = n }
}

// WithSpaceTweetsPaginationToken は X API の pagination_token を設定する。
// EachSpaceTweetsPage は内部で都度上書きするため、初回ページのみ有効。
func WithSpaceTweetsPaginationToken(token string) SpaceTweetsOption {
	return func(c *spaceTweetsConfig) { c.paginationToken = token }
}

// WithSpaceTweetsTweetFields は X API の tweet.fields を設定する。空引数は no-op。
func WithSpaceTweetsTweetFields(fields ...string) SpaceTweetsOption {
	return func(c *spaceTweetsConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// WithSpaceTweetsUserFields は X API の user.fields を設定する。空引数は no-op。
func WithSpaceTweetsUserFields(fields ...string) SpaceTweetsOption {
	return func(c *spaceTweetsConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithSpaceTweetsExpansions は X API の expansions を設定する。空引数は no-op。
func WithSpaceTweetsExpansions(exp ...string) SpaceTweetsOption {
	return func(c *spaceTweetsConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithSpaceTweetsMediaFields は X API の media.fields を設定する。空引数は no-op。
func WithSpaceTweetsMediaFields(fields ...string) SpaceTweetsOption {
	return func(c *spaceTweetsConfig) {
		if len(fields) == 0 {
			return
		}
		c.mediaFields = append([]string(nil), fields...)
	}
}

// WithSpaceTweetsMaxPages は EachSpaceTweetsPage の上限ページ数を設定する (default 50)。
func WithSpaceTweetsMaxPages(n int) SpaceTweetsOption {
	return func(c *spaceTweetsConfig) { c.maxPages = n }
}

// =============================================================================
// GetSpace / GetSpaces / GetSpacesByCreatorIDs (Lookup endpoints)
// =============================================================================

// GetSpace は X API v2 `GET /2/spaces/:id` を呼び出し、単一 Space を返す (M34)。
func (c *Client) GetSpace(ctx context.Context, spaceID string, opts ...SpaceLookupOption) (*SpaceResponse, error) {
	if spaceID == "" {
		return nil, fmt.Errorf("xapi: GetSpace: spaceID must be non-empty")
	}
	cfg := newSpaceLookupConfig(opts)
	endpoint := buildSpaceLookupURL(c.BaseURL(), "/2/spaces/"+url.PathEscape(spaceID), "", nil, &cfg)
	body, err := c.fetchJSON(ctx, endpoint, "GetSpace")
	if err != nil {
		return nil, err
	}
	out := &SpaceResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetSpace response: %w", err)
	}
	return out, nil
}

// GetSpaces は X API v2 `GET /2/spaces?ids=...` を呼び出し、複数 Space をバッチ取得する (M34)。
//
// spaceIDs は 1..100 件 (X API 仕様、`spaceLookupBatchMaxIDs`)。範囲外は ErrInvalidArgument ではなく
// 通常 error を返す (CLI 層が ErrInvalidArgument に wrap する)。
func (c *Client) GetSpaces(ctx context.Context, spaceIDs []string, opts ...SpaceLookupOption) (*SpacesResponse, error) {
	if len(spaceIDs) == 0 {
		return nil, fmt.Errorf("xapi: GetSpaces: spaceIDs must be non-empty")
	}
	if len(spaceIDs) > spaceLookupBatchMaxIDs {
		return nil, fmt.Errorf("xapi: GetSpaces: spaceIDs exceeds limit %d (got %d)", spaceLookupBatchMaxIDs, len(spaceIDs))
	}
	cfg := newSpaceLookupConfig(opts)
	endpoint := buildSpaceLookupURL(c.BaseURL(), "/2/spaces", "ids", spaceIDs, &cfg)
	body, err := c.fetchJSON(ctx, endpoint, "GetSpaces")
	if err != nil {
		return nil, err
	}
	out := &SpacesResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetSpaces response: %w", err)
	}
	return out, nil
}

// GetSpacesByCreatorIDs は X API v2 `GET /2/spaces/by/creator_ids?user_ids=...` を呼び出す (M34)。
//
// creatorIDs は 1..100 件 (X API 仕様)。
func (c *Client) GetSpacesByCreatorIDs(ctx context.Context, creatorIDs []string, opts ...SpaceLookupOption) (*SpacesResponse, error) {
	if len(creatorIDs) == 0 {
		return nil, fmt.Errorf("xapi: GetSpacesByCreatorIDs: creatorIDs must be non-empty")
	}
	if len(creatorIDs) > spaceLookupBatchMaxIDs {
		return nil, fmt.Errorf("xapi: GetSpacesByCreatorIDs: creatorIDs exceeds limit %d (got %d)", spaceLookupBatchMaxIDs, len(creatorIDs))
	}
	cfg := newSpaceLookupConfig(opts)
	endpoint := buildSpaceLookupURL(c.BaseURL(), "/2/spaces/by/creator_ids", "user_ids", creatorIDs, &cfg)
	body, err := c.fetchJSON(ctx, endpoint, "GetSpacesByCreatorIDs")
	if err != nil {
		return nil, err
	}
	out := &SpacesResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetSpacesByCreatorIDs response: %w", err)
	}
	return out, nil
}

// =============================================================================
// SearchSpaces (non-paginated, X API 仕様で next_token 不在 — M34 D-2)
// =============================================================================

// SearchSpaces は X API v2 `GET /2/spaces/search?query=...` を呼び出す (M34)。
//
// 検証済 (2026-05-15): X API は本 endpoint で `meta.next_token` を返さない。
// 将来 X API が next_token サポートを追加した場合に備え、Option 型は独立して定義してある (M34 D-2)。
func (c *Client) SearchSpaces(ctx context.Context, query string, opts ...SpaceSearchOption) (*SpacesResponse, error) {
	if query == "" {
		return nil, fmt.Errorf("xapi: SearchSpaces: query must be non-empty")
	}
	cfg := newSpaceSearchConfig(opts)
	endpoint := buildSpaceSearchURL(c.BaseURL(), query, &cfg)
	body, err := c.fetchJSON(ctx, endpoint, "SearchSpaces")
	if err != nil {
		return nil, err
	}
	out := &SpacesResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode SearchSpaces response: %w", err)
	}
	return out, nil
}

// =============================================================================
// GetSpaceTweets / EachSpaceTweetsPage (paged, Tweet 配列)
// =============================================================================

// GetSpaceTweets は X API v2 `GET /2/spaces/:id/tweets` を呼び出す (M34)。
//
// pagination_token で次ページを取得する。max_results は X API 仕様で 1..100。
func (c *Client) GetSpaceTweets(ctx context.Context, spaceID string, opts ...SpaceTweetsOption) (*SpaceTweetsResponse, error) {
	if spaceID == "" {
		return nil, fmt.Errorf("xapi: GetSpaceTweets: spaceID must be non-empty")
	}
	cfg := newSpaceTweetsConfig(opts)
	fetched, err := c.fetchSpaceTweetsPage(
		ctx,
		"/2/spaces/"+url.PathEscape(spaceID)+"/tweets",
		&cfg,
		"GetSpaceTweets",
	)
	if err != nil {
		return nil, err
	}
	return fetched.body, nil
}

// EachSpaceTweetsPage は GetSpaceTweets を pagination_token で自動辿りする iterator (M34)。
func (c *Client) EachSpaceTweetsPage(
	ctx context.Context,
	spaceID string,
	fn func(*SpaceTweetsResponse) error,
	opts ...SpaceTweetsOption,
) error {
	if spaceID == "" {
		return fmt.Errorf("xapi: EachSpaceTweetsPage: spaceID must be non-empty")
	}
	cfg := newSpaceTweetsConfig(opts)
	maxPages := cfg.maxPages
	if maxPages <= 0 {
		maxPages = spaceTweetsDefaultMaxPages
	}
	path := "/2/spaces/" + url.PathEscape(spaceID) + "/tweets"
	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fetched, err := c.fetchSpaceTweetsPage(ctx, path, &cfg, "EachSpaceTweetsPage")
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
		wait := c.computeInterPageWait(fetched.rateLimit, spaceTweetsRateLimitThreshold)
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
		cfg.paginationToken = next
	}
	return nil
}

// =============================================================================
// 内部 fetch ヘルパ
// =============================================================================

// spaceTweetsPageFetched は Space Tweets ページの取得結果。
type spaceTweetsPageFetched struct {
	body      *SpaceTweetsResponse
	rateLimit RateLimitInfo
}

// fetchSpaceTweetsPage は Space Tweets single page を取得する DRY 内部関数。
func (c *Client) fetchSpaceTweetsPage(
	ctx context.Context, path string, cfg *spaceTweetsConfig, funcName string,
) (*spaceTweetsPageFetched, error) {
	endpoint := buildSpaceTweetsURL(c.BaseURL(), path, cfg)
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
	out := &SpaceTweetsResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode %s response: %w", funcName, err)
	}
	return &spaceTweetsPageFetched{body: out, rateLimit: resp.RateLimit}, nil
}

// =============================================================================
// URL ビルダ
// =============================================================================

// buildSpaceLookupURL は GetSpace / GetSpaces / GetSpacesByCreatorIDs の完全 URL を組み立てる。
//
// batchKey は "ids" / "user_ids" / "" (single lookup の場合は空文字)、batchValues は batch 用の配列。
func buildSpaceLookupURL(baseURL, path, batchKey string, batchValues []string, cfg *spaceLookupConfig) string {
	values := url.Values{}
	if batchKey != "" && len(batchValues) > 0 {
		values.Set(batchKey, strings.Join(batchValues, ","))
	}
	if len(cfg.spaceFields) > 0 {
		values.Set("space.fields", strings.Join(cfg.spaceFields, ","))
	}
	if len(cfg.expansions) > 0 {
		values.Set("expansions", strings.Join(cfg.expansions, ","))
	}
	if len(cfg.userFields) > 0 {
		values.Set("user.fields", strings.Join(cfg.userFields, ","))
	}
	if len(cfg.topicFields) > 0 {
		values.Set("topic.fields", strings.Join(cfg.topicFields, ","))
	}
	if q := values.Encode(); q != "" {
		return baseURL + path + "?" + q
	}
	return baseURL + path
}

// buildSpaceSearchURL は SearchSpaces の完全 URL を組み立てる。
// query は url.Values.Set で querystring に詰める (path 埋め込みではない、M34 D-14)。
func buildSpaceSearchURL(baseURL, query string, cfg *spaceSearchConfig) string {
	values := url.Values{}
	values.Set("query", query)
	if cfg.maxResults > 0 {
		values.Set("max_results", strconv.Itoa(cfg.maxResults))
	}
	if cfg.state != "" {
		values.Set("state", cfg.state)
	}
	if len(cfg.spaceFields) > 0 {
		values.Set("space.fields", strings.Join(cfg.spaceFields, ","))
	}
	if len(cfg.expansions) > 0 {
		values.Set("expansions", strings.Join(cfg.expansions, ","))
	}
	if len(cfg.userFields) > 0 {
		values.Set("user.fields", strings.Join(cfg.userFields, ","))
	}
	if len(cfg.topicFields) > 0 {
		values.Set("topic.fields", strings.Join(cfg.topicFields, ","))
	}
	return baseURL + "/2/spaces/search?" + values.Encode()
}

// buildSpaceTweetsURL は GetSpaceTweets / EachSpaceTweetsPage の完全 URL を組み立てる。
func buildSpaceTweetsURL(baseURL, path string, cfg *spaceTweetsConfig) string {
	values := url.Values{}
	if cfg.maxResults > 0 {
		values.Set("max_results", strconv.Itoa(cfg.maxResults))
	}
	if cfg.paginationToken != "" {
		values.Set("pagination_token", cfg.paginationToken)
	}
	if len(cfg.tweetFields) > 0 {
		values.Set("tweet.fields", strings.Join(cfg.tweetFields, ","))
	}
	if len(cfg.userFields) > 0 {
		values.Set("user.fields", strings.Join(cfg.userFields, ","))
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

func newSpaceLookupConfig(opts []SpaceLookupOption) spaceLookupConfig {
	cfg := spaceLookupConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func newSpaceSearchConfig(opts []SpaceSearchOption) spaceSearchConfig {
	cfg := spaceSearchConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func newSpaceTweetsConfig(opts []SpaceTweetsOption) spaceTweetsConfig {
	cfg := spaceTweetsConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
