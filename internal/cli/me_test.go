package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// stubMeClientFactory は newMeClient を httptest サーバ向けに差し替えるテストヘルパ。
// t.Cleanup で元の関数に戻す。
func stubMeClientFactory(t *testing.T, baseURL string) {
	t.Helper()
	prev := newMeClient
	t.Cleanup(func() { newMeClient = prev })
	newMeClient = func(ctx context.Context, _ *config.Credentials) (meClient, error) {
		return xapi.NewClient(ctx, nil, xapi.WithBaseURL(baseURL)), nil
	}
}

// TestMe_Success_JSON は `x me` が GET /2/users/me を呼び出し、
// JSON 出力 ({"id":"...","username":"...","name":"..."}) を行うことを検証する。
func TestMe_Success_JSON(t *testing.T) {
	setAllXAPIEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
	}))
	defer srv.Close()

	stubMeClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"me"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var got struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON output: %v, output: %q", err, buf.String())
	}
	if got.ID != "42" || got.Username != "alice" || got.Name != "Alice" {
		t.Errorf("got = %+v, want {ID:42 Username:alice Name:Alice}", got)
	}
}

// TestMe_Success_NoJSON は `x me --no-json` が human-readable な
// `id=... username=... name=...` 形式で出力することを検証する。
func TestMe_Success_NoJSON(t *testing.T) {
	setAllXAPIEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
	}))
	defer srv.Close()

	stubMeClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"me", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"id=42", "username=alice", "name=Alice"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %q", want, out)
		}
	}
	// JSON ではないことの確認
	var any map[string]any
	if err := json.Unmarshal(buf.Bytes(), &any); err == nil {
		t.Errorf("expected non-JSON output, got JSON: %q", out)
	}
}

// TestMe_CredentialsMissing は env / file ともに無いとき
// ErrCredentialsMissing を返すことを検証する (caller で exit 3 に写像)。
func TestMe_CredentialsMissing(t *testing.T) {
	clearXAPIEnv(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"me"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrCredentialsMissing) {
		t.Errorf("errors.Is(err, ErrCredentialsMissing) = false (err=%v)", err)
	}
	if !errors.Is(err, xapi.ErrAuthentication) {
		t.Errorf("errors.Is(err, xapi.ErrAuthentication) = false (err=%v)", err)
	}
}

// TestMe_401_AuthError は X API が 401 を返した場合
// xapi.ErrAuthentication が返ることを検証する (exit 3)。
func TestMe_401_AuthError(t *testing.T) {
	setAllXAPIEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401}`))
	}))
	defer srv.Close()

	stubMeClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"me"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, xapi.ErrAuthentication) {
		t.Errorf("errors.Is(err, xapi.ErrAuthentication) = false (err=%v)", err)
	}
}

// TestMe_404_NotFound は X API が 404 を返した場合
// xapi.ErrNotFound が返ることを検証する (exit 5)。
func TestMe_404_NotFound(t *testing.T) {
	setAllXAPIEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	stubMeClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"me"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, xapi.ErrNotFound) {
		t.Errorf("errors.Is(err, xapi.ErrNotFound) = false (err=%v)", err)
	}
}

// TestMe_HelpShowsNoJSON は `x me --help` に --no-json フラグが
// 表示されることを検証する。
func TestMe_HelpShowsNoJSON(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"me", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "--no-json") {
		t.Errorf("help output missing --no-json, got: %s", out)
	}
}

// TestRootHelpShowsMe は `x --help` に me サブコマンドが表示されることを検証する。
func TestRootHelpShowsMe(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "me") {
		t.Errorf("help output missing me, got: %s", out)
	}
}
