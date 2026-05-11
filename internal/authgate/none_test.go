package authgate_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/youyo/x/internal/authgate"
)

// TestNone_Wrap_PassesThroughRequest は None.Wrap がリクエストをそのまま inner handler に
// 委譲し、レスポンスが透過することを確認する。
func TestNone_Wrap_PassesThroughRequest(t *testing.T) {
	t.Parallel()

	innerCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerCalled = true
		w.Header().Set("X-Inner", "called")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "inner-body")
	})

	mw := &authgate.None{}
	wrapped := mw.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if !innerCalled {
		t.Error("inner handler was not called by None.Wrap")
	}
	if got := rec.Code; got != http.StatusOK {
		t.Errorf("status = %d, want 200", got)
	}
	if got := rec.Header().Get("X-Inner"); got != "called" {
		t.Errorf("X-Inner = %q, want %q", got, "called")
	}
	if got := rec.Body.String(); got != "inner-body" {
		t.Errorf("body = %q, want %q", got, "inner-body")
	}
}

// TestNone_Wrap_DoesNotAlterRequest は wrapped handler に渡される *http.Request が
// 元の req と同一であることを確認する (None は何もしない契約)。
func TestNone_Wrap_DoesNotAlterRequest(t *testing.T) {
	t.Parallel()

	var receivedURL string
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
	})

	mw := &authgate.None{}
	wrapped := mw.Wrap(inner)

	req := httptest.NewRequest(http.MethodGet, "/mcp?x=1", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if receivedURL != "/mcp?x=1" {
		t.Errorf("inner received URL = %q, want %q", receivedURL, "/mcp?x=1")
	}
}

// TestNone_Wrap_ImplementsMiddleware は *None が authgate.Middleware interface を
// 満たすことをコンパイル時に確認する。
func TestNone_Wrap_ImplementsMiddleware(t *testing.T) {
	t.Parallel()

	var _ authgate.Middleware = (*authgate.None)(nil)
}
