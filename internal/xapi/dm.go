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

// dm.go は X API v2 の Direct Messages 系 4 エンドポイントをラップする (M35)。
//
//   - GET /2/dm_events                                          → GetDMEvents / EachDMEventsPage
//   - GET /2/dm_events/:event_id                                → GetDMEvent (lookup, no iterator)
//   - GET /2/dm_conversations/:dm_conversation_id/dm_events     → GetDMConversation / EachDMConversationPage
//   - GET /2/dm_conversations/with/:participant_id/dm_events    → GetDMWithUser / EachDMWithUserPage
//
// 設計判断 (詳細は plans/x-m35-dm-read.md):
//   - Option 型を **2 種類に分離** (DMLookupOption / DMPagedOption) — M35 D-1
//   - paged 3 endpoint はレスポンス型 (*DMEventsResponse) が完全一致するため、
//     内部 fetchDMEventsPage + eachDMEventsPaged で 1 fetch + 1 each に DRY 化 — M35 D-2
//   - event_types は CSV クエリ (`event_types=MessageCreate,ParticipantsJoin`) — M35 D-3
//   - Tier 制約 (Basic: 1 回/24h, Pro 推奨) と直近 30 日制限は docs / CLI Long で明示 — M35 D-13
//   - パッケージ doc は他ファイル (oauth1.go) に集約済のため、本ファイルには書かない — M35 D-11

const (
	// dmPagedDefaultMaxPages は EachDM*Page の未指定時の上限ページ数。
	dmPagedDefaultMaxPages = 50
	// dmPagedRateLimitThreshold は Each*Page が rate-limit aware sleep を発動する閾値。
	dmPagedRateLimitThreshold = 2
)

// =============================================================================
// DTOs
// =============================================================================

// DMAttachments は DM の添付情報 (media / card) を表す DTO である (M35)。
type DMAttachments struct {
	MediaKeys []string `json:"media_keys,omitempty"`
	CardIDs   []string `json:"card_ids,omitempty"`
}

// DMEvent は X API v2 の DM イベントを表す DTO である (M35)。
//
// event_type ごとに含まれるフィールドが異なるため全て omitempty。
//
//   - MessageCreate: ID, EventType, Text, SenderID, DMConversationID, CreatedAt, Attachments, ReferencedTweets, Entities
//   - ParticipantsJoin / ParticipantsLeave: ID, EventType, DMConversationID, CreatedAt, ParticipantIDs
type DMEvent struct {
	ID               string            `json:"id"`
	EventType        string            `json:"event_type,omitempty"`
	Text             string            `json:"text,omitempty"`
	SenderID         string            `json:"sender_id,omitempty"`
	DMConversationID string            `json:"dm_conversation_id,omitempty"`
	CreatedAt        string            `json:"created_at,omitempty"`
	Attachments      *DMAttachments    `json:"attachments,omitempty"`
	ReferencedTweets []ReferencedTweet `json:"referenced_tweets,omitempty"`
	ParticipantIDs   []string          `json:"participant_ids,omitempty"`
	Entities         *TweetEntities    `json:"entities,omitempty"`
}

// DMEventResponse は GetDMEvent (単一イベント lookup) のレスポンス本体である (M35)。
type DMEventResponse struct {
	Data     *DMEvent `json:"data,omitempty"`
	Includes Includes `json:"includes,omitempty"`
}

// DMEventsResponse は paged 3 endpoint (GetDMEvents / GetDMConversation / GetDMWithUser)
// が返す配列レスポンス本体である (M35)。
type DMEventsResponse struct {
	Data     []DMEvent `json:"data,omitempty"`
	Includes Includes  `json:"includes,omitempty"`
	Meta     Meta      `json:"meta,omitempty"`
}

// =============================================================================
// Option types — 2 種類に分離 (M35 D-1)
// =============================================================================

// DMLookupOption は GetDMEvent 用の関数オプションである。
// max_results / event_types / pagination は持たない (誤用防止)。
type DMLookupOption func(*dmLookupConfig)

type dmLookupConfig struct {
	dmEventFields []string
	expansions    []string
	userFields    []string
	tweetFields   []string
	mediaFields   []string
}

// WithDMLookupDMEventFields は X API の dm_event.fields を設定する。空引数は no-op。
func WithDMLookupDMEventFields(fields ...string) DMLookupOption {
	return func(c *dmLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.dmEventFields = append([]string(nil), fields...)
	}
}

// WithDMLookupExpansions は X API の expansions を設定する。空引数は no-op。
func WithDMLookupExpansions(exp ...string) DMLookupOption {
	return func(c *dmLookupConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithDMLookupUserFields は X API の user.fields を設定する。空引数は no-op。
func WithDMLookupUserFields(fields ...string) DMLookupOption {
	return func(c *dmLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithDMLookupTweetFields は X API の tweet.fields を設定する。空引数は no-op。
func WithDMLookupTweetFields(fields ...string) DMLookupOption {
	return func(c *dmLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// WithDMLookupMediaFields は X API の media.fields を設定する。空引数は no-op。
func WithDMLookupMediaFields(fields ...string) DMLookupOption {
	return func(c *dmLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.mediaFields = append([]string(nil), fields...)
	}
}

// DMPagedOption は paged 3 endpoint (events list / conversation / with user) で
// 共通の関数オプションである。
type DMPagedOption func(*dmPagedConfig)

type dmPagedConfig struct {
	maxResults      int // 0 は no-op (X API デフォルト 100)
	paginationToken string
	eventTypes      []string // CSV で送信 (M35 D-3)
	dmEventFields   []string
	expansions      []string
	userFields      []string
	tweetFields     []string
	mediaFields     []string

	maxPages int // Each 専用 (0 はデフォルト 50)
}

// WithDMPagedMaxResults は X API の max_results を設定する (1..100、default 100)。
// 0 は no-op (X API デフォルトに任せる)。
func WithDMPagedMaxResults(n int) DMPagedOption {
	return func(c *dmPagedConfig) { c.maxResults = n }
}

// WithDMPagedPaginationToken は X API の pagination_token を設定する。
// EachDM*Page は内部で都度上書きするため、初回ページのみ有効。
func WithDMPagedPaginationToken(token string) DMPagedOption {
	return func(c *dmPagedConfig) { c.paginationToken = token }
}

// WithDMPagedEventTypes は X API の event_types を CSV で設定する (M35 D-3)。
// 許可値は MessageCreate / ParticipantsJoin / ParticipantsLeave。
// 空引数は no-op (X API デフォルトの 3 種全部を取得)。
func WithDMPagedEventTypes(types ...string) DMPagedOption {
	return func(c *dmPagedConfig) {
		if len(types) == 0 {
			return
		}
		c.eventTypes = append([]string(nil), types...)
	}
}

// WithDMPagedDMEventFields は X API の dm_event.fields を設定する。空引数は no-op。
func WithDMPagedDMEventFields(fields ...string) DMPagedOption {
	return func(c *dmPagedConfig) {
		if len(fields) == 0 {
			return
		}
		c.dmEventFields = append([]string(nil), fields...)
	}
}

// WithDMPagedExpansions は X API の expansions を設定する。空引数は no-op。
func WithDMPagedExpansions(exp ...string) DMPagedOption {
	return func(c *dmPagedConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithDMPagedUserFields は X API の user.fields を設定する。空引数は no-op。
func WithDMPagedUserFields(fields ...string) DMPagedOption {
	return func(c *dmPagedConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithDMPagedTweetFields は X API の tweet.fields を設定する。空引数は no-op。
func WithDMPagedTweetFields(fields ...string) DMPagedOption {
	return func(c *dmPagedConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// WithDMPagedMediaFields は X API の media.fields を設定する。空引数は no-op。
func WithDMPagedMediaFields(fields ...string) DMPagedOption {
	return func(c *dmPagedConfig) {
		if len(fields) == 0 {
			return
		}
		c.mediaFields = append([]string(nil), fields...)
	}
}

// WithDMPagedMaxPages は EachDM*Page の上限ページ数を設定する (default 50)。
func WithDMPagedMaxPages(n int) DMPagedOption {
	return func(c *dmPagedConfig) { c.maxPages = n }
}

// =============================================================================
// GetDMEvent (Lookup, single event)
// =============================================================================

// GetDMEvent は X API v2 `GET /2/dm_events/:event_id` を呼び出し、単一 DM イベントを返す (M35)。
func (c *Client) GetDMEvent(ctx context.Context, eventID string, opts ...DMLookupOption) (*DMEventResponse, error) {
	if eventID == "" {
		return nil, fmt.Errorf("xapi: GetDMEvent: eventID must be non-empty")
	}
	cfg := newDMLookupConfig(opts)
	endpoint := buildDMLookupURL(c.BaseURL(), "/2/dm_events/"+url.PathEscape(eventID), &cfg)
	body, err := c.fetchJSON(ctx, endpoint, "GetDMEvent")
	if err != nil {
		return nil, err
	}
	out := &DMEventResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetDMEvent response: %w", err)
	}
	return out, nil
}

// =============================================================================
// GetDMEvents / EachDMEventsPage (paged, all DM events)
// =============================================================================

// GetDMEvents は X API v2 `GET /2/dm_events` を呼び出し、認証ユーザーの DM イベント単一ページを返す (M35)。
//
// 取得可能なのは直近 30 日以内のイベントのみ (X API 仕様)。Basic tier ではレート制限が
// 厳しい (1 回/24h 程度) ため、実用は Pro tier 以上を推奨する。
func (c *Client) GetDMEvents(ctx context.Context, opts ...DMPagedOption) (*DMEventsResponse, error) {
	cfg := newDMPagedConfig(opts)
	fetched, err := c.fetchDMEventsPage(ctx, "/2/dm_events", &cfg, "GetDMEvents")
	if err != nil {
		return nil, err
	}
	return fetched.body, nil
}

// EachDMEventsPage は GetDMEvents を pagination_token で自動辿りする iterator (M35)。
func (c *Client) EachDMEventsPage(
	ctx context.Context,
	fn func(*DMEventsResponse) error,
	opts ...DMPagedOption,
) error {
	cfg := newDMPagedConfig(opts)
	return c.eachDMEventsPaged(ctx, "EachDMEventsPage", "/2/dm_events", &cfg, fn)
}

// =============================================================================
// GetDMConversation / EachDMConversationPage (paged, by conversation ID)
// =============================================================================

// GetDMConversation は X API v2 `GET /2/dm_conversations/:dm_conversation_id/dm_events` を呼び出す (M35)。
func (c *Client) GetDMConversation(
	ctx context.Context, conversationID string, opts ...DMPagedOption,
) (*DMEventsResponse, error) {
	if conversationID == "" {
		return nil, fmt.Errorf("xapi: GetDMConversation: conversationID must be non-empty")
	}
	cfg := newDMPagedConfig(opts)
	path := "/2/dm_conversations/" + url.PathEscape(conversationID) + "/dm_events"
	fetched, err := c.fetchDMEventsPage(ctx, path, &cfg, "GetDMConversation")
	if err != nil {
		return nil, err
	}
	return fetched.body, nil
}

// EachDMConversationPage は GetDMConversation を pagination_token で自動辿りする iterator (M35)。
func (c *Client) EachDMConversationPage(
	ctx context.Context, conversationID string,
	fn func(*DMEventsResponse) error, opts ...DMPagedOption,
) error {
	if conversationID == "" {
		return fmt.Errorf("xapi: EachDMConversationPage: conversationID must be non-empty")
	}
	cfg := newDMPagedConfig(opts)
	path := "/2/dm_conversations/" + url.PathEscape(conversationID) + "/dm_events"
	return c.eachDMEventsPaged(ctx, "EachDMConversationPage", path, &cfg, fn)
}

// =============================================================================
// GetDMWithUser / EachDMWithUserPage (paged, 1on1 with participant_id)
// =============================================================================

// GetDMWithUser は X API v2 `GET /2/dm_conversations/with/:participant_id/dm_events` を呼び出す (M35)。
//
// 1on1 DM に限定される (グループ DM は GetDMConversation で取得する)。
func (c *Client) GetDMWithUser(
	ctx context.Context, participantID string, opts ...DMPagedOption,
) (*DMEventsResponse, error) {
	if participantID == "" {
		return nil, fmt.Errorf("xapi: GetDMWithUser: participantID must be non-empty")
	}
	cfg := newDMPagedConfig(opts)
	path := "/2/dm_conversations/with/" + url.PathEscape(participantID) + "/dm_events"
	fetched, err := c.fetchDMEventsPage(ctx, path, &cfg, "GetDMWithUser")
	if err != nil {
		return nil, err
	}
	return fetched.body, nil
}

// EachDMWithUserPage は GetDMWithUser を pagination_token で自動辿りする iterator (M35)。
func (c *Client) EachDMWithUserPage(
	ctx context.Context, participantID string,
	fn func(*DMEventsResponse) error, opts ...DMPagedOption,
) error {
	if participantID == "" {
		return fmt.Errorf("xapi: EachDMWithUserPage: participantID must be non-empty")
	}
	cfg := newDMPagedConfig(opts)
	path := "/2/dm_conversations/with/" + url.PathEscape(participantID) + "/dm_events"
	return c.eachDMEventsPaged(ctx, "EachDMWithUserPage", path, &cfg, fn)
}

// =============================================================================
// 内部 fetch / each ヘルパ (M35 D-2: paged 3 endpoint で DRY 化)
// =============================================================================

// dmEventsPageFetched は DM Events ページの取得結果。
type dmEventsPageFetched struct {
	body      *DMEventsResponse
	rateLimit RateLimitInfo
}

// fetchDMEventsPage は paged 3 endpoint で共通の single page 取得関数。
func (c *Client) fetchDMEventsPage(
	ctx context.Context, path string, cfg *dmPagedConfig, funcName string,
) (*dmEventsPageFetched, error) {
	endpoint := buildDMPagedURL(c.BaseURL(), path, cfg)
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
	out := &DMEventsResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode %s response: %w", funcName, err)
	}
	return &dmEventsPageFetched{body: out, rateLimit: resp.RateLimit}, nil
}

// eachDMEventsPaged は paged 3 iterator で共通の自動辿りループ。
//
// 終了条件:
//   - meta.next_token が空文字列 (= 最終ページ)
//   - 取得済みページ数が WithDMPagedMaxPages (default 50) に到達
//   - fn が non-nil error を返す
//   - ctx が cancel された
func (c *Client) eachDMEventsPaged(
	ctx context.Context, funcName, path string,
	cfg *dmPagedConfig, fn func(*DMEventsResponse) error,
) error {
	maxPages := cfg.maxPages
	if maxPages <= 0 {
		maxPages = dmPagedDefaultMaxPages
	}
	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fetched, err := c.fetchDMEventsPage(ctx, path, cfg, funcName)
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
		wait := c.computeInterPageWait(fetched.rateLimit, dmPagedRateLimitThreshold)
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
		cfg.paginationToken = next
	}
	return nil
}

// =============================================================================
// URL ビルダ
// =============================================================================

// buildDMLookupURL は GetDMEvent の完全 URL を組み立てる。
func buildDMLookupURL(baseURL, path string, cfg *dmLookupConfig) string {
	values := url.Values{}
	if len(cfg.dmEventFields) > 0 {
		values.Set("dm_event.fields", strings.Join(cfg.dmEventFields, ","))
	}
	if len(cfg.expansions) > 0 {
		values.Set("expansions", strings.Join(cfg.expansions, ","))
	}
	if len(cfg.userFields) > 0 {
		values.Set("user.fields", strings.Join(cfg.userFields, ","))
	}
	if len(cfg.tweetFields) > 0 {
		values.Set("tweet.fields", strings.Join(cfg.tweetFields, ","))
	}
	if len(cfg.mediaFields) > 0 {
		values.Set("media.fields", strings.Join(cfg.mediaFields, ","))
	}
	if q := values.Encode(); q != "" {
		return baseURL + path + "?" + q
	}
	return baseURL + path
}

// buildDMPagedURL は paged 3 endpoint の完全 URL を組み立てる。
// event_types は CSV (`event_types=MessageCreate,ParticipantsJoin`) で送る (M35 D-3)。
func buildDMPagedURL(baseURL, path string, cfg *dmPagedConfig) string {
	values := url.Values{}
	if cfg.maxResults > 0 {
		values.Set("max_results", strconv.Itoa(cfg.maxResults))
	}
	if cfg.paginationToken != "" {
		values.Set("pagination_token", cfg.paginationToken)
	}
	if len(cfg.eventTypes) > 0 {
		values.Set("event_types", strings.Join(cfg.eventTypes, ","))
	}
	if len(cfg.dmEventFields) > 0 {
		values.Set("dm_event.fields", strings.Join(cfg.dmEventFields, ","))
	}
	if len(cfg.expansions) > 0 {
		values.Set("expansions", strings.Join(cfg.expansions, ","))
	}
	if len(cfg.userFields) > 0 {
		values.Set("user.fields", strings.Join(cfg.userFields, ","))
	}
	if len(cfg.tweetFields) > 0 {
		values.Set("tweet.fields", strings.Join(cfg.tweetFields, ","))
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

func newDMLookupConfig(opts []DMLookupOption) dmLookupConfig {
	cfg := dmLookupConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func newDMPagedConfig(opts []DMPagedOption) dmPagedConfig {
	cfg := dmPagedConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
