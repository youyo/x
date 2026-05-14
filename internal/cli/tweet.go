package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

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
