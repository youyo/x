package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// user.go は `x user {get,search,following,followers,blocking,muting}` サブコマンド群を提供する (M32)。
//
// 設計判断:
//   - 6 個別 factory (newUserGetCmd / newUserSearchCmd / newUserFollowingCmd /
//     newUserFollowersCmd / newUserBlockingCmd / newUserMutingCmd)
//   - blocking / muting は self only (X API 仕様) のため --user-id フラグを公開しない (M32 D-5)
//   - extractUserRef で位置引数の ID / @username / URL を判別 (M32 D-4)
//   - --ids (数値) / --usernames (username 文字列) は別フラグ + positional 三者排他 (M32 D-8)
//   - 4 graph + 1 search で 1 つの userAggregator を共有 (User 配列集約、M32 D-2)

const (
	// userDefaultUserFields はユーザー系コマンド共通の既定 user.fields。
	userDefaultUserFields = "username,name,description,public_metrics,verified"
	// userDefaultExpansions / userDefaultTweetFields は基本未指定 (pinned_tweet_id 等を取りたい時のみユーザー指定)。
	userDefaultExpansions  = ""
	userDefaultTweetFields = ""

	// userBatchMaxIDs は --ids / --usernames の per-call 件数上限 (X API 仕様、M32 D-9)。
	userBatchMaxIDs = 100

	// userSearchMaxResultsCap / userGraphMaxResultsCap は X API 仕様の上限 (M32 D-1)。
	userSearchMaxResultsCap = 1000
	userGraphMaxResultsCap  = 1000
)

// usernameRE は X の screen-name 規則 (M32 D-4)。
var usernameRE = regexp.MustCompile(`^[A-Za-z0-9_]{1,15}$`)

// numericIDRE は純粋な数字 ID 判定用 (M32 D-4)。
var numericIDRE = regexp.MustCompile(`^\d+$`)

// userReservedURLPaths は extractUserRef が拒否する予約パス (X の機能/UI 用、M32 D-4)。
var userReservedURLPaths = map[string]struct{}{
	"i":             {},
	"home":          {},
	"explore":       {},
	"messages":      {},
	"notifications": {},
	"search":        {},
	"compose":       {},
	"settings":      {},
	"login":         {},
	"signup":        {},
	"intent":        {},
	"share":         {},
}

// userClient は newUser*Cmd 群が必要とする X API クライアントの最小インターフェイスである (M32 D-12)。
type userClient interface {
	GetUserMe(ctx context.Context, opts ...xapi.UserFieldsOption) (*xapi.User, error)
	GetUser(ctx context.Context, userID string, opts ...xapi.UserLookupOption) (*xapi.UserResponse, error)
	GetUsers(ctx context.Context, ids []string, opts ...xapi.UserLookupOption) (*xapi.UsersResponse, error)
	GetUserByUsername(ctx context.Context, username string, opts ...xapi.UserLookupOption) (*xapi.UserResponse, error)
	GetUsersByUsernames(ctx context.Context, usernames []string, opts ...xapi.UserLookupOption) (*xapi.UsersResponse, error)
	SearchUsers(ctx context.Context, query string, opts ...xapi.UserSearchOption) (*xapi.UsersResponse, error)
	GetFollowing(ctx context.Context, userID string, opts ...xapi.UserGraphOption) (*xapi.UsersResponse, error)
	GetFollowers(ctx context.Context, userID string, opts ...xapi.UserGraphOption) (*xapi.UsersResponse, error)
	GetBlocking(ctx context.Context, userID string, opts ...xapi.UserGraphOption) (*xapi.UsersResponse, error)
	GetMuting(ctx context.Context, userID string, opts ...xapi.UserGraphOption) (*xapi.UsersResponse, error)
	EachSearchUsersPage(ctx context.Context, query string, fn func(*xapi.UsersResponse) error, opts ...xapi.UserSearchOption) error
	EachFollowingPage(ctx context.Context, userID string, fn func(*xapi.UsersResponse) error, opts ...xapi.UserGraphOption) error
	EachFollowersPage(ctx context.Context, userID string, fn func(*xapi.UsersResponse) error, opts ...xapi.UserGraphOption) error
	EachBlockingPage(ctx context.Context, userID string, fn func(*xapi.UsersResponse) error, opts ...xapi.UserGraphOption) error
	EachMutingPage(ctx context.Context, userID string, fn func(*xapi.UsersResponse) error, opts ...xapi.UserGraphOption) error
}

// newUserClient は内部利用の userClient 生成関数 (var-swap でテストから差し替え)。
var newUserClient = func(ctx context.Context, creds *config.Credentials) (userClient, error) {
	return xapi.NewClient(ctx, creds), nil
}

// newUserCmd は `x user` 親コマンドを生成する factory である。
func newUserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "manage X users (lookup / search / graph)",
		Long: "Subcommands to look up users, search by keyword, and inspect follow/block/mute graphs.\n" +
			"`blocking` / `muting` are restricted to the authenticated user (X API spec).",
	}
	cmd.AddCommand(newUserGetCmd())
	cmd.AddCommand(newUserSearchCmd())
	cmd.AddCommand(newUserFollowingCmd())
	cmd.AddCommand(newUserFollowersCmd())
	cmd.AddCommand(newUserBlockingCmd())
	cmd.AddCommand(newUserMutingCmd())
	return cmd
}

// extractUserRef は位置引数 (ID / @username / URL) から (value, isUsername) を抽出する (M32 D-4)。
//
//   - 純粋数字 → (s, false, nil) — ID
//   - "@alice" → ("alice", true, nil)
//   - "https://x.com/alice" / "https://twitter.com/alice" → ("alice", true, nil)
//   - 予約パス (/home, /i/, etc) → ErrInvalidArgument
//   - 上記いずれにも当てはまらない → ErrInvalidArgument
//
// `--ids` / `--usernames` の各要素には適用しない (それぞれ別個のバリデーションで処理)。
func extractUserRef(s string) (value string, isUsername bool, err error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", false, fmt.Errorf("%w: user reference is empty", ErrInvalidArgument)
	}
	// 純粋数字 → ID
	if numericIDRE.MatchString(trimmed) {
		return trimmed, false, nil
	}
	// @alice → username
	if strings.HasPrefix(trimmed, "@") {
		uname := strings.TrimPrefix(trimmed, "@")
		if !usernameRE.MatchString(uname) {
			return "", false, fmt.Errorf("%w: invalid username %q", ErrInvalidArgument, trimmed)
		}
		return uname, true, nil
	}
	// URL → username (path 第 1 セグメント)
	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return "", false, fmt.Errorf("%w: cannot parse URL %q: %v", ErrInvalidArgument, s, err)
		}
		seg := strings.TrimPrefix(parsed.Path, "/")
		// 第 1 セグメントのみ取り出し (e.g. "alice", "alice/status/123" → "alice")
		if idx := strings.Index(seg, "/"); idx >= 0 {
			seg = seg[:idx]
		}
		if seg == "" {
			return "", false, fmt.Errorf("%w: URL %q has no username path segment", ErrInvalidArgument, s)
		}
		if _, reserved := userReservedURLPaths[seg]; reserved {
			return "", false, fmt.Errorf("%w: URL %q points to a reserved X path, not a user", ErrInvalidArgument, s)
		}
		if !usernameRE.MatchString(seg) {
			return "", false, fmt.Errorf("%w: URL %q does not contain a valid username", ErrInvalidArgument, s)
		}
		return seg, true, nil
	}
	// 上記いずれでもない
	return "", false, fmt.Errorf("%w: cannot interpret %q as user ID or @username or URL", ErrInvalidArgument, s)
}

// validateNumericID は --ids 要素のバリデーション (M32 D-4)。
func validateNumericID(s string) (string, error) {
	t := strings.TrimSpace(s)
	if !numericIDRE.MatchString(t) {
		return "", fmt.Errorf("%w: --ids: %q is not a numeric user ID", ErrInvalidArgument, s)
	}
	return t, nil
}

// validateUsername は --usernames 要素のバリデーション。`@` 前置剥がし + 厳格 regex (M32 D-4)。
func validateUsername(s string) (string, error) {
	t := strings.TrimPrefix(strings.TrimSpace(s), "@")
	if !usernameRE.MatchString(t) {
		return "", fmt.Errorf("%w: --usernames: %q is not a valid X username", ErrInvalidArgument, s)
	}
	return t, nil
}

// =============================================================================
// user get
// =============================================================================

func newUserGetCmd() *cobra.Command {
	var (
		ids         string
		usernames   string
		userFields  string
		expansions  string
		tweetFields string
		noJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "get [ID|@username|URL]",
		Short: "look up users by ID, @username, URL, or batch",
		Long: "Look up a user by ID / @username / X URL, or batch via --ids / --usernames.\n" +
			"--ids takes a comma-separated list of numeric IDs (1..100).\n" +
			"--usernames takes a comma-separated list of usernames (1..100, @ optional).\n" +
			"Positional / --ids / --usernames are mutually exclusive.\n" +
			"--no-json prints one user per line as id=...\\tusername=...\\tname=...\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 三者排他 (M32 D-8)
			modes := 0
			if len(args) > 0 {
				modes++
			}
			if ids != "" {
				modes++
			}
			if usernames != "" {
				modes++
			}
			switch modes {
			case 0:
				return fmt.Errorf("%w: user get requires ID/@username/URL argument, --ids, or --usernames", ErrInvalidArgument)
			case 1:
				// OK
			default:
				return fmt.Errorf("%w: user get accepts exactly one of positional argument, --ids, or --usernames", ErrInvalidArgument)
			}

			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newUserClient(ctx, creds)
			if err != nil {
				return err
			}

			opts := []xapi.UserLookupOption{}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithUserLookupUserFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithUserLookupExpansions(fs...))
			}
			if fs := splitCSV(tweetFields); len(fs) > 0 {
				opts = append(opts, xapi.WithUserLookupTweetFields(fs...))
			}

			switch {
			case ids != "":
				idList, err := parseAndValidate(ids, validateNumericID)
				if err != nil {
					return err
				}
				if len(idList) > userBatchMaxIDs {
					return fmt.Errorf("%w: --ids has %d entries (max %d)", ErrInvalidArgument, len(idList), userBatchMaxIDs)
				}
				resp, err := client.GetUsers(ctx, idList, opts...)
				if err != nil {
					return err
				}
				return writeUsersResponseHumanOrJSON(cmd, resp, noJSON)
			case usernames != "":
				nameList, err := parseAndValidate(usernames, validateUsername)
				if err != nil {
					return err
				}
				if len(nameList) > userBatchMaxIDs {
					return fmt.Errorf("%w: --usernames has %d entries (max %d)", ErrInvalidArgument, len(nameList), userBatchMaxIDs)
				}
				resp, err := client.GetUsersByUsernames(ctx, nameList, opts...)
				if err != nil {
					return err
				}
				return writeUsersResponseHumanOrJSON(cmd, resp, noJSON)
			default:
				value, isUsername, err := extractUserRef(args[0])
				if err != nil {
					return err
				}
				if isUsername {
					resp, err := client.GetUserByUsername(ctx, value, opts...)
					if err != nil {
						return err
					}
					return writeUserResponseHumanOrJSON(cmd, resp, noJSON)
				}
				resp, err := client.GetUser(ctx, value, opts...)
				if err != nil {
					return err
				}
				return writeUserResponseHumanOrJSON(cmd, resp, noJSON)
			}
		},
	}
	cmd.Flags().StringVar(&ids, "ids", "", "comma-separated numeric user IDs (1..100, mutually exclusive)")
	cmd.Flags().StringVar(&usernames, "usernames", "", "comma-separated usernames (1..100, @ optional, mutually exclusive)")
	cmd.Flags().StringVar(&userFields, "user-fields", userDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&expansions, "expansions", userDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", userDefaultTweetFields, "comma-separated tweet.fields")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// parseAndValidate は CSV 入力を要素ごとに validator にかけて結果を返す。
func parseAndValidate(csv string, validator func(string) (string, error)) ([]string, error) {
	parts := splitCSV(csv)
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: empty CSV input", ErrInvalidArgument)
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v, err := validator(p)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// =============================================================================
// user search
// =============================================================================

//nolint:gocyclo // CLI コマンドのフラグ処理は分岐が多いが手続き的に追える流れに揃えている
func newUserSearchCmd() *cobra.Command {
	var (
		maxResults      int
		paginationToken string
		all             bool
		maxPages        int
		noJSON          bool
		ndjson          bool
		userFields      string
		expansions      string
		tweetFields     string
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "search users by keyword",
		Long: "Search users via X API v2 /2/users/search.\n" +
			"--max-results 1..1000 (X API per-page, default 100).\n" +
			"--all auto-follows next_token (CLI flag --pagination-token maps to X API next_token).\n" +
			"--no-json prints one user per line, --ndjson streams users as line-delimited JSON.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(args[0])
			if query == "" {
				return fmt.Errorf("%w: user search requires a non-empty query", ErrInvalidArgument)
			}
			if maxResults < 1 || maxResults > userSearchMaxResultsCap {
				return fmt.Errorf("%w: --max-results must be in 1..%d, got %d", ErrInvalidArgument, userSearchMaxResultsCap, maxResults)
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
			client, err := newUserClient(ctx, creds)
			if err != nil {
				return err
			}

			opts := []xapi.UserSearchOption{
				xapi.WithUserSearchMaxResults(maxResults),
			}
			if paginationToken != "" {
				if all {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
						"warning: --pagination-token is ignored when --all is set")
				} else {
					opts = append(opts, xapi.WithUserSearchNextToken(paginationToken))
				}
			}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithUserSearchUserFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithUserSearchExpansions(fs...))
			}
			if fs := splitCSV(tweetFields); len(fs) > 0 {
				opts = append(opts, xapi.WithUserSearchTweetFields(fs...))
			}
			if all {
				opts = append(opts, xapi.WithUserSearchMaxPages(maxPages))
			}

			if !all {
				resp, err := client.SearchUsers(ctx, query, opts...)
				if err != nil {
					return err
				}
				return writeUsersListSinglePage(cmd, resp, outMode)
			}
			return runUserSearchAll(cmd, client, ctx, query, opts, outMode)
		},
	}
	cmd.Flags().IntVar(&maxResults, "max-results", 100, "max users per page (1..1000)")
	cmd.Flags().StringVar(&paginationToken, "pagination-token", "", "resume from a previous page (maps to X API next_token)")
	cmd.Flags().BoolVar(&all, "all", false, "auto-follow next_token until end or --max-pages")
	cmd.Flags().IntVar(&maxPages, "max-pages", 50, "max pages to fetch when --all is set")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	cmd.Flags().BoolVar(&ndjson, "ndjson", false, "output line-delimited JSON (one user per line)")
	cmd.Flags().StringVar(&userFields, "user-fields", userDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&expansions, "expansions", userDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", userDefaultTweetFields, "comma-separated tweet.fields")
	return cmd
}

//nolint:revive // ctx を引数に取るが、引数順は cobra の RunE 流儀に合わせる
func runUserSearchAll(
	cmd *cobra.Command,
	client userClient,
	ctx context.Context,
	query string,
	opts []xapi.UserSearchOption,
	outMode likedOutputMode,
) error {
	if outMode == likedOutputModeNDJSON {
		w := cmd.OutOrStdout()
		return client.EachSearchUsersPage(ctx, query, func(p *xapi.UsersResponse) error {
			if p == nil {
				return nil
			}
			return writeNDJSONUsers(w, p.Data)
		}, opts...)
	}
	agg := &userAggregator{}
	if err := client.EachSearchUsersPage(ctx, query, agg.add, opts...); err != nil {
		return err
	}
	resp := agg.build()
	if outMode == likedOutputModeHuman {
		return writeUsersHuman(cmd, resp)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// =============================================================================
// user following / followers / blocking / muting (graph endpoints)
// =============================================================================

// userGraphKind は 4 graph endpoint を区別する内部 enum である。
type userGraphKind int

const (
	userGraphKindFollowing userGraphKind = iota
	userGraphKindFollowers
	userGraphKindBlocking
	userGraphKindMuting
)

// isSelfOnly は X API 仕様で self のみ参照可能な endpoint かを返す (M32 D-5)。
func (k userGraphKind) isSelfOnly() bool {
	return k == userGraphKindBlocking || k == userGraphKindMuting
}

// dispatch は kind から対応する Get / Each 関数を返す。
type (
	userGraphGetFn  func(context.Context, string, ...xapi.UserGraphOption) (*xapi.UsersResponse, error)
	userGraphEachFn func(context.Context, string, func(*xapi.UsersResponse) error, ...xapi.UserGraphOption) error
)

func (k userGraphKind) dispatch(c userClient) (userGraphGetFn, userGraphEachFn) {
	switch k {
	case userGraphKindFollowing:
		return c.GetFollowing, c.EachFollowingPage
	case userGraphKindFollowers:
		return c.GetFollowers, c.EachFollowersPage
	case userGraphKindBlocking:
		return c.GetBlocking, c.EachBlockingPage
	case userGraphKindMuting:
		return c.GetMuting, c.EachMutingPage
	}
	// 未到達。
	return c.GetFollowing, c.EachFollowingPage
}

// userGraphFlags は graph 系サブコマンドで共通のフラグセット。
type userGraphFlags struct {
	maxResults      int
	paginationToken string
	all             bool
	maxPages        int
	noJSON          bool
	ndjson          bool
	userFields      string
	expansions      string
	tweetFields     string
}

// registerUserGraphCommonFlags は graph 系共通フラグを登録する。
// --user-id は呼び出し側で個別に登録する (登録/非登録で D-5 を実現)。
func registerUserGraphCommonFlags(cmd *cobra.Command, f *userGraphFlags) {
	cmd.Flags().IntVar(&f.maxResults, "max-results", 100, "max users per page (1..1000)")
	cmd.Flags().StringVar(&f.paginationToken, "pagination-token", "", "resume from a previous page using pagination_token")
	cmd.Flags().BoolVar(&f.all, "all", false, "auto-follow pagination_token until end or --max-pages")
	cmd.Flags().IntVar(&f.maxPages, "max-pages", 50, "max pages to fetch when --all is set")
	cmd.Flags().BoolVar(&f.noJSON, "no-json", false, "output human-readable text instead of JSON")
	cmd.Flags().BoolVar(&f.ndjson, "ndjson", false, "output line-delimited JSON (one user per line)")
	cmd.Flags().StringVar(&f.userFields, "user-fields", userDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&f.expansions, "expansions", userDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&f.tweetFields, "tweet-fields", userDefaultTweetFields, "comma-separated tweet.fields")
}

func newUserFollowingCmd() *cobra.Command {
	var userID string
	f := &userGraphFlags{}
	cmd := &cobra.Command{
		Use:   "following [<ID|@username|URL>]",
		Short: "list users that a user follows (defaults to self)",
		Long: "Fetch users that a user follows via GET /2/users/:id/following.\n" +
			"--user-id defaults to the authenticated user via GetUserMe.\n" +
			"@username / URL positional args are resolved via GetUserByUsername first (extra API call).\n" +
			"--max-results 1..1000 (X API per-page).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit := strings.TrimSpace(userID)
			if len(args) == 1 {
				explicit = strings.TrimSpace(args[0])
			}
			return runUserGraph(cmd, f, explicit, userGraphKindFollowing)
		},
	}
	cmd.Flags().StringVar(&userID, "user-id", "", "target user ID (default: authenticated user)")
	registerUserGraphCommonFlags(cmd, f)
	return cmd
}

func newUserFollowersCmd() *cobra.Command {
	var userID string
	f := &userGraphFlags{}
	cmd := &cobra.Command{
		Use:   "followers [<ID|@username|URL>]",
		Short: "list followers of a user (defaults to self)",
		Long: "Fetch followers of a user via GET /2/users/:id/followers.\n" +
			"--user-id defaults to the authenticated user via GetUserMe.\n" +
			"@username / URL positional args are resolved via GetUserByUsername first (extra API call).\n" +
			"--max-results 1..1000 (X API per-page).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			explicit := strings.TrimSpace(userID)
			if len(args) == 1 {
				explicit = strings.TrimSpace(args[0])
			}
			return runUserGraph(cmd, f, explicit, userGraphKindFollowers)
		},
	}
	cmd.Flags().StringVar(&userID, "user-id", "", "target user ID (default: authenticated user)")
	registerUserGraphCommonFlags(cmd, f)
	return cmd
}

func newUserBlockingCmd() *cobra.Command {
	f := &userGraphFlags{}
	cmd := &cobra.Command{
		Use:   "blocking",
		Short: "list users blocked by the authenticated user",
		Long: "Fetch the authenticated user's block list via GET /2/users/:id/blocking.\n" +
			"X API spec restricts this endpoint to the authenticated user; --user-id is intentionally NOT exposed.\n" +
			"--max-results 1..1000 (X API per-page).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUserGraph(cmd, f, "", userGraphKindBlocking)
		},
	}
	registerUserGraphCommonFlags(cmd, f)
	return cmd
}

func newUserMutingCmd() *cobra.Command {
	f := &userGraphFlags{}
	cmd := &cobra.Command{
		Use:   "muting",
		Short: "list users muted by the authenticated user",
		Long: "Fetch the authenticated user's mute list via GET /2/users/:id/muting.\n" +
			"X API spec restricts this endpoint to the authenticated user; --user-id is intentionally NOT exposed.\n" +
			"--max-results 1..1000 (X API per-page).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUserGraph(cmd, f, "", userGraphKindMuting)
		},
	}
	registerUserGraphCommonFlags(cmd, f)
	return cmd
}

//nolint:gocyclo // 共通実行ロジック、分岐が多いが手続き的に追える流れ
func runUserGraph(
	cmd *cobra.Command,
	f *userGraphFlags,
	explicitRef string,
	kind userGraphKind,
) error {
	if f.maxResults < 1 || f.maxResults > userGraphMaxResultsCap {
		return fmt.Errorf("%w: --max-results must be in 1..%d, got %d", ErrInvalidArgument, userGraphMaxResultsCap, f.maxResults)
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
	client, err := newUserClient(ctx, creds)
	if err != nil {
		return err
	}

	// userID 解決 (M32 D-5 / D-7)。
	//   - blocking / muting: 必ず self (GetUserMe)
	//   - following / followers: explicitRef があれば判別、なければ self
	targetUserID, err := resolveGraphTargetUserID(ctx, client, kind, explicitRef)
	if err != nil {
		return err
	}

	opts := []xapi.UserGraphOption{
		xapi.WithUserGraphMaxResults(f.maxResults),
	}
	if f.paginationToken != "" {
		if f.all {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
				"warning: --pagination-token is ignored when --all is set")
		} else {
			opts = append(opts, xapi.WithUserGraphPaginationToken(f.paginationToken))
		}
	}
	if fs := splitCSV(f.userFields); len(fs) > 0 {
		opts = append(opts, xapi.WithUserGraphUserFields(fs...))
	}
	if fs := splitCSV(f.expansions); len(fs) > 0 {
		opts = append(opts, xapi.WithUserGraphExpansions(fs...))
	}
	if fs := splitCSV(f.tweetFields); len(fs) > 0 {
		opts = append(opts, xapi.WithUserGraphTweetFields(fs...))
	}
	if f.all {
		opts = append(opts, xapi.WithUserGraphMaxPages(f.maxPages))
	}

	getFn, eachFn := kind.dispatch(client)
	if !f.all {
		resp, err := getFn(ctx, targetUserID, opts...)
		if err != nil {
			return err
		}
		return writeUsersListSinglePage(cmd, resp, outMode)
	}
	return runUserGraphAll(cmd, eachFn, ctx, targetUserID, opts, outMode)
}

// resolveGraphTargetUserID は kind / explicitRef から実際の userID を返す。
//
//   - blocking / muting → 常に GetUserMe (D-5)
//   - 他: explicitRef が数値 ID なら採用、@username/URL なら GetUserByUsername で解決 (D-7)
//   - explicitRef が空 → GetUserMe
func resolveGraphTargetUserID(ctx context.Context, client userClient, kind userGraphKind, explicitRef string) (string, error) {
	if kind.isSelfOnly() {
		u, err := client.GetUserMe(ctx)
		if err != nil {
			return "", err
		}
		return u.ID, nil
	}
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
	// username → ID 解決
	resp, err := client.GetUserByUsername(ctx, value)
	if err != nil {
		return "", err
	}
	if resp == nil || resp.Data == nil || resp.Data.ID == "" {
		return "", fmt.Errorf("cli: GetUserByUsername returned empty data for %q", value)
	}
	return resp.Data.ID, nil
}

//nolint:revive // ctx を引数に取るが、引数順は cobra の RunE 流儀に合わせる
func runUserGraphAll(
	cmd *cobra.Command,
	eachFn userGraphEachFn,
	ctx context.Context,
	userID string,
	opts []xapi.UserGraphOption,
	outMode likedOutputMode,
) error {
	if outMode == likedOutputModeNDJSON {
		w := cmd.OutOrStdout()
		return eachFn(ctx, userID, func(p *xapi.UsersResponse) error {
			if p == nil {
				return nil
			}
			return writeNDJSONUsers(w, p.Data)
		}, opts...)
	}
	agg := &userAggregator{}
	if err := eachFn(ctx, userID, agg.add, opts...); err != nil {
		return err
	}
	resp := agg.build()
	if outMode == likedOutputModeHuman {
		return writeUsersHuman(cmd, resp)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// =============================================================================
// userAggregator / 出力ヘルパ
// =============================================================================

// userAggregator は Each*Page (search / graph) の callback で複数ページを集約する (M32 D-2)。
//
// liked/search/timeline (3 つの aggregator) は []Tweet 配列を保持するが、本 aggregator は []User を保持する。
// 型が異なるためコピペではなく新規型として定義する。M33 (GetListTweets で 4 つ目の Tweet aggregator)
// で generics 化判断を再評価する (M31 D-2 の約束はそこに繰り越し)。
//
// 集約規則:
//   - Data: 全ページの User を append (重複排除しない)
//   - Includes.Users / Includes.Tweets: 全ページの要素を append
//   - Meta: build() 時に再構築 (ResultCount = len(Data), NextToken = "")
type userAggregator struct {
	data   []xapi.User
	users  []xapi.User
	tweets []xapi.Tweet
}

func (a *userAggregator) add(p *xapi.UsersResponse) error {
	if p == nil {
		return nil
	}
	a.data = append(a.data, p.Data...)
	a.users = append(a.users, p.Includes.Users...)
	a.tweets = append(a.tweets, p.Includes.Tweets...)
	return nil
}

func (a *userAggregator) build() *xapi.UsersResponse {
	return &xapi.UsersResponse{
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

// writeUserResponseHumanOrJSON は GetUser / GetUserByUsername のレスポンスを書く。
func writeUserResponseHumanOrJSON(cmd *cobra.Command, resp *xapi.UserResponse, noJSON bool) error {
	if resp != nil && len(resp.Errors) > 0 && noJSON {
		for _, e := range resp.Errors {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: user not available (id=%s): %s\n", e.ResourceID, e.Detail)
		}
	}
	if noJSON {
		if resp == nil || resp.Data == nil {
			return nil
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), formatUserHumanLine(*resp.Data))
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// writeUsersResponseHumanOrJSON は GetUsers / GetUsersByUsernames のレスポンスを書く。
func writeUsersResponseHumanOrJSON(cmd *cobra.Command, resp *xapi.UsersResponse, noJSON bool) error {
	if resp != nil && len(resp.Errors) > 0 && noJSON {
		for _, e := range resp.Errors {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: user not available (value=%s): %s\n", e.Value, e.Detail)
		}
	}
	if noJSON {
		if resp == nil {
			return nil
		}
		for _, u := range resp.Data {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatUserHumanLine(u)); err != nil {
				return err
			}
		}
		return nil
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// writeUsersListSinglePage は --all=false 時の出力を担う (search / graph 共通)。
func writeUsersListSinglePage(cmd *cobra.Command, resp *xapi.UsersResponse, outMode likedOutputMode) error {
	switch outMode {
	case likedOutputModeHuman:
		return writeUsersHuman(cmd, resp)
	case likedOutputModeNDJSON:
		if resp == nil {
			return nil
		}
		return writeNDJSONUsers(cmd.OutOrStdout(), resp.Data)
	default:
		return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
	}
}

// writeUsersHuman は --no-json 時の human 出力 (ユーザー配列を 1 行/ユーザー)。
func writeUsersHuman(cmd *cobra.Command, resp *xapi.UsersResponse) error {
	if resp == nil || len(resp.Data) == 0 {
		return nil
	}
	for _, u := range resp.Data {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatUserHumanLine(u)); err != nil {
			return err
		}
	}
	return nil
}

// formatUserHumanLine は 1 ユーザーを 1 行に整形する。
func formatUserHumanLine(u xapi.User) string {
	return fmt.Sprintf("id=%s\tusername=%s\tname=%s", u.ID, u.Username, u.Name)
}

// writeNDJSONUsers は User 配列を NDJSON 出力する (HTML escape off)。
func writeNDJSONUsers(w io.Writer, users []xapi.User) error {
	if len(users) == 0 {
		return nil
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for i := range users {
		if err := enc.Encode(users[i]); err != nil {
			return err
		}
	}
	return nil
}
