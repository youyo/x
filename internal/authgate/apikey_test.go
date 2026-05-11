package authgate_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/youyo/x/internal/authgate"
)

// TestNewAPIKey_EmptyKey_ReturnsErrAPIKeyMissing は空文字キーで ErrAPIKeyMissing が返ることを確認する。
func TestNewAPIKey_EmptyKey_ReturnsErrAPIKeyMissing(t *testing.T) {
	t.Parallel()

	mw, err := authgate.NewAPIKey("")
	if err == nil {
		t.Fatal("NewAPIKey(\"\") returned nil error, want ErrAPIKeyMissing")
	}
	if !errors.Is(err, authgate.ErrAPIKeyMissing) {
		t.Errorf("NewAPIKey(\"\") error = %v, want errors.Is ErrAPIKeyMissing", err)
	}
	if mw != nil {
		t.Errorf("NewAPIKey(\"\") returned non-nil *APIKey: %v", mw)
	}
}

// TestNewAPIKey_NonEmptyKey_Success は有効なキーで *APIKey が返ることを確認する。
func TestNewAPIKey_NonEmptyKey_Success(t *testing.T) {
	t.Parallel()

	mw, err := authgate.NewAPIKey("secret-key")
	if err != nil {
		t.Fatalf("NewAPIKey(\"secret-key\") returned error: %v", err)
	}
	if mw == nil {
		t.Fatal("NewAPIKey(\"secret-key\") returned nil *APIKey")
	}
}

// newInner はテスト用に呼び出し有無を記録する inner handler を返す。
func newInner(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
}

// TestAPIKey_Wrap_ValidBearer_PassesThrough は正しい Bearer token で next が呼ばれることを確認する。
func TestAPIKey_Wrap_ValidBearer_PassesThrough(t *testing.T) {
	t.Parallel()

	mw, err := authgate.NewAPIKey("secret")
	if err != nil {
		t.Fatalf("NewAPIKey: %v", err)
	}

	var called bool
	wrapped := mw.Wrap(newInner(&called))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if !called {
		t.Error("inner handler was not called for valid Bearer")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestAPIKey_Wrap_InvalidBearer_Returns401 は不正な Bearer で 401 が返ることを確認する。
func TestAPIKey_Wrap_InvalidBearer_Returns401(t *testing.T) {
	t.Parallel()

	mw, err := authgate.NewAPIKey("secret")
	if err != nil {
		t.Fatalf("NewAPIKey: %v", err)
	}

	var called bool
	wrapped := mw.Wrap(newInner(&called))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if called {
		t.Error("inner handler was called for invalid Bearer")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="x-mcp"` {
		t.Errorf("WWW-Authenticate = %q, want %q", got, `Bearer realm="x-mcp"`)
	}
}

// TestAPIKey_Wrap_MissingAuthHeader_Returns401 は Authorization ヘッダ無しで 401 が返ることを確認する。
func TestAPIKey_Wrap_MissingAuthHeader_Returns401(t *testing.T) {
	t.Parallel()

	mw, err := authgate.NewAPIKey("secret")
	if err != nil {
		t.Fatalf("NewAPIKey: %v", err)
	}

	var called bool
	wrapped := mw.Wrap(newInner(&called))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if called {
		t.Error("inner handler was called when Authorization header was missing")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="x-mcp"` {
		t.Errorf("WWW-Authenticate = %q, want %q", got, `Bearer realm="x-mcp"`)
	}
}

// TestAPIKey_Wrap_MissingBearerPrefix_Returns401 はプレフィックス無しで 401 が返ることを確認する。
func TestAPIKey_Wrap_MissingBearerPrefix_Returns401(t *testing.T) {
	t.Parallel()

	mw, err := authgate.NewAPIKey("secret")
	if err != nil {
		t.Fatalf("NewAPIKey: %v", err)
	}

	var called bool
	wrapped := mw.Wrap(newInner(&called))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "secret")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if called {
		t.Error("inner handler was called when Bearer prefix was missing")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestAPIKey_Wrap_LowercaseBearer_PassesThrough は小文字 bearer プレフィックスでも通過することを確認する (RFC 6750 §2.1)。
func TestAPIKey_Wrap_LowercaseBearer_PassesThrough(t *testing.T) {
	t.Parallel()

	mw, err := authgate.NewAPIKey("secret")
	if err != nil {
		t.Fatalf("NewAPIKey: %v", err)
	}

	var called bool
	wrapped := mw.Wrap(newInner(&called))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "bearer secret")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if !called {
		t.Error("inner handler was not called for lowercase 'bearer' prefix")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestAPIKey_Wrap_EmptyToken_Returns401 は Bearer の後ろが空文字で 401 が返ることを確認する。
func TestAPIKey_Wrap_EmptyToken_Returns401(t *testing.T) {
	t.Parallel()

	mw, err := authgate.NewAPIKey("secret")
	if err != nil {
		t.Fatalf("NewAPIKey: %v", err)
	}

	var called bool
	wrapped := mw.Wrap(newInner(&called))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if called {
		t.Error("inner handler was called for empty token")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestAPIKey_Wrap_DifferentLength_Returns401 は長さの異なる token で 401 が返ることを確認する。
// subtle.ConstantTimeCompare は長さが違うと内容を比較せず 0 を返す挙動を確認。
func TestAPIKey_Wrap_DifferentLength_Returns401(t *testing.T) {
	t.Parallel()

	mw, err := authgate.NewAPIKey("abcdef")
	if err != nil {
		t.Fatalf("NewAPIKey: %v", err)
	}

	var called bool
	wrapped := mw.Wrap(newInner(&called))

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer abc")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if called {
		t.Error("inner handler was called for differently-sized token")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestAPIKey_ImplementsMiddleware は *APIKey が Middleware を満たすことをコンパイル時に確認する。
func TestAPIKey_ImplementsMiddleware(t *testing.T) {
	t.Parallel()

	var _ authgate.Middleware = (*authgate.APIKey)(nil)
}
