package mcp_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"

	mcpinternal "github.com/youyo/x/internal/mcp"
)

func TestSearchSpacesHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedQuery string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	})
	handler := mcpinternal.NewSearchSpacesHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"query": "ai",
		"state": "live",
	}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if !strings.Contains(capturedQuery, "query=ai") {
		t.Errorf("missing query: %s", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "state=live") {
		t.Errorf("missing state: %s", capturedQuery)
	}
}

func TestSearchSpacesHandler_MissingQuery(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("API should not be called without query")
	})
	handler := mcpinternal.NewSearchSpacesHandler(client)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for missing query")
	}
}

func TestSearchSpacesHandler_InvalidState(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("API should not be called for invalid state")
	})
	handler := mcpinternal.NewSearchSpacesHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "ai", "state": "invalid"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for invalid state")
	}
}

func TestGetTrendsHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"trend_name":"#test","tweet_count":100}]}`))
	})
	handler := mcpinternal.NewGetTrendsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"woeid": float64(23424856)}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/trends/by/woeid/23424856" {
		t.Errorf("path = %q", capturedPath)
	}
}

func TestGetTrendsHandler_MissingWoeid(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("API should not be called without woeid")
	})
	handler := mcpinternal.NewGetTrendsHandler(client)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for missing woeid")
	}
}

func TestGetTrendsHandler_MaxTrendsOutOfRange(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("API should not be called for invalid max_trends")
	})
	handler := mcpinternal.NewGetTrendsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"woeid": float64(1), "max_trends": float64(5)}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for max_trends=5 (below 10)")
	}
}
