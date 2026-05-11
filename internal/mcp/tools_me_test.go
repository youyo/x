package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gomcp "github.com/mark3labs/mcp-go/mcp"

	mcpinternal "github.com/youyo/x/internal/mcp"
	"github.com/youyo/x/internal/xapi"
)

// newTestXAPIClient はテスト用の xapi.Client (httptest server を BaseURL に紐付け) を構築する。
// クリーンアップは t.Cleanup で srv.Close を登録する。
func newTestXAPIClient(t *testing.T, handler http.HandlerFunc) *xapi.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return xapi.NewClient(context.Background(), nil, xapi.WithBaseURL(srv.URL))
}

// extractTextContent は CallToolResult の TextContent を 1 件抽出して返す。
// 想定外の構造 (Content 空 / 型違い) ならテストを fail させる。
func extractTextContent(t *testing.T, res *gomcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("CallToolResult is nil")
	}
	if len(res.Content) == 0 {
		t.Fatal("CallToolResult.Content is empty")
	}
	tc, ok := res.Content[0].(gomcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] is not TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

// TestGetUserMeResult_JSONShape は DTO 単体で json タグが MCP 仕様 (user_id) と
// 整合することを回帰防止として保証する。
func TestGetUserMeResult_JSONShape(t *testing.T) {
	t.Parallel()

	r := mcpinternal.GetUserMeResult{
		UserID:   "42",
		Username: "alice",
		Name:     "Alice",
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := string(b)
	for _, want := range []string{`"user_id":"42"`, `"username":"alice"`, `"name":"Alice"`} {
		if !strings.Contains(got, want) {
			t.Errorf("Marshal result does not contain %q: %s", want, got)
		}
	}
	// "id" 単独 (= xapi.User.ID の元 json タグ) が漏れていないこと
	if strings.Contains(got, `"id":`) {
		t.Errorf("Marshal result must not contain bare \"id\" key: %s", got)
	}
}

// TestNewGetUserMeHandler_Success_StructuredContent は 200 OK 時に
// StructuredContent が GetUserMeResult として user_id / username / name を含むことを確認する。
func TestNewGetUserMeHandler_Success_StructuredContent(t *testing.T) {
	t.Parallel()

	client := newTestXAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2/users/me" {
			t.Errorf("path = %q, want /2/users/me", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
	})

	handler := mcpinternal.NewGetUserMeHandler(client)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true, want false; content: %s", extractTextContent(t, res))
	}

	// StructuredContent (NewToolResultJSON は両方を埋める)
	structured, ok := res.StructuredContent.(mcpinternal.GetUserMeResult)
	if !ok {
		t.Fatalf("StructuredContent type = %T, want GetUserMeResult", res.StructuredContent)
	}
	if structured.UserID != "42" {
		t.Errorf("UserID = %q, want %q", structured.UserID, "42")
	}
	if structured.Username != "alice" {
		t.Errorf("Username = %q, want %q", structured.Username, "alice")
	}
	if structured.Name != "Alice" {
		t.Errorf("Name = %q, want %q", structured.Name, "Alice")
	}
}

// TestNewGetUserMeHandler_Success_TextContent は 200 OK 時に
// TextContent (fallback) が user_id キーを含む JSON 文字列であることを確認する。
// MCP クライアントが StructuredContent を扱えない実装に対する後方互換性の検証。
func TestNewGetUserMeHandler_Success_TextContent(t *testing.T) {
	t.Parallel()

	client := newTestXAPIClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
	})

	handler := mcpinternal.NewGetUserMeHandler(client)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true, want false")
	}

	text := extractTextContent(t, res)

	// "user_id" にリネームされたキーで JSON が出る
	var got mcpinternal.GetUserMeResult
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("TextContent is not JSON: %v, text: %s", err, text)
	}
	if got.UserID != "42" || got.Username != "alice" || got.Name != "Alice" {
		t.Errorf("decoded = %+v, want {UserID:42 Username:alice Name:Alice}", got)
	}
	// 念のため生文字列にも user_id キーが含まれる
	if !strings.Contains(text, `"user_id"`) {
		t.Errorf("TextContent should contain \"user_id\" key: %s", text)
	}
}

// TestNewGetUserMeHandler_Error_AuthFailed_401 は X API が 401 を返した場合に
// IsError=true の CallToolResult が返ること (ハンドラ自身は error を返さない) を確認する。
func TestNewGetUserMeHandler_Error_AuthFailed_401(t *testing.T) {
	t.Parallel()

	client := newTestXAPIClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401}`))
	})

	handler := mcpinternal.NewGetUserMeHandler(client)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler returned protocol-level error: %v (want IsError result instead)", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
	text := extractTextContent(t, res)
	if !strings.Contains(text, "401") && !strings.Contains(strings.ToLower(text), "auth") {
		t.Errorf("error text does not look like auth error: %s", text)
	}
}

// TestNewGetUserMeHandler_Error_NetworkFailure は X API が到達不能な場合 (httptest
// を Close 済み) でも IsError=true の CallToolResult が返ることを確認する。
func TestNewGetUserMeHandler_Error_NetworkFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // 直ちにクローズし、以降の接続を失敗させる

	client := xapi.NewClient(
		context.Background(), nil,
		xapi.WithBaseURL(url),
		xapi.WithMaxRetries(0),
	)
	handler := mcpinternal.NewGetUserMeHandler(client)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler returned protocol-level error: %v (want IsError result instead)", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true; content: %s", extractTextContent(t, res))
	}
}

// TestNewGetUserMeHandler_NilClient は nil クライアントを渡しても
// panic せず IsError=true で返ることを確認する。
func TestNewGetUserMeHandler_NilClient(t *testing.T) {
	t.Parallel()

	handler := mcpinternal.NewGetUserMeHandler(nil)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler returned protocol-level error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for nil client")
	}
}
