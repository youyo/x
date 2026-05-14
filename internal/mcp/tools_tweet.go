package mcp

import (
	"context"
	"fmt"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/youyo/x/internal/xapi"
)

// NewGetTweetHandler は MCP tool `get_tweet` のハンドラを返す (M36 T1)。
//
// X API `GET /2/tweets/:id` の薄いラッパー。CLI `x tweet get <ID|URL>` と等価だが、
// MCP 層は URL 解決を行わず数値 ID を素直に受け取る (D-5)。
func NewGetTweetHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
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
		opts, err := buildTweetLookupOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		resp, err := client.GetTweet(ctx, id, opts...)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, "get_tweet")
	}
}

// NewGetTweetsHandler は MCP tool `get_tweets` のハンドラを返す (M36 T1)。
func NewGetTweetsHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		if client == nil {
			return gomcp.NewToolResultError("xapi client is not configured"), nil
		}
		args := req.GetArguments()
		ids, err := argStringSliceRequired(args, "tweet_ids")
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if len(ids) == 0 {
			return gomcp.NewToolResultError("tweet_ids must be a non-empty array"), nil
		}
		if len(ids) > 100 {
			return gomcp.NewToolResultError(
				fmt.Sprintf("tweet_ids length must be <= 100, got %d", len(ids))), nil
		}
		opts, err := buildTweetLookupOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		resp, err := client.GetTweets(ctx, ids, opts...)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, "get_tweets")
	}
}

// NewGetLikingUsersHandler は MCP tool `get_liking_users` のハンドラを返す (M36 T1)。
func NewGetLikingUsersHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return newUsersByTweetHandler(client, "get_liking_users", func(
		ctx context.Context, c *xapi.Client, id string, opts []xapi.UsersByTweetOption,
	) (*xapi.UsersByTweetResponse, error) {
		return c.GetLikingUsers(ctx, id, opts...)
	})
}

// NewGetRetweetedByHandler は MCP tool `get_retweeted_by` のハンドラを返す (M36 T1)。
func NewGetRetweetedByHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return newUsersByTweetHandler(client, "get_retweeted_by", func(
		ctx context.Context, c *xapi.Client, id string, opts []xapi.UsersByTweetOption,
	) (*xapi.UsersByTweetResponse, error) {
		return c.GetRetweetedBy(ctx, id, opts...)
	})
}

// newUsersByTweetHandler は GetLikingUsers / GetRetweetedBy の共通ハンドラ生成器。
func newUsersByTweetHandler(
	client *xapi.Client,
	toolName string,
	call func(context.Context, *xapi.Client, string, []xapi.UsersByTweetOption) (*xapi.UsersByTweetResponse, error),
) mcpserver.ToolHandlerFunc {
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
		opts, err := buildUsersByTweetOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		resp, err := call(ctx, client, id, opts)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, toolName)
	}
}

// NewGetQuoteTweetsHandler は MCP tool `get_quote_tweets` のハンドラを返す (M36 T1)。
func NewGetQuoteTweetsHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
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
		opts, err := buildQuoteTweetsOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		resp, err := client.GetQuoteTweets(ctx, id, opts...)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, "get_quote_tweets")
	}
}

// buildTweetLookupOpts は get_tweet / get_tweets 共通のオプション構築。
func buildTweetLookupOpts(args map[string]any) ([]xapi.TweetLookupOption, error) {
	out := []xapi.TweetLookupOption{}
	tweetFields, err := argStringSliceOptional(args, "tweet_fields")
	if err != nil {
		return nil, err
	}
	if len(tweetFields) > 0 {
		out = append(out, xapi.WithGetTweetFields(tweetFields...))
	}
	expansions, err := argStringSliceOptional(args, "expansions")
	if err != nil {
		return nil, err
	}
	if len(expansions) > 0 {
		out = append(out, xapi.WithGetTweetExpansions(expansions...))
	}
	userFields, err := argStringSliceOptional(args, "user_fields")
	if err != nil {
		return nil, err
	}
	if len(userFields) > 0 {
		out = append(out, xapi.WithGetTweetUserFields(userFields...))
	}
	mediaFields, err := argStringSliceOptional(args, "media_fields")
	if err != nil {
		return nil, err
	}
	if len(mediaFields) > 0 {
		out = append(out, xapi.WithGetTweetMediaFields(mediaFields...))
	}
	return out, nil
}

// buildUsersByTweetOpts は liking_users / retweeted_by 共通のオプション構築。
func buildUsersByTweetOpts(args map[string]any) ([]xapi.UsersByTweetOption, error) {
	out := []xapi.UsersByTweetOption{}
	mr, mrOK, err := argInt(args, "max_results")
	if err != nil {
		return nil, err
	}
	if mrOK {
		if mr < 1 || mr > 100 {
			return nil, fmt.Errorf("max_results must be in 1..100, got %d", mr)
		}
		out = append(out, xapi.WithUsersByTweetMaxResults(mr))
	}
	pt, _, err := argString(args, "pagination_token")
	if err != nil {
		return nil, err
	}
	if pt != "" {
		out = append(out, xapi.WithUsersByTweetPaginationToken(pt))
	}
	userFields, err := argStringSliceOptional(args, "user_fields")
	if err != nil {
		return nil, err
	}
	if len(userFields) > 0 {
		out = append(out, xapi.WithUsersByTweetUserFields(userFields...))
	}
	expansions, err := argStringSliceOptional(args, "expansions")
	if err != nil {
		return nil, err
	}
	if len(expansions) > 0 {
		out = append(out, xapi.WithUsersByTweetExpansions(expansions...))
	}
	tweetFields, err := argStringSliceOptional(args, "tweet_fields")
	if err != nil {
		return nil, err
	}
	if len(tweetFields) > 0 {
		out = append(out, xapi.WithUsersByTweetTweetFields(tweetFields...))
	}
	return out, nil
}

// buildQuoteTweetsOpts は get_quote_tweets のオプション構築。
func buildQuoteTweetsOpts(args map[string]any) ([]xapi.QuoteTweetsOption, error) {
	out := []xapi.QuoteTweetsOption{}
	mr, mrOK, err := argInt(args, "max_results")
	if err != nil {
		return nil, err
	}
	if mrOK {
		if mr < 1 || mr > 100 {
			return nil, fmt.Errorf("max_results must be in 1..100, got %d", mr)
		}
		out = append(out, xapi.WithQuoteTweetsMaxResults(mr))
	}
	pt, _, err := argString(args, "pagination_token")
	if err != nil {
		return nil, err
	}
	if pt != "" {
		out = append(out, xapi.WithQuoteTweetsPaginationToken(pt))
	}
	exclude, err := argStringSliceOptional(args, "exclude")
	if err != nil {
		return nil, err
	}
	if len(exclude) > 0 {
		out = append(out, xapi.WithQuoteTweetsExclude(exclude...))
	}
	tweetFields, err := argStringSliceOptional(args, "tweet_fields")
	if err != nil {
		return nil, err
	}
	if len(tweetFields) > 0 {
		out = append(out, xapi.WithQuoteTweetsTweetFields(tweetFields...))
	}
	expansions, err := argStringSliceOptional(args, "expansions")
	if err != nil {
		return nil, err
	}
	if len(expansions) > 0 {
		out = append(out, xapi.WithQuoteTweetsExpansions(expansions...))
	}
	userFields, err := argStringSliceOptional(args, "user_fields")
	if err != nil {
		return nil, err
	}
	if len(userFields) > 0 {
		out = append(out, xapi.WithQuoteTweetsUserFields(userFields...))
	}
	mediaFields, err := argStringSliceOptional(args, "media_fields")
	if err != nil {
		return nil, err
	}
	if len(mediaFields) > 0 {
		out = append(out, xapi.WithQuoteTweetsMediaFields(mediaFields...))
	}
	return out, nil
}

// registerToolTweet は tweet 系 5 ツールを MCP サーバーに登録する (M36 T7)。
func registerToolTweet(s *mcpserver.MCPServer, client *xapi.Client) {
	// get_tweet
	s.AddTool(gomcp.NewTool(
		"get_tweet",
		gomcp.WithDescription("Fetch a single tweet by numeric ID. Pass `tweet_id` (URL resolution is the caller's responsibility)."),
		gomcp.WithString("tweet_id", gomcp.Required(),
			gomcp.Description("Numeric tweet ID (e.g. '1700000000000000000').")),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("media_fields", gomcp.Description("media.fields query parameter (with expansions=attachments.media_keys)."), gomcp.WithStringItems()),
	), NewGetTweetHandler(client))

	// get_tweets
	s.AddTool(gomcp.NewTool(
		"get_tweets",
		gomcp.WithDescription("Fetch multiple tweets by IDs (1-100). Partial errors land in response.errors."),
		gomcp.WithArray("tweet_ids", gomcp.Required(),
			gomcp.Description("Array of numeric tweet IDs (1-100)."),
			gomcp.WithStringItems()),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("media_fields", gomcp.Description("media.fields query parameter."), gomcp.WithStringItems()),
	), NewGetTweetsHandler(client))

	// get_liking_users
	s.AddTool(gomcp.NewTool(
		"get_liking_users",
		gomcp.WithDescription("List users who liked a tweet."),
		gomcp.WithString("tweet_id", gomcp.Required(), gomcp.Description("Numeric tweet ID.")),
		gomcp.WithNumber("max_results", gomcp.Min(1), gomcp.Max(100), gomcp.Description("1..100.")),
		gomcp.WithString("pagination_token", gomcp.Description("Pagination cursor.")),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields query parameter (with expansions=pinned_tweet_id)."), gomcp.WithStringItems()),
	), NewGetLikingUsersHandler(client))

	// get_retweeted_by
	s.AddTool(gomcp.NewTool(
		"get_retweeted_by",
		gomcp.WithDescription("List users who retweeted a tweet (excluding quote retweets)."),
		gomcp.WithString("tweet_id", gomcp.Required(), gomcp.Description("Numeric tweet ID.")),
		gomcp.WithNumber("max_results", gomcp.Min(1), gomcp.Max(100), gomcp.Description("1..100.")),
		gomcp.WithString("pagination_token", gomcp.Description("Pagination cursor.")),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields query parameter."), gomcp.WithStringItems()),
	), NewGetRetweetedByHandler(client))

	// get_quote_tweets
	s.AddTool(gomcp.NewTool(
		"get_quote_tweets",
		gomcp.WithDescription("List quote tweets of a tweet."),
		gomcp.WithString("tweet_id", gomcp.Required(), gomcp.Description("Numeric tweet ID.")),
		gomcp.WithNumber("max_results", gomcp.Min(1), gomcp.Max(100), gomcp.Description("1..100.")),
		gomcp.WithString("pagination_token", gomcp.Description("Pagination cursor.")),
		gomcp.WithArray("exclude", gomcp.Description("Exclude types (e.g. ['replies'])."), gomcp.WithStringItems()),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields query parameter."), gomcp.WithStringItems()),
		gomcp.WithArray("media_fields", gomcp.Description("media.fields query parameter."), gomcp.WithStringItems()),
	), NewGetQuoteTweetsHandler(client))
}
