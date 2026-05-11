package xapi_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/youyo/x/internal/xapi"
)

// TestExitCodeFor は ExitCodeFor が internal/app の定数値と一致する exit code を
// 番兵エラーから写像することを確認する。
func TestExitCodeFor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil は 0", nil, 0},
		{"認証 401 は 3", xapi.ErrAuthentication, 3},
		{"権限 403 は 4", xapi.ErrPermission, 4},
		{"未発見 404 は 5", xapi.ErrNotFound, 5},
		{"レートリミット枯渇は 1 (generic)", xapi.ErrRateLimit, 1},
		{"分類不能エラーは 1", errors.New("unclassified"), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := xapi.ExitCodeFor(tc.err)
			if got != tc.want {
				t.Errorf("ExitCodeFor(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestExitCodeFor_WrappedError は fmt.Errorf("%w") で wrap された番兵も
// errors.Is 経由で正しく検出されることを確認する。
func TestExitCodeFor_WrappedError(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("outer: %w", xapi.ErrPermission)
	if got := xapi.ExitCodeFor(wrapped); got != 4 {
		t.Errorf("wrapped permission exit = %d, want 4", got)
	}
}

// TestAPIError_Error は Error() 文字列がステータスコードと body の冒頭を含むことを確認する。
func TestAPIError_Error(t *testing.T) {
	t.Parallel()

	apiErr := newAPIErrorForTest(401, []byte(`{"error":"bad token"}`), nil)
	got := apiErr.Error()
	if !strings.Contains(got, "HTTP 401") {
		t.Errorf("Error() %q does not contain HTTP 401", got)
	}
	if !strings.Contains(got, "bad token") {
		t.Errorf("Error() %q does not contain body fragment", got)
	}
}

// TestAPIError_Error_Truncate は 200 バイトを超える body が truncate されることを確認する。
func TestAPIError_Error_Truncate(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("A", 500)
	apiErr := newAPIErrorForTest(500, []byte(long), nil)
	got := apiErr.Error()
	if !strings.Contains(got, "...(truncated)") {
		t.Errorf("Error() did not truncate long body: %q", got)
	}
}

// TestAPIError_NilSafety は (*APIError)(nil) でも panic しないことを確認する。
func TestAPIError_NilSafety(t *testing.T) {
	t.Parallel()

	var apiErr *xapi.APIError
	if got := apiErr.Error(); got != "<nil APIError>" {
		t.Errorf("nil Error() = %q, want %q", got, "<nil APIError>")
	}
	if u := apiErr.Unwrap(); u != nil {
		t.Errorf("nil Unwrap() = %v, want nil", u)
	}
}

// TestAPIError_ErrorsAs は errors.As で APIError を取り出して内容にアクセスできることを確認する。
func TestAPIError_ErrorsAs(t *testing.T) {
	t.Parallel()

	src := newAPIErrorForTest(404, []byte("not found body"), http.Header{"X-Trace-Id": []string{"abc"}})
	wrapped := fmt.Errorf("call failed: %w", src)

	var got *xapi.APIError
	if !errors.As(wrapped, &got) {
		t.Fatalf("errors.As failed to extract *APIError from %T", wrapped)
	}
	if got.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", got.StatusCode)
	}
	if string(got.Body) != "not found body" {
		t.Errorf("Body = %q, want %q", got.Body, "not found body")
	}
	if got.Header.Get("X-Trace-Id") != "abc" {
		t.Errorf("Header[X-Trace-Id] = %q, want abc", got.Header.Get("X-Trace-Id"))
	}
	// 番兵照合は Client.Do 経由で生成された APIError でのみ動作する (sentinel フィールドが未公開のため)。
	// 本テストではフィールドアクセスのみを検証し、番兵照合は client_test.go 側で確認する。
}

// newAPIErrorForTest は外部テストから APIError を組み立てるヘルパ。
// xapi パッケージ外なので unexported フィールドは設定できないが、本テストでは
// Client.Do 経由ではなく振る舞いだけ検証したいので *APIError を直接組む経路は使わず、
// Client.Do の挙動テスト (client_test.go) で生成された APIError を流用する方が安全。
// ここでは APIError の表層 (StatusCode/Body/Header) だけ確認すれば十分なので、
// 公開フィールドのみ設定した値を返す。
func newAPIErrorForTest(status int, body []byte, h http.Header) *xapi.APIError {
	return &xapi.APIError{
		StatusCode: status,
		Body:       body,
		Header:     h,
	}
}
