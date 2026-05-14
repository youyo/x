package mcp_test

import (
	"context"
	"net/http"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"

	mcpinternal "github.com/youyo/x/internal/mcp"
)

func TestGetListHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"100","name":"Tech"}}`))
	})
	handler := mcpinternal.NewGetListHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"list_id": "100"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/lists/100" {
		t.Errorf("path = %q, want /2/lists/100", capturedPath)
	}
}

func TestGetListHandler_MissingListID(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("API should not be called without list_id")
	})
	handler := mcpinternal.NewGetListHandler(client)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for missing list_id")
	}
}

func TestGetListTweetsHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"hi"}],"meta":{"result_count":1}}`))
	})
	handler := mcpinternal.NewGetListTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"list_id": "100"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/lists/100/tweets" {
		t.Errorf("path = %q, want /2/lists/100/tweets", capturedPath)
	}
}

func TestGetListTweetsHandler_MaxResultsOutOfRange(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("API should not be called for invalid max_results")
	})
	handler := mcpinternal.NewGetListTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"list_id": "100", "max_results": float64(0)}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for max_results=0")
	}
}
