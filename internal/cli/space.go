package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// space.go は `x space {get,by-ids,search,by-creator,tweets}` を提供する (M34)。
//
// 設計判断 (詳細は plans/x-m34-spaces-trends.md):
//   - 5 個別 factory + spaceClient interface + var-swap (M33 listClient パターン継承)
//   - Option 型 3 種類 (Lookup / Search / Tweets) で誤用防止 (M34 D-1)
//   - SearchSpaces は X API がページネーション非対応のため --all を提供しない (M34 D-2)
//   - by-ids / by-creator は cobra.NoArgs + --ids MarkFlagRequired (M34 D-5)
//   - aggregator generics 化は見送り、spaceTweetsAggregator をコピー定義 (M34 D-8)

const (
	spaceDefaultSpaceFields = "id,state,title,host_ids,creator_id,started_at,participant_count,scheduled_start,lang"
	spaceDefaultUserFields  = "username,name"
	spaceDefaultTweetFields = "id,text,author_id,created_at,entities,public_metrics,note_tweet,conversation_id"
	spaceDefaultTopicFields = "id,name,description"
	spaceDefaultExpansions  = ""
	spaceDefaultMediaFields = ""

	// spaceTweetsMaxResultsCap は GetSpaceTweets の per-page 上限 (X API 仕様)。
	spaceTweetsMaxResultsCap = 100
	// spaceSearchMaxResultsCap は SearchSpaces の per-call 上限 (X API 仕様)。
	spaceSearchMaxResultsCap = 100
	// spaceBatchMaxIDs は by-ids / by-creator の CSV 件数上限 (X API 仕様)。
	spaceBatchMaxIDs = 100
)

// spaceIDURLRE は Space 共有 URL の正規表現 (M34 D-15)。
// Space ID は base62 風の英数字混合 (例: 1OdJrXWaPVPGX)。
var spaceIDURLRE = regexp.MustCompile(`^https?://(?:x|twitter)\.com/i/spaces/([A-Za-z0-9]+)/?$`)

// spaceAlnumRE は Space ID の素朴な英数字判定。
var spaceAlnumRE = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// spaceClient は newSpace*Cmd 群が必要とする X API クライアントの最小インターフェイス (M34 D-10)。
type spaceClient interface {
	GetSpace(ctx context.Context, spaceID string, opts ...xapi.SpaceLookupOption) (*xapi.SpaceResponse, error)
	GetSpaces(ctx context.Context, ids []string, opts ...xapi.SpaceLookupOption) (*xapi.SpacesResponse, error)
	SearchSpaces(ctx context.Context, query string, opts ...xapi.SpaceSearchOption) (*xapi.SpacesResponse, error)
	GetSpacesByCreatorIDs(ctx context.Context, creatorIDs []string, opts ...xapi.SpaceLookupOption) (*xapi.SpacesResponse, error)
	GetSpaceTweets(ctx context.Context, spaceID string, opts ...xapi.SpaceTweetsOption) (*xapi.SpaceTweetsResponse, error)
	EachSpaceTweetsPage(ctx context.Context, spaceID string, fn func(*xapi.SpaceTweetsResponse) error, opts ...xapi.SpaceTweetsOption) error
}

// newSpaceClient は newSpace*Cmd が使う spaceClient の生成関数 (var-swap でテストから差し替え)。
var newSpaceClient = func(ctx context.Context, creds *config.Credentials) (spaceClient, error) {
	return xapi.NewClient(ctx, creds), nil
}

// newSpaceCmd は `x space` 親コマンドを生成する factory。
func newSpaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "space",
		Short: "look up / search X Spaces (live audio rooms)",
		Long: "Subcommands to look up, search, and inspect active X Spaces.\n" +
			"NOTE: Only active Spaces (live / scheduled) can be fetched; ended Spaces are not retrievable.",
	}
	cmd.AddCommand(newSpaceGetCmd())
	cmd.AddCommand(newSpaceByIDsCmd())
	cmd.AddCommand(newSpaceSearchCmd())
	cmd.AddCommand(newSpaceByCreatorCmd())
	cmd.AddCommand(newSpaceTweetsCmd())
	return cmd
}

// extractSpaceID は位置引数 (英数字 ID / `https://(x|twitter).com/i/spaces/<alnum>` URL) を解析して Space ID を返す (M34 D-15)。
func extractSpaceID(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("%w: space ID/URL is empty", ErrInvalidArgument)
	}
	// URL を先に試す (alnum マッチは URL 全体に成立しないため重複しない)
	if m := spaceIDURLRE.FindStringSubmatch(trimmed); len(m) == 2 {
		return m[1], nil
	}
	if spaceAlnumRE.MatchString(trimmed) {
		return trimmed, nil
	}
	return "", fmt.Errorf("%w: %q is not a Space ID or https://x.com/i/spaces/<ID> URL", ErrInvalidArgument, s)
}

// =============================================================================
// space get
// =============================================================================

func newSpaceGetCmd() *cobra.Command {
	var (
		spaceFields string
		expansions  string
		userFields  string
		topicFields string
		noJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "get <ID|URL>",
		Short: "look up a single Space by ID or URL",
		Long: "Look up a single Space by alphanumeric ID (e.g. 1OdJrXWaPVPGX) or URL\n" +
			"(https://x.com/i/spaces/<ID>). Active Spaces only.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spaceID, err := extractSpaceID(args[0])
			if err != nil {
				return err
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newSpaceClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := []xapi.SpaceLookupOption{}
			if fs := splitCSV(spaceFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSpaceLookupSpaceFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithSpaceLookupExpansions(fs...))
			}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSpaceLookupUserFields(fs...))
			}
			if fs := splitCSV(topicFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSpaceLookupTopicFields(fs...))
			}
			resp, err := client.GetSpace(ctx, spaceID, opts...)
			if err != nil {
				return err
			}
			if noJSON {
				if resp != nil && resp.Data != nil {
					_, err := fmt.Fprintln(cmd.OutOrStdout(), formatSpaceHumanLine(*resp.Data))
					return err
				}
				return nil
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
		},
	}
	cmd.Flags().StringVar(&spaceFields, "space-fields", spaceDefaultSpaceFields, "comma-separated space.fields")
	cmd.Flags().StringVar(&expansions, "expansions", spaceDefaultExpansions, "comma-separated expansions (e.g. host_ids,creator_id)")
	cmd.Flags().StringVar(&userFields, "user-fields", spaceDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&topicFields, "topic-fields", spaceDefaultTopicFields, "comma-separated topic.fields")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// =============================================================================
// space by-ids (--ids 必須フラグ、位置引数なし)
// =============================================================================

func newSpaceByIDsCmd() *cobra.Command {
	var (
		ids         string
		spaceFields string
		expansions  string
		userFields  string
		topicFields string
		noJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "by-ids",
		Short: "look up multiple Spaces by IDs (batch, 1..100)",
		Long: "Look up multiple Spaces in a single call. --ids accepts a comma-separated list (1..100 IDs).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			parts := splitCSV(ids)
			if len(parts) == 0 {
				return fmt.Errorf("%w: --ids must contain at least one Space ID", ErrInvalidArgument)
			}
			if len(parts) > spaceBatchMaxIDs {
				return fmt.Errorf("%w: --ids contains %d entries (max %d)", ErrInvalidArgument, len(parts), spaceBatchMaxIDs)
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newSpaceClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := buildSpaceLookupOpts(spaceFields, expansions, userFields, topicFields)
			resp, err := client.GetSpaces(ctx, parts, opts...)
			if err != nil {
				return err
			}
			return writeSpacesHumanOrJSON(cmd, resp, noJSON)
		},
	}
	// NOTE: cobra の MarkFlagRequired は exit code 1 (generic) を返してしまうため使わない。
	// RunE 内で splitCSV(ids) == 0 を ErrInvalidArgument で wrap して exit 2 を保証する (M34 D-5)。
	cmd.Flags().StringVar(&ids, "ids", "", "comma-separated Space IDs (1..100, required)")
	cmd.Flags().StringVar(&spaceFields, "space-fields", spaceDefaultSpaceFields, "comma-separated space.fields")
	cmd.Flags().StringVar(&expansions, "expansions", spaceDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&userFields, "user-fields", spaceDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&topicFields, "topic-fields", spaceDefaultTopicFields, "comma-separated topic.fields")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// =============================================================================
// space search
// =============================================================================

func newSpaceSearchCmd() *cobra.Command {
	var (
		state       string
		maxResults  int
		spaceFields string
		expansions  string
		userFields  string
		topicFields string
		noJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "search active Spaces by keyword",
		Long: "Search active Spaces by keyword.\n" +
			"NOTE: X API does not paginate this endpoint (--all is not provided).\n" +
			"--state accepts live / scheduled / all (X API default: all).\n" +
			"--max-results 1..100 (X API per-call, default 100).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(args[0])
			if query == "" {
				return fmt.Errorf("%w: query must be non-empty", ErrInvalidArgument)
			}
			if maxResults < 0 || maxResults > spaceSearchMaxResultsCap {
				return fmt.Errorf("%w: --max-results must be in 0..%d, got %d", ErrInvalidArgument, spaceSearchMaxResultsCap, maxResults)
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newSpaceClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := []xapi.SpaceSearchOption{}
			if maxResults > 0 {
				opts = append(opts, xapi.WithSpaceSearchMaxResults(maxResults))
			}
			if state != "" {
				opts = append(opts, xapi.WithSpaceSearchState(state))
			}
			if fs := splitCSV(spaceFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSpaceSearchSpaceFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithSpaceSearchExpansions(fs...))
			}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSpaceSearchUserFields(fs...))
			}
			if fs := splitCSV(topicFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSpaceSearchTopicFields(fs...))
			}
			resp, err := client.SearchSpaces(ctx, query, opts...)
			if err != nil {
				return err
			}
			return writeSpacesHumanOrJSON(cmd, resp, noJSON)
		},
	}
	cmd.Flags().StringVar(&state, "state", "", `filter by state ("live" / "scheduled" / "all", default all)`)
	cmd.Flags().IntVar(&maxResults, "max-results", 0, "max results per call (1..100, 0 = X API default)")
	cmd.Flags().StringVar(&spaceFields, "space-fields", spaceDefaultSpaceFields, "comma-separated space.fields")
	cmd.Flags().StringVar(&expansions, "expansions", spaceDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&userFields, "user-fields", spaceDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&topicFields, "topic-fields", spaceDefaultTopicFields, "comma-separated topic.fields")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// =============================================================================
// space by-creator
// =============================================================================

func newSpaceByCreatorCmd() *cobra.Command {
	var (
		ids         string
		spaceFields string
		expansions  string
		userFields  string
		topicFields string
		noJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "by-creator",
		Short: "look up Spaces created by specified user IDs (1..100)",
		Long: "Look up Spaces created by one or more user IDs. --ids accepts a comma-separated list (1..100).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			parts := splitCSV(ids)
			if len(parts) == 0 {
				return fmt.Errorf("%w: --ids must contain at least one user ID", ErrInvalidArgument)
			}
			if len(parts) > spaceBatchMaxIDs {
				return fmt.Errorf("%w: --ids contains %d entries (max %d)", ErrInvalidArgument, len(parts), spaceBatchMaxIDs)
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newSpaceClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := buildSpaceLookupOpts(spaceFields, expansions, userFields, topicFields)
			resp, err := client.GetSpacesByCreatorIDs(ctx, parts, opts...)
			if err != nil {
				return err
			}
			return writeSpacesHumanOrJSON(cmd, resp, noJSON)
		},
	}
	// NOTE: MarkFlagRequired は exit code 1 を返すため使わず、RunE で ErrInvalidArgument wrap する (M34 D-5)。
	cmd.Flags().StringVar(&ids, "ids", "", "comma-separated user IDs (1..100, required)")
	cmd.Flags().StringVar(&spaceFields, "space-fields", spaceDefaultSpaceFields, "comma-separated space.fields")
	cmd.Flags().StringVar(&expansions, "expansions", spaceDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&userFields, "user-fields", spaceDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&topicFields, "topic-fields", spaceDefaultTopicFields, "comma-separated topic.fields")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// =============================================================================
// space tweets
// =============================================================================

func newSpaceTweetsCmd() *cobra.Command {
	var (
		maxResults      int
		paginationToken string
		all             bool
		maxPages        int
		noJSON          bool
		ndjson          bool
		tweetFields     string
		userFields      string
		expansions      string
		mediaFields     string
	)
	cmd := &cobra.Command{
		Use:   "tweets <ID|URL>",
		Short: "fetch tweets associated with a Space",
		Long: "Fetch tweets from a Space via GET /2/spaces/:id/tweets.\n" +
			"--max-results 1..100, --all auto-follows pagination_token up to --max-pages (default 50).\n" +
			"--no-json prints one tweet per line, --ndjson streams tweets as line-delimited JSON.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spaceID, err := extractSpaceID(args[0])
			if err != nil {
				return err
			}
			if maxResults < 1 || maxResults > spaceTweetsMaxResultsCap {
				return fmt.Errorf("%w: --max-results must be in 1..%d, got %d", ErrInvalidArgument, spaceTweetsMaxResultsCap, maxResults)
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
			client, err := newSpaceClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := []xapi.SpaceTweetsOption{
				xapi.WithSpaceTweetsMaxResults(maxResults),
			}
			if paginationToken != "" {
				if all {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
						"warning: --pagination-token is ignored when --all is set")
				} else {
					opts = append(opts, xapi.WithSpaceTweetsPaginationToken(paginationToken))
				}
			}
			if fs := splitCSV(tweetFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSpaceTweetsTweetFields(fs...))
			}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSpaceTweetsUserFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithSpaceTweetsExpansions(fs...))
			}
			if fs := splitCSV(mediaFields); len(fs) > 0 {
				opts = append(opts, xapi.WithSpaceTweetsMediaFields(fs...))
			}
			if all {
				opts = append(opts, xapi.WithSpaceTweetsMaxPages(maxPages))
				return runSpaceTweetsAll(cmd, client, ctx, spaceID, opts, outMode)
			}
			resp, err := client.GetSpaceTweets(ctx, spaceID, opts...)
			if err != nil {
				return err
			}
			return writeSpaceTweetsSinglePage(cmd, resp, outMode)
		},
	}
	cmd.Flags().IntVar(&maxResults, "max-results", spaceTweetsMaxResultsCap, "max results per page (1..100)")
	cmd.Flags().StringVar(&paginationToken, "pagination-token", "", "resume using pagination_token")
	cmd.Flags().BoolVar(&all, "all", false, "auto-follow pagination_token until end or --max-pages")
	cmd.Flags().IntVar(&maxPages, "max-pages", 50, "max pages when --all is set")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	cmd.Flags().BoolVar(&ndjson, "ndjson", false, "output line-delimited JSON (one tweet per line)")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", spaceDefaultTweetFields, "comma-separated tweet.fields")
	cmd.Flags().StringVar(&userFields, "user-fields", spaceDefaultUserFields, "comma-separated user.fields")
	cmd.Flags().StringVar(&expansions, "expansions", spaceDefaultExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&mediaFields, "media-fields", spaceDefaultMediaFields, "comma-separated media.fields")
	return cmd
}

// =============================================================================
// 共通ヘルパ
// =============================================================================

// buildSpaceLookupOpts は by-ids / by-creator の共通フラグから Option スライスを組み立てる。
func buildSpaceLookupOpts(spaceFields, expansions, userFields, topicFields string) []xapi.SpaceLookupOption {
	opts := []xapi.SpaceLookupOption{}
	if fs := splitCSV(spaceFields); len(fs) > 0 {
		opts = append(opts, xapi.WithSpaceLookupSpaceFields(fs...))
	}
	if fs := splitCSV(expansions); len(fs) > 0 {
		opts = append(opts, xapi.WithSpaceLookupExpansions(fs...))
	}
	if fs := splitCSV(userFields); len(fs) > 0 {
		opts = append(opts, xapi.WithSpaceLookupUserFields(fs...))
	}
	if fs := splitCSV(topicFields); len(fs) > 0 {
		opts = append(opts, xapi.WithSpaceLookupTopicFields(fs...))
	}
	return opts
}

//nolint:revive // ctx を引数に取るが、引数順は cobra の RunE 流儀に合わせる
func runSpaceTweetsAll(
	cmd *cobra.Command, client spaceClient, ctx context.Context,
	spaceID string, opts []xapi.SpaceTweetsOption, outMode likedOutputMode,
) error {
	if outMode == likedOutputModeNDJSON {
		w := cmd.OutOrStdout()
		return client.EachSpaceTweetsPage(ctx, spaceID, func(p *xapi.SpaceTweetsResponse) error {
			if p == nil {
				return nil
			}
			return writeNDJSONTweets(w, p.Data)
		}, opts...)
	}
	agg := &spaceTweetsAggregator{}
	if err := client.EachSpaceTweetsPage(ctx, spaceID, agg.add, opts...); err != nil {
		return err
	}
	resp := agg.build()
	if outMode == likedOutputModeHuman {
		return writeSpaceTweetsHuman(cmd, resp)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// spaceTweetsAggregator は Space Tweets ページを集約する (M34 D-8、generics 化見送り)。
type spaceTweetsAggregator struct {
	data   []xapi.Tweet
	users  []xapi.User
	tweets []xapi.Tweet
}

func (a *spaceTweetsAggregator) add(p *xapi.SpaceTweetsResponse) error {
	if p == nil {
		return nil
	}
	a.data = append(a.data, p.Data...)
	a.users = append(a.users, p.Includes.Users...)
	a.tweets = append(a.tweets, p.Includes.Tweets...)
	return nil
}

func (a *spaceTweetsAggregator) build() *xapi.SpaceTweetsResponse {
	return &xapi.SpaceTweetsResponse{
		Data: a.data,
		Includes: xapi.Includes{
			Users:  a.users,
			Tweets: a.tweets,
		},
		Meta: xapi.Meta{
			ResultCount: len(a.data),
		},
	}
}

// writeSpaceTweetsSinglePage は --all=false 時の space tweets 出力。
func writeSpaceTweetsSinglePage(cmd *cobra.Command, resp *xapi.SpaceTweetsResponse, outMode likedOutputMode) error {
	switch outMode {
	case likedOutputModeHuman:
		return writeSpaceTweetsHuman(cmd, resp)
	case likedOutputModeNDJSON:
		if resp == nil {
			return nil
		}
		return writeNDJSONTweets(cmd.OutOrStdout(), resp.Data)
	default:
		return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
	}
}

// writeSpaceTweetsHuman は Space Tweets を human フォーマット (1 ツイート/行) で出力する。
// `formatTweetHumanLine` (M29) を再利用 (M34 D-11)。
func writeSpaceTweetsHuman(cmd *cobra.Command, resp *xapi.SpaceTweetsResponse) error {
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

// writeSpacesHumanOrJSON は SpacesResponse の出力 (JSON or human、M34 D-12)。
func writeSpacesHumanOrJSON(cmd *cobra.Command, resp *xapi.SpacesResponse, noJSON bool) error {
	if noJSON {
		if resp == nil || len(resp.Data) == 0 {
			return nil
		}
		for _, s := range resp.Data {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatSpaceHumanLine(s)); err != nil {
				return err
			}
		}
		return nil
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// formatSpaceHumanLine は 1 Space を 1 行に整形する (M34 D-12)。
func formatSpaceHumanLine(s xapi.Space) string {
	return fmt.Sprintf("id=%s\tstate=%s\ttitle=%s\tcreator_id=%s\tparticipants=%d\thost_ids=%s",
		s.ID, s.State, s.Title, s.CreatorID, s.ParticipantCount, strings.Join(s.HostIDs, ","))
}
