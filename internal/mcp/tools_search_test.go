package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"

	mcpinternal "github.com/youyo/x/internal/mcp"
	"github.com/youyo/x/internal/xapi"
)

// ---------- search_recent_tweets ----------

func TestSearchRecentHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedQuery string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"hi"}],"meta":{"result_count":1}}`))
	})
	handler := mcpinternal.NewSearchRecentTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"query":       "from:alice",
		"max_results": float64(10),
	}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if !strings.Contains(capturedQuery, "query=from%3Aalice") {
		t.Errorf("query missing search query: %s", capturedQuery)
	}
}

func TestSearchRecentHandler_MissingQuery(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("API should not be called without query")
	})
	handler := mcpinternal.NewSearchRecentTweetsHandler(client)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for missing query")
	}
}

func TestSearchRecentHandler_MaxResultsBelow10Rejected(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("API should not be called for max_results=5")
	})
	handler := mcpinternal.NewSearchRecentTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"query":       "test",
		"max_results": float64(5),
	}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true (max_results<10 must be rejected in MCP)")
	}
}

func TestSearchRecentHandler_TimeWindow_SinceJST(t *testing.T) {
	t.Parallel()
	var capturedQuery string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	})
	handler := mcpinternal.NewSearchRecentTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"query":     "test",
		"since_jst": "2026-05-11",
	}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if !strings.Contains(capturedQuery, "start_time=2026-05-10T15%3A00%3A00Z") {
		t.Errorf("missing JST-converted start_time: %s", capturedQuery)
	}
}

func TestSearchRecentHandler_All_AggregatesTwoPages(t *testing.T) {
	t.Parallel()
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idx := count.Add(1)
		w.WriteHeader(http.StatusOK)
		switch idx {
		case 1:
			_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"a"}],"meta":{"result_count":1,"next_token":"t1"}}`))
		case 2:
			_, _ = w.Write([]byte(`{"data":[{"id":"2","text":"b"}],"meta":{"result_count":1}}`))
		}
	}))
	t.Cleanup(srv.Close)
	client := xapi.NewClient(context.Background(), nil, xapi.WithBaseURL(srv.URL))
	handler := mcpinternal.NewSearchRecentTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "test", "all": true}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	structured, ok := res.StructuredContent.(*xapi.SearchResponse)
	if !ok {
		t.Fatalf("StructuredContent type = %T", res.StructuredContent)
	}
	if got := len(structured.Data); got != 2 {
		t.Errorf("aggregated len = %d, want 2", got)
	}
}

func TestSearchRecentHandler_NilClient(t *testing.T) {
	t.Parallel()
	handler := mcpinternal.NewSearchRecentTweetsHandler(nil)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"query": "x"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for nil client")
	}
}

// ---------- get_tweet_thread ----------

func TestGetTweetThreadHandler_Success_SortedAsc(t *testing.T) {
	t.Parallel()
	var step atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idx := step.Add(1)
		w.WriteHeader(http.StatusOK)
		switch idx {
		case 1:
			// GetTweet → conversation_id を返す
			_, _ = w.Write([]byte(`{"data":{"id":"42","text":"root","author_id":"u1","conversation_id":"42","created_at":"2026-05-01T00:00:00.000Z"}}`))
		default:
			// SearchRecent → 新しい順で 2 件返す (sort 検証)
			_, _ = w.Write([]byte(`{"data":[
				{"id":"44","text":"reply2","author_id":"u2","created_at":"2026-05-01T02:00:00.000Z"},
				{"id":"43","text":"reply1","author_id":"u2","created_at":"2026-05-01T01:00:00.000Z"}
			],"meta":{"result_count":2}}`))
		}
	}))
	t.Cleanup(srv.Close)
	client := xapi.NewClient(context.Background(), nil, xapi.WithBaseURL(srv.URL))
	handler := mcpinternal.NewGetTweetThreadHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"tweet_id": "42"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	structured, ok := res.StructuredContent.(*xapi.SearchResponse)
	if !ok {
		t.Fatalf("StructuredContent type = %T", res.StructuredContent)
	}
	if len(structured.Data) != 2 {
		t.Fatalf("Data len = %d, want 2", len(structured.Data))
	}
	// created_at 昇順
	if structured.Data[0].ID != "43" {
		t.Errorf("Data[0].ID = %q, want 43 (oldest)", structured.Data[0].ID)
	}
	if structured.Data[1].ID != "44" {
		t.Errorf("Data[1].ID = %q, want 44", structured.Data[1].ID)
	}
}

func TestGetTweetThreadHandler_AuthorOnlyFilter(t *testing.T) {
	t.Parallel()
	var step atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idx := step.Add(1)
		w.WriteHeader(http.StatusOK)
		switch idx {
		case 1:
			_, _ = w.Write([]byte(`{"data":{"id":"42","text":"root","author_id":"u1","conversation_id":"42","created_at":"2026-05-01T00:00:00.000Z"}}`))
		default:
			_, _ = w.Write([]byte(`{"data":[
				{"id":"43","text":"by u2","author_id":"u2","created_at":"2026-05-01T01:00:00.000Z"},
				{"id":"44","text":"by u1","author_id":"u1","created_at":"2026-05-01T02:00:00.000Z"}
			],"meta":{"result_count":2}}`))
		}
	}))
	t.Cleanup(srv.Close)
	client := xapi.NewClient(context.Background(), nil, xapi.WithBaseURL(srv.URL))
	handler := mcpinternal.NewGetTweetThreadHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tweet_id":    "42",
		"author_only": true,
	}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	structured := res.StructuredContent.(*xapi.SearchResponse)
	if len(structured.Data) != 1 {
		t.Fatalf("filtered Data len = %d, want 1", len(structured.Data))
	}
	if structured.Data[0].AuthorID != "u1" {
		t.Errorf("Data[0].AuthorID = %q, want u1", structured.Data[0].AuthorID)
	}
}

func TestGetTweetThreadHandler_MissingTweetID(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {})
	handler := mcpinternal.NewGetTweetThreadHandler(client)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for missing tweet_id")
	}
}

func TestGetTweetThreadHandler_MissingConversationID(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// conversation_id を含まないレスポンス
		_, _ = w.Write([]byte(`{"data":{"id":"42","text":"root","author_id":"u1"}}`))
	})
	handler := mcpinternal.NewGetTweetThreadHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"tweet_id": "42"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true when conversation_id is missing")
	}
}
