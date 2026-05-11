package http_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdnet "net"
	stdhttp "net/http"
	"strings"
	"testing"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"

	mcpinternal "github.com/youyo/x/internal/mcp"
	transporthttp "github.com/youyo/x/internal/transport/http"
)

// freePort はテスト用にランダムなローカルポートを払い出す。
// listen → 即 close → ポート番号返却 (短時間 race は許容)。
func freePort(t *testing.T) string {
	t.Helper()
	l, err := stdnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen 0: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

// waitListening は addr/path に POST OPTIONS 連打して接続成立まで待つ。
// 最大 4 秒 (200ms × 20)。
func waitListening(t *testing.T, addr, path string) {
	t.Helper()
	url := fmt.Sprintf("http://%s%s", addr, path)
	client := &stdhttp.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		req, err := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodPost, url, strings.NewReader(""))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		// connection refused のうちは listen 未完了。short sleep でリトライ。
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("server did not start listening on %s within 4s", addr)
}

// TestNewServer_Defaults は Option 未指定で既定値が反映されることを確認する。
func TestNewServer_Defaults(t *testing.T) {
	t.Parallel()

	mcp := mcpinternal.NewServer(nil, "test")
	srv := transporthttp.NewServer(mcp)

	if got := srv.Addr(); got != transporthttp.DefaultAddr {
		t.Errorf("Addr() = %q, want %q", got, transporthttp.DefaultAddr)
	}
	if got := srv.Path(); got != transporthttp.DefaultPath {
		t.Errorf("Path() = %q, want %q", got, transporthttp.DefaultPath)
	}
	if got := srv.ShutdownTimeout(); got != transporthttp.DefaultShutdownTimeout {
		t.Errorf("ShutdownTimeout() = %v, want %v", got, transporthttp.DefaultShutdownTimeout)
	}
}

// TestNewServer_WithOptions は各 Option が反映されることを確認する。
func TestNewServer_WithOptions(t *testing.T) {
	t.Parallel()

	mcp := mcpinternal.NewServer(nil, "test")
	srv := transporthttp.NewServer(mcp,
		transporthttp.WithAddr("0.0.0.0:9090"),
		transporthttp.WithPath("/custom"),
		transporthttp.WithShutdownTimeout(5*time.Second),
	)

	if got := srv.Addr(); got != "0.0.0.0:9090" {
		t.Errorf("Addr() = %q, want %q", got, "0.0.0.0:9090")
	}
	if got := srv.Path(); got != "/custom" {
		t.Errorf("Path() = %q, want %q", got, "/custom")
	}
	if got := srv.ShutdownTimeout(); got != 5*time.Second {
		t.Errorf("ShutdownTimeout() = %v, want %v", got, 5*time.Second)
	}
}

// TestRun_ContextCancel_GracefulShutdown は ctx 終了で Run が nil を返すことを確認する。
func TestRun_ContextCancel_GracefulShutdown(t *testing.T) {
	t.Parallel()

	addr := freePort(t)
	mcp := mcpinternal.NewServer(nil, "test")
	srv := transporthttp.NewServer(mcp,
		transporthttp.WithAddr(addr),
		transporthttp.WithShutdownTimeout(2*time.Second),
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	runErr := make(chan error, 1)
	go func() {
		runErr <- srv.Run(ctx)
	}()

	waitListening(t, addr, transporthttp.DefaultPath)
	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run() returned error on graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return within 5s after ctx cancel")
	}
}

// TestRun_HandlesInitializeRequest は initialize リクエストに正しく応答することを確認する。
// Content-Type は application/json または text/event-stream の両方に対応する。
func TestRun_HandlesInitializeRequest(t *testing.T) {
	t.Parallel()

	addr := freePort(t)
	// version は server.serverInfo.version に反映されるので明示的に設定
	mcpSrv := mcpserver.NewMCPServer(
		mcpinternal.ServerName,
		"1.2.3",
		mcpserver.WithToolCapabilities(true),
	)
	srv := transporthttp.NewServer(mcpSrv, transporthttp.WithAddr(addr))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

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

	waitListening(t, addr, transporthttp.DefaultPath)

	url := fmt.Sprintf("http://%s%s", addr, transporthttp.DefaultPath)
	payload := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"clientInfo":{"name":"test","version":"1.0.0"}}}`

	req, err := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	client := &stdhttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST initialize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != stdhttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(body))
	}

	ct := resp.Header.Get("Content-Type")
	t.Logf("response Content-Type = %q", ct)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	envelope := extractJSONRPCEnvelope(t, ct, body)
	verifyInitializeResult(t, envelope, "x", "1.2.3")
}

// extractJSONRPCEnvelope は Content-Type に応じて body を JSON-RPC envelope に decode する。
func extractJSONRPCEnvelope(t *testing.T, contentType string, body []byte) map[string]any {
	t.Helper()

	var jsonBytes []byte
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		jsonBytes = body
	case strings.HasPrefix(contentType, "text/event-stream"):
		// SSE: "data: {...}\n\n" の data: 行を抽出
		scanner := bufio.NewScanner(bytes.NewReader(body))
		// SSE event は長くなりうるので buffer サイズを拡張
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				jsonBytes = []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				break
			}
		}
		if scanner.Err() != nil {
			t.Fatalf("scan SSE body: %v", scanner.Err())
		}
		if len(jsonBytes) == 0 {
			t.Fatalf("SSE response has no 'data:' line; body=%s", string(body))
		}
	default:
		t.Fatalf("unsupported Content-Type %q; body=%s", contentType, string(body))
	}

	var envelope map[string]any
	if err := json.Unmarshal(jsonBytes, &envelope); err != nil {
		t.Fatalf("unmarshal JSON-RPC envelope: %v; raw=%s", err, string(jsonBytes))
	}
	return envelope
}

// verifyInitializeResult は initialize レスポンスの serverInfo を検証する。
func verifyInitializeResult(t *testing.T, envelope map[string]any, wantName, wantVersion string) {
	t.Helper()

	if v, ok := envelope["jsonrpc"].(string); !ok || v != "2.0" {
		t.Errorf("jsonrpc = %v, want \"2.0\"", envelope["jsonrpc"])
	}

	result, ok := envelope["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing or wrong-typed result: %v", envelope)
	}
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("missing or wrong-typed serverInfo: %v", result)
	}
	if got, _ := serverInfo["name"].(string); got != wantName {
		t.Errorf("serverInfo.name = %q, want %q", got, wantName)
	}
	if got, _ := serverInfo["version"].(string); got != wantVersion {
		t.Errorf("serverInfo.version = %q, want %q", got, wantVersion)
	}
}

// TestRun_ReturnsListenError は既使用ポートへの bind で error が返ることを確認する。
// 具体的な syscall.EADDRINUSE 比較は OS 依存のため避け、「非 nil error が短時間で返る」のみを契約とする。
func TestRun_ReturnsListenError(t *testing.T) {
	t.Parallel()

	// 自前で listen して占有
	l, err := stdnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for occupy: %v", err)
	}
	defer func() { _ = l.Close() }()
	addr := l.Addr().String()

	mcp := mcpinternal.NewServer(nil, "test")
	srv := transporthttp.NewServer(mcp, transporthttp.WithAddr(addr))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- srv.Run(ctx)
	}()

	select {
	case err := <-runErr:
		if err == nil {
			t.Fatal("Run() returned nil error on occupied port, want non-nil")
		}
		// EADDRINUSE / address already in use の文字列は OS 依存だが、
		// errors.Is / 文字列ともに contains で軽くチェック (情報目的)
		t.Logf("listen error (informational): %v", err)
		// errors.Is でも見られるなら追加検証
		var opErr *stdnet.OpError
		if errors.As(err, &opErr) {
			t.Logf("net.OpError detected: op=%s", opErr.Op)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return within 2s on listen error")
	}
}
