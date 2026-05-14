package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// dm.go は `x dm {list,get,conversation,with}` を提供する (M35)。
//
// 設計判断 (詳細は plans/x-m35-dm-read.md):
//   - 4 個別 factory + dmClient interface + var-swap (M34 spaceClient パターン継承)
//   - Option 型 2 種類 (Lookup / Paged) で誤用防止 (M35 D-1)
//   - event_types CSV ホワイトリスト検証は CLI 層で先に行う (M35 D-3 / D-4)
//   - --user-id は不公開 (X API 認証ユーザー固定、M35 D-14)
//   - aggregator generics 化は見送り、dmEventsAggregator をコピー定義 (M35 D-9)
//   - Tier 制約 (Basic 1 回/24h, Pro 推奨) と直近 30 日制限は Long doc で明示 (M35 D-13)

const (
	// dmDefaultDMEventFields は dm_event.fields の既定値 (全フィールド要求)。
	dmDefaultDMEventFields = "id,event_type,text,sender_id,dm_conversation_id,created_at,attachments,referenced_tweets,participant_ids,entities"
	dmDefaultUserFields    = "username,name"
	dmDefaultTweetFields   = "id,text,author_id,created_at"
	dmDefaultMediaFields   = ""
	dmDefaultExpansions    = ""
	dmDefaultEventTypes    = ""

	// dmMaxResultsCap は paged endpoint の per-page 上限 (X API 仕様)。
	dmMaxResultsCap = 100

	// dmHumanTextMaxRunes は human 出力で text を truncate するルーン数。
	dmHumanTextMaxRunes = 80
)

// dmEventIDRE は DM event ID の検証 (X API docs で 19 桁数値固定)。
var dmEventIDRE = regexp.MustCompile(`^\d{1,19}$`)

// dmConvIDRE は DM conversation ID の検証。`<userA>-<userB>` 形式が典型だが
// `group:<id>` 等のバリアントもあるため英数+`-:_` の組み合わせを許容する (M35 D-7)。
// `-` は character class 末尾でリテラル、可読性のためエスケープ表記を併用 (M35 D-17)。
var dmConvIDRE = regexp.MustCompile(`^[A-Za-z0-9_:\-]+$`)

// dmValidEventTypes は --event-types ホワイトリスト (M35 D-4、case-sensitive)。
var dmValidEventTypes = map[string]struct{}{
	"MessageCreate":     {},
	"ParticipantsJoin":  {},
	"ParticipantsLeave": {},
}

// dmClient は newDM*Cmd 群が必要とする X API クライアントの最小インターフェイス (M35 D-10)。
type dmClient interface {
	GetUserMe(ctx context.Context, opts ...xapi.UserFieldsOption) (*xapi.User, error)
	GetUserByUsername(ctx context.Context, username string, opts ...xapi.UserLookupOption) (*xapi.UserResponse, error)
	GetDMEvent(ctx context.Context, eventID string, opts ...xapi.DMLookupOption) (*xapi.DMEventResponse, error)
	GetDMEvents(ctx context.Context, opts ...xapi.DMPagedOption) (*xapi.DMEventsResponse, error)
	EachDMEventsPage(ctx context.Context, fn func(*xapi.DMEventsResponse) error, opts ...xapi.DMPagedOption) error
	GetDMConversation(ctx context.Context, conversationID string, opts ...xapi.DMPagedOption) (*xapi.DMEventsResponse, error)
	EachDMConversationPage(ctx context.Context, conversationID string, fn func(*xapi.DMEventsResponse) error, opts ...xapi.DMPagedOption) error
	GetDMWithUser(ctx context.Context, participantID string, opts ...xapi.DMPagedOption) (*xapi.DMEventsResponse, error)
	EachDMWithUserPage(ctx context.Context, participantID string, fn func(*xapi.DMEventsResponse) error, opts ...xapi.DMPagedOption) error
}

// newDMClient は newDM*Cmd が使う dmClient の生成関数 (var-swap でテストから差し替え)。
var newDMClient = func(ctx context.Context, creds *config.Credentials) (dmClient, error) {
	return xapi.NewClient(ctx, creds), nil
}

// newDMCmd は `x dm` 親コマンドを生成する factory。
func newDMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dm",
		Short: "fetch Direct Messages (Pro tier recommended)",
		Long: "Subcommands to fetch Direct Message events from the X API v2.\n" +
			"\n" +
			"IMPORTANT TIER NOTES:\n" +
			"  - Basic tier has a very strict rate limit (~1 call / 24h); these commands are effectively unusable.\n" +
			"  - Pro tier ($5,000/mo) or above is recommended for practical use.\n" +
			"  - Only DM events within the last 30 days are retrievable.\n" +
			"  - Your X App must have the Direct Messages read permission enabled.\n",
	}
	cmd.AddCommand(newDMListCmd())
	cmd.AddCommand(newDMGetCmd())
	cmd.AddCommand(newDMConversationCmd())
	cmd.AddCommand(newDMWithCmd())
	return cmd
}

// extractDMEventID は数値のみの DM event ID を検証する (M35 D-6)。
func extractDMEventID(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("%w: DM event ID is empty", ErrInvalidArgument)
	}
	if !dmEventIDRE.MatchString(trimmed) {
		return "", fmt.Errorf("%w: %q is not a valid DM event ID (must be 1..19 digits)", ErrInvalidArgument, s)
	}
	return trimmed, nil
}

// extractDMConversationID は DM conversation ID を検証する (M35 D-7)。
//
//   - `<userA>-<userB>` (1on1 の典型形)
//   - `group:<id>` (グループ DM)
//   - 数値のみ
//
// いずれも英数 + `-:_` の組み合わせで表現可能なので緩い regex で受ける。
func extractDMConversationID(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("%w: DM conversation ID is empty", ErrInvalidArgument)
	}
	if !dmConvIDRE.MatchString(trimmed) {
		return "", fmt.Errorf("%w: %q is not a valid DM conversation ID", ErrInvalidArgument, s)
	}
	return trimmed, nil
}

// validateEventTypes は --event-types CSV をホワイトリスト検証する (M35 D-4)。
// 空文字は no-op (X API デフォルトで 3 種全てを取得)。
// case-sensitive (X API 仕様、M35 D-16)。
func validateEventTypes(csv string) ([]string, error) {
	if strings.TrimSpace(csv) == "" {
		return nil, nil
	}
	parts := splitCSV(csv)
	for _, p := range parts {
		if _, ok := dmValidEventTypes[p]; !ok {
			return nil, fmt.Errorf("%w: --event-types: %q is not a valid event type (allowed: MessageCreate, ParticipantsJoin, ParticipantsLeave)", ErrInvalidArgument, p)
		}
	}
	return parts, nil
}

// =============================================================================
// dm list — /2/dm_events (paged)
// =============================================================================

//nolint:gocyclo // CLI コマンドのフラグ処理は分岐が多いが手続き的に追える流れに揃えている
func newDMListCmd() *cobra.Command {
	var (
		eventTypes      string
		maxResults      int
		paginationToken string
		all             bool
		maxPages        int
		noJSON          bool
		ndjson          bool
		dmEventFields   string
		expansions      string
		userFields      string
		tweetFields     string
		mediaFields     string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list recent DM events for the authenticated user",
		Long: "Fetch DM events for the authenticated user via GET /2/dm_events.\n" +
			"Returns events from the last 30 days only. Pro tier recommended.\n" +
			"--event-types: comma-separated subset of MessageCreate, ParticipantsJoin, ParticipantsLeave (default: all three).\n" +
			"--max-results 1..100 (X API per-page, default 100). --all auto-follows pagination_token.\n" +
			"--no-json prints one event per line, --ndjson streams events as line-delimited JSON.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			types, err := validateEventTypes(eventTypes)
			if err != nil {
				return err
			}
			if maxResults < 1 || maxResults > dmMaxResultsCap {
				return fmt.Errorf("%w: --max-results must be in 1..%d, got %d", ErrInvalidArgument, dmMaxResultsCap, maxResults)
			}
			outMode, err := decideOutputMode(noJSON, ndjson)
			if err != nil {
				return err
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newDMClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := buildDMPagedOpts(cmd, dmPagedFlagSet{
				maxResults:      maxResults,
				paginationToken: paginationToken,
				all:             all,
				maxPages:        maxPages,
				eventTypes:      types,
				dmEventFields:   dmEventFields,
				expansions:      expansions,
				userFields:      userFields,
				tweetFields:     tweetFields,
				mediaFields:     mediaFields,
			})
			if all {
				return runDMEventsAll(cmd, client, ctx, dmKindEvents, "", opts, outMode)
			}
			resp, err := client.GetDMEvents(ctx, opts...)
			if err != nil {
				return err
			}
			return writeDMEventsSinglePage(cmd, resp, outMode)
		},
	}
	registerDMPagedFlags(cmd,
		&eventTypes, &maxResults, &paginationToken, &all, &maxPages,
		&noJSON, &ndjson,
		&dmEventFields, &expansions, &userFields, &tweetFields, &mediaFields)
	return cmd
}

// =============================================================================
// dm get — /2/dm_events/:event_id (lookup)
// =============================================================================

func newDMGetCmd() *cobra.Command {
	var (
		dmEventFields string
		expansions    string
		userFields    string
		tweetFields   string
		mediaFields   string
		noJSON        bool
	)
	cmd := &cobra.Command{
		Use:   "get <eventID>",
		Short: "look up a single DM event by ID",
		Long: "Look up a single DM event by ID via GET /2/dm_events/:event_id.\n" +
			"event ID must be 1..19 digits.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eventID, err := extractDMEventID(args[0])
			if err != nil {
				return err
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newDMClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := []xapi.DMLookupOption{}
			if fs := splitCSV(dmEventFields); len(fs) > 0 {
				opts = append(opts, xapi.WithDMLookupDMEventFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithDMLookupExpansions(fs...))
			}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithDMLookupUserFields(fs...))
			}
			if fs := splitCSV(tweetFields); len(fs) > 0 {
				opts = append(opts, xapi.WithDMLookupTweetFields(fs...))
			}
			if fs := splitCSV(mediaFields); len(fs) > 0 {
				opts = append(opts, xapi.WithDMLookupMediaFields(fs...))
			}
			resp, err := client.GetDMEvent(ctx, eventID, opts...)
			if err != nil {
				return err
			}
			if noJSON {
				if resp != nil && resp.Data != nil {
					_, err := fmt.Fprintln(cmd.OutOrStdout(), formatDMEventHumanLine(*resp.Data))
					return err
				}
				return nil
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
		},
	}
	cmd.Flags().StringVar(&dmEventFields, "dm-event-fields", dmDefaultDMEventFields, "comma-separated dm_event.fields")
	cmd.Flags().StringVar(&expansions, "expansions", dmDefaultExpansions, "comma-separated expansions (e.g. sender_id,attachments.media_keys)")
	cmd.Flags().StringVar(&userFields, "user-fields", dmDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", dmDefaultTweetFields, "comma-separated tweet.fields")
	cmd.Flags().StringVar(&mediaFields, "media-fields", dmDefaultMediaFields, "comma-separated media.fields")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// =============================================================================
// dm conversation — /2/dm_conversations/:id/dm_events (paged)
// =============================================================================

//nolint:gocyclo // CLI コマンドのフラグ処理は分岐が多いが手続き的に追える流れに揃えている
func newDMConversationCmd() *cobra.Command {
	var (
		eventTypes      string
		maxResults      int
		paginationToken string
		all             bool
		maxPages        int
		noJSON          bool
		ndjson          bool
		dmEventFields   string
		expansions      string
		userFields      string
		tweetFields     string
		mediaFields     string
	)
	cmd := &cobra.Command{
		Use:   "conversation <conversationID>",
		Short: "fetch DM events for a specific conversation",
		Long: "Fetch DM events for a specific conversation via GET /2/dm_conversations/:id/dm_events.\n" +
			"Typical IDs are `<userA>-<userB>` (1on1) or `group:<id>` (group DM).\n" +
			"--max-results 1..100 (X API per-page, default 100). --all auto-follows pagination_token.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			convID, err := extractDMConversationID(args[0])
			if err != nil {
				return err
			}
			types, err := validateEventTypes(eventTypes)
			if err != nil {
				return err
			}
			if maxResults < 1 || maxResults > dmMaxResultsCap {
				return fmt.Errorf("%w: --max-results must be in 1..%d, got %d", ErrInvalidArgument, dmMaxResultsCap, maxResults)
			}
			outMode, err := decideOutputMode(noJSON, ndjson)
			if err != nil {
				return err
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newDMClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := buildDMPagedOpts(cmd, dmPagedFlagSet{
				maxResults:      maxResults,
				paginationToken: paginationToken,
				all:             all,
				maxPages:        maxPages,
				eventTypes:      types,
				dmEventFields:   dmEventFields,
				expansions:      expansions,
				userFields:      userFields,
				tweetFields:     tweetFields,
				mediaFields:     mediaFields,
			})
			if all {
				return runDMEventsAll(cmd, client, ctx, dmKindConversation, convID, opts, outMode)
			}
			resp, err := client.GetDMConversation(ctx, convID, opts...)
			if err != nil {
				return err
			}
			return writeDMEventsSinglePage(cmd, resp, outMode)
		},
	}
	registerDMPagedFlags(cmd,
		&eventTypes, &maxResults, &paginationToken, &all, &maxPages,
		&noJSON, &ndjson,
		&dmEventFields, &expansions, &userFields, &tweetFields, &mediaFields)
	return cmd
}

// =============================================================================
// dm with — /2/dm_conversations/with/:participant_id/dm_events (paged, 1on1)
// =============================================================================

//nolint:gocyclo // CLI コマンドのフラグ処理は分岐が多いが手続き的に追える流れに揃えている
func newDMWithCmd() *cobra.Command {
	var (
		eventTypes      string
		maxResults      int
		paginationToken string
		all             bool
		maxPages        int
		noJSON          bool
		ndjson          bool
		dmEventFields   string
		expansions      string
		userFields      string
		tweetFields     string
		mediaFields     string
	)
	cmd := &cobra.Command{
		Use:   "with <ID|@username|URL>",
		Short: "fetch 1on1 DM events with a specific user",
		Long: "Fetch 1on1 DM events with a specific user via GET /2/dm_conversations/with/:participant_id/dm_events.\n" +
			"Accepts a numeric user ID, @username, or X profile URL.\n" +
			"--max-results 1..100 (X API per-page, default 100). --all auto-follows pagination_token.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			value, isUsername, err := extractUserRef(args[0])
			if err != nil {
				return err
			}
			types, err := validateEventTypes(eventTypes)
			if err != nil {
				return err
			}
			if maxResults < 1 || maxResults > dmMaxResultsCap {
				return fmt.Errorf("%w: --max-results must be in 1..%d, got %d", ErrInvalidArgument, dmMaxResultsCap, maxResults)
			}
			outMode, err := decideOutputMode(noJSON, ndjson)
			if err != nil {
				return err
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newDMClient(ctx, creds)
			if err != nil {
				return err
			}
			participantID := value
			if isUsername {
				resp, uerr := client.GetUserByUsername(ctx, value)
				if uerr != nil {
					return uerr
				}
				if resp == nil || resp.Data == nil || resp.Data.ID == "" {
					return fmt.Errorf("cli: GetUserByUsername returned empty data for %q", value)
				}
				participantID = resp.Data.ID
			}
			opts := buildDMPagedOpts(cmd, dmPagedFlagSet{
				maxResults:      maxResults,
				paginationToken: paginationToken,
				all:             all,
				maxPages:        maxPages,
				eventTypes:      types,
				dmEventFields:   dmEventFields,
				expansions:      expansions,
				userFields:      userFields,
				tweetFields:     tweetFields,
				mediaFields:     mediaFields,
			})
			if all {
				return runDMEventsAll(cmd, client, ctx, dmKindWithUser, participantID, opts, outMode)
			}
			resp, err := client.GetDMWithUser(ctx, participantID, opts...)
			if err != nil {
				return err
			}
			return writeDMEventsSinglePage(cmd, resp, outMode)
		},
	}
	registerDMPagedFlags(cmd,
		&eventTypes, &maxResults, &paginationToken, &all, &maxPages,
		&noJSON, &ndjson,
		&dmEventFields, &expansions, &userFields, &tweetFields, &mediaFields)
	return cmd
}

// =============================================================================
// paged 共通フラグ + Option 組み立て
// =============================================================================

func registerDMPagedFlags(
	cmd *cobra.Command,
	eventTypes *string, maxResults *int, paginationToken *string, all *bool, maxPages *int,
	noJSON *bool, ndjson *bool,
	dmEventFields *string, expansions *string, userFields *string, tweetFields *string, mediaFields *string,
) {
	cmd.Flags().StringVar(eventTypes, "event-types", dmDefaultEventTypes,
		"comma-separated event types (MessageCreate, ParticipantsJoin, ParticipantsLeave; default: all)")
	cmd.Flags().IntVar(maxResults, "max-results", dmMaxResultsCap, "max results per page (1..100)")
	cmd.Flags().StringVar(paginationToken, "pagination-token", "", "resume using pagination_token")
	cmd.Flags().BoolVar(all, "all", false, "auto-follow pagination_token until end or --max-pages")
	cmd.Flags().IntVar(maxPages, "max-pages", 50, "max pages when --all is set")
	cmd.Flags().BoolVar(noJSON, "no-json", false, "output human-readable text instead of JSON")
	cmd.Flags().BoolVar(ndjson, "ndjson", false, "output line-delimited JSON (one event per line)")
	cmd.Flags().StringVar(dmEventFields, "dm-event-fields", dmDefaultDMEventFields, "comma-separated dm_event.fields")
	cmd.Flags().StringVar(expansions, "expansions", dmDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(userFields, "user-fields", dmDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(tweetFields, "tweet-fields", dmDefaultTweetFields, "comma-separated tweet.fields")
	cmd.Flags().StringVar(mediaFields, "media-fields", dmDefaultMediaFields, "comma-separated media.fields")
}

// dmPagedFlagSet は registerDMPagedFlags で集めた値を 1 struct にまとめる中間表現。
type dmPagedFlagSet struct {
	maxResults      int
	paginationToken string
	all             bool
	maxPages        int
	eventTypes      []string
	dmEventFields   string
	expansions      string
	userFields      string
	tweetFields     string
	mediaFields     string
}

func buildDMPagedOpts(cmd *cobra.Command, f dmPagedFlagSet) []xapi.DMPagedOption {
	opts := []xapi.DMPagedOption{
		xapi.WithDMPagedMaxResults(f.maxResults),
	}
	if f.paginationToken != "" {
		if f.all {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
				"warning: --pagination-token is ignored when --all is set")
		} else {
			opts = append(opts, xapi.WithDMPagedPaginationToken(f.paginationToken))
		}
	}
	if len(f.eventTypes) > 0 {
		opts = append(opts, xapi.WithDMPagedEventTypes(f.eventTypes...))
	}
	if fs := splitCSV(f.dmEventFields); len(fs) > 0 {
		opts = append(opts, xapi.WithDMPagedDMEventFields(fs...))
	}
	if fs := splitCSV(f.expansions); len(fs) > 0 {
		opts = append(opts, xapi.WithDMPagedExpansions(fs...))
	}
	if fs := splitCSV(f.userFields); len(fs) > 0 {
		opts = append(opts, xapi.WithDMPagedUserFields(fs...))
	}
	if fs := splitCSV(f.tweetFields); len(fs) > 0 {
		opts = append(opts, xapi.WithDMPagedTweetFields(fs...))
	}
	if fs := splitCSV(f.mediaFields); len(fs) > 0 {
		opts = append(opts, xapi.WithDMPagedMediaFields(fs...))
	}
	if f.all {
		opts = append(opts, xapi.WithDMPagedMaxPages(f.maxPages))
	}
	return opts
}

// =============================================================================
// --all 集約: 3 endpoint で eachFn が異なるため dispatch
// =============================================================================

// dmKind は 3 paged endpoint を区別する内部 enum。
type dmKind int

const (
	dmKindEvents dmKind = iota
	dmKindConversation
	dmKindWithUser
)

//nolint:revive // ctx を引数に取るが、引数順は cobra の RunE 流儀に合わせる
func runDMEventsAll(
	cmd *cobra.Command, client dmClient, ctx context.Context,
	kind dmKind, id string, opts []xapi.DMPagedOption, outMode likedOutputMode,
) error {
	eachFn := dispatchDMEach(client, kind, id)
	if outMode == likedOutputModeNDJSON {
		w := cmd.OutOrStdout()
		return eachFn(ctx, func(p *xapi.DMEventsResponse) error {
			if p == nil {
				return nil
			}
			return writeNDJSONDMEvents(w, p.Data)
		}, opts...)
	}
	agg := &dmEventsAggregator{}
	if err := eachFn(ctx, agg.add, opts...); err != nil {
		return err
	}
	resp := agg.build()
	if outMode == likedOutputModeHuman {
		return writeDMEventsHuman(cmd, resp)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// dmEachFn は 3 paged iterator の共通シグネチャ (ID をクロージャに閉じ込めた形)。
type dmEachFn func(context.Context, func(*xapi.DMEventsResponse) error, ...xapi.DMPagedOption) error

func dispatchDMEach(c dmClient, kind dmKind, id string) dmEachFn {
	switch kind {
	case dmKindEvents:
		return c.EachDMEventsPage
	case dmKindConversation:
		return func(ctx context.Context, fn func(*xapi.DMEventsResponse) error, opts ...xapi.DMPagedOption) error {
			return c.EachDMConversationPage(ctx, id, fn, opts...)
		}
	case dmKindWithUser:
		return func(ctx context.Context, fn func(*xapi.DMEventsResponse) error, opts ...xapi.DMPagedOption) error {
			return c.EachDMWithUserPage(ctx, id, fn, opts...)
		}
	}
	return c.EachDMEventsPage
}

// dmEventsAggregator は DM Events ページを集約する (M35 D-9、generics 化見送り)。
type dmEventsAggregator struct {
	data   []xapi.DMEvent
	users  []xapi.User
	tweets []xapi.Tweet
	media  []xapi.Media
}

func (a *dmEventsAggregator) add(p *xapi.DMEventsResponse) error {
	if p == nil {
		return nil
	}
	a.data = append(a.data, p.Data...)
	a.users = append(a.users, p.Includes.Users...)
	a.tweets = append(a.tweets, p.Includes.Tweets...)
	a.media = append(a.media, p.Includes.Media...)
	return nil
}

func (a *dmEventsAggregator) build() *xapi.DMEventsResponse {
	return &xapi.DMEventsResponse{
		Data: a.data,
		Includes: xapi.Includes{
			Users:  a.users,
			Tweets: a.tweets,
			Media:  a.media,
		},
		Meta: xapi.Meta{
			ResultCount: len(a.data),
		},
	}
}

// =============================================================================
// 出力ヘルパ
// =============================================================================

// writeDMEventsSinglePage は --all=false 時の出力。
func writeDMEventsSinglePage(cmd *cobra.Command, resp *xapi.DMEventsResponse, outMode likedOutputMode) error {
	switch outMode {
	case likedOutputModeHuman:
		return writeDMEventsHuman(cmd, resp)
	case likedOutputModeNDJSON:
		if resp == nil {
			return nil
		}
		return writeNDJSONDMEvents(cmd.OutOrStdout(), resp.Data)
	default:
		return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
	}
}

// writeDMEventsHuman は DM Events を human フォーマット (1 event/行) で出力する。
func writeDMEventsHuman(cmd *cobra.Command, resp *xapi.DMEventsResponse) error {
	if resp == nil || len(resp.Data) == 0 {
		return nil
	}
	for _, e := range resp.Data {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatDMEventHumanLine(e)); err != nil {
			return err
		}
	}
	return nil
}

// formatDMEventHumanLine は 1 DM event を 1 行に整形する (M35 D-12)。
// text は改行/タブ正規化 + 80 ルーン truncate (liked と同じ流儀)。
func formatDMEventHumanLine(e xapi.DMEvent) string {
	text := sanitizeLikedText(e.Text)
	text = truncateRunes(text, dmHumanTextMaxRunes)
	return fmt.Sprintf("id=%s\ttype=%s\tconv=%s\tsender=%s\tcreated=%s\ttext=%s",
		e.ID, e.EventType, e.DMConversationID, e.SenderID, e.CreatedAt, text)
}

// writeNDJSONDMEvents は DM Event 配列を NDJSON 出力する (HTML escape off)。
func writeNDJSONDMEvents(w io.Writer, events []xapi.DMEvent) error {
	if len(events) == 0 {
		return nil
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for i := range events {
		if err := enc.Encode(events[i]); err != nil {
			return err
		}
	}
	return nil
}
