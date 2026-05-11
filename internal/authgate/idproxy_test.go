package authgate_test

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/youyo/idproxy/testutil"
	"github.com/youyo/x/internal/authgate"
)

// validCookieSecretHex は 32 バイト分の hex 文字列 (64 文字) を返す。
// テストで `WithCookieSecret` に渡す既定値として利用する。
func validCookieSecretHex(t *testing.T) string {
	t.Helper()
	return hex.EncodeToString(make([]byte, 32))
}

// validExternalURL は idproxy.Config.Validate の `isLocalhostURL` を通過する
// http://localhost:8080 を返す。
const validExternalURL = "http://localhost:8080"

// ---------- 失敗系: 設定不足 / 不正値 ----------

// TestIDProxy_NewIDProxy_MissingIssuer_ReturnsErrConfigInvalid は OIDC_ISSUER 未指定で
// ErrIDProxyConfigInvalid を返すことを確認する。
func TestIDProxy_NewIDProxy_MissingIssuer_ReturnsErrConfigInvalid(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer(""),
		authgate.WithOIDCClientID("client-id"),
		authgate.WithOIDCClientSecret("client-secret"),
		authgate.WithCookieSecret(validCookieSecretHex(t)),
		authgate.WithExternalURL(validExternalURL),
	)
	if err == nil {
		t.Fatal("expected ErrIDProxyConfigInvalid, got nil")
	}
	if !errors.Is(err, authgate.ErrIDProxyConfigInvalid) {
		t.Errorf("err = %v, want errors.Is ErrIDProxyConfigInvalid", err)
	}
	if mw != nil {
		t.Errorf("expected nil Middleware, got %T", mw)
	}
}

// TestIDProxy_NewIDProxy_MissingClientID_ReturnsErrConfigInvalid は OIDC_CLIENT_ID 未指定で
// ErrIDProxyConfigInvalid を返すことを確認する。
func TestIDProxy_NewIDProxy_MissingClientID_ReturnsErrConfigInvalid(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer("https://accounts.example.com"),
		authgate.WithOIDCClientID(""),
		authgate.WithOIDCClientSecret("client-secret"),
		authgate.WithCookieSecret(validCookieSecretHex(t)),
		authgate.WithExternalURL(validExternalURL),
	)
	if err == nil {
		t.Fatal("expected ErrIDProxyConfigInvalid, got nil")
	}
	if !errors.Is(err, authgate.ErrIDProxyConfigInvalid) {
		t.Errorf("err = %v, want errors.Is ErrIDProxyConfigInvalid", err)
	}
	if mw != nil {
		t.Errorf("expected nil Middleware, got %T", mw)
	}
}

// TestIDProxy_NewIDProxy_MissingClientSecret_ReturnsErrConfigInvalid は OIDC_CLIENT_SECRET 未指定で
// ErrIDProxyConfigInvalid を返すことを確認する。
func TestIDProxy_NewIDProxy_MissingClientSecret_ReturnsErrConfigInvalid(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer("https://accounts.example.com"),
		authgate.WithOIDCClientID("client-id"),
		authgate.WithOIDCClientSecret(""),
		authgate.WithCookieSecret(validCookieSecretHex(t)),
		authgate.WithExternalURL(validExternalURL),
	)
	if err == nil {
		t.Fatal("expected ErrIDProxyConfigInvalid, got nil")
	}
	if !errors.Is(err, authgate.ErrIDProxyConfigInvalid) {
		t.Errorf("err = %v, want errors.Is ErrIDProxyConfigInvalid", err)
	}
	if mw != nil {
		t.Errorf("expected nil Middleware, got %T", mw)
	}
}

// TestIDProxy_NewIDProxy_MissingCookieSecret_ReturnsErrConfigInvalid は COOKIE_SECRET 未指定で
// ErrIDProxyConfigInvalid を返すことを確認する。
func TestIDProxy_NewIDProxy_MissingCookieSecret_ReturnsErrConfigInvalid(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer("https://accounts.example.com"),
		authgate.WithOIDCClientID("client-id"),
		authgate.WithOIDCClientSecret("client-secret"),
		authgate.WithCookieSecret(""),
		authgate.WithExternalURL(validExternalURL),
	)
	if err == nil {
		t.Fatal("expected ErrIDProxyConfigInvalid, got nil")
	}
	if !errors.Is(err, authgate.ErrIDProxyConfigInvalid) {
		t.Errorf("err = %v, want errors.Is ErrIDProxyConfigInvalid", err)
	}
	if mw != nil {
		t.Errorf("expected nil Middleware, got %T", mw)
	}
}

// TestIDProxy_NewIDProxy_MissingExternalURL_ReturnsErrConfigInvalid は EXTERNAL_URL 未指定で
// ErrIDProxyConfigInvalid を返すことを確認する。
func TestIDProxy_NewIDProxy_MissingExternalURL_ReturnsErrConfigInvalid(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer("https://accounts.example.com"),
		authgate.WithOIDCClientID("client-id"),
		authgate.WithOIDCClientSecret("client-secret"),
		authgate.WithCookieSecret(validCookieSecretHex(t)),
		authgate.WithExternalURL(""),
	)
	if err == nil {
		t.Fatal("expected ErrIDProxyConfigInvalid, got nil")
	}
	if !errors.Is(err, authgate.ErrIDProxyConfigInvalid) {
		t.Errorf("err = %v, want errors.Is ErrIDProxyConfigInvalid", err)
	}
	if mw != nil {
		t.Errorf("expected nil Middleware, got %T", mw)
	}
}

// TestIDProxy_NewIDProxy_InvalidHexCookieSecret_ReturnsErrConfigInvalid は COOKIE_SECRET が
// hex デコードに失敗するケースで ErrIDProxyConfigInvalid を返すことを確認する。
func TestIDProxy_NewIDProxy_InvalidHexCookieSecret_ReturnsErrConfigInvalid(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer("https://accounts.example.com"),
		authgate.WithOIDCClientID("client-id"),
		authgate.WithOIDCClientSecret("client-secret"),
		authgate.WithCookieSecret("ZZZZZZZZ"), // 非 hex
		authgate.WithExternalURL(validExternalURL),
	)
	if err == nil {
		t.Fatal("expected ErrIDProxyConfigInvalid, got nil")
	}
	if !errors.Is(err, authgate.ErrIDProxyConfigInvalid) {
		t.Errorf("err = %v, want errors.Is ErrIDProxyConfigInvalid", err)
	}
	if mw != nil {
		t.Errorf("expected nil Middleware, got %T", mw)
	}
}

// TestIDProxy_NewIDProxy_ShortCookieSecret_ReturnsErrConfigInvalid は COOKIE_SECRET が
// 32 バイト未満の場合に ErrIDProxyConfigInvalid を返すことを確認する。
func TestIDProxy_NewIDProxy_ShortCookieSecret_ReturnsErrConfigInvalid(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer("https://accounts.example.com"),
		authgate.WithOIDCClientID("client-id"),
		authgate.WithOIDCClientSecret("client-secret"),
		authgate.WithCookieSecret("00ff"), // 2 バイトのみ
		authgate.WithExternalURL(validExternalURL),
	)
	if err == nil {
		t.Fatal("expected ErrIDProxyConfigInvalid, got nil")
	}
	if !errors.Is(err, authgate.ErrIDProxyConfigInvalid) {
		t.Errorf("err = %v, want errors.Is ErrIDProxyConfigInvalid", err)
	}
	if mw != nil {
		t.Errorf("expected nil Middleware, got %T", mw)
	}
}

// TestIDProxy_NewIDProxy_CSVLengthMismatch_ReturnsErrProvidersMismatch は OIDC_ISSUER と
// OIDC_CLIENT_ID の CSV 件数が異なる場合に ErrIDProxyProvidersMismatch を返すことを確認する。
// ErrIDProxyProvidersMismatch は ErrIDProxyConfigInvalid でも errors.Is が真になる。
func TestIDProxy_NewIDProxy_CSVLengthMismatch_ReturnsErrProvidersMismatch(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer("https://a.example.com,https://b.example.com"),
		authgate.WithOIDCClientID("client-id"), // 1 件のみ
		authgate.WithOIDCClientSecret("client-secret-a,client-secret-b"),
		authgate.WithCookieSecret(validCookieSecretHex(t)),
		authgate.WithExternalURL(validExternalURL),
	)
	if err == nil {
		t.Fatal("expected ErrIDProxyProvidersMismatch, got nil")
	}
	if !errors.Is(err, authgate.ErrIDProxyProvidersMismatch) {
		t.Errorf("err = %v, want errors.Is ErrIDProxyProvidersMismatch", err)
	}
	if !errors.Is(err, authgate.ErrIDProxyConfigInvalid) {
		t.Errorf("err = %v, want also errors.Is ErrIDProxyConfigInvalid (wrap)", err)
	}
	if mw != nil {
		t.Errorf("expected nil Middleware, got %T", mw)
	}
}

// TestIDProxy_NewIDProxy_EmptyEntryInCSV_ReturnsErrConfigInvalid は CSV のエントリに
// 空文字が含まれる (trim 後) 場合に ErrIDProxyConfigInvalid を返すことを確認する。
func TestIDProxy_NewIDProxy_EmptyEntryInCSV_ReturnsErrConfigInvalid(t *testing.T) {
	t.Parallel()

	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer("https://a.example.com, ,https://c.example.com"), // 真ん中が空
		authgate.WithOIDCClientID("a,b,c"),
		authgate.WithOIDCClientSecret("sa,sb,sc"),
		authgate.WithCookieSecret(validCookieSecretHex(t)),
		authgate.WithExternalURL(validExternalURL),
	)
	if err == nil {
		t.Fatal("expected ErrIDProxyConfigInvalid, got nil")
	}
	if !errors.Is(err, authgate.ErrIDProxyConfigInvalid) {
		t.Errorf("err = %v, want errors.Is ErrIDProxyConfigInvalid", err)
	}
	if mw != nil {
		t.Errorf("expected nil Middleware, got %T", mw)
	}
}

// ---------- 成功系: MockIdP を使った正常初期化 ----------

// TestIDProxy_NewIDProxy_ValidLocalhostHTTPS_Success は MockIdP の Issuer + 全必須 Option で
// IDProxy が成功生成されることを確認する。
func TestIDProxy_NewIDProxy_ValidLocalhostHTTPS_Success(t *testing.T) {
	t.Parallel()

	mock := testutil.NewMockIdP(t)

	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer(mock.Issuer()),
		authgate.WithOIDCClientID("client-id"),
		authgate.WithOIDCClientSecret("client-secret"),
		authgate.WithCookieSecret(validCookieSecretHex(t)),
		authgate.WithExternalURL(validExternalURL),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mw == nil {
		t.Fatal("expected non-nil Middleware")
	}
	if _, ok := mw.(*authgate.IDProxy); !ok {
		t.Errorf("got %T, want *authgate.IDProxy", mw)
	}
}

// TestIDProxy_NewIDProxy_ValidWithMemoryStoreDefault_Success は WithIDProxyStore 未指定でも
// (内部で memory store が使われる) 成功生成されることを確認する。
func TestIDProxy_NewIDProxy_ValidWithMemoryStoreDefault_Success(t *testing.T) {
	t.Parallel()

	mock := testutil.NewMockIdP(t)

	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer(mock.Issuer()),
		authgate.WithOIDCClientID("client-id"),
		authgate.WithOIDCClientSecret("client-secret"),
		authgate.WithCookieSecret(validCookieSecretHex(t)),
		authgate.WithExternalURL(validExternalURL),
		// WithIDProxyStore は指定しない → default の memory store が使われる
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mw == nil {
		t.Fatal("expected non-nil Middleware")
	}
}

// TestIDProxy_NewIDProxy_ValidWithExplicitStore_Success は WithIDProxyStore で
// 明示的に Store を渡しても成功生成されることを確認する。
func TestIDProxy_NewIDProxy_ValidWithExplicitStore_Success(t *testing.T) {
	t.Parallel()

	mock := testutil.NewMockIdP(t)

	store := authgate.NewMemoryStore()
	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer(mock.Issuer()),
		authgate.WithOIDCClientID("client-id"),
		authgate.WithOIDCClientSecret("client-secret"),
		authgate.WithCookieSecret(validCookieSecretHex(t)),
		authgate.WithExternalURL(validExternalURL),
		authgate.WithIDProxyStore(store),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mw == nil {
		t.Fatal("expected non-nil Middleware")
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
}

// TestIDProxy_NewIDProxy_MultipleProviders_Success は CSV で複数 provider を指定した
// ケースで成功することを確認する。
func TestIDProxy_NewIDProxy_MultipleProviders_Success(t *testing.T) {
	t.Parallel()

	mockA := testutil.NewMockIdP(t)
	mockB := testutil.NewMockIdP(t)

	issuers := strings.Join([]string{mockA.Issuer(), mockB.Issuer()}, ",")
	mw, err := authgate.New(authgate.ModeIDProxy,
		authgate.WithOIDCIssuer(issuers),
		authgate.WithOIDCClientID("client-a,client-b"),
		authgate.WithOIDCClientSecret("secret-a,secret-b"),
		authgate.WithCookieSecret(validCookieSecretHex(t)),
		authgate.WithExternalURL(validExternalURL),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mw == nil {
		t.Fatal("expected non-nil Middleware")
	}
}

// TestIDProxy_ImplementsMiddleware は *IDProxy が Middleware interface を満たすことを
// コンパイル時に確認する。
func TestIDProxy_ImplementsMiddleware(t *testing.T) {
	t.Parallel()

	var _ authgate.Middleware = (*authgate.IDProxy)(nil)
}
