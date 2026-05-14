package mcp

import (
	"context"
	"fmt"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/youyo/x/internal/xapi"
)

// NewGetListHandler は MCP tool `get_list` のハンドラを返す (M36 T5)。
func NewGetListHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		if client == nil {
			return gomcp.NewToolResultError("xapi client is not configured"), nil
		}
		args := req.GetArguments()
		id, ok, err := argString(args, "list_id")
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if !ok || id == "" {
			return gomcp.NewToolResultError("list_id is required"), nil
		}
		opts, err := buildListLookupOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		resp, err := client.GetList(ctx, id, opts...)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, "get_list")
	}
}

// NewGetListTweetsHandler は MCP tool `get_list_tweets` のハンドラを返す (M36 T5)。
func NewGetListTweetsHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		if client == nil {
			return gomcp.NewToolResultError("xapi client is not configured"), nil
		}
		args := req.GetArguments()
		id, ok, err := argString(args, "list_id")
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if !ok || id == "" {
			return gomcp.NewToolResultError("list_id is required"), nil
		}
		opts, all, err := buildListPagedOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if all {
			agg := &listTweetsAggregator{}
			if err := client.EachListTweetsPage(ctx, id, agg.add, opts...); err != nil {
				return gomcp.NewToolResultError(err.Error()), nil
			}
			return toolResultJSON(agg.build(), "get_list_tweets")
		}
		resp, err := client.GetListTweets(ctx, id, opts...)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, "get_list_tweets")
	}
}

// buildListLookupOpts は get_list 用のオプション構築。
func buildListLookupOpts(args map[string]any) ([]xapi.ListLookupOption, error) {
	out := []xapi.ListLookupOption{}
	listFields, err := argStringSliceOptional(args, "list_fields")
	if err != nil {
		return nil, err
	}
	if len(listFields) > 0 {
		out = append(out, xapi.WithListLookupListFields(listFields...))
	}
	expansions, err := argStringSliceOptional(args, "expansions")
	if err != nil {
		return nil, err
	}
	if len(expansions) > 0 {
		out = append(out, xapi.WithListLookupExpansions(expansions...))
	}
	userFields, err := argStringSliceOptional(args, "user_fields")
	if err != nil {
		return nil, err
	}
	if len(userFields) > 0 {
		out = append(out, xapi.WithListLookupUserFields(userFields...))
	}
	return out, nil
}

// buildListPagedOpts は get_list_tweets 用のオプション構築。
//
//nolint:gocyclo // 線形なオプション処理
func buildListPagedOpts(args map[string]any) ([]xapi.ListPagedOption, bool, error) {
	out := []xapi.ListPagedOption{}
	mr, mrOK, err := argInt(args, "max_results")
	if err != nil {
		return nil, false, err
	}
	if mrOK {
		if mr < 1 || mr > 100 {
			return nil, false, fmt.Errorf("max_results must be in 1..100, got %d", mr)
		}
		out = append(out, xapi.WithListPagedMaxResults(mr))
	}
	pt, _, err := argString(args, "pagination_token")
	if err != nil {
		return nil, false, err
	}
	if pt != "" {
		out = append(out, xapi.WithListPagedPaginationToken(pt))
	}
	listFields, err := argStringSliceOptional(args, "list_fields")
	if err != nil {
		return nil, false, err
	}
	if len(listFields) > 0 {
		out = append(out, xapi.WithListPagedListFields(listFields...))
	}
	userFields, err := argStringSliceOptional(args, "user_fields")
	if err != nil {
		return nil, false, err
	}
	if len(userFields) > 0 {
		out = append(out, xapi.WithListPagedUserFields(userFields...))
	}
	tweetFields, err := argStringSliceOptional(args, "tweet_fields")
	if err != nil {
		return nil, false, err
	}
	if len(tweetFields) > 0 {
		out = append(out, xapi.WithListPagedTweetFields(tweetFields...))
	}
	expansions, err := argStringSliceOptional(args, "expansions")
	if err != nil {
		return nil, false, err
	}
	if len(expansions) > 0 {
		out = append(out, xapi.WithListPagedExpansions(expansions...))
	}
	mediaFields, err := argStringSliceOptional(args, "media_fields")
	if err != nil {
		return nil, false, err
	}
	if len(mediaFields) > 0 {
		out = append(out, xapi.WithListPagedMediaFields(mediaFields...))
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
		out = append(out, xapi.WithListPagedMaxPages(mp))
	}
	return out, all, nil
}

// listTweetsAggregator は EachListTweetsPage の callback として複数ページを集約する。
type listTweetsAggregator struct {
	data   []xapi.Tweet
	users  []xapi.User
	tweets []xapi.Tweet
	media  []xapi.Media
}

func (a *listTweetsAggregator) add(p *xapi.ListTweetsResponse) error {
	if p == nil {
		return nil
	}
	a.data = append(a.data, p.Data...)
	a.users = append(a.users, p.Includes.Users...)
	a.tweets = append(a.tweets, p.Includes.Tweets...)
	a.media = append(a.media, p.Includes.Media...)
	return nil
}

func (a *listTweetsAggregator) build() *xapi.ListTweetsResponse {
	return &xapi.ListTweetsResponse{
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

// registerToolLists は lists 系 2 ツールを MCP サーバーに登録する (M36 T7)。
func registerToolLists(s *mcpserver.MCPServer, client *xapi.Client) {
	// get_list
	s.AddTool(gomcp.NewTool(
		"get_list",
		gomcp.WithDescription("Fetch a single list by numeric ID."),
		gomcp.WithString("list_id", gomcp.Required(), gomcp.Description("Numeric list ID.")),
		gomcp.WithArray("list_fields", gomcp.Description("list.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions."), gomcp.WithStringItems()),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields (with expansions=owner_id)."), gomcp.WithStringItems()),
	), NewGetListHandler(client))

	// get_list_tweets
	s.AddTool(gomcp.NewTool(
		"get_list_tweets",
		gomcp.WithDescription("Fetch tweets in a list."),
		gomcp.WithString("list_id", gomcp.Required(), gomcp.Description("Numeric list ID.")),
		gomcp.WithNumber("max_results", gomcp.Min(1), gomcp.Max(100), gomcp.Description("1..100.")),
		gomcp.WithString("pagination_token", gomcp.Description("Pagination cursor.")),
		gomcp.WithBoolean("all", gomcp.DefaultBool(false), gomcp.Description("Aggregate all pages.")),
		gomcp.WithNumber("max_pages", gomcp.Min(1), gomcp.Description("Max pages when all=true.")),
		gomcp.WithArray("list_fields", gomcp.Description("list.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("tweet_fields", gomcp.Description("tweet.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions."), gomcp.WithStringItems()),
		gomcp.WithArray("media_fields", gomcp.Description("media.fields."), gomcp.WithStringItems()),
	), NewGetListTweetsHandler(client))
}
