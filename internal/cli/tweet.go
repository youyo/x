package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// M29 で導入する `x tweet` サブコマンド群が共有するデフォルト fields 値 (D-10)。
// `[tweet]` セクションを config.toml に追加するのは将来 M (M30 以降) で再評価する。
const (
	tweetDefaultTweetFields = "id,text,author_id,created_at,entities,public_metrics,note_tweet,conversation_id"
	tweetDefaultExpansions  = "author_id"
	tweetDefaultUserFields  = "username,name"
	tweetDefaultMediaFields = ""

	// usersByTweetDefaultUserFields は liking-users / retweeted-by のデフォルト user.fields。
	usersByTweetDefaultUserFields = "username,name"

	// searchAPIMinMaxResults は X API `search/recent` の per-page 下限 (M30 D-1)。
	// CLI 側で n<10 のときは X API に 10 を送り、レスポンスを [:n] で truncate する。
	searchAPIMinMaxResults = 10
)

// tweetURLPathRE は `/status(?:es)?/<id>` を path から抽出する正規表現である (D-1)。
//
// 対応する URL バリアント (path のみ走査するため、クエリ・fragment は url.Parse で分離済み):
//   - https://x.com/<u>/status/<id>
//   - https://twitter.com/<u>/status/<id>
//   - https://mobile.twitter.com/<u>/status/<id>
//   - https://x.com/<u>/statuses/<id>    (旧表記)
//   - https://x.com/i/web/status/<id>
var tweetURLPathRE = regexp.MustCompile(`/status(?:es)?/(\d+)`)

// tweetNumericIDRE は引数全体が数値 ID 単独であるかを判定する正規表現である (D-1)。
var tweetNumericIDRE = regexp.MustCompile(`^\d+$`)

// extractTweetID は引数文字列 (URL / 数値 ID) から Tweet ID を抽出する (D-1)。
//
//   - 入力の前後を strings.TrimSpace
//   - 全 ASCII 数字なら ID としてそのまま返す (パススルー)
//   - そうでなければ url.Parse → Path 部分のみに regex 適用
//   - 上記いずれにも該当しなければ ErrInvalidArgument を wrap して返す
func extractTweetID(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("%w: tweet ID/URL is empty", ErrInvalidArgument)
	}
	if tweetNumericIDRE.MatchString(trimmed) {
		return trimmed, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: cannot parse tweet URL %q: %v", ErrInvalidArgument, s, err)
	}
	matches := tweetURLPathRE.FindStringSubmatch(parsed.Path)
	if len(matches) < 2 {
		return "", fmt.Errorf("%w: cannot extract tweet ID from %q", ErrInvalidArgument, s)
	}
	return matches[1], nil
}

// tweetClient は newTweet*Cmd 群が必要とする X API クライアントの最小インターフェイスである。
//
// 本番では *xapi.Client がすべてのメソッドを満たす。テストでは httptest.Server に紐付いた
// 別実装を newTweetClient 経由で差し替える (me / liked と同じ流儀)。
type tweetClient interface {
	GetTweet(ctx context.Context, id string, opts ...xapi.TweetLookupOption) (*xapi.TweetResponse, error)
	GetTweets(ctx context.Context, ids []string, opts ...xapi.TweetLookupOption) (*xapi.TweetsResponse, error)
	GetLikingUsers(ctx context.Context, id string, opts ...xapi.UsersByTweetOption) (*xapi.UsersByTweetResponse, error)
	GetRetweetedBy(ctx context.Context, id string, opts ...xapi.UsersByTweetOption) (*xapi.UsersByTweetResponse, error)
	GetQuoteTweets(ctx context.Context, id string, opts ...xapi.QuoteTweetsOption) (*xapi.QuoteTweetsResponse, error)
	SearchRecent(ctx context.Context, query string, opts ...xapi.SearchOption) (*xapi.SearchResponse, error)
	EachSearchPage(ctx context.Context, query string, fn func(*xapi.SearchResponse) error, opts ...xapi.SearchOption) error
}

// newTweetClient は内部利用の tweetClient 生成関数 (var-swap でテストから差し替え)。
var newTweetClient = func(ctx context.Context, creds *config.Credentials) (tweetClient, error) {
	return xapi.NewClient(ctx, creds), nil
}

// newTweetCmd は `x tweet` 親コマンドを生成する factory である。
//
// 親コマンド自体は help を表示するのみ。実体は各サブコマンド (get / liking-users /
// retweeted-by / quote-tweets) に委譲する。
func newTweetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tweet",
		Short: "manage X tweets",
		Long:  "Subcommands to look up tweets and their social signals from the X API v2.",
	}
	cmd.AddCommand(newTweetGetCmd())
	cmd.AddCommand(newTweetLikingUsersCmd())
	cmd.AddCommand(newTweetRetweetedByCmd())
	cmd.AddCommand(newTweetQuoteTweetsCmd())
	cmd.AddCommand(newTweetSearchCmd())
	cmd.AddCommand(newTweetThreadCmd())
	return cmd
}

// newTweetGetCmd は `x tweet get <ID|URL>` / `x tweet get --ids ID,ID,...` を生成する。
func newTweetGetCmd() *cobra.Command {
	var (
		ids         string
		tweetFields string
		expansions  string
		userFields  string
		mediaFields string
		noJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "get [ID|URL]",
		Short: "look up a tweet by ID or URL (or multiple via --ids)",
		Long: "Look up a single tweet by ID or X URL, or multiple tweets in a single batch via --ids.\n" +
			"--ids takes a comma-separated list (1..100 IDs).\n" +
			"--no-json prints one tweet per line as id=...\\tauthor=...\\tcreated=...\\ttext=...\n" +
			"For long-form tweets the note_tweet.text is shown in human output when non-empty.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if ids == "" && len(args) == 0 {
				return fmt.Errorf("%w: tweet get requires either an ID/URL argument or --ids", ErrInvalidArgument)
			}
			if ids != "" && len(args) > 0 {
				return fmt.Errorf("%w: tweet get accepts either an ID/URL argument or --ids, not both", ErrInvalidArgument)
			}

			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newTweetClient(ctx, creds)
			if err != nil {
				return err
			}

			opts := []xapi.TweetLookupOption{}
			if fs := splitCSV(tweetFields); len(fs) > 0 {
				opts = append(opts, xapi.WithGetTweetFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithGetTweetExpansions(fs...))
			}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithGetTweetUserFields(fs...))
			}
			if fs := splitCSV(mediaFields); len(fs) > 0 {
				opts = append(opts, xapi.WithGetTweetMediaFields(fs...))
			}

			if ids != "" {
				idList, err := parseTweetIDList(ids)
				if err != nil {
					return err
				}
				resp, err := client.GetTweets(ctx, idList, opts...)
				if err != nil {
					return err
				}
				return writeTweetsResponse(cmd, resp, noJSON)
			}
			id, err := extractTweetID(args[0])
			if err != nil {
				return err
			}
			resp, err := client.GetTweet(ctx, id, opts...)
			if err != nil {
				return err
			}
			return writeTweetResponse(cmd, resp, noJSON)
		},
	}
	cmd.Flags().StringVar(&ids, "ids", "", "comma-separated tweet IDs (1..100, mutually exclusive with positional arg)")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", tweetDefaultTweetFields, "comma-separated tweet.fields")
	cmd.Flags().StringVar(&expansions, "expansions", tweetDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&userFields, "user-fields", tweetDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&mediaFields, "media-fields", tweetDefaultMediaFields, "comma-separated media.fields")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// newTweetLikingUsersCmd は `x tweet liking-users <ID|URL>` を生成する。
func newTweetLikingUsersCmd() *cobra.Command {
	var (
		maxResults      int
		paginationToken string
		userFields      string
		expansions      string
		tweetFields     string
		noJSON          bool
	)
	cmd := &cobra.Command{
		Use:   "liking-users <ID|URL>",
		Short: "list users who liked a tweet",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if maxResults < 1 || maxResults > 100 {
				return fmt.Errorf("%w: --max-results must be in 1..100, got %d", ErrInvalidArgument, maxResults)
			}
			id, err := extractTweetID(args[0])
			if err != nil {
				return err
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newTweetClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := buildUsersByTweetOpts(maxResults, paginationToken, userFields, expansions, tweetFields)
			resp, err := client.GetLikingUsers(ctx, id, opts...)
			if err != nil {
				return err
			}
			return writeUsersByTweetResponse(cmd, resp, noJSON)
		},
	}
	cmd.Flags().IntVar(&maxResults, "max-results", 100, "max users per page (1..100)")
	cmd.Flags().StringVar(&paginationToken, "pagination-token", "", "resume from a previous page using next_token")
	cmd.Flags().StringVar(&userFields, "user-fields", usersByTweetDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&expansions, "expansions", "", "comma-separated expansions (e.g. pinned_tweet_id)")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", "", "comma-separated tweet.fields (used with expansions=pinned_tweet_id)")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// newTweetRetweetedByCmd は `x tweet retweeted-by <ID|URL>` を生成する。
func newTweetRetweetedByCmd() *cobra.Command {
	var (
		maxResults      int
		paginationToken string
		userFields      string
		expansions      string
		tweetFields     string
		noJSON          bool
	)
	cmd := &cobra.Command{
		Use:   "retweeted-by <ID|URL>",
		Short: "list users who retweeted a tweet",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if maxResults < 1 || maxResults > 100 {
				return fmt.Errorf("%w: --max-results must be in 1..100, got %d", ErrInvalidArgument, maxResults)
			}
			id, err := extractTweetID(args[0])
			if err != nil {
				return err
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newTweetClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := buildUsersByTweetOpts(maxResults, paginationToken, userFields, expansions, tweetFields)
			resp, err := client.GetRetweetedBy(ctx, id, opts...)
			if err != nil {
				return err
			}
			return writeUsersByTweetResponse(cmd, resp, noJSON)
		},
	}
	cmd.Flags().IntVar(&maxResults, "max-results", 100, "max users per page (1..100)")
	cmd.Flags().StringVar(&paginationToken, "pagination-token", "", "resume from a previous page using next_token")
	cmd.Flags().StringVar(&userFields, "user-fields", usersByTweetDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&expansions, "expansions", "", "comma-separated expansions")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", "", "comma-separated tweet.fields")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// newTweetQuoteTweetsCmd は `x tweet quote-tweets <ID|URL>` を生成する。
func newTweetQuoteTweetsCmd() *cobra.Command {
	var (
		maxResults      int
		paginationToken string
		exclude         string
		tweetFields     string
		expansions      string
		userFields      string
		mediaFields     string
		noJSON          bool
	)
	cmd := &cobra.Command{
		Use:   "quote-tweets <ID|URL>",
		Short: "list quote tweets of a tweet",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if maxResults < 1 || maxResults > 100 {
				return fmt.Errorf("%w: --max-results must be in 1..100, got %d", ErrInvalidArgument, maxResults)
			}
			id, err := extractTweetID(args[0])
			if err != nil {
				return err
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newTweetClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := []xapi.QuoteTweetsOption{
				xapi.WithQuoteTweetsMaxResults(maxResults),
			}
			if paginationToken != "" {
				opts = append(opts, xapi.WithQuoteTweetsPaginationToken(paginationToken))
			}
			if fs := splitCSV(exclude); len(fs) > 0 {
				opts = append(opts, xapi.WithQuoteTweetsExclude(fs...))
			}
			if fs := splitCSV(tweetFields); len(fs) > 0 {
				opts = append(opts, xapi.WithQuoteTweetsTweetFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithQuoteTweetsExpansions(fs...))
			}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithQuoteTweetsUserFields(fs...))
			}
			if fs := splitCSV(mediaFields); len(fs) > 0 {
				opts = append(opts, xapi.WithQuoteTweetsMediaFields(fs...))
			}
			resp, err := client.GetQuoteTweets(ctx, id, opts...)
			if err != nil {
				return err
			}
			return writeQuoteTweetsResponse(cmd, resp, noJSON)
		},
	}
	cmd.Flags().IntVar(&maxResults, "max-results", 100, "max quote tweets per page (1..100)")
	cmd.Flags().StringVar(&paginationToken, "pagination-token", "", "resume from a previous page using next_token")
	cmd.Flags().StringVar(&exclude, "exclude", "", "comma-separated exclude flags (retweets / replies)")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", tweetDefaultTweetFields, "comma-separated tweet.fields")
	cmd.Flags().StringVar(&expansions, "expansions", tweetDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&userFields, "user-fields", tweetDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&mediaFields, "media-fields", tweetDefaultMediaFields, "comma-separated media.fields")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// parseTweetIDList は CSV 入力をパースし、各要素を extractTweetID で正規化する。
// 1..100 件のレンジ検証もここで行う。
func parseTweetIDList(csv string) ([]string, error) {
	parts := splitCSV(csv)
	if len(parts) == 0 {
		return nil, fmt.Errorf("%w: --ids is empty", ErrInvalidArgument)
	}
	if len(parts) > 100 {
		return nil, fmt.Errorf("%w: --ids must have at most 100 entries, got %d", ErrInvalidArgument, len(parts))
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		id, err := extractTweetID(p)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// buildUsersByTweetOpts は liking-users / retweeted-by 共通の Option 組み立て。
func buildUsersByTweetOpts(maxResults int, paginationToken, userFields, expansions, tweetFields string) []xapi.UsersByTweetOption {
	opts := []xapi.UsersByTweetOption{
		xapi.WithUsersByTweetMaxResults(maxResults),
	}
	if paginationToken != "" {
		opts = append(opts, xapi.WithUsersByTweetPaginationToken(paginationToken))
	}
	if fs := splitCSV(userFields); len(fs) > 0 {
		opts = append(opts, xapi.WithUsersByTweetUserFields(fs...))
	}
	if fs := splitCSV(expansions); len(fs) > 0 {
		opts = append(opts, xapi.WithUsersByTweetExpansions(fs...))
	}
	if fs := splitCSV(tweetFields); len(fs) > 0 {
		opts = append(opts, xapi.WithUsersByTweetTweetFields(fs...))
	}
	return opts
}

// writeTweetResponse は GetTweet レスポンスを stdout に書く。
func writeTweetResponse(cmd *cobra.Command, resp *xapi.TweetResponse, noJSON bool) error {
	if noJSON {
		if resp == nil || resp.Data == nil {
			return nil
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), formatTweetHumanLine(*resp.Data))
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// writeTweetsResponse は GetTweets レスポンスを stdout に書く。
// partial error は stderr に warning として出す。
func writeTweetsResponse(cmd *cobra.Command, resp *xapi.TweetsResponse, noJSON bool) error {
	if resp != nil && len(resp.Errors) > 0 && noJSON {
		for _, e := range resp.Errors {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: tweet not available (id=%s): %s\n", e.ResourceID, e.Detail)
		}
	}
	if noJSON {
		if resp == nil {
			return nil
		}
		for _, tw := range resp.Data {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatTweetHumanLine(tw)); err != nil {
				return err
			}
		}
		return nil
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// writeUsersByTweetResponse は liking_users / retweeted_by のレスポンスを書く。
func writeUsersByTweetResponse(cmd *cobra.Command, resp *xapi.UsersByTweetResponse, noJSON bool) error {
	if noJSON {
		if resp == nil {
			return nil
		}
		for _, u := range resp.Data {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "id=%s\tusername=%s\tname=%s\n", u.ID, u.Username, u.Name); err != nil {
				return err
			}
		}
		return nil
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// writeQuoteTweetsResponse は quote_tweets のレスポンスを書く。
func writeQuoteTweetsResponse(cmd *cobra.Command, resp *xapi.QuoteTweetsResponse, noJSON bool) error {
	if noJSON {
		if resp == nil {
			return nil
		}
		for _, tw := range resp.Data {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatTweetHumanLine(tw)); err != nil {
				return err
			}
		}
		return nil
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// formatTweetHumanLine は単一 tweet を 1 行に整形する (liked.go と統一仕様)。
// note_tweet.text が非空ならそれを優先表示する (M29 D-3)。
func formatTweetHumanLine(tw xapi.Tweet) string {
	text := tw.Text
	if tw.NoteTweet != nil && tw.NoteTweet.Text != "" {
		text = tw.NoteTweet.Text
	}
	text = sanitizeLikedText(text)
	text = truncateRunes(text, likedHumanTextMaxRunes)
	return fmt.Sprintf("id=%s\tauthor=%s\tcreated=%s\ttext=%s", tw.ID, tw.AuthorID, tw.CreatedAt, text)
}

// =========================================================================
// M30: tweet search / tweet thread
// =========================================================================

// newTweetSearchCmd は `x tweet search <query>` を生成する (M30 T2)。
//
// X API v2 `GET /2/tweets/search/recent` (Basic tier 以上必須) のラッパ。
// liked list と同じ JST 系フラグ規約 (`--yesterday-jst > --since-jst > --start-time/--end-time`) と
// `--all` / `--ndjson` ストリーミングを実装する。
//
//nolint:gocyclo // CLI コマンドのフラグ処理は分岐が多いが手続き的に追える流れに揃えている
func newTweetSearchCmd() *cobra.Command {
	var (
		startTime       string
		endTime         string
		sinceJST        string
		yesterdayJST    bool
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
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "search recent tweets (past 7 days, Basic tier required)",
		Long: "Search recent tweets via X API v2 /2/tweets/search/recent (past 7 days, Basic tier or higher).\n" +
			"Free tier returns 403 (exit 4). `query` accepts X search operators (from: / lang: / conversation_id: etc.).\n" +
			"--yesterday-jst > --since-jst > --start-time/--end-time (same priority as liked list).\n" +
			"--max-results 1..9 is auto-corrected to 10 (X API per-page minimum) and the response is sliced.\n" +
			"--all + --max-results 1..9 is rejected (exit 2) because the per-page floor conflicts with the intent.\n" +
			"--no-json prints id=...\\tauthor=...\\tcreated=...\\ttext=... per tweet (note_tweet.text preferred).\n" +
			"--ndjson prints one JSON object per tweet (streamed when combined with --all).\n" +
			"--no-json and --ndjson are mutually exclusive.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(args[0])
			if query == "" {
				return fmt.Errorf("%w: tweet search requires a non-empty query", ErrInvalidArgument)
			}
			if maxResults < 1 || maxResults > 100 {
				return fmt.Errorf("%w: --max-results must be in 1..100, got %d", ErrInvalidArgument, maxResults)
			}
			if all && maxResults < searchAPIMinMaxResults {
				return fmt.Errorf("%w: --max-results 1..9 cannot be combined with --all (X API per-page minimum is 10)", ErrInvalidArgument)
			}
			outMode, err := decideOutputMode(noJSON, ndjson)
			if err != nil {
				return err
			}

			startT, endT, err := resolveSearchTimeWindow(startTime, endTime, sinceJST, yesterdayJST)
			if err != nil {
				return err
			}

			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newTweetClient(ctx, creds)
			if err != nil {
				return err
			}

			// max-results 下限補正 (D-1, --all は上で拒否済なので --all=false のみここに来る)。
			effectiveMaxResults := maxResults
			truncateTo := 0
			if maxResults < searchAPIMinMaxResults {
				effectiveMaxResults = searchAPIMinMaxResults
				truncateTo = maxResults
			}

			opts := []xapi.SearchOption{
				xapi.WithSearchMaxResults(effectiveMaxResults),
			}
			if !startT.IsZero() {
				opts = append(opts, xapi.WithSearchStartTime(startT))
			}
			if !endT.IsZero() {
				opts = append(opts, xapi.WithSearchEndTime(endT))
			}
			if paginationToken != "" {
				if all {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
						"warning: --pagination-token is ignored when --all is set")
				} else {
					opts = append(opts, xapi.WithSearchPaginationToken(paginationToken))
				}
			}
			if fs := splitCSV(tweetFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSearchTweetFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithSearchExpansions(fs...))
			}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSearchUserFields(fs...))
			}
			if fs := splitCSV(mediaFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSearchMediaFields(fs...))
			}
			if all {
				opts = append(opts, xapi.WithSearchMaxPages(maxPages))
			}

			if !all {
				resp, err := client.SearchRecent(ctx, query, opts...)
				if err != nil {
					return err
				}
				if truncateTo > 0 && resp != nil && len(resp.Data) > truncateTo {
					resp.Data = resp.Data[:truncateTo]
					resp.Meta.ResultCount = truncateTo
				}
				return writeSearchSinglePage(cmd, resp, outMode)
			}
			return runSearchAll(cmd, client, ctx, query, opts, outMode)
		},
	}
	cmd.Flags().StringVar(&startTime, "start-time", "", "earliest tweet time in RFC3339 (e.g. 2026-05-11T15:00:00Z)")
	cmd.Flags().StringVar(&endTime, "end-time", "", "latest tweet time in RFC3339")
	cmd.Flags().StringVar(&sinceJST, "since-jst", "", "JST date YYYY-MM-DD (overrides --start-time/--end-time)")
	cmd.Flags().BoolVar(&yesterdayJST, "yesterday-jst", false, "fetch the previous JST day (overrides --since-jst)")
	cmd.Flags().IntVar(&maxResults, "max-results", 100, "max tweets per page (1..100, X API floor is 10)")
	cmd.Flags().StringVar(&paginationToken, "pagination-token", "", "resume from a previous page using next_token")
	cmd.Flags().BoolVar(&all, "all", false, "auto-follow next_token until end or --max-pages")
	cmd.Flags().IntVar(&maxPages, "max-pages", 50, "max pages to fetch when --all is set")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	cmd.Flags().BoolVar(&ndjson, "ndjson", false, "output line-delimited JSON (one tweet per line)")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", tweetDefaultTweetFields, "comma-separated tweet.fields")
	cmd.Flags().StringVar(&expansions, "expansions", tweetDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&userFields, "user-fields", tweetDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&mediaFields, "media-fields", tweetDefaultMediaFields, "comma-separated media.fields")
	return cmd
}

// resolveSearchTimeWindow は search/thread 共通の時間窓決定ロジック (liked と同優先順位)。
// --yesterday-jst > --since-jst > --start-time/--end-time
func resolveSearchTimeWindow(startTime, endTime, sinceJST string, yesterdayJST bool) (start, end time.Time, err error) {
	var startT, endT time.Time
	if startTime != "" {
		t, err := time.Parse(time.RFC3339, startTime)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("%w: --start-time: %v", ErrInvalidArgument, err)
		}
		startT = t
	}
	if endTime != "" {
		t, err := time.Parse(time.RFC3339, endTime)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("%w: --end-time: %v", ErrInvalidArgument, err)
		}
		endT = t
	}
	switch {
	case yesterdayJST:
		s, e, err := yesterdayJSTRange(time.Now())
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		startT, endT = s, e
	case sinceJST != "":
		s, e, err := parseJSTDate(sinceJST)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		startT, endT = s, e
	}
	return startT, endT, nil
}

// writeSearchSinglePage は --all=false 時の出力を担う。
func writeSearchSinglePage(cmd *cobra.Command, resp *xapi.SearchResponse, outMode likedOutputMode) error {
	switch outMode {
	case likedOutputModeHuman:
		return writeSearchHuman(cmd, resp)
	case likedOutputModeNDJSON:
		if resp == nil {
			return nil
		}
		return writeNDJSONTweets(cmd.OutOrStdout(), resp.Data)
	default:
		return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
	}
}

// runSearchAll は --all=true 時の取得 + 出力を担う (NDJSON はストリーミング、それ以外は集約)。
//
// liked の runLikedAll と構造が同じだが、`*xapi.LikedTweetsResponse` と
// `*xapi.SearchResponse` で型が異なるためコピーで対応する (M30 D-10、M31 で 3 つ目が出たら generics 化検討)。
//
//nolint:revive // ctx を引数に取るが、引数順は cobra の RunE 流儀に合わせる
func runSearchAll(
	cmd *cobra.Command,
	client tweetClient,
	ctx context.Context,
	query string,
	opts []xapi.SearchOption,
	outMode likedOutputMode,
) error {
	if outMode == likedOutputModeNDJSON {
		w := cmd.OutOrStdout()
		return client.EachSearchPage(ctx, query, func(p *xapi.SearchResponse) error {
			if p == nil {
				return nil
			}
			return writeNDJSONTweets(w, p.Data)
		}, opts...)
	}
	agg := &searchAggregator{}
	if err := client.EachSearchPage(ctx, query, agg.add, opts...); err != nil {
		return err
	}
	resp := agg.build()
	if outMode == likedOutputModeHuman {
		return writeSearchHuman(cmd, resp)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// searchAggregator は EachSearchPage の callback で複数ページを集約する (likedAggregator の search 版、D-10)。
type searchAggregator struct {
	data   []xapi.Tweet
	users  []xapi.User
	tweets []xapi.Tweet
}

func (a *searchAggregator) add(p *xapi.SearchResponse) error {
	if p == nil {
		return nil
	}
	a.data = append(a.data, p.Data...)
	a.users = append(a.users, p.Includes.Users...)
	a.tweets = append(a.tweets, p.Includes.Tweets...)
	return nil
}

func (a *searchAggregator) build() *xapi.SearchResponse {
	return &xapi.SearchResponse{
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

// writeSearchHuman は --no-json 時の human 出力 (formatTweetHumanLine を再利用)。
func writeSearchHuman(cmd *cobra.Command, resp *xapi.SearchResponse) error {
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

// newTweetThreadCmd は `x tweet thread <ID|URL>` を生成する (M30 T3)。
//
// 2 段呼び出し:
//  1. GetTweet(id, tweet.fields=conversation_id,...) で会話 ID を取得
//  2. SearchRecent(query="conversation_id:<convID>") でスレッド構成ツイートを取得
//
// `--author-only` は CLI 層で `Tweet.AuthorID == root.AuthorID` でフィルタする (D-2、xapi の汎用性を維持)。
// JSON / Human 出力は時系列昇順にソート (D-4)。NDJSON はストリーミング順 (X API 新しい順) のまま。
//
//nolint:gocyclo // 2 段呼び出し + フィルタ + ソート + 出力モード分岐で分岐が多いが手続き的に追える
func newTweetThreadCmd() *cobra.Command {
	var (
		authorOnly      bool
		maxResults      int
		maxPages        int
		all             bool
		noJSON          bool
		ndjson          bool
		paginationToken string
		tweetFields     string
		expansions      string
		userFields      string
	)
	cmd := &cobra.Command{
		Use:   "thread <ID|URL>",
		Short: "fetch a tweet's conversation thread via search/recent",
		Long: "Resolve the root tweet's conversation_id, then query /2/tweets/search/recent for all replies.\n" +
			"--author-only keeps only tweets authored by the root tweet's author (filtered client-side).\n" +
			"JSON / human output is sorted by created_at ascending. NDJSON preserves X API order (newest first).\n" +
			"Note: this consumes 2 requests per call (one GetTweet + one or more search/recent pages).\n" +
			"The root tweet itself is included in search/recent results (X API behavior) unless older than 7 days.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := extractTweetID(args[0])
			if err != nil {
				return err
			}
			if maxResults < 1 || maxResults > 100 {
				return fmt.Errorf("%w: --max-results must be in 1..100, got %d", ErrInvalidArgument, maxResults)
			}
			if all && maxResults < searchAPIMinMaxResults {
				return fmt.Errorf("%w: --max-results 1..9 cannot be combined with --all (X API per-page minimum is 10)", ErrInvalidArgument)
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
			client, err := newTweetClient(ctx, creds)
			if err != nil {
				return err
			}

			// Step 1: GetTweet で conversation_id を取得。
			rootResp, err := client.GetTweet(ctx, id,
				xapi.WithGetTweetFields("id", "text", "author_id", "created_at", "conversation_id"),
				xapi.WithGetTweetExpansions("author_id"),
				xapi.WithGetTweetUserFields("username", "name"),
			)
			if err != nil {
				return err
			}
			if rootResp == nil || rootResp.Data == nil {
				return fmt.Errorf("cli: thread root tweet not returned (id=%s)", id)
			}
			if rootResp.Data.ConversationID == "" {
				// D-5: plain error → exit 1 (引数は正しいが API レスポンスが想定外)
				return fmt.Errorf("cli: thread root tweet missing conversation_id (id=%s)", id)
			}
			rootAuthor := rootResp.Data.AuthorID
			query := "conversation_id:" + rootResp.Data.ConversationID

			// max-results 下限補正 (--all=false のみ。--all はバリデーション済)
			effectiveMaxResults := maxResults
			truncateTo := 0
			if maxResults < searchAPIMinMaxResults {
				effectiveMaxResults = searchAPIMinMaxResults
				truncateTo = maxResults
			}

			opts := []xapi.SearchOption{
				xapi.WithSearchMaxResults(effectiveMaxResults),
			}
			if paginationToken != "" {
				if all {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
						"warning: --pagination-token is ignored when --all is set")
				} else {
					opts = append(opts, xapi.WithSearchPaginationToken(paginationToken))
				}
			}
			if fs := splitCSV(tweetFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSearchTweetFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithSearchExpansions(fs...))
			}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSearchUserFields(fs...))
			}
			if all {
				opts = append(opts, xapi.WithSearchMaxPages(maxPages))
			}

			filterFn := func(tw xapi.Tweet) bool {
				if !authorOnly {
					return true
				}
				return tw.AuthorID == rootAuthor
			}

			// NDJSON ストリーミング (--all 時は逐次、それ以外は単一ページを 1 回出力)。
			if outMode == likedOutputModeNDJSON {
				w := cmd.OutOrStdout()
				emit := func(p *xapi.SearchResponse) error {
					if p == nil {
						return nil
					}
					filtered := filterTweets(p.Data, filterFn)
					if truncateTo > 0 && len(filtered) > truncateTo {
						filtered = filtered[:truncateTo]
					}
					return writeNDJSONTweets(w, filtered)
				}
				if all {
					return client.EachSearchPage(ctx, query, emit, opts...)
				}
				resp, err := client.SearchRecent(ctx, query, opts...)
				if err != nil {
					return err
				}
				return emit(resp)
			}

			// JSON / Human (集約 → ソート → 出力)
			var aggregated *xapi.SearchResponse
			if all {
				agg := &searchAggregator{}
				if err := client.EachSearchPage(ctx, query, agg.add, opts...); err != nil {
					return err
				}
				aggregated = agg.build()
			} else {
				resp, err := client.SearchRecent(ctx, query, opts...)
				if err != nil {
					return err
				}
				if resp == nil {
					resp = &xapi.SearchResponse{}
				}
				aggregated = resp
			}
			aggregated.Data = filterTweets(aggregated.Data, filterFn)
			if truncateTo > 0 && len(aggregated.Data) > truncateTo {
				aggregated.Data = aggregated.Data[:truncateTo]
			}
			sortTweetsByCreatedAtAsc(aggregated.Data)
			aggregated.Meta.ResultCount = len(aggregated.Data)
			if all {
				aggregated.Meta.NextToken = ""
			}

			if outMode == likedOutputModeHuman {
				return writeSearchHuman(cmd, aggregated)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(aggregated)
		},
	}
	cmd.Flags().BoolVar(&authorOnly, "author-only", false, "keep only tweets authored by the root tweet's author")
	cmd.Flags().IntVar(&maxResults, "max-results", 100, "max tweets per page (1..100, X API floor is 10)")
	cmd.Flags().IntVar(&maxPages, "max-pages", 50, "max pages to fetch when --all is set")
	cmd.Flags().BoolVar(&all, "all", false, "auto-follow next_token until end or --max-pages")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	cmd.Flags().BoolVar(&ndjson, "ndjson", false, "output line-delimited JSON (one tweet per line)")
	cmd.Flags().StringVar(&paginationToken, "pagination-token", "", "resume from a previous page using next_token")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", tweetDefaultTweetFields, "comma-separated tweet.fields")
	cmd.Flags().StringVar(&expansions, "expansions", tweetDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&userFields, "user-fields", tweetDefaultUserFields, "comma-separated user.fields")
	return cmd
}

// filterTweets は keep == true の要素のみ残した新スライスを返す。
func filterTweets(tweets []xapi.Tweet, keep func(xapi.Tweet) bool) []xapi.Tweet {
	if keep == nil {
		return tweets
	}
	out := make([]xapi.Tweet, 0, len(tweets))
	for _, tw := range tweets {
		if keep(tw) {
			out = append(out, tw)
		}
	}
	return out
}

// sortTweetsByCreatedAtAsc は CreatedAt の ISO8601 文字列で昇順 (古い順) にソートする (D-4)。
// ISO8601 (RFC3339) の lexicographic 比較は時系列と一致する。
func sortTweetsByCreatedAtAsc(tweets []xapi.Tweet) {
	sort.SliceStable(tweets, func(i, j int) bool {
		return tweets[i].CreatedAt < tweets[j].CreatedAt
	})
}
