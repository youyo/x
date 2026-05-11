package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youyo/x/internal/authgate"
	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// clearMCPEnv は MCP モード関連の環境変数 (X_MCP_*) を空文字に固定する。
// 親プロセス側に既に値が設定されている場合の汚染を防ぐ。
func clearMCPEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"X_MCP_HOST",
		"X_MCP_PORT",
		"X_MCP_PATH",
		"X_MCP_AUTH",
		"X_MCP_API_KEY",
		"STORE_BACKEND",
		"SQLITE_PATH",
		"REDIS_URL",
		"DYNAMODB_TABLE_NAME",
		"AWS_REGION",
		"OIDC_ISSUER",
		"OIDC_CLIENT_ID",
		"OIDC_CLIENT_SECRET",
		"COOKIE_SECRET",
		"EXTERNAL_URL",
	} {
		t.Setenv(k, "")
	}
}

// TestNewMcpCmd_Defaults は env 未設定で newMcpCmd() を呼ぶと spec §6 の
// デフォルト値が反映されることを確認する。
func TestNewMcpCmd_Defaults(t *testing.T) {
	// NOTE: t.Parallel() を使うと t.Setenv との相性が悪いので逐次実行
	clearMCPEnv(t)

	cmd := newMcpCmd()

	cases := []struct {
		name string
		flag string
		want string
	}{
		{"host", "host", "127.0.0.1"},
		{"port", "port", "8080"},
		{"path", "path", "/mcp"},
		{"auth", "auth", "idproxy"},
		{"apikey-env", "apikey-env", "X_MCP_API_KEY"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(c.flag)
			if f == nil {
				t.Fatalf("flag %q not defined", c.flag)
			}
			if f.DefValue != c.want {
				t.Errorf("flag %q DefValue = %q, want %q", c.flag, f.DefValue, c.want)
			}
		})
	}
}

// TestNewMcpCmd_EnvDefaulting は X_MCP_* env が flag default に反映されることを確認する。
//
// 注意: t.Setenv は **必ず newMcpCmd() 呼び出しの前** に呼ぶ必要がある。
// flag default 値は cmd factory 呼び出し時点で env を読んで確定するためである
// (plans/x-m24-cli-mcp-e2e.md D-8 / advisor non-blocker #1)。
func TestNewMcpCmd_EnvDefaulting(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("X_MCP_HOST", "0.0.0.0")
	t.Setenv("X_MCP_PORT", "9090")
	t.Setenv("X_MCP_PATH", "/foo")
	t.Setenv("X_MCP_AUTH", "apikey")

	cmd := newMcpCmd()

	cases := []struct {
		flag string
		want string
	}{
		{"host", "0.0.0.0"},
		{"port", "9090"},
		{"path", "/foo"},
		{"auth", "apikey"},
	}
	for _, c := range cases {
		f := cmd.Flags().Lookup(c.flag)
		if f == nil {
			t.Fatalf("flag %q not defined", c.flag)
		}
		if f.DefValue != c.want {
			t.Errorf("flag %q DefValue = %q, want %q", c.flag, f.DefValue, c.want)
		}
	}
}

// TestNewMcpCmd_ApikeyEnvIsLiteralDefault は --apikey-env のデフォルトが env 連動しないことを確認する。
//
// `X_MCP_API_KEY` は共有シークレットの **値** を保持する env であり、`--apikey-env` フラグの
// デフォルト値を env で上書きする用途ではない。env 連動させると `X_MCP_API_KEY=hunter2` を
// 設定したときに flag 値が `"hunter2"` になり、`os.Getenv("hunter2")` で空文字を取得して
// 常に 401 になるバグになる (plans D-1 / advisor 指摘 #1)。
func TestNewMcpCmd_ApikeyEnvIsLiteralDefault(t *testing.T) {
	clearMCPEnv(t)
	// あえて X_MCP_API_KEY を非空に設定する
	t.Setenv("X_MCP_API_KEY", "hunter2")

	cmd := newMcpCmd()

	f := cmd.Flags().Lookup("apikey-env")
	if f == nil {
		t.Fatal("flag apikey-env not defined")
	}
	if f.DefValue != "X_MCP_API_KEY" {
		t.Errorf("flag apikey-env DefValue = %q, want %q (literal, no env binding)",
			f.DefValue, "X_MCP_API_KEY")
	}
}

// TestNewMcpCmd_PortEnvInvalid は X_MCP_PORT に整数以外を入れても panic せず default に
// フォールバックすることを確認する。
func TestNewMcpCmd_PortEnvInvalid(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("X_MCP_PORT", "not-a-number")

	cmd := newMcpCmd()
	f := cmd.Flags().Lookup("port")
	if f.DefValue != "8080" {
		t.Errorf("flag port DefValue = %q, want %q (fallback)", f.DefValue, "8080")
	}
}

// TestNormalizeAuthMode_Valid は受理される 3 モードを確認する。
func TestNormalizeAuthMode_Valid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want authgate.Mode
	}{
		{"none", authgate.ModeNone},
		{"apikey", authgate.ModeAPIKey},
		{"idproxy", authgate.ModeIDProxy},
		{"NONE", authgate.ModeNone},       // 大文字許容
		{" apikey ", authgate.ModeAPIKey}, // trim
	}
	for _, c := range cases {
		got, err := normalizeAuthMode(c.in)
		if err != nil {
			t.Errorf("normalizeAuthMode(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("normalizeAuthMode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizeAuthMode_Invalid は不正値で ErrInvalidArgument を返すことを確認する。
func TestNormalizeAuthMode_Invalid(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "foo", "Basic", "bearer"} {
		_, err := normalizeAuthMode(in)
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("normalizeAuthMode(%q) error = %v, want errors.Is ErrInvalidArgument", in, err)
		}
	}
}

// TestRunMcp_AuthModeInvalid は --auth に不正値で ErrInvalidArgument が返ることを確認する。
func TestRunMcp_AuthModeInvalid(t *testing.T) {
	clearMCPEnv(t)
	clearXAPIEnv(t)
	isolateXDG(t)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"mcp", "--auth", "invalid"})

	err := cmd.ExecuteContext(context.Background())
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Execute err = %v, want errors.Is ErrInvalidArgument", err)
	}
}

// TestRunMcp_CredentialsMissing は X API env 欠落で ErrCredentialsMissing (= xapi.ErrAuthentication)
// が返ることを確認する。--auth none で credentials 検証を最後まで通すよう簡略化。
func TestRunMcp_CredentialsMissing(t *testing.T) {
	clearMCPEnv(t)
	clearXAPIEnv(t)
	isolateXDG(t)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"mcp", "--auth", "none"})

	err := cmd.ExecuteContext(context.Background())
	if !errors.Is(err, ErrCredentialsMissing) {
		t.Fatalf("Execute err = %v, want errors.Is ErrCredentialsMissing", err)
	}
	if !errors.Is(err, xapi.ErrAuthentication) {
		t.Errorf("Execute err = %v, expected to unwrap xapi.ErrAuthentication", err)
	}
}

// ---------- loadMCPCredentials ----------

// TestLoadMCPCredentials_MissingEnv は env 欠落で ErrCredentialsMissing を返すことを確認する。
func TestLoadMCPCredentials_MissingEnv(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
	}{
		{"all empty", map[string]string{}},
		{"only api_key", map[string]string{"X_API_KEY": "k"}},
		{"missing access_token_secret", map[string]string{
			"X_API_KEY":      "k",
			"X_API_SECRET":   "s",
			"X_ACCESS_TOKEN": "t",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearXAPIEnv(t)
			for k, v := range c.set {
				t.Setenv(k, v)
			}
			_, err := loadMCPCredentials()
			if !errors.Is(err, ErrCredentialsMissing) {
				t.Fatalf("err = %v, want errors.Is ErrCredentialsMissing", err)
			}
			if !errors.Is(err, xapi.ErrAuthentication) {
				t.Errorf("err = %v, want errors.Is xapi.ErrAuthentication", err)
			}
		})
	}
}

// TestLoadMCPCredentials_AllEnvSet は 4 env を揃えると non-nil の Credentials が返ることを確認する。
func TestLoadMCPCredentials_AllEnvSet(t *testing.T) {
	clearXAPIEnv(t)
	t.Setenv("X_API_KEY", "k")
	t.Setenv("X_API_SECRET", "s")
	t.Setenv("X_ACCESS_TOKEN", "t")
	t.Setenv("X_ACCESS_TOKEN_SECRET", "ts")

	got, err := loadMCPCredentials()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := &config.Credentials{
		APIKey:            "k",
		APISecret:         "s",
		AccessToken:       "t",
		AccessTokenSecret: "ts",
	}
	if *got != *want {
		t.Errorf("got = %+v, want %+v", got, want)
	}
}

// TestLoadMCPCredentials_IgnoresFile は credentials.toml が存在する状況でも
// MCP モードでは env のみが評価されることを確認する (spec §11 不変条件)。
func TestLoadMCPCredentials_IgnoresFile(t *testing.T) {
	isolateXDG(t)
	clearXAPIEnv(t)
	// XDG_DATA_HOME 配下に credentials.toml を配置 (loadCLI 側からは読める状態)
	dataHome := os.Getenv("XDG_DATA_HOME")
	_ = writeCredentialsFile(t, dataHome, &config.Credentials{
		APIKey:            "file-k",
		APISecret:         "file-s",
		AccessToken:       "file-t",
		AccessTokenSecret: "file-ts",
	})

	// env は欠落のまま → MCP モードは file を読まないので ErrCredentialsMissing
	_, err := loadMCPCredentials()
	if !errors.Is(err, ErrCredentialsMissing) {
		t.Fatalf("err = %v, want errors.Is ErrCredentialsMissing (file must NOT be read in MCP mode)", err)
	}
}

// ---------- buildIDProxyStore ----------

// TestBuildIDProxyStore_DefaultMemory は STORE_BACKEND 未設定で memory store が返ることを確認する。
func TestBuildIDProxyStore_DefaultMemory(t *testing.T) {
	clearMCPEnv(t)
	isolateXDG(t)

	got, err := buildIDProxyStore()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got == nil {
		t.Fatal("got nil store")
	}
}

// TestBuildIDProxyStore_ExplicitMemory は STORE_BACKEND=memory を明示しても動くことを確認する。
func TestBuildIDProxyStore_ExplicitMemory(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("STORE_BACKEND", "memory")

	got, err := buildIDProxyStore()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got == nil {
		t.Fatal("got nil store")
	}
}

// TestBuildIDProxyStore_SQLitePathDefault は SQLITE_PATH 未設定で
// XDG_DATA_HOME 既定パス配下に DB が生成されることを確認する。
func TestBuildIDProxyStore_SQLitePathDefault(t *testing.T) {
	clearMCPEnv(t)
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("STORE_BACKEND", "sqlite")

	got, err := buildIDProxyStore()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got == nil {
		t.Fatal("got nil store")
	}
	t.Cleanup(func() { _ = got.Close() })

	wantPath := filepath.Join(tmp, "x", "idproxy.db")
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Errorf("expected DB at %q, stat err: %v", wantPath, statErr)
	}
}

// TestBuildIDProxyStore_SQLiteExplicitPath は SQLITE_PATH 指定が優先されることを確認する。
func TestBuildIDProxyStore_SQLiteExplicitPath(t *testing.T) {
	clearMCPEnv(t)
	tmp := t.TempDir()
	customPath := filepath.Join(tmp, "custom.db")
	t.Setenv("STORE_BACKEND", "sqlite")
	t.Setenv("SQLITE_PATH", customPath)

	got, err := buildIDProxyStore()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got == nil {
		t.Fatal("got nil store")
	}
	t.Cleanup(func() { _ = got.Close() })

	if _, statErr := os.Stat(customPath); statErr != nil {
		t.Errorf("expected DB at %q, stat err: %v", customPath, statErr)
	}
}

// TestBuildIDProxyStore_RedisURLRequired は REDIS_URL 未指定で ErrRedisURLRequired を返すことを確認する。
func TestBuildIDProxyStore_RedisURLRequired(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("STORE_BACKEND", "redis")

	_, err := buildIDProxyStore()
	if !errors.Is(err, authgate.ErrRedisURLRequired) {
		t.Fatalf("err = %v, want errors.Is authgate.ErrRedisURLRequired", err)
	}
}

// TestBuildIDProxyStore_DynamoDBTableRequired は DYNAMODB_TABLE_NAME 未指定で
// ErrDynamoDBTableRequired を返すことを確認する。
func TestBuildIDProxyStore_DynamoDBTableRequired(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("STORE_BACKEND", "dynamodb")

	_, err := buildIDProxyStore()
	if !errors.Is(err, authgate.ErrDynamoDBTableRequired) {
		t.Fatalf("err = %v, want errors.Is authgate.ErrDynamoDBTableRequired", err)
	}
}

// TestBuildIDProxyStore_DynamoDBRegionRequired は AWS_REGION 未指定で ErrDynamoDBRegionRequired を返すことを確認する。
func TestBuildIDProxyStore_DynamoDBRegionRequired(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("STORE_BACKEND", "dynamodb")
	t.Setenv("DYNAMODB_TABLE_NAME", "x-test")

	_, err := buildIDProxyStore()
	if !errors.Is(err, authgate.ErrDynamoDBRegionRequired) {
		t.Fatalf("err = %v, want errors.Is authgate.ErrDynamoDBRegionRequired", err)
	}
}

// TestBuildIDProxyStore_UnknownBackend は未知の STORE_BACKEND で ErrInvalidArgument を返すことを確認する。
func TestBuildIDProxyStore_UnknownBackend(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("STORE_BACKEND", "foo")

	_, err := buildIDProxyStore()
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("err = %v, want errors.Is ErrInvalidArgument", err)
	}
	if !strings.Contains(err.Error(), "STORE_BACKEND") {
		t.Errorf("err message should mention STORE_BACKEND, got: %v", err)
	}
}

// TestBuildIDProxyStore_BackendCaseInsensitive は大文字小文字に依存しないことを確認する。
func TestBuildIDProxyStore_BackendCaseInsensitive(t *testing.T) {
	clearMCPEnv(t)
	t.Setenv("STORE_BACKEND", "MEMORY")

	got, err := buildIDProxyStore()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got == nil {
		t.Fatal("got nil store")
	}
}
