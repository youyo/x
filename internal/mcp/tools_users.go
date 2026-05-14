package mcp

import (
	"context"
	"fmt"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/youyo/x/internal/xapi"
)

// userGraphKind は GetFollowing / GetFollowers を識別する内部 enum。
type userGraphKind int

const (
	userGraphKindFollowing userGraphKind = iota
	userGraphKindFollowers
)

// NewGetUserHandler は MCP tool `get_user` のハンドラを返す (M36 T4)。
func NewGetUserHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		if client == nil {
			return gomcp.NewToolResultError("xapi client is not configured"), nil
		}
		args := req.GetArguments()
		id, ok, err := argString(args, "user_id")
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if !ok || id == "" {
			return gomcp.NewToolResultError("user_id is required"), nil
		}
		opts, err := buildUserLookupOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		resp, err := client.GetUser(ctx, id, opts...)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, "get_user")
	}
}

// NewGetUserByUsernameHandler は MCP tool `get_user_by_username` のハンドラを返す (M36 T4)。
func NewGetUserByUsernameHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		if client == nil {
			return gomcp.NewToolResultError("xapi client is not configured"), nil
		}
		args := req.GetArguments()
		username, ok, err := argString(args, "username")
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if !ok || username == "" {
			return gomcp.NewToolResultError("username is required"), nil
		}
		opts, err := buildUserLookupOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		resp, err := client.GetUserByUsername(ctx, username, opts...)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, "get_user_by_username")
	}
}

// NewGetUserFollowingHandler は MCP tool `get_user_following` のハンドラを返す (M36 T4)。
func NewGetUserFollowingHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return newUserGraphHandler(client, userGraphKindFollowing, "get_user_following")
}

// NewGetUserFollowersHandler は MCP tool `get_user_followers` のハンドラを返す (M36 T4)。
func NewGetUserFollowersHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return newUserGraphHandler(client, userGraphKindFollowers, "get_user_followers")
}

// newUserGraphHandler は following / followers 共通のハンドラ生成器。
func newUserGraphHandler(
	client *xapi.Client,
	kind userGraphKind,
	toolName string,
) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		if client == nil {
			return gomcp.NewToolResultError("xapi client is not configured"), nil
		}
		args := req.GetArguments()
		id, ok, err := argString(args, "user_id")
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if !ok || id == "" {
			return gomcp.NewToolResultError("user_id is required"), nil
		}
		opts, all, err := buildUserGraphOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if all {
			agg := &usersAggregator{}
			var pageErr error
			switch kind {
			case userGraphKindFollowing:
				pageErr = client.EachFollowingPage(ctx, id, agg.add, opts...)
			case userGraphKindFollowers:
				pageErr = client.EachFollowersPage(ctx, id, agg.add, opts...)
			}
			if pageErr != nil {
				return gomcp.NewToolResultError(pageErr.Error()), nil
			}
			return toolResultJSON(agg.build(), toolName)
		}
		var resp *xapi.UsersResponse
		switch kind {
		case userGraphKindFollowing:
			resp, err = client.GetFollowing(ctx, id, opts...)
		case userGraphKindFollowers:
			resp, err = client.GetFollowers(ctx, id, opts...)
		}
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, toolName)
	}
}

// buildUserLookupOpts は get_user / get_user_by_username 共通のオプション構築。
func buildUserLookupOpts(args map[string]any) ([]xapi.UserLookupOption, error) {
	out := []xapi.UserLookupOption{}
	userFields, err := argStringSliceOptional(args, "user_fields")
	if err != nil {
		return nil, err
	}
	if len(userFields) > 0 {
		out = append(out, xapi.WithUserLookupUserFields(userFields...))
	}
	expansions, err := argStringSliceOptional(args, "expansions")
	if err != nil {
		return nil, err
	}
	if len(expansions) > 0 {
		out = append(out, xapi.WithUserLookupExpansions(expansions...))
	}
	tweetFields, err := argStringSliceOptional(args, "tweet_fields")
	if err != nil {
		return nil, err
	}
	if len(tweetFields) > 0 {
		out = append(out, xapi.WithUserLookupTweetFields(tweetFields...))
	}
	return out, nil
}

// buildUserGraphOpts は following / followers 共通のオプション構築。
//
//nolint:gocyclo // 線形なオプション処理で責務分散済み
func buildUserGraphOpts(args map[string]any) ([]xapi.UserGraphOption, bool, error) {
	out := []xapi.UserGraphOption{}
	mr, mrOK, err := argInt(args, "max_results")
	if err != nil {
		return nil, false, err
	}
	if mrOK {
		if mr < 1 || mr > 1000 {
			return nil, false, fmt.Errorf("max_results must be in 1..1000, got %d", mr)
		}
		out = append(out, xapi.WithUserGraphMaxResults(mr))
	}
	pt, _, err := argString(args, "pagination_token")
	if err != nil {
		return nil, false, err
	}
	if pt != "" {
		out = append(out, xapi.WithUserGraphPaginationToken(pt))
	}
	userFields, err := argStringSliceOptional(args, "user_fields")
	if err != nil {
		return nil, false, err
	}
	if len(userFields) > 0 {
		out = append(out, xapi.WithUserGraphUserFields(userFields...))
	}
	expansions, err := argStringSliceOptional(args, "expansions")
	if err != nil {
		return nil, false, err
	}
	if len(expansions) > 0 {
		out = append(out, xapi.WithUserGraphExpansions(expansions...))
	}
	tweetFields, err := argStringSliceOptional(args, "tweet_fields")
	if err != nil {
		return nil, false, err
	}
	if len(tweetFields) > 0 {
		out = append(out, xapi.WithUserGraphTweetFields(tweetFields...))
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
		out = append(out, xapi.WithUserGraphMaxPages(mp))
	}
	return out, all, nil
}

// usersAggregator は EachFollowing/FollowersPage の callback として複数ページを集約する。
type usersAggregator struct {
	data   []xapi.User
	tweets []xapi.Tweet
}

func (a *usersAggregator) add(p *xapi.UsersResponse) error {
	if p == nil {
		return nil
	}
	a.data = append(a.data, p.Data...)
	a.tweets = append(a.tweets, p.Includes.Tweets...)
	return nil
}

func (a *usersAggregator) build() *xapi.UsersResponse {
	return &xapi.UsersResponse{
		Data: a.data,
		Includes: xapi.Includes{
			Tweets: a.tweets,
		},
		Meta: xapi.Meta{
			ResultCount: len(a.data),
			NextToken:   "",
		},
	}
}

// registerToolUsers は users 系 4 ツールを MCP サーバーに登録する (M36 T7)。
func registerToolUsers(s *mcpserver.MCPServer, client *xapi.Client) {
	// get_user
	s.AddTool(gomcp.NewTool(
		"get_user",
		gomcp.WithDescription("Fetch a single user by numeric ID."),
		gomcp.WithString("user_id", gomcp.Required(), gomcp.Description("Numeric user ID.")),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions."), gomcp.WithStringItems()),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields (with expansions=pinned_tweet_id)."), gomcp.WithStringItems()),
	), NewGetUserHandler(client))

	// get_user_by_username
	s.AddTool(gomcp.NewTool(
		"get_user_by_username",
		gomcp.WithDescription("Fetch a user by username (without @ prefix)."),
		gomcp.WithString("username", gomcp.Required(), gomcp.Description("Username without @ prefix.")),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions."), gomcp.WithStringItems()),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields."), gomcp.WithStringItems()),
	), NewGetUserByUsernameHandler(client))

	// get_user_following
	s.AddTool(gomcp.NewTool(
		"get_user_following",
		gomcp.WithDescription("List users that the specified user follows."),
		gomcp.WithString("user_id", gomcp.Required(), gomcp.Description("Numeric user ID.")),
		gomcp.WithNumber("max_results", gomcp.Min(1), gomcp.Max(1000), gomcp.Description("1..1000.")),
		gomcp.WithString("pagination_token", gomcp.Description("Pagination cursor.")),
		gomcp.WithBoolean("all", gomcp.DefaultBool(false), gomcp.Description("Aggregate all pages.")),
		gomcp.WithNumber("max_pages", gomcp.Min(1), gomcp.Description("Max pages when all=true.")),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions."), gomcp.WithStringItems()),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields."), gomcp.WithStringItems()),
	), NewGetUserFollowingHandler(client))

	// get_user_followers
	s.AddTool(gomcp.NewTool(
		"get_user_followers",
		gomcp.WithDescription("List users following the specified user."),
		gomcp.WithString("user_id", gomcp.Required(), gomcp.Description("Numeric user ID.")),
		gomcp.WithNumber("max_results", gomcp.Min(1), gomcp.Max(1000), gomcp.Description("1..1000.")),
		gomcp.WithString("pagination_token", gomcp.Description("Pagination cursor.")),
		gomcp.WithBoolean("all", gomcp.DefaultBool(false), gomcp.Description("Aggregate all pages.")),
		gomcp.WithNumber("max_pages", gomcp.Min(1), gomcp.Description("Max pages when all=true.")),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions."), gomcp.WithStringItems()),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields."), gomcp.WithStringItems()),
	), NewGetUserFollowersHandler(client))
}
