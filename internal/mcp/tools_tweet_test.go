package mcp_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"

	mcpinternal "github.com/youyo/x/internal/mcp"
)

// ---------- get_tweet ----------

func TestGetTweetHandler_Success(t *testing.T) {
	t.Parallel()

	var capturedPath string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","text":"hello"}}`))
	})
	handler := mcpinternal.NewGetTweetHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"tweet_id": "42"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/tweets/42" {
		t.Errorf("path = %q, want /2/tweets/42", capturedPath)
	}
}

func TestGetTweetHandler_MissingTweetID(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("X API should not be called when tweet_id is missing")
		w.WriteHeader(http.StatusOK)
	})
	handler := mcpinternal.NewGetTweetHandler(client)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for missing tweet_id")
	}
}

func TestGetTweetHandler_NilClient(t *testing.T) {
	t.Parallel()
	handler := mcpinternal.NewGetTweetHandler(nil)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"tweet_id": "42"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for nil client")
	}
}

func TestGetTweetHandler_ForwardsFields(t *testing.T) {
	t.Parallel()
	var capturedQuery string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","text":"hi"}}`))
	})
	handler := mcpinternal.NewGetTweetHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tweet_id":     "42",
		"tweet_fields": []any{"id", "text", "note_tweet"},
		"expansions":   []any{"author_id"},
	}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if !strings.Contains(capturedQuery, "tweet.fields=id%2Ctext%2Cnote_tweet") {
		t.Errorf("query missing tweet.fields: %s", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "expansions=author_id") {
		t.Errorf("query missing expansions: %s", capturedQuery)
	}
}

// ---------- get_tweets ----------

func TestGetTweetsHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedQuery string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"42","text":"a"},{"id":"43","text":"b"}]}`))
	})
	handler := mcpinternal.NewGetTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"tweet_ids": []any{"42", "43"}}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if !strings.Contains(capturedQuery, "ids=42%2C43") {
		t.Errorf("query missing ids: %s", capturedQuery)
	}
}

func TestGetTweetsHandler_EmptyIDsRejected(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("X API should not be called for empty ids")
		w.WriteHeader(http.StatusOK)
	})
	handler := mcpinternal.NewGetTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"tweet_ids": []any{}}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for empty tweet_ids")
	}
}

func TestGetTweetsHandler_TooManyIDsRejected(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("X API should not be called for >100 ids")
	})
	handler := mcpinternal.NewGetTweetsHandler(client)
	ids := make([]any, 101)
	for i := range ids {
		ids[i] = "1"
	}
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"tweet_ids": ids}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for too many tweet_ids")
	}
}

// ---------- get_liking_users / get_retweeted_by ----------

func TestGetLikingUsersHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath, capturedQuery string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"7","username":"alice","name":"Alice"}],"meta":{"result_count":1}}`))
	})
	handler := mcpinternal.NewGetLikingUsersHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tweet_id":    "42",
		"max_results": float64(50),
		"user_fields": []any{"username", "name"},
	}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/tweets/42/liking_users" {
		t.Errorf("path = %q, want /2/tweets/42/liking_users", capturedPath)
	}
	if !strings.Contains(capturedQuery, "max_results=50") {
		t.Errorf("query missing max_results: %s", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "user.fields=username%2Cname") {
		t.Errorf("query missing user.fields: %s", capturedQuery)
	}
}

func TestGetRetweetedByHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	})
	handler := mcpinternal.NewGetRetweetedByHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"tweet_id": "42"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/tweets/42/retweeted_by" {
		t.Errorf("path = %q, want /2/tweets/42/retweeted_by", capturedPath)
	}
}

func TestGetLikingUsersHandler_MaxResultsOutOfRange(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("X API should not be called for invalid max_results")
	})
	handler := mcpinternal.NewGetLikingUsersHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tweet_id":    "42",
		"max_results": float64(0),
	}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for max_results=0")
	}
}

// ---------- get_quote_tweets ----------

func TestGetQuoteTweetsHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath, capturedQuery string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"99","text":"quote"}],"meta":{"result_count":1}}`))
	})
	handler := mcpinternal.NewGetQuoteTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"tweet_id":    "42",
		"max_results": float64(20),
		"exclude":     []any{"replies"},
	}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/tweets/42/quote_tweets" {
		t.Errorf("path = %q, want /2/tweets/42/quote_tweets", capturedPath)
	}
	if !strings.Contains(capturedQuery, "max_results=20") {
		t.Errorf("query missing max_results: %s", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "exclude=replies") {
		t.Errorf("query missing exclude: %s", capturedQuery)
	}
}

// ---------- 4xx propagation ----------

func TestGetTweetHandler_404(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found Error","status":404}`))
	})
	handler := mcpinternal.NewGetTweetHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"tweet_id": "missing"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for 404")
	}
}
