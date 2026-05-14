package mcp

import (
	"context"
	"fmt"
	"sort"
	"time"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/youyo/x/internal/xapi"
)

// searchAPIMinMaxResults は X API search/recent の per-page 下限である。
// CLI 層と同値 (10) で、MCP は CLI と異なり下限未満を補正せずバリデーションエラーにする (M36 D-8)。
const searchAPIMinMaxResults = 10

// searchTimeConfig は時間窓パラメータ (start_time / end_time / since_jst / yesterday_jst)
// を解釈した結果を持つ。MCP の複数ツール (search / timeline 系) で共有される。
type searchTimeConfig struct {
	startTime time.Time
	endTime   time.Time
}

// buildSearchTimeConfig は時間窓 4 種類のパラメータから start/end を決定する。
// 優先順位: yesterday_jst > since_jst > start_time/end_time (CLI M11 / MCP M18 と同じ)。
func buildSearchTimeConfig(args map[string]any) (searchTimeConfig, error) {
	cfg := searchTimeConfig{}

	startStr, _, err := argString(args, "start_time")
	if err != nil {
		return cfg, err
	}
	endStr, _, err := argString(args, "end_time")
	if err != nil {
		return cfg, err
	}
	if startStr != "" {
		t, perr := time.Parse(time.RFC3339, startStr)
		if perr != nil {
			return cfg, fmt.Errorf("start_time: %w", perr)
		}
		cfg.startTime = t
	}
	if endStr != "" {
		t, perr := time.Parse(time.RFC3339, endStr)
		if perr != nil {
			return cfg, fmt.Errorf("end_time: %w", perr)
		}
		cfg.endTime = t
	}

	sinceStr, _, err := argString(args, "since_jst")
	if err != nil {
		return cfg, err
	}
	if sinceStr != "" {
		s, e, perr := parseJSTDate(sinceStr)
		if perr != nil {
			return cfg, perr
		}
		cfg.startTime, cfg.endTime = s, e
	}

	yesterday, _, err := argBool(args, "yesterday_jst")
	if err != nil {
		return cfg, err
	}
	if yesterday {
		s, e, perr := yesterdayJSTRange(time.Now())
		if perr != nil {
			return cfg, perr
		}
		cfg.startTime, cfg.endTime = s, e
	}
	return cfg, nil
}

// NewSearchRecentTweetsHandler は MCP tool `search_recent_tweets` のハンドラを返す (M36 T2)。
func NewSearchRecentTweetsHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		if client == nil {
			return gomcp.NewToolResultError("xapi client is not configured"), nil
		}
		args := req.GetArguments()
		query, ok, err := argString(args, "query")
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if !ok || query == "" {
			return gomcp.NewToolResultError("query is required"), nil
		}
		opts, all, err := buildSearchOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if all {
			agg := &searchAggregator{}
			if err := client.EachSearchPage(ctx, query, agg.add, opts...); err != nil {
				return gomcp.NewToolResultError(err.Error()), nil
			}
			return toolResultJSON(agg.build(), "search_recent_tweets")
		}
		resp, err := client.SearchRecent(ctx, query, opts...)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, "search_recent_tweets")
	}
}

// NewGetTweetThreadHandler は MCP tool `get_tweet_thread` のハンドラを返す (M36 T2)。
//
// 2 段呼び出し:
//  1. GetTweet(id, fields=conversation_id,author_id,created_at) で会話 ID を取得
//  2. SearchRecent(query="conversation_id:<convID>") でスレッド構成ツイートを取得
//
// `author_only=true` の場合は Tweet.AuthorID == root.AuthorID でフィルタする (CLI と同じ振る舞い)。
// 出力は created_at 昇順 sort (CLI JSON モードと整合、advisor 指摘)。
func NewGetTweetThreadHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		if client == nil {
			return gomcp.NewToolResultError("xapi client is not configured"), nil
		}
		args := req.GetArguments()
		id, ok, err := argString(args, "tweet_id")
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if !ok || id == "" {
			return gomcp.NewToolResultError("tweet_id is required"), nil
		}
		authorOnly, _, err := argBool(args, "author_only")
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}

		// Step 1: root tweet を取得 (conversation_id を引く)
		rootResp, err := client.GetTweet(ctx, id,
			xapi.WithGetTweetFields("id", "text", "author_id", "created_at", "conversation_id"),
			xapi.WithGetTweetExpansions("author_id"),
			xapi.WithGetTweetUserFields("username", "name"),
		)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if rootResp == nil || rootResp.Data == nil {
			return gomcp.NewToolResultError(
				fmt.Sprintf("thread root tweet not returned (id=%s)", id)), nil
		}
		if rootResp.Data.ConversationID == "" {
			return gomcp.NewToolResultError(
				fmt.Sprintf("thread root tweet missing conversation_id (id=%s)", id)), nil
		}
		rootAuthor := rootResp.Data.AuthorID

		opts, all, err := buildSearchOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		query := "conversation_id:" + rootResp.Data.ConversationID

		// Step 2: search/recent でスレッドを集約
		filterFn := func(tw xapi.Tweet) bool {
			if !authorOnly {
				return true
			}
			return tw.AuthorID == rootAuthor
		}
		var resp *xapi.SearchResponse
		if all {
			agg := &searchAggregator{filter: filterFn}
			if err := client.EachSearchPage(ctx, query, agg.add, opts...); err != nil {
				return gomcp.NewToolResultError(err.Error()), nil
			}
			resp = agg.build()
		} else {
			single, err := client.SearchRecent(ctx, query, opts...)
			if err != nil {
				return gomcp.NewToolResultError(err.Error()), nil
			}
			resp = filterSingleSearchResponse(single, filterFn)
		}

		// CLI JSON モードと整合させて created_at 昇順 sort (advisor 指摘)
		sortTweetsByCreatedAtAsc(resp.Data)
		return toolResultJSON(resp, "get_tweet_thread")
	}
}

// buildSearchOpts は search_recent_tweets / get_tweet_thread 共通のオプション構築。
// 戻り値の bool は all フラグの有無 (true なら EachSearchPage、false なら SearchRecent を使う)。
//
//nolint:gocyclo // 多くの引数を直線的に処理しており分岐は素直
func buildSearchOpts(args map[string]any) ([]xapi.SearchOption, bool, error) {
	out := []xapi.SearchOption{}

	// max_results: 10..100 を要求 (MCP は CLI のような下限補正をしない、D-8)
	mr, mrOK, err := argInt(args, "max_results")
	if err != nil {
		return nil, false, err
	}
	if mrOK {
		if mr < searchAPIMinMaxResults || mr > 100 {
			return nil, false, fmt.Errorf(
				"max_results must be in %d..100, got %d", searchAPIMinMaxResults, mr)
		}
		out = append(out, xapi.WithSearchMaxResults(mr))
	}

	// 時間窓
	tcfg, err := buildSearchTimeConfig(args)
	if err != nil {
		return nil, false, err
	}
	if !tcfg.startTime.IsZero() {
		out = append(out, xapi.WithSearchStartTime(tcfg.startTime))
	}
	if !tcfg.endTime.IsZero() {
		out = append(out, xapi.WithSearchEndTime(tcfg.endTime))
	}

	pt, _, err := argString(args, "pagination_token")
	if err != nil {
		return nil, false, err
	}
	if pt != "" {
		out = append(out, xapi.WithSearchPaginationToken(pt))
	}

	tweetFields, err := argStringSliceOptional(args, "tweet_fields")
	if err != nil {
		return nil, false, err
	}
	if len(tweetFields) > 0 {
		out = append(out, xapi.WithSearchTweetFields(tweetFields...))
	}
	expansions, err := argStringSliceOptional(args, "expansions")
	if err != nil {
		return nil, false, err
	}
	if len(expansions) > 0 {
		out = append(out, xapi.WithSearchExpansions(expansions...))
	}
	userFields, err := argStringSliceOptional(args, "user_fields")
	if err != nil {
		return nil, false, err
	}
	if len(userFields) > 0 {
		out = append(out, xapi.WithSearchUserFields(userFields...))
	}
	mediaFields, err := argStringSliceOptional(args, "media_fields")
	if err != nil {
		return nil, false, err
	}
	if len(mediaFields) > 0 {
		out = append(out, xapi.WithSearchMediaFields(mediaFields...))
	}

	all, _, err := argBool(args, "all")
	if err != nil {
		return nil, false, err
	}
	mp, mpOK, err := argInt(args, "max_pages")
	if err != nil {
		return nil, false, err
	}
	if mpOK {
		if mp <= 0 {
			return nil, false, fmt.Errorf("max_pages must be > 0, got %d", mp)
		}
		out = append(out, xapi.WithSearchMaxPages(mp))
	}
	return out, all, nil
}

// searchAggregator は EachSearchPage の callback として複数ページを集約する。
//
// `filter` は author_only 等のページ単位フィルタ (advisor 指摘: メモリを bounded に保つため
// 全部 collect してから filter ではなく add() 内で適用する)。nil なら all-pass。
type searchAggregator struct {
	data   []xapi.Tweet
	users  []xapi.User
	tweets []xapi.Tweet
	media  []xapi.Media
	filter func(xapi.Tweet) bool
}

func (a *searchAggregator) add(p *xapi.SearchResponse) error {
	if p == nil {
		return nil
	}
	for _, tw := range p.Data {
		if a.filter == nil || a.filter(tw) {
			a.data = append(a.data, tw)
		}
	}
	a.users = append(a.users, p.Includes.Users...)
	a.tweets = append(a.tweets, p.Includes.Tweets...)
	a.media = append(a.media, p.Includes.Media...)
	return nil
}

func (a *searchAggregator) build() *xapi.SearchResponse {
	return &xapi.SearchResponse{
		Data: a.data,
		Includes: xapi.Includes{
			Users:  a.users,
			Tweets: a.tweets,
			Media:  a.media,
		},
		Meta: xapi.Meta{
			ResultCount: len(a.data),
			NextToken:   "",
		},
	}
}

// filterSingleSearchResponse は単一ページの SearchResponse から filterFn を満たす Tweet のみ残す。
// Includes はそのまま保持 (filter は Tweet 単位のため)。Meta.ResultCount を再計算。
func filterSingleSearchResponse(
	resp *xapi.SearchResponse, filterFn func(xapi.Tweet) bool,
) *xapi.SearchResponse {
	if resp == nil || filterFn == nil {
		return resp
	}
	out := &xapi.SearchResponse{
		Includes: resp.Includes,
		Meta:     resp.Meta,
		Errors:   resp.Errors,
	}
	for _, tw := range resp.Data {
		if filterFn(tw) {
			out.Data = append(out.Data, tw)
		}
	}
	out.Meta.ResultCount = len(out.Data)
	return out
}

// sortTweetsByCreatedAtAsc は Tweet を created_at 昇順に sort する (in-place)。
// created_at が同値 / 空の Tweet は元順を保つ (sort.SliceStable)。
func sortTweetsByCreatedAtAsc(tweets []xapi.Tweet) {
	sort.SliceStable(tweets, func(i, j int) bool {
		return tweets[i].CreatedAt < tweets[j].CreatedAt
	})
}

// registerToolSearch は search 系 2 ツールを MCP サーバーに登録する (M36 T7)。
func registerToolSearch(s *mcpserver.MCPServer, client *xapi.Client) {
	// search_recent_tweets
	s.AddTool(gomcp.NewTool(
		"search_recent_tweets",
		gomcp.WithDescription(
			"Search tweets from the last 7 days using X API search operators (e.g. 'from:alice'). "+
				"max_results is 10..100 (CLI's <10 lower-bound auto-correction is not applied in MCP). "+
				"Time window: start_time/end_time (RFC3339 UTC), since_jst (YYYY-MM-DD), or yesterday_jst.",
		),
		gomcp.WithString("query", gomcp.Required(),
			gomcp.Description("Search query (X API operators supported).")),
		gomcp.WithString("start_time", gomcp.Description("RFC3339 UTC. Example: 2026-05-11T15:00:00Z")),
		gomcp.WithString("end_time", gomcp.Description("RFC3339 UTC.")),
		gomcp.WithString("since_jst", gomcp.Description("JST date YYYY-MM-DD (overrides start_time/end_time).")),
		gomcp.WithBoolean("yesterday_jst", gomcp.DefaultBool(false),
			gomcp.Description("If true, time window is yesterday 00:00-23:59 JST.")),
		gomcp.WithNumber("max_results", gomcp.Min(10), gomcp.Max(100),
			gomcp.Description("Tweets per page (10..100).")),
		gomcp.WithBoolean("all", gomcp.DefaultBool(false),
			gomcp.Description("If true, follow next_token and aggregate all pages.")),
		gomcp.WithNumber("max_pages", gomcp.Min(1),
			gomcp.Description("Maximum pages when all=true.")),
		gomcp.WithString("pagination_token", gomcp.Description("Pagination cursor (single-page mode only).")),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("media_fields", gomcp.Description("media.fields query parameter."), gomcp.WithStringItems()),
	), NewSearchRecentTweetsHandler(client))

	// get_tweet_thread
	s.AddTool(gomcp.NewTool(
		"get_tweet_thread",
		gomcp.WithDescription(
			"Fetch a tweet's conversation thread via search/recent. "+
				"Internally calls GetTweet to resolve conversation_id, then SearchRecent. "+
				"Output is sorted by created_at ascending. "+
				"Note: consumes 2+ requests per call.",
		),
		gomcp.WithString("tweet_id", gomcp.Required(), gomcp.Description("Root tweet numeric ID.")),
		gomcp.WithBoolean("author_only", gomcp.DefaultBool(false),
			gomcp.Description("If true, keep only tweets by the root tweet's author.")),
		gomcp.WithNumber("max_results", gomcp.Min(10), gomcp.Max(100), gomcp.Description("10..100.")),
		gomcp.WithBoolean("all", gomcp.DefaultBool(false),
			gomcp.Description("If true, follow next_token and aggregate all pages.")),
		gomcp.WithNumber("max_pages", gomcp.Min(1), gomcp.Description("Maximum pages when all=true.")),
		gomcp.WithString("pagination_token", gomcp.Description("Pagination cursor (single-page mode only).")),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields query parameter."), gomcp.WithStringItems()),
	), NewGetTweetThreadHandler(client))
}
