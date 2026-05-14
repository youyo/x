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

// list.go は `x list {get,tweets,members,owned,followed,memberships,pinned}` を提供する (M33)。
//
// 設計判断 (詳細は plans/x-m33-lists.md):
//   - 7 個別 factory + listClient interface + var-swap (M32 D-12 パターン継承)
//   - pinned は self only (X API 仕様) のため --user-id フラグを公開しない (M33 D-7)
//   - extractListID で位置引数の数値 / URL を判別 (M33 D-6)
//   - owned/followed/memberships の username 位置引数は GetUserByUsername で先に ID 解決 (M33 D-10)
//   - List 配列 aggregator (listsAggregator) と Tweet 配列 aggregator (listTweetsAggregator) を新規定義 (M33 D-2)
//     User 配列は M32 userAggregator を再利用 (M33 D-15)

const (
	// listDefaultListFields は List 系コマンド共通の既定 list.fields。
	listDefaultListFields = "id,name,description,private,owner_id,member_count,follower_count"
	// listDefaultUserFields は members / owner 解決 expansions 用の既定 user.fields。
	listDefaultUserFields = "username,name"
	// listDefaultTweetFields は list tweets の既定 tweet.fields。
	listDefaultTweetFields = "id,text,author_id,created_at,entities,public_metrics,note_tweet,conversation_id"
	listDefaultExpansions  = ""
	listDefaultMediaFields = ""

	// listMaxResultsCap は paged endpoint の per-page 上限 (X API 仕様、全 endpoint で 100、M33 D-1)。
	listMaxResultsCap = 100
)

// listIDURLRE は List 共有 URL の正規表現 (M33 D-6)。`https://(x|twitter).com/i/lists/<NUM>` のみ許可。
var listIDURLRE = regexp.MustCompile(`^https?://(?:x|twitter)\.com/i/lists/(\d+)/?$`)

// listClient は newList*Cmd 群が必要とする X API クライアントの最小インターフェイスである (M33 D-12)。
type listClient interface {
	GetUserMe(ctx context.Context, opts ...xapi.UserFieldsOption) (*xapi.User, error)
	GetUserByUsername(ctx context.Context, username string, opts ...xapi.UserLookupOption) (*xapi.UserResponse, error)
	GetList(ctx context.Context, listID string, opts ...xapi.ListLookupOption) (*xapi.ListResponse, error)
	GetListTweets(ctx context.Context, listID string, opts ...xapi.ListPagedOption) (*xapi.ListTweetsResponse, error)
	GetListMembers(ctx context.Context, listID string, opts ...xapi.ListPagedOption) (*xapi.UsersResponse, error)
	GetOwnedLists(ctx context.Context, userID string, opts ...xapi.ListPagedOption) (*xapi.ListsResponse, error)
	GetListMemberships(ctx context.Context, userID string, opts ...xapi.ListPagedOption) (*xapi.ListsResponse, error)
	GetFollowedLists(ctx context.Context, userID string, opts ...xapi.ListPagedOption) (*xapi.ListsResponse, error)
	GetPinnedLists(ctx context.Context, userID string, opts ...xapi.ListLookupOption) (*xapi.ListsResponse, error)
	EachListTweetsPage(ctx context.Context, listID string, fn func(*xapi.ListTweetsResponse) error, opts ...xapi.ListPagedOption) error
	EachListMembersPage(ctx context.Context, listID string, fn func(*xapi.UsersResponse) error, opts ...xapi.ListPagedOption) error
	EachOwnedListsPage(ctx context.Context, userID string, fn func(*xapi.ListsResponse) error, opts ...xapi.ListPagedOption) error
	EachListMembershipsPage(ctx context.Context, userID string, fn func(*xapi.ListsResponse) error, opts ...xapi.ListPagedOption) error
	EachFollowedListsPage(ctx context.Context, userID string, fn func(*xapi.ListsResponse) error, opts ...xapi.ListPagedOption) error
}

// newListClient は newList*Cmd が使う listClient の生成関数 (var-swap でテストから差し替え)。
var newListClient = func(ctx context.Context, creds *config.Credentials) (listClient, error) {
	return xapi.NewClient(ctx, creds), nil
}

// newListCmd は `x list` 親コマンドを生成する factory である。
func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "manage X Lists (lookup / tweets / members / owned / followed / memberships / pinned)",
		Long: "Subcommands to look up X Lists, list tweets/members, and inspect a user's owned/followed/memberships/pinned lists.\n" +
			"`pinned` is restricted to the authenticated user (X API spec).",
	}
	cmd.AddCommand(newListGetCmd())
	cmd.AddCommand(newListTweetsCmd())
	cmd.AddCommand(newListMembersCmd())
	cmd.AddCommand(newListOwnedCmd())
	cmd.AddCommand(newListFollowedCmd())
	cmd.AddCommand(newListMembershipsCmd())
	cmd.AddCommand(newListPinnedCmd())
	return cmd
}

// extractListID は位置引数 (数値 ID / `https://(x|twitter).com/i/lists/<NUM>` URL) を解析して List ID を返す (M33 D-6)。
//
//   - 純粋数字 (`^\d+$`) → そのまま ID
//   - 公式 URL 形式 → 数値部分を抽出
//   - 上記以外 → ErrInvalidArgument
func extractListID(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("%w: list ID/URL is empty", ErrInvalidArgument)
	}
	if numericIDRE.MatchString(trimmed) {
		return trimmed, nil
	}
	if m := listIDURLRE.FindStringSubmatch(trimmed); len(m) == 2 {
		return m[1], nil
	}
	return "", fmt.Errorf("%w: %q is not a numeric list ID or https://x.com/i/lists/<ID> URL", ErrInvalidArgument, s)
}

// =============================================================================
// list get
// =============================================================================

func newListGetCmd() *cobra.Command {
	var (
		listFields string
		expansions string
		userFields string
		noJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "get <ID|URL>",
		Short: "look up a List by numeric ID or X List URL",
		Long: "Look up a List by ID (`12345`) or URL (`https://x.com/i/lists/12345`).\n" +
			"--no-json prints id=...\\tname=...\\tprivate=...\\towner_id=... line.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			listID, err := extractListID(args[0])
			if err != nil {
				return err
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newListClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := []xapi.ListLookupOption{}
			if fs := splitCSV(listFields); len(fs) > 0 {
				opts = append(opts, xapi.WithListLookupListFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithListLookupExpansions(fs...))
			}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithListLookupUserFields(fs...))
			}
			resp, err := client.GetList(ctx, listID, opts...)
			if err != nil {
				return err
			}
			return writeListResponseHumanOrJSON(cmd, resp, noJSON)
		},
	}
	cmd.Flags().StringVar(&listFields, "list-fields", listDefaultListFields, "comma-separated list.fields")
	cmd.Flags().StringVar(&expansions, "expansions", listDefaultExpansions, "comma-separated expansions (e.g. owner_id)")
	cmd.Flags().StringVar(&userFields, "user-fields", listDefaultUserFields, "comma-separated user.fields (for owner_id expansion)")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// =============================================================================
// list tweets
// =============================================================================

func newListTweetsCmd() *cobra.Command {
	f := &listPagedFlags{}
	cmd := &cobra.Command{
		Use:   "tweets <ID|URL>",
		Short: "fetch tweets from a List",
		Long: "Fetch tweets from a List via GET /2/lists/:id/tweets.\n" +
			"--max-results 1..100 (X API per-page, default 100). --all auto-follows pagination_token up to --max-pages.\n" +
			"--no-json prints one tweet per line, --ndjson streams tweets as line-delimited JSON.\n" +
			"NOTE: Start-time/End-time filters are NOT supported by the X API for List tweets (M33 D-9).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			listID, err := extractListID(args[0])
			if err != nil {
				return err
			}
			return runListTweets(cmd, f, listID)
		},
	}
	registerListPagedCommonFlags(cmd, f, true /* withTweetFields */, true /* withMediaFields */, false /* withListFields */)
	return cmd
}

// =============================================================================
// list members
// =============================================================================

func newListMembersCmd() *cobra.Command {
	f := &listPagedFlags{}
	cmd := &cobra.Command{
		Use:   "members <ID|URL>",
		Short: "list users that are members of a List",
		Long: "Fetch members of a List via GET /2/lists/:id/members.\n" +
			"--max-results 1..100 (X API per-page, default 100). --all auto-follows pagination_token.\n" +
			"--no-json prints one user per line (id=... username=... name=...).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			listID, err := extractListID(args[0])
			if err != nil {
				return err
			}
			return runListMembers(cmd, f, listID)
		},
	}
	registerListPagedCommonFlags(cmd, f, true /* withTweetFields */, false /* withMediaFields */, false /* withListFields */)
	return cmd
}

// =============================================================================
// list owned / followed / memberships
// =============================================================================

func newListOwnedCmd() *cobra.Command {
	var userID string
	f := &listPagedFlags{}
	cmd := &cobra.Command{
		Use:   "owned [<ID|@username|URL>]",
		Short: "list Lists owned by a user (defaults to self)",
		Long: "Fetch Lists owned by a user via GET /2/users/:id/owned_lists.\n" +
			"--user-id defaults to the authenticated user via GetUserMe.\n" +
			"@username / X profile URL positional args are resolved via GetUserByUsername first (extra API call).\n" +
			"--max-results 1..100 (X API per-page).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit := strings.TrimSpace(userID)
			if len(args) == 1 {
				explicit = strings.TrimSpace(args[0])
			}
			return runUserLists(cmd, f, explicit, userListsKindOwned)
		},
	}
	cmd.Flags().StringVar(&userID, "user-id", "", "target user ID (default: authenticated user)")
	registerListPagedCommonFlags(cmd, f, false /* withTweetFields */, false /* withMediaFields */, true /* withListFields */)
	return cmd
}

func newListFollowedCmd() *cobra.Command {
	var userID string
	f := &listPagedFlags{}
	cmd := &cobra.Command{
		Use:   "followed [<ID|@username|URL>]",
		Short: "list Lists followed by a user (defaults to self)",
		Long: "Fetch Lists followed by a user via GET /2/users/:id/followed_lists.\n" +
			"--user-id defaults to the authenticated user via GetUserMe.\n" +
			"@username / X profile URL positional args are resolved via GetUserByUsername first (extra API call).\n" +
			"--max-results 1..100 (X API per-page).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit := strings.TrimSpace(userID)
			if len(args) == 1 {
				explicit = strings.TrimSpace(args[0])
			}
			return runUserLists(cmd, f, explicit, userListsKindFollowed)
		},
	}
	cmd.Flags().StringVar(&userID, "user-id", "", "target user ID (default: authenticated user)")
	registerListPagedCommonFlags(cmd, f, false /* withTweetFields */, false /* withMediaFields */, true /* withListFields */)
	return cmd
}

func newListMembershipsCmd() *cobra.Command {
	var userID string
	f := &listPagedFlags{}
	cmd := &cobra.Command{
		Use:   "memberships [<ID|@username|URL>]",
		Short: "list Lists that a user is a member of (defaults to self)",
		Long: "Fetch Lists that a user is a member of via GET /2/users/:id/list_memberships.\n" +
			"--user-id defaults to the authenticated user via GetUserMe.\n" +
			"@username / X profile URL positional args are resolved via GetUserByUsername first (extra API call).\n" +
			"--max-results 1..100 (X API per-page).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit := strings.TrimSpace(userID)
			if len(args) == 1 {
				explicit = strings.TrimSpace(args[0])
			}
			return runUserLists(cmd, f, explicit, userListsKindMemberships)
		},
	}
	cmd.Flags().StringVar(&userID, "user-id", "", "target user ID (default: authenticated user)")
	registerListPagedCommonFlags(cmd, f, false /* withTweetFields */, false /* withMediaFields */, true /* withListFields */)
	return cmd
}

// =============================================================================
// list pinned (self only)
// =============================================================================

func newListPinnedCmd() *cobra.Command {
	var (
		listFields string
		expansions string
		userFields string
		noJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "pinned",
		Short: "list Lists pinned by the authenticated user",
		Long: "Fetch the authenticated user's pinned Lists via GET /2/users/:id/pinned_lists.\n" +
			"X API spec restricts this endpoint to the authenticated user; --user-id is intentionally NOT exposed (M33 D-7).\n" +
			"This endpoint does NOT support pagination.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newListClient(ctx, creds)
			if err != nil {
				return err
			}
			user, err := client.GetUserMe(ctx)
			if err != nil {
				return err
			}
			opts := []xapi.ListLookupOption{}
			if fs := splitCSV(listFields); len(fs) > 0 {
				opts = append(opts, xapi.WithListLookupListFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithListLookupExpansions(fs...))
			}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithListLookupUserFields(fs...))
			}
			resp, err := client.GetPinnedLists(ctx, user.ID, opts...)
			if err != nil {
				return err
			}
			return writeListsResponseHumanOrJSON(cmd, resp, noJSON)
		},
	}
	cmd.Flags().StringVar(&listFields, "list-fields", listDefaultListFields, "comma-separated list.fields")
	cmd.Flags().StringVar(&expansions, "expansions", listDefaultExpansions, "comma-separated expansions (e.g. owner_id)")
	cmd.Flags().StringVar(&userFields, "user-fields", listDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// =============================================================================
// paged 共通フラグセット + ロジック
// =============================================================================

// listPagedFlags は paged 系サブコマンドで共通のフラグセット。
// 各 endpoint で必要なフラグだけ register*Flags で個別登録する。
type listPagedFlags struct {
	maxResults      int
	paginationToken string
	all             bool
	maxPages        int
	noJSON          bool
	ndjson          bool
	listFields      string
	tweetFields     string
	userFields      string
	expansions      string
	mediaFields     string
}

// registerListPagedCommonFlags は paged 系共通フラグを登録する。
//
// withTweetFields=true: tweet.fields を登録 (list tweets / members で利用)
// withMediaFields=true: media.fields を登録 (list tweets のみ)
// withListFields=true:  list.fields を登録 (owned/followed/memberships で利用)
func registerListPagedCommonFlags(cmd *cobra.Command, f *listPagedFlags, withTweetFields, withMediaFields, withListFields bool) {
	cmd.Flags().IntVar(&f.maxResults, "max-results", listMaxResultsCap, "max results per page (1..100)")
	cmd.Flags().StringVar(&f.paginationToken, "pagination-token", "", "resume from a previous page using pagination_token")
	cmd.Flags().BoolVar(&f.all, "all", false, "auto-follow pagination_token until end or --max-pages")
	cmd.Flags().IntVar(&f.maxPages, "max-pages", 50, "max pages to fetch when --all is set")
	cmd.Flags().BoolVar(&f.noJSON, "no-json", false, "output human-readable text instead of JSON")
	cmd.Flags().BoolVar(&f.ndjson, "ndjson", false, "output line-delimited JSON (one item per line)")
	cmd.Flags().StringVar(&f.userFields, "user-fields", listDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&f.expansions, "expansions", listDefaultExpansions, "comma-separated expansions")
	if withListFields {
		cmd.Flags().StringVar(&f.listFields, "list-fields", listDefaultListFields, "comma-separated list.fields")
	}
	if withTweetFields {
		cmd.Flags().StringVar(&f.tweetFields, "tweet-fields", listDefaultTweetFields, "comma-separated tweet.fields")
	}
	if withMediaFields {
		cmd.Flags().StringVar(&f.mediaFields, "media-fields", listDefaultMediaFields, "comma-separated media.fields")
	}
}

// buildListPagedOpts は listPagedFlags から xapi.ListPagedOption スライスを組み立てる。
func buildListPagedOpts(cmd *cobra.Command, f *listPagedFlags) []xapi.ListPagedOption {
	opts := []xapi.ListPagedOption{
		xapi.WithListPagedMaxResults(f.maxResults),
	}
	if f.paginationToken != "" {
		if f.all {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
				"warning: --pagination-token is ignored when --all is set")
		} else {
			opts = append(opts, xapi.WithListPagedPaginationToken(f.paginationToken))
		}
	}
	if fs := splitCSV(f.listFields); len(fs) > 0 {
		opts = append(opts, xapi.WithListPagedListFields(fs...))
	}
	if fs := splitCSV(f.userFields); len(fs) > 0 {
		opts = append(opts, xapi.WithListPagedUserFields(fs...))
	}
	if fs := splitCSV(f.tweetFields); len(fs) > 0 {
		opts = append(opts, xapi.WithListPagedTweetFields(fs...))
	}
	if fs := splitCSV(f.expansions); len(fs) > 0 {
		opts = append(opts, xapi.WithListPagedExpansions(fs...))
	}
	if fs := splitCSV(f.mediaFields); len(fs) > 0 {
		opts = append(opts, xapi.WithListPagedMediaFields(fs...))
	}
	if f.all {
		opts = append(opts, xapi.WithListPagedMaxPages(f.maxPages))
	}
	return opts
}

// runListTweets は `x list tweets` の共通実行ロジック。
func runListTweets(cmd *cobra.Command, f *listPagedFlags, listID string) error {
	if f.maxResults < 1 || f.maxResults > listMaxResultsCap {
		return fmt.Errorf("%w: --max-results must be in 1..%d, got %d", ErrInvalidArgument, listMaxResultsCap, f.maxResults)
	}
	outMode, err := decideOutputMode(f.noJSON, f.ndjson)
	if err != nil {
		return err
	}
	creds, err := LoadCredentialsFromEnvOrFile()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	client, err := newListClient(ctx, creds)
	if err != nil {
		return err
	}
	opts := buildListPagedOpts(cmd, f)

	if !f.all {
		resp, err := client.GetListTweets(ctx, listID, opts...)
		if err != nil {
			return err
		}
		return writeListTweetsSinglePage(cmd, resp, outMode)
	}
	return runListTweetsAll(cmd, client, ctx, listID, opts, outMode)
}

// runListMembers は `x list members` の共通実行ロジック。
func runListMembers(cmd *cobra.Command, f *listPagedFlags, listID string) error {
	if f.maxResults < 1 || f.maxResults > listMaxResultsCap {
		return fmt.Errorf("%w: --max-results must be in 1..%d, got %d", ErrInvalidArgument, listMaxResultsCap, f.maxResults)
	}
	outMode, err := decideOutputMode(f.noJSON, f.ndjson)
	if err != nil {
		return err
	}
	creds, err := LoadCredentialsFromEnvOrFile()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	client, err := newListClient(ctx, creds)
	if err != nil {
		return err
	}
	opts := buildListPagedOpts(cmd, f)

	if !f.all {
		resp, err := client.GetListMembers(ctx, listID, opts...)
		if err != nil {
			return err
		}
		return writeUsersListSinglePage(cmd, resp, outMode)
	}
	return runListMembersAll(cmd, client, ctx, listID, opts, outMode)
}

// userListsKind は owned/followed/memberships を区別する内部 enum。
type userListsKind int

const (
	userListsKindOwned userListsKind = iota
	userListsKindFollowed
	userListsKindMemberships
)

type (
	userListsGetFn  func(context.Context, string, ...xapi.ListPagedOption) (*xapi.ListsResponse, error)
	userListsEachFn func(context.Context, string, func(*xapi.ListsResponse) error, ...xapi.ListPagedOption) error
)

func (k userListsKind) dispatch(c listClient) (userListsGetFn, userListsEachFn) {
	switch k {
	case userListsKindOwned:
		return c.GetOwnedLists, c.EachOwnedListsPage
	case userListsKindFollowed:
		return c.GetFollowedLists, c.EachFollowedListsPage
	case userListsKindMemberships:
		return c.GetListMemberships, c.EachListMembershipsPage
	}
	return c.GetOwnedLists, c.EachOwnedListsPage
}

// runUserLists は owned/followed/memberships 3 サブコマンドの共通実行ロジック。
func runUserLists(cmd *cobra.Command, f *listPagedFlags, explicitRef string, kind userListsKind) error {
	if f.maxResults < 1 || f.maxResults > listMaxResultsCap {
		return fmt.Errorf("%w: --max-results must be in 1..%d, got %d", ErrInvalidArgument, listMaxResultsCap, f.maxResults)
	}
	outMode, err := decideOutputMode(f.noJSON, f.ndjson)
	if err != nil {
		return err
	}
	creds, err := LoadCredentialsFromEnvOrFile()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	client, err := newListClient(ctx, creds)
	if err != nil {
		return err
	}
	targetUserID, err := resolveListsTargetUserID(ctx, client, explicitRef)
	if err != nil {
		return err
	}
	opts := buildListPagedOpts(cmd, f)
	getFn, eachFn := kind.dispatch(client)
	if !f.all {
		resp, err := getFn(ctx, targetUserID, opts...)
		if err != nil {
			return err
		}
		return writeListsSinglePage(cmd, resp, outMode)
	}
	return runUserListsAll(cmd, eachFn, ctx, targetUserID, opts, outMode)
}

// resolveListsTargetUserID は owned/followed/memberships で位置引数から target userID を解決する (M33 D-10)。
//
// 引数が空 → GetUserMe で self
// 数値 ID → そのまま採用
// @username / URL → extractUserRef → GetUserByUsername で ID 解決 (2 API call)
func resolveListsTargetUserID(ctx context.Context, client listClient, explicitRef string) (string, error) {
	if explicitRef == "" {
		u, err := client.GetUserMe(ctx)
		if err != nil {
			return "", err
		}
		return u.ID, nil
	}
	value, isUsername, err := extractUserRef(explicitRef)
	if err != nil {
		return "", err
	}
	if !isUsername {
		return value, nil
	}
	resp, err := client.GetUserByUsername(ctx, value)
	if err != nil {
		return "", err
	}
	if resp == nil || resp.Data == nil || resp.Data.ID == "" {
		return "", fmt.Errorf("cli: GetUserByUsername returned empty data for %q", value)
	}
	return resp.Data.ID, nil
}

// =============================================================================
// --all 集約ヘルパ (3 種類のレスポンス型ごとに分離、M33 D-2 / D-3)
// =============================================================================

//nolint:revive // ctx を引数に取るが、引数順は cobra の RunE 流儀に合わせる
func runListTweetsAll(
	cmd *cobra.Command, client listClient, ctx context.Context,
	listID string, opts []xapi.ListPagedOption, outMode likedOutputMode,
) error {
	if outMode == likedOutputModeNDJSON {
		w := cmd.OutOrStdout()
		return client.EachListTweetsPage(ctx, listID, func(p *xapi.ListTweetsResponse) error {
			if p == nil {
				return nil
			}
			return writeNDJSONTweets(w, p.Data)
		}, opts...)
	}
	agg := &listTweetsAggregator{}
	if err := client.EachListTweetsPage(ctx, listID, agg.add, opts...); err != nil {
		return err
	}
	resp := agg.build()
	if outMode == likedOutputModeHuman {
		return writeListTweetsHuman(cmd, resp)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

//nolint:revive // ctx を引数に取るが、引数順は cobra の RunE 流儀に合わせる
func runListMembersAll(
	cmd *cobra.Command, client listClient, ctx context.Context,
	listID string, opts []xapi.ListPagedOption, outMode likedOutputMode,
) error {
	if outMode == likedOutputModeNDJSON {
		w := cmd.OutOrStdout()
		return client.EachListMembersPage(ctx, listID, func(p *xapi.UsersResponse) error {
			if p == nil {
				return nil
			}
			return writeNDJSONUsers(w, p.Data)
		}, opts...)
	}
	agg := &userAggregator{}
	if err := client.EachListMembersPage(ctx, listID, agg.add, opts...); err != nil {
		return err
	}
	resp := agg.build()
	if outMode == likedOutputModeHuman {
		return writeUsersHuman(cmd, resp)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

//nolint:revive // ctx を引数に取るが、引数順は cobra の RunE 流儀に合わせる
func runUserListsAll(
	cmd *cobra.Command, eachFn userListsEachFn, ctx context.Context,
	userID string, opts []xapi.ListPagedOption, outMode likedOutputMode,
) error {
	if outMode == likedOutputModeNDJSON {
		w := cmd.OutOrStdout()
		return eachFn(ctx, userID, func(p *xapi.ListsResponse) error {
			if p == nil {
				return nil
			}
			return writeNDJSONLists(w, p.Data)
		}, opts...)
	}
	agg := &listsAggregator{}
	if err := eachFn(ctx, userID, agg.add, opts...); err != nil {
		return err
	}
	resp := agg.build()
	if outMode == likedOutputModeHuman {
		return writeListsHuman(cmd, resp)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// =============================================================================
// aggregators (M33 D-2: List 配列 / Tweet 配列 を新規定義、User 配列は M32 再利用)
// =============================================================================

// listsAggregator は List 配列ページを集約する。
type listsAggregator struct {
	data   []xapi.List
	users  []xapi.User
	tweets []xapi.Tweet
}

func (a *listsAggregator) add(p *xapi.ListsResponse) error {
	if p == nil {
		return nil
	}
	a.data = append(a.data, p.Data...)
	a.users = append(a.users, p.Includes.Users...)
	a.tweets = append(a.tweets, p.Includes.Tweets...)
	return nil
}

func (a *listsAggregator) build() *xapi.ListsResponse {
	return &xapi.ListsResponse{
		Data: a.data,
		Includes: xapi.Includes{
			Users:  a.users,
			Tweets: a.tweets,
		},
		Meta: xapi.Meta{
			ResultCount: len(a.data),
			NextToken:   "",
		},
	}
}

// listTweetsAggregator は List Tweets ページを集約する (timelineAggregator の 4 つ目のコピー、M33 D-2)。
type listTweetsAggregator struct {
	data   []xapi.Tweet
	users  []xapi.User
	tweets []xapi.Tweet
}

func (a *listTweetsAggregator) add(p *xapi.ListTweetsResponse) error {
	if p == nil {
		return nil
	}
	a.data = append(a.data, p.Data...)
	a.users = append(a.users, p.Includes.Users...)
	a.tweets = append(a.tweets, p.Includes.Tweets...)
	return nil
}

func (a *listTweetsAggregator) build() *xapi.ListTweetsResponse {
	return &xapi.ListTweetsResponse{
		Data: a.data,
		Includes: xapi.Includes{
			Users:  a.users,
			Tweets: a.tweets,
		},
		Meta: xapi.Meta{
			ResultCount: len(a.data),
			NextToken:   "",
		},
	}
}

// =============================================================================
// 出力ヘルパ
// =============================================================================

// writeListResponseHumanOrJSON は GetList のレスポンスを書く。
func writeListResponseHumanOrJSON(cmd *cobra.Command, resp *xapi.ListResponse, noJSON bool) error {
	if noJSON {
		if resp == nil || resp.Data == nil {
			return nil
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), formatListHumanLine(*resp.Data))
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// writeListsResponseHumanOrJSON は GetPinnedLists のレスポンスを書く (paged ではない List 配列)。
func writeListsResponseHumanOrJSON(cmd *cobra.Command, resp *xapi.ListsResponse, noJSON bool) error {
	if noJSON {
		return writeListsHuman(cmd, resp)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// writeListTweetsSinglePage は --all=false 時の list tweets 出力。
func writeListTweetsSinglePage(cmd *cobra.Command, resp *xapi.ListTweetsResponse, outMode likedOutputMode) error {
	switch outMode {
	case likedOutputModeHuman:
		return writeListTweetsHuman(cmd, resp)
	case likedOutputModeNDJSON:
		if resp == nil {
			return nil
		}
		return writeNDJSONTweets(cmd.OutOrStdout(), resp.Data)
	default:
		return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
	}
}

// writeListsSinglePage は --all=false 時の owned/followed/memberships 出力。
func writeListsSinglePage(cmd *cobra.Command, resp *xapi.ListsResponse, outMode likedOutputMode) error {
	switch outMode {
	case likedOutputModeHuman:
		return writeListsHuman(cmd, resp)
	case likedOutputModeNDJSON:
		if resp == nil {
			return nil
		}
		return writeNDJSONLists(cmd.OutOrStdout(), resp.Data)
	default:
		return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
	}
}

// writeListTweetsHuman は List Tweets を human フォーマット (1 ツイート/行) で出力する。
// `formatTweetHumanLine` (M29) を再利用 (M33 D-14)。
func writeListTweetsHuman(cmd *cobra.Command, resp *xapi.ListTweetsResponse) error {
	if resp == nil || len(resp.Data) == 0 {
		return nil
	}
	for _, tw := range resp.Data {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatTweetHumanLine(tw)); err != nil {
			return err
		}
	}
	return nil
}

// writeListsHuman は List 配列を human フォーマット (1 List/行) で出力する。
func writeListsHuman(cmd *cobra.Command, resp *xapi.ListsResponse) error {
	if resp == nil || len(resp.Data) == 0 {
		return nil
	}
	for _, l := range resp.Data {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatListHumanLine(l)); err != nil {
			return err
		}
	}
	return nil
}

// formatListHumanLine は 1 List を 1 行に整形する (M33 D-16)。
func formatListHumanLine(l xapi.List) string {
	return fmt.Sprintf("id=%s\tname=%s\tprivate=%v\towner_id=%s",
		l.ID, l.Name, l.Private, l.OwnerID)
}

// writeNDJSONLists は List 配列を NDJSON 出力する (HTML escape off)。
func writeNDJSONLists(w io.Writer, lists []xapi.List) error {
	if len(lists) == 0 {
		return nil
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for i := range lists {
		if err := enc.Encode(lists[i]); err != nil {
			return err
		}
	}
	return nil
}
