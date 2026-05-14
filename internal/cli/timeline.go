package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// timeline.go は `x timeline {tweets,mentions,home}` サブコマンド群を提供する (M31)。
//
// 3 サブコマンドは独立 factory で実装する (M31 D-13):
//   - newTimelineTweetsCmd:   /2/users/:id/tweets    (--user-id 公開、--exclude 公開)
//   - newTimelineMentionsCmd: /2/users/:id/mentions  (--user-id 公開、--exclude 非公開 D-9)
//   - newTimelineHomeCmd:     /2/users/:id/timelines/reverse_chronological
//                                                   (--user-id 非公開 D-4、--exclude 公開)
//
// max_results 下限の非対称性 (D-1):
//   - tweets/mentions: 5..100 → n<5 (--all=false) のとき X API に 5 を送り [:n] で truncate
//                                n<5 & --all=true → ErrInvalidArgument
//   - home (reverse_chronological): 1..100 → 補正不要
//
// 共通ロジック:
//   - JST 系時刻フラグ + RFC3339 → resolveSearchTimeWindow (M30 で共通化) を再利用
//   - --no-json/--ndjson 排他 → decideOutputMode (M11) を再利用
//   - --all aggregation → timelineAggregator (本ファイル、3 つ目の aggregator コピー、M33 で generics 化再評価 D-2)
//   - human / NDJSON 出力 → formatTweetHumanLine (M29) / writeNDJSONTweets (M11) を再利用

// timelineDefault* は timeline 系コマンド共通の既定 fields 値 (M29 D-10 継続)。
const (
	timelineDefaultTweetFields = "id,text,author_id,created_at,entities,public_metrics,note_tweet,conversation_id"
	timelineDefaultExpansions  = "author_id"
	timelineDefaultUserFields  = "username,name"
	timelineDefaultMediaFields = ""

	// timelineAPIMinMaxResults は tweets / mentions endpoint の per-page 下限 (X API 仕様、M31 D-1)。
	// home (reverse_chronological) は下限 1 で補正不要 (D-1)。
	timelineAPIMinMaxResults = 5
)

// timelineClient は newTimeline*Cmd が必要とする X API クライアントの最小インターフェイスである。
//
// 新規 interface (M31 D-12): tweetClient を肥大化させず、timeline コマンドのテストモックを
// 独立させる。GetUserMe は --user-id 自動解決 / home self 解決のため必須。
type timelineClient interface {
	GetUserMe(ctx context.Context, opts ...xapi.UserFieldsOption) (*xapi.User, error)
	GetUserTweets(ctx context.Context, userID string, opts ...xapi.TimelineOption) (*xapi.TimelineResponse, error)
	GetUserMentions(ctx context.Context, userID string, opts ...xapi.TimelineOption) (*xapi.TimelineResponse, error)
	GetHomeTimeline(ctx context.Context, userID string, opts ...xapi.TimelineOption) (*xapi.TimelineResponse, error)
	EachUserTweetsPage(ctx context.Context, userID string, fn func(*xapi.TimelineResponse) error, opts ...xapi.TimelineOption) error
	EachUserMentionsPage(ctx context.Context, userID string, fn func(*xapi.TimelineResponse) error, opts ...xapi.TimelineOption) error
	EachHomeTimelinePage(ctx context.Context, userID string, fn func(*xapi.TimelineResponse) error, opts ...xapi.TimelineOption) error
}

// newTimelineClient は内部利用の timelineClient 生成関数 (var-swap でテストから差し替え)。
var newTimelineClient = func(ctx context.Context, creds *config.Credentials) (timelineClient, error) {
	return xapi.NewClient(ctx, creds), nil
}

// newTimelineCmd は `x timeline` 親コマンドを生成する factory である。
//
// 親コマンド自体は help を表示するのみ。実体は 3 つのサブコマンド (tweets / mentions / home)
// に委譲する (M31 D-13)。
func newTimelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "timeline",
		Short: "fetch X timelines (user tweets / mentions / home)",
		Long: "Subcommands to fetch various X API v2 timelines.\n" +
			"`tweets` / `mentions` accept --user-id (default: self). " +
			"`home` always uses the authenticated user (reverse_chronological).",
	}
	cmd.AddCommand(newTimelineTweetsCmd())
	cmd.AddCommand(newTimelineMentionsCmd())
	cmd.AddCommand(newTimelineHomeCmd())
	return cmd
}

// timelineCommonFlags は 3 サブコマンドで共通のフラグセットを保持する構造体である。
//
// `--user-id` (tweets/mentions のみ) / `--exclude` (tweets/home のみ) は各 factory が個別に登録する。
type timelineCommonFlags struct {
	startTime       string
	endTime         string
	sinceJST        string
	yesterdayJST    bool
	sinceID         string
	untilID         string
	maxResults      int
	paginationToken string
	all             bool
	maxPages        int
	noJSON          bool
	ndjson          bool
	tweetFields     string
	expansions      string
	userFields      string
	mediaFields     string
}

// registerTimelineCommonFlags は 3 サブコマンドで共通のフラグを cobra コマンドに登録する。
//
// --user-id と --exclude は呼び出し側で個別に登録する (登録/非登録の差で D-4 / D-9 を実現)。
func registerTimelineCommonFlags(cmd *cobra.Command, f *timelineCommonFlags) {
	cmd.Flags().StringVar(&f.startTime, "start-time", "", "earliest tweet time in RFC3339 (e.g. 2026-05-11T15:00:00Z)")
	cmd.Flags().StringVar(&f.endTime, "end-time", "", "latest tweet time in RFC3339")
	cmd.Flags().StringVar(&f.sinceJST, "since-jst", "", "JST date YYYY-MM-DD (overrides --start-time/--end-time)")
	cmd.Flags().BoolVar(&f.yesterdayJST, "yesterday-jst", false, "fetch the previous JST day (overrides --since-jst)")
	cmd.Flags().StringVar(&f.sinceID, "since-id", "", "minimum tweet ID (returns tweets newer than this)")
	cmd.Flags().StringVar(&f.untilID, "until-id", "", "maximum tweet ID (returns tweets older than this)")
	cmd.Flags().IntVar(&f.maxResults, "max-results", 100, "max tweets per page (1..100)")
	cmd.Flags().StringVar(&f.paginationToken, "pagination-token", "", "resume from a previous page using next_token")
	cmd.Flags().BoolVar(&f.all, "all", false, "auto-follow next_token until end or --max-pages")
	cmd.Flags().IntVar(&f.maxPages, "max-pages", 50, "max pages to fetch when --all is set")
	cmd.Flags().BoolVar(&f.noJSON, "no-json", false, "output human-readable text instead of JSON")
	cmd.Flags().BoolVar(&f.ndjson, "ndjson", false, "output line-delimited JSON (one tweet per line)")
	cmd.Flags().StringVar(&f.tweetFields, "tweet-fields", timelineDefaultTweetFields, "comma-separated tweet.fields")
	cmd.Flags().StringVar(&f.expansions, "expansions", timelineDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&f.userFields, "user-fields", timelineDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&f.mediaFields, "media-fields", timelineDefaultMediaFields, "comma-separated media.fields")
}

// newTimelineTweetsCmd は `x timeline tweets [<ID>]` を生成する。
func newTimelineTweetsCmd() *cobra.Command {
	var (
		userID  string
		exclude string
	)
	f := &timelineCommonFlags{}
	cmd := &cobra.Command{
		Use:   "tweets [<ID>]",
		Short: "fetch a user's tweet timeline (defaults to self)",
		Long: "Fetch tweets posted by a user via GET /2/users/:id/tweets.\n" +
			"--user-id defaults to the authenticated user (self) via GetUserMe.\n" +
			"--max-results 1..4 is auto-corrected to 5 (X API per-page minimum) and the response is sliced.\n" +
			"--all + --max-results 1..4 is rejected (exit 2). --exclude takes 'retweets' and/or 'replies'.\n" +
			"--yesterday-jst > --since-jst > --start-time/--end-time (same priority as liked list).\n" +
			"--since-id / --until-id are independent of time window (can be combined, X API spec).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 位置引数 <ID> 指定時は --user-id を上書きする (両指定可だが位置引数が勝つ)。
			explicitID := userID
			if len(args) == 1 {
				explicitID = strings.TrimSpace(args[0])
			}
			return runTimelineGeneric(cmd, f, exclude, explicitID, timelineKindTweets)
		},
	}
	cmd.Flags().StringVar(&userID, "user-id", "", "target user ID (default: authenticated user)")
	cmd.Flags().StringVar(&exclude, "exclude", "", "comma-separated exclude flags (retweets / replies)")
	registerTimelineCommonFlags(cmd, f)
	return cmd
}

// newTimelineMentionsCmd は `x timeline mentions [<ID>]` を生成する。
//
// --exclude フラグは X API 仕様で mentions 非サポートのため登録しない (M31 D-9)。
func newTimelineMentionsCmd() *cobra.Command {
	var userID string
	f := &timelineCommonFlags{}
	cmd := &cobra.Command{
		Use:   "mentions [<ID>]",
		Short: "fetch tweets mentioning a user (defaults to self)",
		Long: "Fetch tweets mentioning a user via GET /2/users/:id/mentions.\n" +
			"--user-id defaults to the authenticated user (self) via GetUserMe.\n" +
			"--max-results 1..4 is auto-corrected to 5 (X API per-page minimum) and the response is sliced.\n" +
			"--all + --max-results 1..4 is rejected (exit 2).\n" +
			"NOTE: --exclude is NOT supported (X API does not accept it on mentions endpoint, M31 D-9).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 位置引数 <ID> 指定時は --user-id を上書きする (両指定可だが位置引数が勝つ)。
			explicitID := userID
			if len(args) == 1 {
				explicitID = strings.TrimSpace(args[0])
			}
			// mentions では exclude は未登録なので空のまま渡す。
			return runTimelineGeneric(cmd, f, "", explicitID, timelineKindMentions)
		},
	}
	cmd.Flags().StringVar(&userID, "user-id", "", "target user ID (default: authenticated user)")
	registerTimelineCommonFlags(cmd, f)
	return cmd
}

// newTimelineHomeCmd は `x timeline home` を生成する。
//
// reverse_chronological は X 仕様で認証ユーザー固定のため --user-id フラグを公開しない (M31 D-4)。
// GetUserMe で self を必ず解決する。max_results 下限は 1 のため補正不要 (D-1)。
func newTimelineHomeCmd() *cobra.Command {
	var exclude string
	f := &timelineCommonFlags{}
	cmd := &cobra.Command{
		Use:   "home",
		Short: "fetch the authenticated user's home timeline (reverse_chronological)",
		Long: "Fetch the authenticated user's home timeline via GET /2/users/:id/timelines/reverse_chronological.\n" +
			"The target user is always the authenticated user (X API spec); --user-id is intentionally NOT exposed.\n" +
			"--max-results 1..100 (no auto-correction; X API supports min=1 on this endpoint).\n" +
			"--exclude takes 'retweets' and/or 'replies'.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// home は --user-id を公開しないので explicit ID は常に空 → 必ず self 解決。
			return runTimelineGeneric(cmd, f, exclude, "", timelineKindHome)
		},
	}
	cmd.Flags().StringVar(&exclude, "exclude", "", "comma-separated exclude flags (retweets / replies)")
	registerTimelineCommonFlags(cmd, f)
	return cmd
}

// timelineKind は 3 endpoint を区別する内部 enum である (RunE の分岐に使う)。
type timelineKind int

const (
	timelineKindTweets timelineKind = iota
	timelineKindMentions
	timelineKindHome
)

// supportsLowerBoundCorrection は max_results<5 補正を適用するか否かを返す。
// tweets/mentions は下限 5 で補正対象、home は下限 1 で補正不要 (D-1)。
func (k timelineKind) supportsLowerBoundCorrection() bool {
	return k == timelineKindTweets || k == timelineKindMentions
}

// runTimelineGeneric は 3 サブコマンドの共通実行ロジックである。
//
// kind に応じて適切な xapi 呼び出しと max_results 補正を行う。
// exclude が空文字列なら --exclude フラグなし or 未指定として X API に渡さない。
//
//nolint:gocyclo // CLI コマンドのフラグ処理は分岐が多いが手続き的に追える流れに揃えている
func runTimelineGeneric(
	cmd *cobra.Command,
	f *timelineCommonFlags,
	exclude, explicitUserID string,
	kind timelineKind,
) error {
	// 1. バリデーション (liked / search と統一)。
	if f.maxResults < 1 || f.maxResults > 100 {
		return fmt.Errorf("%w: --max-results must be in 1..100, got %d", ErrInvalidArgument, f.maxResults)
	}
	// max_results<5 + --all は tweets/mentions のみ拒否 (home は下限 1 なので関係なし)。
	if kind.supportsLowerBoundCorrection() && f.all && f.maxResults < timelineAPIMinMaxResults {
		return fmt.Errorf("%w: --max-results 1..4 cannot be combined with --all (X API per-page minimum is 5)", ErrInvalidArgument)
	}
	outMode, err := decideOutputMode(f.noJSON, f.ndjson)
	if err != nil {
		return err
	}

	// 2. 時間窓決定 (--yesterday-jst > --since-jst > --start-time/--end-time, M30 で共通化済み)。
	startT, endT, err := resolveSearchTimeWindow(f.startTime, f.endTime, f.sinceJST, f.yesterdayJST)
	if err != nil {
		return err
	}

	// 3. 認証情報ロード + クライアント生成。
	creds, err := LoadCredentialsFromEnvOrFile()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	client, err := newTimelineClient(ctx, creds)
	if err != nil {
		return err
	}

	// 4. user ID 解決 (home は常に self、tweets/mentions は explicit → fallback self)。
	targetUserID := explicitUserID
	if targetUserID == "" {
		user, err := client.GetUserMe(ctx)
		if err != nil {
			return err
		}
		targetUserID = user.ID
	}

	// 5. max_results 補正 (tweets/mentions のみ、--all=false の単一ページ用)。
	//    --all=true は上で拒否済なので、ここに来るのは --all=false のみ。
	effectiveMaxResults := f.maxResults
	truncateTo := 0
	if kind.supportsLowerBoundCorrection() && f.maxResults < timelineAPIMinMaxResults {
		effectiveMaxResults = timelineAPIMinMaxResults
		truncateTo = f.maxResults
	}

	// 6. xapi.TimelineOption 組み立て。
	opts := []xapi.TimelineOption{
		xapi.WithTimelineMaxResults(effectiveMaxResults),
	}
	if !startT.IsZero() {
		opts = append(opts, xapi.WithTimelineStartTime(startT))
	}
	if !endT.IsZero() {
		opts = append(opts, xapi.WithTimelineEndTime(endT))
	}
	if f.sinceID != "" {
		opts = append(opts, xapi.WithTimelineSinceID(f.sinceID))
	}
	if f.untilID != "" {
		opts = append(opts, xapi.WithTimelineUntilID(f.untilID))
	}
	if f.paginationToken != "" {
		if f.all {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
				"warning: --pagination-token is ignored when --all is set")
		} else {
			opts = append(opts, xapi.WithTimelinePaginationToken(f.paginationToken))
		}
	}
	if fs := splitCSV(exclude); len(fs) > 0 {
		opts = append(opts, xapi.WithTimelineExclude(fs...))
	}
	if fs := splitCSV(f.tweetFields); len(fs) > 0 {
		opts = append(opts, xapi.WithTimelineTweetFields(fs...))
	}
	if fs := splitCSV(f.expansions); len(fs) > 0 {
		opts = append(opts, xapi.WithTimelineExpansions(fs...))
	}
	if fs := splitCSV(f.userFields); len(fs) > 0 {
		opts = append(opts, xapi.WithTimelineUserFields(fs...))
	}
	if fs := splitCSV(f.mediaFields); len(fs) > 0 {
		opts = append(opts, xapi.WithTimelineMediaFields(fs...))
	}
	if f.all {
		opts = append(opts, xapi.WithTimelineMaxPages(f.maxPages))
	}

	// 7. 取得 & 出力。
	getFn, eachFn := kind.dispatch(client)
	if !f.all {
		resp, err := getFn(ctx, targetUserID, opts...)
		if err != nil {
			return err
		}
		if truncateTo > 0 && resp != nil && len(resp.Data) > truncateTo {
			resp.Data = resp.Data[:truncateTo]
			resp.Meta.ResultCount = truncateTo
		}
		return writeTimelineSinglePage(cmd, resp, outMode)
	}
	return runTimelineAll(cmd, eachFn, ctx, targetUserID, opts, outMode)
}

// timelineGetFn / timelineEachFn は kind 別に xapi 呼び出しを選択するための関数型である。
type (
	timelineGetFn  func(context.Context, string, ...xapi.TimelineOption) (*xapi.TimelineResponse, error)
	timelineEachFn func(context.Context, string, func(*xapi.TimelineResponse) error, ...xapi.TimelineOption) error
)

// dispatch は kind から対応する Get / Each 関数を返す (D-8: eachFn パラメータ化で runTimelineAll を 1 関数に統一)。
func (k timelineKind) dispatch(c timelineClient) (timelineGetFn, timelineEachFn) {
	switch k {
	case timelineKindTweets:
		return c.GetUserTweets, c.EachUserTweetsPage
	case timelineKindMentions:
		return c.GetUserMentions, c.EachUserMentionsPage
	case timelineKindHome:
		return c.GetHomeTimeline, c.EachHomeTimelinePage
	}
	// 未到達 (kind は internal enum)。
	return c.GetUserTweets, c.EachUserTweetsPage
}

// writeTimelineSinglePage は --all=false 時の出力を担う共通ヘルパ。
func writeTimelineSinglePage(cmd *cobra.Command, resp *xapi.TimelineResponse, outMode likedOutputMode) error {
	switch outMode {
	case likedOutputModeHuman:
		return writeTimelineHuman(cmd, resp)
	case likedOutputModeNDJSON:
		if resp == nil {
			return nil
		}
		return writeNDJSONTweets(cmd.OutOrStdout(), resp.Data)
	default:
		return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
	}
}

// runTimelineAll は --all=true 時の取得 + 出力を担う共通ヘルパ (M31 D-8)。
//
// eachFn を引数で受けることで tweets / mentions / home 3 endpoint で 1 関数を再利用する。
//
//nolint:revive // ctx を引数に取るが、引数順は cobra の RunE 流儀に合わせる
func runTimelineAll(
	cmd *cobra.Command,
	eachFn timelineEachFn,
	ctx context.Context,
	userID string,
	opts []xapi.TimelineOption,
	outMode likedOutputMode,
) error {
	if outMode == likedOutputModeNDJSON {
		w := cmd.OutOrStdout()
		return eachFn(ctx, userID, func(p *xapi.TimelineResponse) error {
			if p == nil {
				return nil
			}
			return writeNDJSONTweets(w, p.Data)
		}, opts...)
	}
	agg := &timelineAggregator{}
	if err := eachFn(ctx, userID, agg.add, opts...); err != nil {
		return err
	}
	resp := agg.build()
	if outMode == likedOutputModeHuman {
		return writeTimelineHuman(cmd, resp)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// timelineAggregator は Each*TimelinePage の callback で複数ページを集約する。
//
// 3 つ目の aggregator コピー (M31 D-2): likedAggregator (M11) / searchAggregator (M30) と同形だが、
// `*xapi.LikedTweetsResponse` / `*xapi.SearchResponse` / `*xapi.TimelineResponse` で型が異なる
// ためコピーで対応する。M33 (4 つ目の `GetListTweets` 等) で generics 化を再評価する。
//
// 集約規則 (likedAggregator と同じ):
//   - Data: 全ページの Tweet を append (重複排除しない)
//   - Includes.Users / Includes.Tweets: 全ページの要素を append
//   - Meta: build() 時に再構築 (ResultCount = len(Data), NextToken = "")
type timelineAggregator struct {
	data   []xapi.Tweet
	users  []xapi.User
	tweets []xapi.Tweet
}

func (a *timelineAggregator) add(p *xapi.TimelineResponse) error {
	if p == nil {
		return nil
	}
	a.data = append(a.data, p.Data...)
	a.users = append(a.users, p.Includes.Users...)
	a.tweets = append(a.tweets, p.Includes.Tweets...)
	return nil
}

func (a *timelineAggregator) build() *xapi.TimelineResponse {
	return &xapi.TimelineResponse{
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

// writeTimelineHuman は --no-json 時の human 出力 (formatTweetHumanLine M29 を再利用)。
func writeTimelineHuman(cmd *cobra.Command, resp *xapi.TimelineResponse) error {
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
