package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// isolateXDG は XDG_CONFIG_HOME / XDG_DATA_HOME をテスト用一時ディレクトリに固定し、
// 開発機 / CI ホストの実 config.toml / credentials.toml がテストへ漏れ込むのを防ぐ。
//
// M12 で `internal/cli/liked.go` がデフォルト値を `config.toml` から読むようになるため、
// 既存テストが偶発的に `~/.config/x/config.toml` を拾うと query 期待値がずれて
// silently fail する可能性がある (plans/x-m12-cli-configure.md リスク #2)。
// すべての CLI テストの先頭で呼び出すことで、テスト間の独立性とローカル/CI 整合性を保つ。
func isolateXDG(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// clearXAPIEnv は X API 関連の環境変数を t.Setenv で空文字に固定する。
// 親プロセス側に既に値が設定されている場合の汚染を防ぐ。
func clearXAPIEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"X_API_KEY",
		"X_API_SECRET",
		"X_ACCESS_TOKEN",
		"X_ACCESS_TOKEN_SECRET",
	} {
		t.Setenv(k, "")
	}
}

// setAllXAPIEnv は X API 関連の環境変数を 4 つすべて非空にセットする。
//
// M12 以降は isolateXDG も同時に呼び出し、`liked.go` が `config.toml` を読みに行く
// パスが開発機 / CI の実ファイルに到達しないようにする。
func setAllXAPIEnv(t *testing.T) {
	t.Helper()
	isolateXDG(t)
	t.Setenv("X_API_KEY", "env-key")
	t.Setenv("X_API_SECRET", "env-secret")
	t.Setenv("X_ACCESS_TOKEN", "env-token")
	t.Setenv("X_ACCESS_TOKEN_SECRET", "env-token-secret")
}

// writeCredentialsFile は XDG_DATA_HOME 配下に credentials.toml を 0600 で書く。
func writeCredentialsFile(t *testing.T, dataHome string, c *config.Credentials) string {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", dataHome)
	path, err := config.DefaultCredentialsPath()
	if err != nil {
		t.Fatalf("DefaultCredentialsPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := config.SaveCredentials(path, c); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	return path
}

// TestErrCredentialsMissing_UnwrapsAuthError は ErrCredentialsMissing が
// xapi.ErrAuthentication を Unwrap で内包することを検証する (exit 3 写像のため)。
func TestErrCredentialsMissing_UnwrapsAuthError(t *testing.T) {
	if !errors.Is(ErrCredentialsMissing, xapi.ErrAuthentication) {
		t.Errorf("errors.Is(ErrCredentialsMissing, xapi.ErrAuthentication) = false, want true")
	}
}

// TestCredentialsFromEnv_AllSet は 4 つの env がすべて非空のとき
// ok=true と完備 creds が返ることを検証する。
func TestCredentialsFromEnv_AllSet(t *testing.T) {
	setAllXAPIEnv(t)
	c, ok := credentialsFromEnv()
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if c.APIKey != "env-key" || c.APISecret != "env-secret" ||
		c.AccessToken != "env-token" || c.AccessTokenSecret != "env-token-secret" {
		t.Errorf("creds = %+v, want all env-* values", c)
	}
}

// TestCredentialsFromEnv_PartialMissing は 1 つでも空の場合 ok=false が返ることを検証する。
func TestCredentialsFromEnv_PartialMissing(t *testing.T) {
	clearXAPIEnv(t)
	t.Setenv("X_API_KEY", "k")
	t.Setenv("X_API_SECRET", "s")
	t.Setenv("X_ACCESS_TOKEN", "t")
	// X_ACCESS_TOKEN_SECRET は空のまま
	_, ok := credentialsFromEnv()
	if ok {
		t.Errorf("ok = true, want false (one field missing)")
	}
}

// TestCredentialsFromEnv_AllMissing は env が全て空のとき ok=false が返ることを検証する。
func TestCredentialsFromEnv_AllMissing(t *testing.T) {
	clearXAPIEnv(t)
	c, ok := credentialsFromEnv()
	if ok {
		t.Errorf("ok = true, want false")
	}
	if c == nil {
		t.Fatal("creds = nil, want non-nil zero value")
	}
}

// TestLoadCredentialsFromEnvOrFile_EnvOnly は env のみで file 不在の場合
// env から取得した creds が返ることを検証する。
func TestLoadCredentialsFromEnvOrFile_EnvOnly(t *testing.T) {
	clearXAPIEnv(t)
	setAllXAPIEnv(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	c, err := LoadCredentialsFromEnvOrFile()
	if err != nil {
		t.Fatalf("LoadCredentialsFromEnvOrFile: %v", err)
	}
	if c.APIKey != "env-key" {
		t.Errorf("APIKey = %q, want env-key", c.APIKey)
	}
}

// TestLoadCredentialsFromEnvOrFile_FileOnly は env 全空 + file 完備で
// file の creds が返ることを検証する。
func TestLoadCredentialsFromEnvOrFile_FileOnly(t *testing.T) {
	clearXAPIEnv(t)
	dataHome := t.TempDir()
	writeCredentialsFile(t, dataHome, &config.Credentials{
		APIKey:            "file-key",
		APISecret:         "file-secret",
		AccessToken:       "file-token",
		AccessTokenSecret: "file-token-secret",
	})

	c, err := LoadCredentialsFromEnvOrFile()
	if err != nil {
		t.Fatalf("LoadCredentialsFromEnvOrFile: %v", err)
	}
	if c.APIKey != "file-key" {
		t.Errorf("APIKey = %q, want file-key", c.APIKey)
	}
	if c.AccessTokenSecret != "file-token-secret" {
		t.Errorf("AccessTokenSecret = %q, want file-token-secret", c.AccessTokenSecret)
	}
}

// TestLoadCredentialsFromEnvOrFile_EnvBeatsFile は env 4 つ揃っているとき
// file の値ではなく env の値が返ることを検証する (ファイル単位での優先順位)。
func TestLoadCredentialsFromEnvOrFile_EnvBeatsFile(t *testing.T) {
	clearXAPIEnv(t)
	setAllXAPIEnv(t)
	dataHome := t.TempDir()
	writeCredentialsFile(t, dataHome, &config.Credentials{
		APIKey:            "file-key",
		APISecret:         "file-secret",
		AccessToken:       "file-token",
		AccessTokenSecret: "file-token-secret",
	})

	c, err := LoadCredentialsFromEnvOrFile()
	if err != nil {
		t.Fatalf("LoadCredentialsFromEnvOrFile: %v", err)
	}
	if c.APIKey != "env-key" {
		t.Errorf("APIKey = %q, want env-key (env should beat file)", c.APIKey)
	}
}

// TestLoadCredentialsFromEnvOrFile_PartialEnvFallsBackToFile は env 部分欠落 + file 完備のとき
// file の creds が返ることを検証する (env 部分上書きはしない / ファイル単位)。
func TestLoadCredentialsFromEnvOrFile_PartialEnvFallsBackToFile(t *testing.T) {
	clearXAPIEnv(t)
	t.Setenv("X_API_KEY", "env-key")
	t.Setenv("X_API_SECRET", "env-secret")
	t.Setenv("X_ACCESS_TOKEN", "env-token")
	// X_ACCESS_TOKEN_SECRET は空 → env 不足

	dataHome := t.TempDir()
	writeCredentialsFile(t, dataHome, &config.Credentials{
		APIKey:            "file-key",
		APISecret:         "file-secret",
		AccessToken:       "file-token",
		AccessTokenSecret: "file-token-secret",
	})

	c, err := LoadCredentialsFromEnvOrFile()
	if err != nil {
		t.Fatalf("LoadCredentialsFromEnvOrFile: %v", err)
	}
	if c.APIKey != "file-key" {
		t.Errorf("APIKey = %q, want file-key (env incomplete → fallback to file)", c.APIKey)
	}
}

// TestLoadCredentialsFromEnvOrFile_Missing は env 欠落 + file 不在のとき
// ErrCredentialsMissing が返り xapi.ErrAuthentication を Unwrap することを検証する。
func TestLoadCredentialsFromEnvOrFile_Missing(t *testing.T) {
	clearXAPIEnv(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	_, err := LoadCredentialsFromEnvOrFile()
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

// TestLoadCredentialsFromEnvOrFile_FileIncomplete は env 欠落 + file が 1 フィールド空のとき
// ErrCredentialsMissing が返ることを検証する。
func TestLoadCredentialsFromEnvOrFile_FileIncomplete(t *testing.T) {
	clearXAPIEnv(t)
	dataHome := t.TempDir()
	writeCredentialsFile(t, dataHome, &config.Credentials{
		APIKey:            "file-key",
		APISecret:         "file-secret",
		AccessToken:       "file-token",
		AccessTokenSecret: "", // 欠落
	})

	_, err := LoadCredentialsFromEnvOrFile()
	if !errors.Is(err, ErrCredentialsMissing) {
		t.Errorf("errors.Is(err, ErrCredentialsMissing) = false (err=%v)", err)
	}
}

// TestLoadCredentialsFromEnvOrFile_ParseError は credentials.toml が不正な TOML のとき
// wrap されたパースエラーが返ること、xapi.ErrAuthentication は Unwrap しないことを検証する。
func TestLoadCredentialsFromEnvOrFile_ParseError(t *testing.T) {
	clearXAPIEnv(t)
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	path, err := config.DefaultCredentialsPath()
	if err != nil {
		t.Fatalf("DefaultCredentialsPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// 不正な TOML を 0600 で書き出す
	if err := os.WriteFile(path, []byte("this is not = valid = toml ==="), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = LoadCredentialsFromEnvOrFile()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrCredentialsMissing) {
		t.Errorf("errors.Is(err, ErrCredentialsMissing) = true, want false (parse error should not map to missing)")
	}
	if errors.Is(err, xapi.ErrAuthentication) {
		t.Errorf("errors.Is(err, xapi.ErrAuthentication) = true, want false (parse error → exit 1)")
	}
	if !strings.Contains(err.Error(), "auth loader") {
		t.Errorf("error message %q, want substring 'auth loader'", err.Error())
	}
}
