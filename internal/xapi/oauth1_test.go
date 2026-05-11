package xapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// TestNewOAuth1Config_MapsConsumerCredentials は Credentials.APIKey/APISecret が
// oauth1.Config.ConsumerKey/ConsumerSecret に正しくマッピングされることを確認する。
func TestNewOAuth1Config_MapsConsumerCredentials(t *testing.T) {
	creds := &config.Credentials{
		APIKey:    "test-consumer-key",
		APISecret: "test-consumer-secret",
	}
	cfg := xapi.NewOAuth1Config(creds)
	if cfg == nil {
		t.Fatalf("NewOAuth1Config returned nil")
	}
	if cfg.ConsumerKey != "test-consumer-key" {
		t.Errorf("ConsumerKey = %q, want %q", cfg.ConsumerKey, "test-consumer-key")
	}
	if cfg.ConsumerSecret != "test-consumer-secret" {
		t.Errorf("ConsumerSecret = %q, want %q", cfg.ConsumerSecret, "test-consumer-secret")
	}
}

// TestNewOAuth1Config_NilCredentials は nil 入力でも panic せず空文字 Config を返すことを確認する。
func TestNewOAuth1Config_NilCredentials(t *testing.T) {
	cfg := xapi.NewOAuth1Config(nil)
	if cfg == nil {
		t.Fatalf("NewOAuth1Config(nil) returned nil")
	}
	if cfg.ConsumerKey != "" || cfg.ConsumerSecret != "" {
		t.Errorf("expected empty consumer key/secret, got key=%q secret=%q", cfg.ConsumerKey, cfg.ConsumerSecret)
	}
}

// TestNewHTTPClient_AuthorizationHeader は httptest サーバーに対して
// NewHTTPClient で作った Client がリクエストを送ると、Authorization ヘッダに
// OAuth スキームと必須パラメータ群が含まれることを確認する。
func TestNewHTTPClient_AuthorizationHeader(t *testing.T) {
	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	creds := &config.Credentials{
		APIKey:            "ck",
		APISecret:         "cs",
		AccessToken:       "at",
		AccessTokenSecret: "ats",
	}
	client := xapi.NewHTTPClient(context.Background(), creds)
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	_ = resp.Body.Close()

	auth := captured.Get("Authorization")
	if !strings.HasPrefix(auth, "OAuth ") {
		t.Fatalf("Authorization header does not start with %q: %q", "OAuth ", auth)
	}
	// OAuth 1.0a (RFC 5849) の必須パラメータ群が全て含まれること。
	// oauth_version は OPTIONAL なので必須リストには含めない。
	requiredParams := []string{
		`oauth_consumer_key="ck"`,
		`oauth_token="at"`,
		`oauth_signature_method="HMAC-SHA1"`,
		"oauth_nonce=",
		"oauth_timestamp=",
		"oauth_signature=",
	}
	for _, p := range requiredParams {
		if !strings.Contains(auth, p) {
			t.Errorf("Authorization header missing %q: %s", p, auth)
		}
	}
	// oauth_version は OPTIONAL だが、もし emit されていれば値は "1.0" であること。
	if strings.Contains(auth, "oauth_version=") && !strings.Contains(auth, `oauth_version="1.0"`) {
		t.Errorf(`oauth_version present but not "1.0": %s`, auth)
	}
}

// TestNewHTTPClient_EmptyCredentials は空 Credentials でも Client が作成され、
// リクエストに Authorization ヘッダが付与されることを確認する (値は意味なし)。
func TestNewHTTPClient_EmptyCredentials(t *testing.T) {
	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := xapi.NewHTTPClient(context.Background(), &config.Credentials{})
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	_ = resp.Body.Close()

	auth := captured.Get("Authorization")
	if !strings.HasPrefix(auth, "OAuth ") {
		t.Fatalf("expected OAuth scheme even with empty credentials, got %q", auth)
	}
}

// TestNewHTTPClient_NilCredentials は nil 入力でも panic せず Client を返すことを確認する。
func TestNewHTTPClient_NilCredentials(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewHTTPClient(nil) panicked: %v", r)
		}
	}()
	client := xapi.NewHTTPClient(context.Background(), nil)
	if client == nil {
		t.Fatalf("NewHTTPClient(nil) returned nil")
	}
}

// TestNewHTTPClient_DifferentNoncePerRequest は同一 Client から複数リクエストを
// 送ると oauth_nonce / oauth_timestamp / oauth_signature がリクエストごとに
// 再計算されることを確認する (replay 防止)。
func TestNewHTTPClient_DifferentNoncePerRequest(t *testing.T) {
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	creds := &config.Credentials{APIKey: "ck", APISecret: "cs", AccessToken: "at", AccessTokenSecret: "ats"}
	client := xapi.NewHTTPClient(context.Background(), creds)
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("client.Do[%d]: %v", i, err)
		}
		_ = resp.Body.Close()
	}
	if len(auths) != 2 {
		t.Fatalf("expected 2 authorizations, got %d", len(auths))
	}
	if auths[0] == auths[1] {
		t.Errorf("Authorization headers should differ between requests due to nonce/timestamp: %q", auths[0])
	}
}

// TestNewHTTPClient_RealRequestPath は path 付き URL でも Authorization ヘッダが
// 正しく付与されること (署名 base string にパスを含めても破綻しないこと) を確認する。
func TestNewHTTPClient_RealRequestPath(t *testing.T) {
	var capturedPath string
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	creds := &config.Credentials{APIKey: "ck", APISecret: "cs", AccessToken: "at", AccessTokenSecret: "ats"}
	client := xapi.NewHTTPClient(context.Background(), creds)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/2/users/me", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	_ = resp.Body.Close()
	if capturedPath != "/2/users/me" {
		t.Errorf("path = %q, want /2/users/me", capturedPath)
	}
	if !strings.HasPrefix(capturedAuth, "OAuth ") {
		t.Errorf("Authorization scheme broken: %q", capturedAuth)
	}
}
