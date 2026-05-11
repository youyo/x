package authgate_test

import (
	"errors"
	"testing"

	"github.com/youyo/x/internal/authgate"
)

// TestNew_None_ReturnsNoneMiddleware は ModeNone が *None 型の Middleware を返すことを確認する。
func TestNew_None_ReturnsNoneMiddleware(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeNone)
	if err != nil {
		t.Fatalf("New(ModeNone) returned error: %v", err)
	}
	if mw == nil {
		t.Fatal("New(ModeNone) returned nil Middleware")
	}
	if _, ok := mw.(*authgate.None); !ok {
		t.Errorf("New(ModeNone) returned %T, want *authgate.None", mw)
	}
}

// TestNew_APIKey_NoOption_ReturnsErrAPIKeyMissing は Option 無しで ModeAPIKey を指定すると
// ErrAPIKeyMissing が返ることを確認する。env 直読をしない契約を pin する。
func TestNew_APIKey_NoOption_ReturnsErrAPIKeyMissing(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeAPIKey)
	if err == nil {
		t.Fatal("New(ModeAPIKey) returned nil error, want ErrAPIKeyMissing")
	}
	if !errors.Is(err, authgate.ErrAPIKeyMissing) {
		t.Errorf("New(ModeAPIKey) error = %v, want errors.Is ErrAPIKeyMissing", err)
	}
	if mw != nil {
		t.Errorf("New(ModeAPIKey) returned non-nil Middleware: %T", mw)
	}
}

// TestNew_APIKey_WithEmptyAPIKey_ReturnsErrAPIKeyMissing は WithAPIKey("") でも
// ErrAPIKeyMissing が返ることを確認する。
func TestNew_APIKey_WithEmptyAPIKey_ReturnsErrAPIKeyMissing(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeAPIKey, authgate.WithAPIKey(""))
	if err == nil {
		t.Fatal("New(ModeAPIKey, WithAPIKey(\"\")) returned nil error, want ErrAPIKeyMissing")
	}
	if !errors.Is(err, authgate.ErrAPIKeyMissing) {
		t.Errorf("error = %v, want errors.Is ErrAPIKeyMissing", err)
	}
	if mw != nil {
		t.Errorf("returned non-nil Middleware: %T", mw)
	}
}

// TestNew_APIKey_WithAPIKey_Success は WithAPIKey("key") で *APIKey が返ることを確認する。
func TestNew_APIKey_WithAPIKey_Success(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeAPIKey, authgate.WithAPIKey("secret-key"))
	if err != nil {
		t.Fatalf("New(ModeAPIKey, WithAPIKey(\"secret-key\")) returned error: %v", err)
	}
	if mw == nil {
		t.Fatal("returned nil Middleware")
	}
	if _, ok := mw.(*authgate.APIKey); !ok {
		t.Errorf("returned %T, want *authgate.APIKey", mw)
	}
}

// TestNew_IDProxy_ReturnsErrUnsupportedMode は M16 で idproxy が未実装であることを契約として pin する。
func TestNew_IDProxy_ReturnsErrUnsupportedMode(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeIDProxy)
	if err == nil {
		t.Fatal("New(ModeIDProxy) returned nil error, want ErrUnsupportedMode")
	}
	if !errors.Is(err, authgate.ErrUnsupportedMode) {
		t.Errorf("New(ModeIDProxy) error = %v, want errors.Is ErrUnsupportedMode", err)
	}
	if mw != nil {
		t.Errorf("New(ModeIDProxy) returned non-nil Middleware: %T", mw)
	}
}

// TestNew_EmptyMode_ReturnsErrUnsupportedMode は authgate.New が defaulting しないことを pin する。
// 空文字の defaulting は呼び出し側 CLI 層 (M24) の責務である。
func TestNew_EmptyMode_ReturnsErrUnsupportedMode(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.Mode(""))
	if err == nil {
		t.Fatal("New(\"\") returned nil error, want ErrUnsupportedMode")
	}
	if !errors.Is(err, authgate.ErrUnsupportedMode) {
		t.Errorf("New(\"\") error = %v, want errors.Is ErrUnsupportedMode", err)
	}
	if mw != nil {
		t.Errorf("New(\"\") returned non-nil Middleware: %T", mw)
	}
}

// TestNew_UnknownMode_ReturnsErrUnsupportedMode は未知のモードでエラーになることを確認する。
func TestNew_UnknownMode_ReturnsErrUnsupportedMode(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.Mode("oauth2-pkce"))
	if err == nil {
		t.Fatal("New(\"oauth2-pkce\") returned nil error, want ErrUnsupportedMode")
	}
	if !errors.Is(err, authgate.ErrUnsupportedMode) {
		t.Errorf("New unknown error = %v, want errors.Is ErrUnsupportedMode", err)
	}
	if mw != nil {
		t.Errorf("New unknown returned non-nil Middleware: %T", mw)
	}
}

// TestMode_StringValues はモード定数値が spec §11 (X_MCP_AUTH) と一致することを確認する。
func TestMode_StringValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  authgate.Mode
		want string
	}{
		{"ModeNone", authgate.ModeNone, "none"},
		{"ModeAPIKey", authgate.ModeAPIKey, "apikey"},
		{"ModeIDProxy", authgate.ModeIDProxy, "idproxy"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if string(tc.got) != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, string(tc.got), tc.want)
			}
		})
	}
}
