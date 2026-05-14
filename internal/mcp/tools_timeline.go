package mcp

import (
	"context"
	"fmt"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/youyo/x/internal/xapi"
)

// timelineKind は 3 つの timeline endpoint を識別する内部 enum。
type timelineKind int

const (
	timelineKindUserTweets timelineKind = iota
	timelineKindUserMentions
	timelineKindHome
)

// NewGetUserTweetsHandler は MCP tool `get_user_tweets` のハンドラを返す (M36 T3)。
func NewGetUserTweetsHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return newTimelineHandler(client, timelineKindUserTweets, "get_user_tweets")
}

// NewGetUserMentionsHandler は MCP tool `get_user_mentions` のハンドラを返す (M36 T3)。
func NewGetUserMentionsHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return newTimelineHandler(client, timelineKindUserMentions, "get_user_mentions")
}

// NewGetHomeTimelineHandler は MCP tool `get_home_timeline` のハンドラを返す (M36 T3)。
func NewGetHomeTimelineHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return newTimelineHandler(client, timelineKindHome, "get_home_timeline")
}

// newTimelineHandler は 3 つの timeline endpoint 共通のハンドラ生成器。
//
//nolint:gocyclo // 3 endpoint を kind で switch しているのが主因、責務は build/run ヘルパに分離済
func newTimelineHandler(
	client *xapi.Client,
	kind timelineKind,
	toolName string,
) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		if client == nil {
			return gomcp.NewToolResultError("xapi client is not configured"), nil
		}
		args := req.GetArguments()
		userID, _, err := argString(args, "user_id")
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		// user_id 未指定 → GetUserMe で self 解決 (CLI と同じ振る舞い、D-4)
		if userID == "" {
			user, gErr := client.GetUserMe(ctx)
			if gErr != nil {
				return gomcp.NewToolResultError(gErr.Error()), nil
			}
			userID = user.ID
		}
		opts, all, err := buildTimelineOpts(args, kind)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if all {
			agg := &timelineAggregator{}
			var pageErr error
			switch kind {
			case timelineKindUserTweets:
				pageErr = client.EachUserTweetsPage(ctx, userID, agg.add, opts...)
			case timelineKindUserMentions:
				pageErr = client.EachUserMentionsPage(ctx, userID, agg.add, opts...)
			case timelineKindHome:
				pageErr = client.EachHomeTimelinePage(ctx, userID, agg.add, opts...)
			}
			if pageErr != nil {
				return gomcp.NewToolResultError(pageErr.Error()), nil
			}
			return toolResultJSON(agg.build(), toolName)
		}
		var resp *xapi.TimelineResponse
		switch kind {
		case timelineKindUserTweets:
			resp, err = client.GetUserTweets(ctx, userID, opts...)
		case timelineKindUserMentions:
			resp, err = client.GetUserMentions(ctx, userID, opts...)
		case timelineKindHome:
			resp, err = client.GetHomeTimeline(ctx, userID, opts...)
		}
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, toolName)
	}
}

// buildTimelineOpts は timeline 3 endpoint 共通のオプション構築。
// 戻り値 bool は all フラグ。
//
//nolint:gocyclo // 多くの引数を直線的に処理しており分岐は素直
func buildTimelineOpts(args map[string]any, kind timelineKind) ([]xapi.TimelineOption, bool, error) {
	out := []xapi.TimelineOption{}

	// max_results: 各 endpoint 仕様に従う (user_tweets/mentions: 5-100, home: 1-100)
	mr, mrOK, err := argInt(args, "max_results")
	if err != nil {
		return nil, false, err
	}
	if mrOK {
		minVal := 5
		if kind == timelineKindHome {
			minVal = 1
		}
		if mr < minVal || mr > 100 {
			return nil, false, fmt.Errorf("max_results must be in %d..100, got %d", minVal, mr)
		}
		out = append(out, xapi.WithTimelineMaxResults(mr))
	}

	// 時間窓
	tcfg, err := buildSearchTimeConfig(args)
	if err != nil {
		return nil, false, err
	}
	if !tcfg.startTime.IsZero() {
		out = append(out, xapi.WithTimelineStartTime(tcfg.startTime))
	}
	if !tcfg.endTime.IsZero() {
		out = append(out, xapi.WithTimelineEndTime(tcfg.endTime))
	}

	sinceID, _, err := argString(args, "since_id")
	if err != nil {
		return nil, false, err
	}
	if sinceID != "" {
		out = append(out, xapi.WithTimelineSinceID(sinceID))
	}
	untilID, _, err := argString(args, "until_id")
	if err != nil {
		return nil, false, err
	}
	if untilID != "" {
		out = append(out, xapi.WithTimelineUntilID(untilID))
	}
	pt, _, err := argString(args, "pagination_token")
	if err != nil {
		return nil, false, err
	}
	if pt != "" {
		out = append(out, xapi.WithTimelinePaginationToken(pt))
	}

	// exclude は user_tweets / home のみサポート (mentions は X API 非対応)
	if kind != timelineKindUserMentions {
		exclude, eErr := argStringSliceOptional(args, "exclude")
		if eErr != nil {
			return nil, false, eErr
		}
		if len(exclude) > 0 {
			out = append(out, xapi.WithTimelineExclude(exclude...))
		}
	}

	tweetFields, err := argStringSliceOptional(args, "tweet_fields")
	if err != nil {
		return nil, false, err
	}
	if len(tweetFields) > 0 {
		out = append(out, xapi.WithTimelineTweetFields(tweetFields...))
	}
	expansions, err := argStringSliceOptional(args, "expansions")
	if err != nil {
		return nil, false, err
	}
	if len(expansions) > 0 {
		out = append(out, xapi.WithTimelineExpansions(expansions...))
	}
	userFields, err := argStringSliceOptional(args, "user_fields")
	if err != nil {
		return nil, false, err
	}
	if len(userFields) > 0 {
		out = append(out, xapi.WithTimelineUserFields(userFields...))
	}
	mediaFields, err := argStringSliceOptional(args, "media_fields")
	if err != nil {
		return nil, false, err
	}
	if len(mediaFields) > 0 {
		out = append(out, xapi.WithTimelineMediaFields(mediaFields...))
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
		out = append(out, xapi.WithTimelineMaxPages(mp))
	}
	return out, all, nil
}

// timelineAggregator は EachXxxTimelinePage の callback として複数ページを集約する。
type timelineAggregator struct {
	data   []xapi.Tweet
	users  []xapi.User
	tweets []xapi.Tweet
	media  []xapi.Media
}

func (a *timelineAggregator) add(p *xapi.TimelineResponse) error {
	if p == nil {
		return nil
	}
	a.data = append(a.data, p.Data...)
	a.users = append(a.users, p.Includes.Users...)
	a.tweets = append(a.tweets, p.Includes.Tweets...)
	a.media = append(a.media, p.Includes.Media...)
	return nil
}

func (a *timelineAggregator) build() *xapi.TimelineResponse {
	return &xapi.TimelineResponse{
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

// registerToolTimeline は timeline 系 3 ツールを MCP サーバーに登録する (M36 T7)。
func registerToolTimeline(s *mcpserver.MCPServer, client *xapi.Client) {
	// get_user_tweets
	s.AddTool(gomcp.NewTool(
		"get_user_tweets",
		gomcp.WithDescription(
			"Fetch tweets posted by a user (defaults to self when user_id is omitted). "+
				"max_results: 5..100. Supports exclude=replies/retweets.",
		),
		gomcp.WithString("user_id", gomcp.Description("Numeric user ID. Defaults to self.")),
		gomcp.WithNumber("max_results", gomcp.Min(5), gomcp.Max(100), gomcp.Description("5..100.")),
		gomcp.WithString("start_time", gomcp.Description("RFC3339 UTC.")),
		gomcp.WithString("end_time", gomcp.Description("RFC3339 UTC.")),
		gomcp.WithString("since_jst", gomcp.Description("JST date YYYY-MM-DD.")),
		gomcp.WithBoolean("yesterday_jst", gomcp.DefaultBool(false),
			gomcp.Description("If true, time window is yesterday 00:00-23:59 JST.")),
		gomcp.WithString("since_id", gomcp.Description("Lower bound tweet ID (exclusive).")),
		gomcp.WithString("until_id", gomcp.Description("Upper bound tweet ID (exclusive).")),
		gomcp.WithString("pagination_token", gomcp.Description("Pagination cursor.")),
		gomcp.WithArray("exclude", gomcp.Description("Exclude types: 'replies' and/or 'retweets'."), gomcp.WithStringItems()),
		gomcp.WithBoolean("all", gomcp.DefaultBool(false), gomcp.Description("Aggregate all pages.")),
		gomcp.WithNumber("max_pages", gomcp.Min(1), gomcp.Description("Max pages when all=true.")),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions."), gomcp.WithStringItems()),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("media_fields", gomcp.Description("media.fields."), gomcp.WithStringItems()),
	), NewGetUserTweetsHandler(client))

	// get_user_mentions
	s.AddTool(gomcp.NewTool(
		"get_user_mentions",
		gomcp.WithDescription(
			"Fetch tweets mentioning a user (defaults to self). "+
				"max_results: 5..100. exclude is NOT supported by X API.",
		),
		gomcp.WithString("user_id", gomcp.Description("Numeric user ID. Defaults to self.")),
		gomcp.WithNumber("max_results", gomcp.Min(5), gomcp.Max(100), gomcp.Description("5..100.")),
		gomcp.WithString("start_time", gomcp.Description("RFC3339 UTC.")),
		gomcp.WithString("end_time", gomcp.Description("RFC3339 UTC.")),
		gomcp.WithString("since_jst", gomcp.Description("JST date YYYY-MM-DD.")),
		gomcp.WithBoolean("yesterday_jst", gomcp.DefaultBool(false), gomcp.Description("If true, yesterday JST.")),
		gomcp.WithString("since_id", gomcp.Description("Lower bound tweet ID.")),
		gomcp.WithString("until_id", gomcp.Description("Upper bound tweet ID.")),
		gomcp.WithString("pagination_token", gomcp.Description("Pagination cursor.")),
		gomcp.WithBoolean("all", gomcp.DefaultBool(false), gomcp.Description("Aggregate all pages.")),
		gomcp.WithNumber("max_pages", gomcp.Min(1), gomcp.Description("Max pages when all=true.")),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions."), gomcp.WithStringItems()),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("media_fields", gomcp.Description("media.fields."), gomcp.WithStringItems()),
	), NewGetUserMentionsHandler(client))

	// get_home_timeline
	s.AddTool(gomcp.NewTool(
		"get_home_timeline",
		gomcp.WithDescription(
			"Fetch the authenticated user's home timeline (reverse chronological). "+
				"max_results: 1..100. user_id must be the authenticated user (defaults to self).",
		),
		gomcp.WithString("user_id", gomcp.Description("Must be authenticated user ID. Defaults to self.")),
		gomcp.WithNumber("max_results", gomcp.Min(1), gomcp.Max(100), gomcp.Description("1..100.")),
		gomcp.WithString("start_time", gomcp.Description("RFC3339 UTC.")),
		gomcp.WithString("end_time", gomcp.Description("RFC3339 UTC.")),
		gomcp.WithString("since_jst", gomcp.Description("JST date YYYY-MM-DD.")),
		gomcp.WithBoolean("yesterday_jst", gomcp.DefaultBool(false), gomcp.Description("If true, yesterday JST.")),
		gomcp.WithString("since_id", gomcp.Description("Lower bound tweet ID.")),
		gomcp.WithString("until_id", gomcp.Description("Upper bound tweet ID.")),
		gomcp.WithString("pagination_token", gomcp.Description("Pagination cursor.")),
		gomcp.WithArray("exclude", gomcp.Description("Exclude types: 'replies' and/or 'retweets'."), gomcp.WithStringItems()),
		gomcp.WithBoolean("all", gomcp.DefaultBool(false), gomcp.Description("Aggregate all pages.")),
		gomcp.WithNumber("max_pages", gomcp.Min(1), gomcp.Description("Max pages when all=true.")),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions."), gomcp.WithStringItems()),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("media_fields", gomcp.Description("media.fields."), gomcp.WithStringItems()),
	), NewGetHomeTimelineHandler(client))
}
