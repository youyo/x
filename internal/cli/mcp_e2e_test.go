package cli_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	gomcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/youyo/idproxy/testutil"
	"github.com/youyo/x/internal/authgate"
	"github.com/youyo/x/internal/config"
	mcpinternal "github.com/youyo/x/internal/mcp"
	transporthttp "github.com/youyo/x/internal/transport/http"
	"github.com/youyo/x/internal/xapi"
)

// e2e_user_me_response は X API GET /2/users/me のモック応答。
const e2eUserMeResponse = `{"data":{"id":"99","username":"e2e-bob","name":"E2E Bob"}}`

// e2e_liked_response は X API GET /2/users/:id/liked_tweets のモック応答。
const e2eLikedResponse = `{"data":[{"id":"1001","text":"hello","author_id":"99","created_at":"2026-05-11T00:00:00Z"}],"includes":{},"meta":{"result_count":1}}`

// startMockXAPI は X API モックサーバーを起動する。
func startMockXAPI(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/2/users/me", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(e2eUserMeResponse))
	})
	mux.HandleFunc("/2/users/", func(w http.ResponseWriter, r *http.Request) {
		// /2/users/{id}/liked_tweets
		if !strings.HasSuffix(r.URL.Path, "/liked_tweets") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(e2eLikedResponse))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// freeAddr はランダムなローカルアドレス (127.0.0.1:0) を払い出す。
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen 0: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

// waitForMcpListening は addr/path に POST を打って listen 成立まで待つ (最大 4 秒)。
func waitForMcpListening(t *testing.T, addr, path string) {
	t.Helper()
	url := fmt.Sprintf("http://%s%s", addr, path)
	c := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(""))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := c.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("server did not start listening on %s within 4s", addr)
}

// mcpServerHandle は startMcpServer の戻り値で、サーバー URL と停止関数を持つ。
type mcpServerHandle struct {
	BaseURL string // 例: "http://127.0.0.1:38123"
	Path    string // 例: "/mcp"
}

// startMcpServer は xapi モック + 指定 middleware で transport/http.Server を起動する。
// テスト終了時に自動停止する。
func startMcpServer(t *testing.T, mw authgate.Middleware, xapiBaseURL string) *mcpServerHandle {
	t.Helper()
	addr := freeAddr(t)
	xapiClient := xapi.NewClient(context.Background(), &config.Credentials{
		APIKey:            "k",
		APISecret:         "s",
		AccessToken:       "tk",
		AccessTokenSecret: "tks",
	}, xapi.WithBaseURL(xapiBaseURL))
	mcpSrv := mcpinternal.NewServer(xapiClient, "test")

	srv := transporthttp.NewServer(
		mcpSrv,
		transporthttp.WithAddr(addr),
		transporthttp.WithPath("/mcp"),
		transporthttp.WithHandlerMiddleware(mw.Wrap),
		transporthttp.WithShutdownTimeout(2*time.Second),
	)

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() {
		runErr <- srv.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-runErr:
		case <-time.After(3 * time.Second):
		}
	})

	waitForMcpListening(t, addr, "/mcp")
	return &mcpServerHandle{
		BaseURL: "http://" + addr,
		Path:    "/mcp",
	}
}

// connectMCPClient は MCP client を立ち上げて Initialize までを行う。
// headers が non-nil なら HTTP 経路で Authorization 等のヘッダを付与する。
func connectMCPClient(t *testing.T, h *mcpServerHandle, headers map[string]string) *client.Client {
	t.Helper()
	opts := []transport.StreamableHTTPCOption{}
	if headers != nil {
		opts = append(opts, transport.WithHTTPHeaders(headers))
	}
	c, err := client.NewStreamableHttpClient(h.BaseURL+h.Path, opts...)
	if err != nil {
		t.Fatalf("NewStreamableHttpClient: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("client.Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	req := gomcp.InitializeRequest{}
	req.Params.ProtocolVersion = gomcp.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = gomcp.Implementation{Name: "e2e-test", Version: "1.0.0"}

	if _, err := c.Initialize(ctx, req); err != nil {
		t.Fatalf("client.Initialize: %v", err)
	}
	return c
}

// callTool は CallTool ヘルパ。
func callTool(t *testing.T, c *client.Client, name string) *gomcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := gomcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = map[string]any{}
	res, err := c.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}

// assertCallToolSuccess は IsError=false と structuredContent / text の整合性を確認する。
func assertCallToolSuccess(t *testing.T, res *gomcp.CallToolResult, wantTextSubstring string) {
	t.Helper()
	if res == nil {
		t.Fatal("nil CallToolResult")
	}
	if res.IsError {
		// content から payload を引っ張ってデバッグ
		for _, c := range res.Content {
			if tc, ok := c.(gomcp.TextContent); ok {
				t.Logf("IsError=true text: %s", tc.Text)
			}
		}
		t.Fatal("CallToolResult.IsError = true, want false")
	}
	if wantTextSubstring != "" {
		found := false
		for _, c := range res.Content {
			if tc, ok := c.(gomcp.TextContent); ok && strings.Contains(tc.Text, wantTextSubstring) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("CallToolResult.Content does not contain %q; got: %+v", wantTextSubstring, res.Content)
		}
	}
}

// ---------- Mode: none ----------

func TestMCP_E2E_NoneMode_GetUserMe(t *testing.T) {
	xapiMock := startMockXAPI(t)
	mw, err := authgate.New(authgate.ModeNone)
	if err != nil {
		t.Fatalf("authgate.New(none): %v", err)
	}

	h := startMcpServer(t, mw, xapiMock.URL)
	c := connectMCPClient(t, h, nil)
	res := callTool(t, c, "get_user_me")
	assertCallToolSuccess(t, res, "e2e-bob")
}

func TestMCP_E2E_NoneMode_GetLikedTweets(t *testing.T) {
	xapiMock := startMockXAPI(t)
	mw, err := authgate.New(authgate.ModeNone)
	if err != nil {
		t.Fatalf("authgate.New(none): %v", err)
	}

	h := startMcpServer(t, mw, xapiMock.URL)
	c := connectMCPClient(t, h, nil)
	res := callTool(t, c, "get_liked_tweets")
	assertCallToolSuccess(t, res, "1001")
}

// ---------- Mode: apikey ----------

func TestMCP_E2E_ApiKeyMode_ValidBearer_GetUserMe(t *testing.T) {
	xapiMock := startMockXAPI(t)
	const apiKey = "secret-key-1234"
	mw, err := authgate.New(authgate.ModeAPIKey, authgate.WithAPIKey(apiKey))
	if err != nil {
		t.Fatalf("authgate.New(apikey): %v", err)
	}

	h := startMcpServer(t, mw, xapiMock.URL)
	c := connectMCPClient(t, h, map[string]string{"Authorization": "Bearer " + apiKey})
	res := callTool(t, c, "get_user_me")
	assertCallToolSuccess(t, res, "e2e-bob")
}

func TestMCP_E2E_ApiKeyMode_ValidBearer_GetLikedTweets(t *testing.T) {
	xapiMock := startMockXAPI(t)
	const apiKey = "secret-key-1234"
	mw, err := authgate.New(authgate.ModeAPIKey, authgate.WithAPIKey(apiKey))
	if err != nil {
		t.Fatalf("authgate.New(apikey): %v", err)
	}

	h := startMcpServer(t, mw, xapiMock.URL)
	c := connectMCPClient(t, h, map[string]string{"Authorization": "Bearer " + apiKey})
	res := callTool(t, c, "get_liked_tweets")
	assertCallToolSuccess(t, res, "1001")
}

// TestMCP_E2E_ApiKeyMode_InvalidBearer_Returns401 は不正な Bearer で 401 が返ることを HTTP raw で確認する。
// mark3labs/mcp-go の client は 401 をエラーとして扱うが、エラー型が安定しないので raw HTTP で検証する。
func TestMCP_E2E_ApiKeyMode_InvalidBearer_Returns401(t *testing.T) {
	xapiMock := startMockXAPI(t)
	const apiKey = "secret-key-1234"
	mw, err := authgate.New(authgate.ModeAPIKey, authgate.WithAPIKey(apiKey))
	if err != nil {
		t.Fatalf("authgate.New(apikey): %v", err)
	}

	h := startMcpServer(t, mw, xapiMock.URL)
	resp := postInitialize(t, h, "Bearer wrong-token")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 with invalid Bearer", resp.StatusCode)
	}
}

func TestMCP_E2E_ApiKeyMode_MissingBearer_Returns401(t *testing.T) {
	xapiMock := startMockXAPI(t)
	const apiKey = "secret-key-1234"
	mw, err := authgate.New(authgate.ModeAPIKey, authgate.WithAPIKey(apiKey))
	if err != nil {
		t.Fatalf("authgate.New(apikey): %v", err)
	}

	h := startMcpServer(t, mw, xapiMock.URL)
	resp := postInitialize(t, h, "")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 without Bearer", resp.StatusCode)
	}
}

// ---------- Mode: idproxy (memory store) ----------

// TestMCP_E2E_IdProxyMode_Unauthenticated_RejectsRequest は idproxy + memory store で
// 未認証リクエストが reject されることを確認する。
//
// 認証成功パスは OIDC callback 実行が必要で本 E2E ではスコープ外。
// 「3 モード切替が middleware 注入として正しく機能する」までを契約として pin する。
func TestMCP_E2E_IdProxyMode_Unauthenticated_RejectsRequest(t *testing.T) {
	xapiMock := startMockXAPI(t)

	// idproxy 用 mock IdP を立ち上げる (httptest ベース)
	mockIDP := testutil.NewMockIdP(t)

	mw, err := authgate.New(
		authgate.ModeIDProxy,
		authgate.WithOIDCIssuer(mockIDP.Issuer()),
		authgate.WithOIDCClientID("client-id"),
		authgate.WithOIDCClientSecret("client-secret"),
		authgate.WithCookieSecret(randomCookieSecretHex(t)),
		authgate.WithExternalURL("http://localhost:8080"),
	)
	if err != nil {
		t.Fatalf("authgate.New(idproxy): %v", err)
	}

	h := startMcpServer(t, mw, xapiMock.URL)
	// cookie なしで POST /mcp → idproxy が未認証として reject する
	resp := postInitialize(t, h, "")
	defer func() { _ = resp.Body.Close() }()

	// idproxy は未認証時に 401 or redirect (302/303) のいずれかを返しうる。
	// 「200 で素通りしないこと」を契約とする。
	if resp.StatusCode == http.StatusOK {
		t.Errorf("status = 200, want non-200 (unauthenticated MCP request must be rejected by idproxy)")
	}
}

// TestMCP_E2E_IdProxyMode_SecondTool_StillRejected は get_liked_tweets でも同じく未認証で reject されることを確認する。
func TestMCP_E2E_IdProxyMode_SecondTool_StillRejected(t *testing.T) {
	xapiMock := startMockXAPI(t)
	mockIDP := testutil.NewMockIdP(t)

	mw, err := authgate.New(
		authgate.ModeIDProxy,
		authgate.WithOIDCIssuer(mockIDP.Issuer()),
		authgate.WithOIDCClientID("client-id"),
		authgate.WithOIDCClientSecret("client-secret"),
		authgate.WithCookieSecret(randomCookieSecretHex(t)),
		authgate.WithExternalURL("http://localhost:8080"),
	)
	if err != nil {
		t.Fatalf("authgate.New(idproxy): %v", err)
	}

	h := startMcpServer(t, mw, xapiMock.URL)
	// 直接 tools/call (initialize しないが、middleware が先で reject されるので無問題)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_liked_tweets","arguments":{}}}`
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.BaseURL+h.Path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST tools/call: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Errorf("status = 200, want non-200 (unauthenticated tools/call must be rejected by idproxy)")
	}
}

// ---------- helpers ----------

// postInitialize は initialize POST リクエストを送り、レスポンスを返す。
// authHeader が非空なら Authorization ヘッダに付与する。
func postInitialize(t *testing.T, h *mcpServerHandle, authHeader string) *http.Response {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"clientInfo":{"name":"e2e-test","version":"1.0.0"}}}`
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.BaseURL+h.Path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST initialize: %v", err)
	}
	return resp
}

// randomCookieSecretHex は 32 バイト分のランダム hex を返す。
func randomCookieSecretHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b)
}
