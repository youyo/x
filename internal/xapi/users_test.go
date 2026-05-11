package xapi

// 注意: 本ファイルは package xapi の internal test (xapi_test ではない)。
// client_test.go と同じテストヘルパ (newTestClient / withHTTPClient / withSleep) を共有するため。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// TestGetUserMe_HitsCorrectEndpoint は GET /2/users/me に対して正しいパスで
// HTTP リクエストが送られることを確認する。
func TestGetUserMe_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserMe(context.Background())
	if err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	if gotPath != "/2/users/me" {
		t.Errorf("path = %q, want /2/users/me", gotPath)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
}

// TestGetUserMe_Success は 200 OK + 正常 JSON で User が返ることを確認する。
func TestGetUserMe_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	u, err := c.GetUserMe(context.Background())
	if err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	if u == nil {
		t.Fatal("user = nil, want non-nil")
	}
	if u.ID != "42" {
		t.Errorf("ID = %q, want %q", u.ID, "42")
	}
	if u.Username != "alice" {
		t.Errorf("Username = %q, want %q", u.Username, "alice")
	}
	if u.Name != "Alice" {
		t.Errorf("Name = %q, want %q", u.Name, "Alice")
	}
}

// TestGetUserMe_Success_WithFields は user.fields 指定時のレスポンスが
// User.Verified / User.PublicMetrics に展開されることを確認する。
func TestGetUserMe_Success_WithFields(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{
			"id":"42","username":"alice","name":"Alice",
			"verified":true,
			"public_metrics":{"followers_count":100,"following_count":50,"tweet_count":300,"listed_count":4}
		}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	u, err := c.GetUserMe(context.Background(), WithUserFields("verified", "public_metrics"))
	if err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	if !u.Verified {
		t.Errorf("Verified = false, want true")
	}
	if u.PublicMetrics == nil {
		t.Fatal("PublicMetrics = nil")
	}
	if u.PublicMetrics.FollowersCount != 100 {
		t.Errorf("FollowersCount = %d, want 100", u.PublicMetrics.FollowersCount)
	}
}

// TestGetUserMe_WithUserFields_AppendsQueryParam は
// WithUserFields(...) が ?user.fields=... に変換されることを確認する。
func TestGetUserMe_WithUserFields_AppendsQueryParam(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"u","name":"N"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserMe(
		context.Background(),
		WithUserFields("username", "name", "verified", "public_metrics"),
	)
	if err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	got := gotQuery.Get("user.fields")
	want := "username,name,verified,public_metrics"
	if got != want {
		t.Errorf("user.fields = %q, want %q", got, want)
	}
}

// TestGetUserMe_NoOptions_NoQuery は opts 無しで呼んだ場合 URL に ? が付かないことを確認する。
func TestGetUserMe_NoOptions_NoQuery(t *testing.T) {
	t.Parallel()

	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"u","name":"N"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetUserMe(context.Background()); err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	if gotRawQuery != "" {
		t.Errorf("RawQuery = %q, want empty", gotRawQuery)
	}
}

// TestGetUserMe_WithUserFields_Empty は WithUserFields() を空引数で呼んでも
// クエリパラメータが付与されないことを確認する。
func TestGetUserMe_WithUserFields_Empty(t *testing.T) {
	t.Parallel()

	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"u","name":"N"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetUserMe(context.Background(), WithUserFields()); err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	if gotRawQuery != "" {
		t.Errorf("RawQuery = %q, want empty (WithUserFields() should be no-op)", gotRawQuery)
	}
}

// TestGetUserMe_401_AuthError は 401 で ErrAuthentication が返ることを確認する。
func TestGetUserMe_401_AuthError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserMe(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAuthentication) {
		t.Errorf("errors.Is(err, ErrAuthentication) = false, want true (err=%v)", err)
	}
}

// TestGetUserMe_404_NotFound は 404 で ErrNotFound が返ることを確認する。
func TestGetUserMe_404_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserMe(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err=%v)", err)
	}
}

// TestGetUserMe_ContextCanceled は事前に cancel された context を渡すと
// context.Canceled が返ることを確認する。
func TestGetUserMe_ContextCanceled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// このハンドラは呼ばれない想定 (context cancel 済みなので)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 即 cancel

	_, err := c.GetUserMe(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestGetUserMe_InvalidJSON は 200 だが本体が型不一致 JSON のとき
// 明示的なエラーが返り、リトライしないこと (server call 1 回) を確認する。
func TestGetUserMe_InvalidJSON(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		// data が文字列 (型不一致) で確実に Unmarshal を失敗させる
		_, _ = w.Write([]byte(`{"data":"not-an-object"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserMe(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 番兵エラーには該当しないことを確認
	for _, sentinel := range []error{ErrAuthentication, ErrPermission, ErrNotFound, ErrRateLimit} {
		if errors.Is(err, sentinel) {
			t.Errorf("err unexpectedly matched sentinel %v", sentinel)
		}
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error message %q, want substring 'decode'", err.Error())
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server calls = %d, want 1 (no retry on decode error)", got)
	}
}
