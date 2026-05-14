package mcp_test

import (
	"context"
	"net/http"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"

	mcpinternal "github.com/youyo/x/internal/mcp"
)

func TestGetUserHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
	})
	handler := mcpinternal.NewGetUserHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"user_id": "42"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/users/42" {
		t.Errorf("path = %q, want /2/users/42", capturedPath)
	}
}

func TestGetUserHandler_MissingUserID(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("API should not be called without user_id")
	})
	handler := mcpinternal.NewGetUserHandler(client)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for missing user_id")
	}
}

func TestGetUserByUsernameHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
	})
	handler := mcpinternal.NewGetUserByUsernameHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"username": "alice"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/users/by/username/alice" {
		t.Errorf("path = %q, want /2/users/by/username/alice", capturedPath)
	}
}

func TestGetUserFollowingHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	})
	handler := mcpinternal.NewGetUserFollowingHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"user_id": "42"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/users/42/following" {
		t.Errorf("path = %q, want /2/users/42/following", capturedPath)
	}
}

func TestGetUserFollowersHandler_Success(t *testing.T) {
	t.Parallel()
	var capturedPath string
	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	})
	handler := mcpinternal.NewGetUserFollowersHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"user_id": "42"}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true; content=%s", extractTextContent(t, res))
	}
	if capturedPath != "/2/users/42/followers" {
		t.Errorf("path = %q, want /2/users/42/followers", capturedPath)
	}
}

func TestGetUserFollowingHandler_MaxResultsOutOfRange(t *testing.T) {
	t.Parallel()
	client := newTestXAPIClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("API should not be called for out-of-range max_results")
	})
	handler := mcpinternal.NewGetUserFollowingHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"user_id": "42", "max_results": float64(1001)}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for max_results=1001")
	}
}
