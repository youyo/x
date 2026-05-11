package xapi

// 注意: 本ファイルは package xapi の internal test (xapi_test ではない)。
// withSleep / withNow / withHTTPClient といった未公開オプションへアクセスするため。

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient はテスト用 httptest.Server に紐付いた Client を組み立てる。
// sleep はデフォルトで no-op spy にして実時間待機を避ける。
func newTestClient(t *testing.T, srv *httptest.Server, extra ...Option) (*Client, *[]time.Duration) {
	t.Helper()
	spy, recorded := recordedSleeper()
	opts := []Option{
		withHTTPClient(srv.Client()),
		WithBaseURL(srv.URL),
		withSleep(spy),
	}
	opts = append(opts, extra...)
	c := NewClient(context.Background(), nil, opts...)
	return c, recorded
}

// recordedSleeper は呼び出された duration を捕捉する no-op sleep を返す。
// ctx が cancel されていれば err を返す挙動も保持する。
func recordedSleeper() (func(context.Context, time.Duration) error, *[]time.Duration) {
	var (
		mu sync.Mutex
		d  []time.Duration
	)
	fn := func(ctx context.Context, t time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		mu.Lock()
		d = append(d, t)
		mu.Unlock()
		return nil
	}
	return fn, &d
}

func newRequest(t *testing.T, ctx context.Context, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	return req
}

// TestDo_Success_ParsesRateLimitHeaders は 200 OK 時に x-rate-limit-* が
// Response.RateLimit に構造化されることを確認する。
func TestDo_Success_ParsesRateLimitHeaders(t *testing.T) {
	t.Parallel()

	resetUnix := time.Now().Add(60 * time.Second).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-rate-limit-limit", "75")
		w.Header().Set("x-rate-limit-remaining", "42")
		w.Header().Set("x-rate-limit-reset", strconv.FormatInt(resetUnix, 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.Do(newRequest(t, context.Background(), srv.URL+"/2/users/me"))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.RateLimit.Limit != 75 {
		t.Errorf("Limit = %d, want 75", resp.RateLimit.Limit)
	}
	if resp.RateLimit.Remaining != 42 {
		t.Errorf("Remaining = %d, want 42", resp.RateLimit.Remaining)
	}
	if resp.RateLimit.Reset.Unix() != resetUnix {
		t.Errorf("Reset = %d, want %d", resp.RateLimit.Reset.Unix(), resetUnix)
	}
	if !resp.RateLimit.Raw {
		t.Error("Raw = false, want true")
	}
}

// TestDo_Success_NoRateLimitHeaders は x-rate-limit-* が無いレスポンスでは
// Remaining=-1, Raw=false で返ることを確認する (M8 で誤発火させないため)。
func TestDo_Success_NoRateLimitHeaders(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.Do(newRequest(t, context.Background(), srv.URL))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.RateLimit.Raw {
		t.Error("Raw = true, want false (no headers)")
	}
	if resp.RateLimit.Remaining != -1 {
		t.Errorf("Remaining = %d, want -1 (sentinel for missing)", resp.RateLimit.Remaining)
	}
}

// TestDo_401_ReturnsAuthenticationError は 401 で ErrAuthentication と
// APIError がどちらも errors パッケージ経由で取り出せることを確認する。
func TestDo_401_ReturnsAuthenticationError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.Do(newRequest(t, context.Background(), srv.URL))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAuthentication) {
		t.Errorf("errors.Is(err, ErrAuthentication) = false, want true (err=%v)", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As failed for %T", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
	if !strings.Contains(string(apiErr.Body), "unauthorized") {
		t.Errorf("Body = %q, want substring 'unauthorized'", apiErr.Body)
	}
}

// TestDo_403_ReturnsPermissionError は 403 が ErrPermission に写像されることを確認する。
func TestDo_403_ReturnsPermissionError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.Do(newRequest(t, context.Background(), srv.URL))
	if !errors.Is(err, ErrPermission) {
		t.Errorf("errors.Is(err, ErrPermission) = false, want true (err=%v)", err)
	}
}

// TestDo_404_ReturnsNotFoundError は 404 が ErrNotFound に写像されることを確認する。
func TestDo_404_ReturnsNotFoundError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.Do(newRequest(t, context.Background(), srv.URL))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err=%v)", err)
	}
}

// TestDo_400_ReturnsAPIErrorOnly は 400 のように番兵にマッピングされない 4xx で
// APIError は返るが番兵 errors.Is は全て false になることを確認する。
func TestDo_400_ReturnsAPIErrorOnly(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.Do(newRequest(t, context.Background(), srv.URL))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, sentinel := range []error{ErrAuthentication, ErrPermission, ErrNotFound, ErrRateLimit} {
		if errors.Is(err, sentinel) {
			t.Errorf("err unexpectedly matched sentinel %v", sentinel)
		}
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("APIError = %v, want StatusCode 400", apiErr)
	}
}

// TestDo_429_RetriesAndSucceeds は 429 を 3 連続で受けた後 200 になるシナリオで
// max_retries=3 ぎりぎりで成功することを確認する。
func TestDo_429_RetriesAndSucceeds(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n <= 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, sleeps := newTestClient(t, srv)
	resp, err := c.Do(newRequest(t, context.Background(), srv.URL))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Errorf("server calls = %d, want 4 (3 retries + final success)", got)
	}
	if len(*sleeps) != 3 {
		t.Errorf("sleep count = %d, want 3", len(*sleeps))
	}
}

// TestDo_429_ExhaustsRetries_ReturnsRateLimitSentinel は 429 が max_retries+1 回
// 続いた場合に ErrRateLimit が返ることを確認する。
func TestDo_429_ExhaustsRetries_ReturnsRateLimitSentinel(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.Do(newRequest(t, context.Background(), srv.URL))
	if !errors.Is(err, ErrRateLimit) {
		t.Errorf("errors.Is(err, ErrRateLimit) = false, want true (err=%v)", err)
	}
}

// TestDo_5xx_RetriesAndSucceeds は 500 を 1 度受けた後 200 になるシナリオを確認する。
func TestDo_5xx_RetriesAndSucceeds(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, sleeps := newTestClient(t, srv)
	resp, err := c.Do(newRequest(t, context.Background(), srv.URL))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	if len(*sleeps) != 1 {
		t.Errorf("sleep count = %d, want 1", len(*sleeps))
	}
}

// TestDo_5xx_Exhausts_NoSentinel は 5xx 枯渇時に APIError は返るが
// 番兵 (ErrRateLimit / ErrAuthentication 等) は付かないことを確認する。
func TestDo_5xx_Exhausts_NoSentinel(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.Do(newRequest(t, context.Background(), srv.URL))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, sentinel := range []error{ErrAuthentication, ErrPermission, ErrNotFound, ErrRateLimit} {
		if errors.Is(err, sentinel) {
			t.Errorf("err unexpectedly matched sentinel %v", sentinel)
		}
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As failed for %T", err)
	}
	if apiErr.StatusCode != http.StatusBadGateway {
		t.Errorf("StatusCode = %d, want 502", apiErr.StatusCode)
	}
}

// TestDo_429_RespectsRateLimitResetHeader は 429 で x-rate-limit-reset があれば
// reset 時刻までの差分が backoff として採用されることを確認する。
func TestDo_429_RespectsRateLimitResetHeader(t *testing.T) {
	t.Parallel()

	// "現在時刻" を固定 (now() を差し替える)
	fixedNow := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	resetAt := fixedNow.Add(5 * time.Second)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("x-rate-limit-reset", strconv.FormatInt(resetAt.Unix(), 10))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, sleeps := newTestClient(t, srv, withNow(func() time.Time { return fixedNow }))
	resp, err := c.Do(newRequest(t, context.Background(), srv.URL))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()

	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1", len(*sleeps))
	}
	// reset Unix は秒精度のため誤差 ±1s を許容する
	diff := (*sleeps)[0] - 5*time.Second
	if diff < -time.Second || diff > time.Second {
		t.Errorf("sleep[0] = %v, want ≈ 5s", (*sleeps)[0])
	}
}

// TestDo_5xx_ExponentialBackoffProgression は 5xx 連続時の backoff 系列が
// exp backoff (2s, 4s, 8s) になることを確認する (maxBackoff=30s 内で cap されない)。
func TestDo_5xx_ExponentialBackoffProgression(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, sleeps := newTestClient(t, srv)
	_, err := c.Do(newRequest(t, context.Background(), srv.URL))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	if len(*sleeps) != len(want) {
		t.Fatalf("sleep count = %d, want %d (sleeps=%v)", len(*sleeps), len(want), *sleeps)
	}
	for i, w := range want {
		if (*sleeps)[i] != w {
			t.Errorf("sleep[%d] = %v, want %v", i, (*sleeps)[i], w)
		}
	}
}

// TestDo_ExponentialBackoffCappedByMax は WithBackoff(2s, 5s) で base*2^attempt が
// 上限 5s で頭打ちになることを確認する。
func TestDo_ExponentialBackoffCappedByMax(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, sleeps := newTestClient(t, srv, WithBackoff(2*time.Second, 5*time.Second))
	_, err := c.Do(newRequest(t, context.Background(), srv.URL))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 2s, 4s, 8s→cap 5s
	want := []time.Duration{2 * time.Second, 4 * time.Second, 5 * time.Second}
	if len(*sleeps) != len(want) {
		t.Fatalf("sleep count = %d, want %d (sleeps=%v)", len(*sleeps), len(want), *sleeps)
	}
	for i, w := range want {
		if (*sleeps)[i] != w {
			t.Errorf("sleep[%d] = %v, want %v", i, (*sleeps)[i], w)
		}
	}
}

// TestDo_ContextCancelDuringSleep は backoff 中の sleep が context.Cancel で
// 即座に解除されることを確認する。本テストのみ defaultSleep (本物) を使う。
func TestDo_ContextCancelDuringSleep(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// no-op spy を使わず defaultSleep のまま、ただし baseBackoff を 10s にして
	// 確実に sleep 中 cancel が発生するようにする。
	c := NewClient(
		context.Background(), nil,
		withHTTPClient(srv.Client()),
		WithBaseURL(srv.URL),
		WithBackoff(10*time.Second, 30*time.Second),
	)

	ctx, cancel := context.WithCancel(context.Background())
	// 50ms 後に cancel
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := c.Do(newRequest(t, ctx, srv.URL))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("Do took too long (%v); cancel did not interrupt sleep", elapsed)
	}
}

// TestNewClient_DefaultBaseURL は WithBaseURL を渡さない場合のデフォルト値を確認する。
func TestNewClient_DefaultBaseURL(t *testing.T) {
	t.Parallel()

	c := NewClient(context.Background(), nil)
	if got := c.BaseURL(); got != "https://api.x.com" {
		t.Errorf("BaseURL() = %q, want https://api.x.com", got)
	}
}

// TestDo_RetryRewindsBody は POST 等で body を持つリクエストでも GetBody がある場合に
// retry 試行ごとに body を巻き戻すことを確認する (将来 POST が必要になった時の予防確認)。
func TestDo_RetryRewindsBody(t *testing.T) {
	t.Parallel()

	var receivedBodies []string
	var mu sync.Mutex
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedBodies = append(receivedBodies, string(b))
		mu.Unlock()
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		srv.URL,
		strings.NewReader(`{"hello":"world"}`),
	)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(receivedBodies) != 2 {
		t.Fatalf("server received %d requests, want 2 (bodies=%v)", len(receivedBodies), receivedBodies)
	}
	for i, b := range receivedBodies {
		if b != `{"hello":"world"}` {
			t.Errorf("body[%d] = %q, want %q", i, b, `{"hello":"world"}`)
		}
	}
}
