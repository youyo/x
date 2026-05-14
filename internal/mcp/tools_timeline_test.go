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

// ---------- get_user_tweets ----------

func TestGetUserTweetsHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"t1"}],"meta":{"result_count":1}}`))
	})
	handler := mcpinternal.NewGetUserTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"user_id": "42"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/users/42/tweets" {
		t.Errorf("path = %q, want /2/users/42/tweets", capturedPath)
	}
}

func TestGetUserTweetsHandler_SelfResolveWhenUserIDOmitted(t *testing.T) {
	t.Parallel()
	var paths []string
	var step atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := step.Add(1)
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		switch idx {
		case 1:
			_, _ = w.Write([]byte(`{"data":{"id":"99","username":"a","name":"A"}}`))
		default:
			_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
		}
	}))
	t.Cleanup(srv.Close)
	client := xapi.NewClient(context.Background(), nil, xapi.WithBaseURL(srv.URL))
	handler := mcpinternal.NewGetUserTweetsHandler(client)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if len(paths) < 2 || paths[0] != "/2/users/me" {
		t.Fatalf("expected 1st request to /2/users/me, got %v", paths)
	}
	if paths[1] != "/2/users/99/tweets" {
		t.Errorf("2nd path = %q, want /2/users/99/tweets", paths[1])
	}
}

func TestGetUserTweetsHandler_MaxResultsBelow5Rejected(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("API should not be called for max_results=3")
	})
	handler := mcpinternal.NewGetUserTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"user_id": "42", "max_results": float64(3)}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for max_results=3")
	}
}

func TestGetUserTweetsHandler_All_Aggregates(t *testing.T) {
	t.Parallel()
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idx := count.Add(1)
		w.WriteHeader(http.StatusOK)
		switch idx {
		case 1:
			_, _ = w.Write([]byte(`{"data":[{"id":"1"}],"meta":{"result_count":1,"next_token":"t1"}}`))
		case 2:
			_, _ = w.Write([]byte(`{"data":[{"id":"2"}],"meta":{"result_count":1}}`))
		}
	}))
	t.Cleanup(srv.Close)
	client := xapi.NewClient(context.Background(), nil, xapi.WithBaseURL(srv.URL))
	handler := mcpinternal.NewGetUserTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"user_id": "42", "all": true}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	structured, ok := res.StructuredContent.(*xapi.TimelineResponse)
	if !ok {
		t.Fatalf("StructuredContent type = %T", res.StructuredContent)
	}
	if got := len(structured.Data); got != 2 {
		t.Errorf("Data len = %d, want 2", got)
	}
}

// ---------- get_user_mentions ----------

func TestGetUserMentionsHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	})
	handler := mcpinternal.NewGetUserMentionsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"user_id": "42"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/users/42/mentions" {
		t.Errorf("path = %q, want /2/users/42/mentions", capturedPath)
	}
}

func TestGetUserMentionsHandler_ExcludeIgnored(t *testing.T) {
	t.Parallel()
	// mentions endpoint は exclude をサポートしないので、引数として渡されても
	// クエリには載せない (build 時に kind=Mentions では skip)
	var capturedQuery string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	})
	handler := mcpinternal.NewGetUserMentionsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"user_id": "42",
		"exclude": []any{"replies"},
	}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if strings.Contains(capturedQuery, "exclude") {
		t.Errorf("exclude should be dropped for mentions, got query: %s", capturedQuery)
	}
}

// ---------- get_home_timeline ----------

func TestGetHomeTimelineHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	})
	handler := mcpinternal.NewGetHomeTimelineHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"user_id": "42"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/users/42/timelines/reverse_chronological" {
		t.Errorf("path = %q, want home timeline path", capturedPath)
	}
}

func TestGetHomeTimelineHandler_MaxResults1Allowed(t *testing.T) {
	t.Parallel()
	// home は下限 1
	var capturedQuery string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	})
	handler := mcpinternal.NewGetHomeTimelineHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"user_id":     "42",
		"max_results": float64(1),
	}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if !strings.Contains(capturedQuery, "max_results=1") {
		t.Errorf("missing max_results=1: %s", capturedQuery)
	}
}
