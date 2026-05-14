package mcp

import (
	"context"
	"fmt"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/youyo/x/internal/xapi"
)

// NewSearchSpacesHandler は MCP tool `search_spaces` のハンドラを返す (M36 T6)。
func NewSearchSpacesHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
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
		opts, err := buildSpaceSearchOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		resp, err := client.SearchSpaces(ctx, query, opts...)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, "search_spaces")
	}
}

// NewGetTrendsHandler は MCP tool `get_trends` のハンドラを返す (M36 T6)。
func NewGetTrendsHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		if client == nil {
			return gomcp.NewToolResultError("xapi client is not configured"), nil
		}
		args := req.GetArguments()
		woeid, ok, err := argInt(args, "woeid")
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		if !ok {
			return gomcp.NewToolResultError("woeid is required"), nil
		}
		opts, err := buildTrendsOpts(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		resp, err := client.GetTrends(ctx, woeid, opts...)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		return toolResultJSON(resp, "get_trends")
	}
}

// buildSpaceSearchOpts は search_spaces のオプション構築。
func buildSpaceSearchOpts(args map[string]any) ([]xapi.SpaceSearchOption, error) {
	out := []xapi.SpaceSearchOption{}
	mr, mrOK, err := argInt(args, "max_results")
	if err != nil {
		return nil, err
	}
	if mrOK {
		if mr < 1 || mr > 100 {
			return nil, fmt.Errorf("max_results must be in 1..100, got %d", mr)
		}
		out = append(out, xapi.WithSpaceSearchMaxResults(mr))
	}
	state, _, err := argString(args, "state")
	if err != nil {
		return nil, err
	}
	if state != "" {
		switch state {
		case "live", "scheduled", "all":
			out = append(out, xapi.WithSpaceSearchState(state))
		default:
			return nil, fmt.Errorf("state must be one of live/scheduled/all, got %q", state)
		}
	}
	spaceFields, err := argStringSliceOptional(args, "space_fields")
	if err != nil {
		return nil, err
	}
	if len(spaceFields) > 0 {
		out = append(out, xapi.WithSpaceSearchSpaceFields(spaceFields...))
	}
	expansions, err := argStringSliceOptional(args, "expansions")
	if err != nil {
		return nil, err
	}
	if len(expansions) > 0 {
		out = append(out, xapi.WithSpaceSearchExpansions(expansions...))
	}
	userFields, err := argStringSliceOptional(args, "user_fields")
	if err != nil {
		return nil, err
	}
	if len(userFields) > 0 {
		out = append(out, xapi.WithSpaceSearchUserFields(userFields...))
	}
	topicFields, err := argStringSliceOptional(args, "topic_fields")
	if err != nil {
		return nil, err
	}
	if len(topicFields) > 0 {
		out = append(out, xapi.WithSpaceSearchTopicFields(topicFields...))
	}
	return out, nil
}

// buildTrendsOpts は get_trends (by WOEID) のオプション構築。
func buildTrendsOpts(args map[string]any) ([]xapi.TrendWoeidOption, error) {
	out := []xapi.TrendWoeidOption{}
	mt, mtOK, err := argInt(args, "max_trends")
	if err != nil {
		return nil, err
	}
	if mtOK {
		if mt < 10 || mt > 50 {
			return nil, fmt.Errorf("max_trends must be in 10..50, got %d", mt)
		}
		out = append(out, xapi.WithTrendWoeidMaxTrends(mt))
	}
	trendFields, err := argStringSliceOptional(args, "trend_fields")
	if err != nil {
		return nil, err
	}
	if len(trendFields) > 0 {
		out = append(out, xapi.WithTrendWoeidTrendFields(trendFields...))
	}
	return out, nil
}

// registerToolMisc は misc 系 2 ツールを MCP サーバーに登録する (M36 T7)。
func registerToolMisc(s *mcpserver.MCPServer, client *xapi.Client) {
	// search_spaces
	s.AddTool(gomcp.NewTool(
		"search_spaces",
		gomcp.WithDescription(
			"Search Spaces by query (pagination not supported by X API). "+
				"state: live/scheduled/all (default: all).",
		),
		gomcp.WithString("query", gomcp.Required(), gomcp.Description("Search query.")),
		gomcp.WithString("state",
			gomcp.Enum("live", "scheduled", "all"),
			gomcp.Description("Filter by space state.")),
		gomcp.WithNumber("max_results", gomcp.Min(1), gomcp.Max(100), gomcp.Description("1..100.")),
		gomcp.WithArray("space_fields", gomcp.Description("space.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("expansions", gomcp.Description("expansions."), gomcp.WithStringItems()),
		gomcp.WithArray("user_fields", gomcp.Description("user.fields."), gomcp.WithStringItems()),
		gomcp.WithArray("topic_fields", gomcp.Description("topic.fields."), gomcp.WithStringItems()),
	), NewSearchSpacesHandler(client))

	// get_trends
	s.AddTool(gomcp.NewTool(
		"get_trends",
		gomcp.WithDescription(
			"Fetch trending topics for a WOEID (Where On Earth ID). "+
				"max_trends: 10..50 (X API does not support pagination).",
		),
		gomcp.WithNumber("woeid", gomcp.Required(),
			gomcp.Description("Yahoo WOEID (e.g. 1 = worldwide, 23424856 = Japan).")),
		gomcp.WithNumber("max_trends", gomcp.Min(10), gomcp.Max(50), gomcp.Description("10..50.")),
		gomcp.WithArray("trend_fields", gomcp.Description("trend.fields (trend_name, tweet_count)."), gomcp.WithStringItems()),
	), NewGetTrendsHandler(client))
}
