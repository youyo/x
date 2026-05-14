package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// trends.go は `x trends {get,personal}` を提供する (M34)。
//
// 設計判断 (詳細は plans/x-m34-spaces-trends.md):
//   - 2 endpoint で X API のパラメータ名が異なるため Option 型を分離 (M34 D-4)
//   - personal は --user-id を公開しない (X API 認証ユーザー固定、M34 D-7 / D-17)
//   - Trend DTO は両 endpoint の union 構造体 (M34 D-3)

const (
	trendsDefaultWoeidFields    = "trend_name,tweet_count"
	trendsDefaultPersonalFields = "trend_name,category,post_count,trending_since"
	// trendsWoeidMaxTrendsCap は GetTrends の per-call 上限 (X API 仕様で 50)。
	trendsWoeidMaxTrendsCap = 50
)

// trendsClient は newTrends*Cmd 群が必要とする X API クライアントの最小インターフェイス (M34 D-10)。
type trendsClient interface {
	GetTrends(ctx context.Context, woeid int, opts ...xapi.TrendWoeidOption) (*xapi.TrendsResponse, error)
	GetPersonalizedTrends(ctx context.Context, opts ...xapi.TrendPersonalOption) (*xapi.TrendsResponse, error)
}

// newTrendsClient は newTrends*Cmd が使う trendsClient の生成関数 (var-swap でテストから差し替え)。
var newTrendsClient = func(ctx context.Context, creds *config.Credentials) (trendsClient, error) {
	return xapi.NewClient(ctx, creds), nil
}

// newTrendsCmd は `x trends` 親コマンドを生成する factory。
func newTrendsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trends",
		Short: "fetch X Trends by WOEID or personalized trends",
		Long: "Subcommands to fetch trending topics on X.\n" +
			"Common WOEIDs: 1118370 (Tokyo) / 23424856 (Japan) / 1 (Worldwide).",
	}
	cmd.AddCommand(newTrendsGetCmd())
	cmd.AddCommand(newTrendsPersonalCmd())
	return cmd
}

// =============================================================================
// trends get
// =============================================================================

func newTrendsGetCmd() *cobra.Command {
	var (
		maxTrends   int
		trendFields string
		noJSON      bool
	)
	cmd := &cobra.Command{
		Use:   "get <woeid>",
		Short: "fetch trends by WOEID",
		Long: "Fetch trends for a specific WOEID (Where On Earth ID, Yahoo system).\n" +
			"Common values: 1118370 (Tokyo) / 23424856 (Japan) / 1 (Worldwide).\n" +
			"--max-trends 1..50 (X API per-call default 20, parameter name `max_trends`).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			woeid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("%w: woeid must be an integer, got %q", ErrInvalidArgument, args[0])
			}
			if woeid <= 0 {
				return fmt.Errorf("%w: woeid must be positive, got %d", ErrInvalidArgument, woeid)
			}
			if maxTrends < 0 || maxTrends > trendsWoeidMaxTrendsCap {
				return fmt.Errorf("%w: --max-trends must be in 0..%d, got %d", ErrInvalidArgument, trendsWoeidMaxTrendsCap, maxTrends)
			}
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newTrendsClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := []xapi.TrendWoeidOption{}
			if maxTrends > 0 {
				opts = append(opts, xapi.WithTrendWoeidMaxTrends(maxTrends))
			}
			if fs := splitCSV(trendFields); len(fs) > 0 {
				opts = append(opts, xapi.WithTrendWoeidTrendFields(fs...))
			}
			resp, err := client.GetTrends(ctx, woeid, opts...)
			if err != nil {
				return err
			}
			return writeTrendsHumanOrJSON(cmd, resp, noJSON)
		},
	}
	cmd.Flags().IntVar(&maxTrends, "max-trends", 0, "max trends per call (1..50, 0 = X API default 20)")
	cmd.Flags().StringVar(&trendFields, "trend-fields", trendsDefaultWoeidFields, "comma-separated trend.fields (trend_name,tweet_count)")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// =============================================================================
// trends personal (authenticated user only)
// =============================================================================

func newTrendsPersonalCmd() *cobra.Command {
	var (
		personalizedTrendFields string
		noJSON                  bool
	)
	cmd := &cobra.Command{
		Use:   "personal",
		Short: "fetch personalized trends for the authenticated user",
		Long: "Fetch personalized trends for the authenticated user via GET /2/users/personalized_trends.\n" +
			"X API auto-resolves the target user from the auth token; --user-id is intentionally NOT exposed.\n" +
			"X API uses the parameter name `personalized_trend.fields` (NOT `trend.fields`).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newTrendsClient(ctx, creds)
			if err != nil {
				return err
			}
			opts := []xapi.TrendPersonalOption{}
			if fs := splitCSV(personalizedTrendFields); len(fs) > 0 {
				opts = append(opts, xapi.WithTrendPersonalFields(fs...))
			}
			resp, err := client.GetPersonalizedTrends(ctx, opts...)
			if err != nil {
				return err
			}
			return writeTrendsHumanOrJSON(cmd, resp, noJSON)
		},
	}
	cmd.Flags().StringVar(&personalizedTrendFields, "personalized-trend-fields", trendsDefaultPersonalFields,
		"comma-separated personalized_trend.fields (trend_name,category,post_count,trending_since)")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// =============================================================================
// 共通ヘルパ
// =============================================================================

func writeTrendsHumanOrJSON(cmd *cobra.Command, resp *xapi.TrendsResponse, noJSON bool) error {
	if noJSON {
		if resp == nil || len(resp.Data) == 0 {
			return nil
		}
		for _, t := range resp.Data {
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatTrendHumanLine(t)); err != nil {
				return err
			}
		}
		return nil
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// formatTrendHumanLine は 1 Trend を 1 行に整形する (M34 D-13、union DTO 用)。
// woeid endpoint と personalized endpoint で返却フィールドが異なるが、union DTO の
// omitempty により未返却フィールドはゼロ値 (空文字 / 0) で表示される。
func formatTrendHumanLine(t xapi.Trend) string {
	return fmt.Sprintf("trend_name=%s\ttweet_count=%d\tpost_count=%d\tcategory=%s\ttrending_since=%s",
		t.TrendName, t.TweetCount, t.PostCount, t.Category, t.TrendingSince)
}
