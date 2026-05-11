package config

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultCredentialsPath(t *testing.T) {
	tests := []struct {
		name        string
		xdgDataHome string
		home        string
		want        string
	}{
		{
			name:        "xdg_set",
			xdgDataHome: "/tmp/xdg-data",
			home:        "/tmp/home",
			want:        filepath.Join("/tmp/xdg-data", "x", "credentials.toml"),
		},
		{
			name:        "xdg_unset_falls_back_to_home",
			xdgDataHome: "",
			home:        "/tmp/home",
			want:        filepath.Join("/tmp/home", ".local", "share", "x", "credentials.toml"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", tc.xdgDataHome)
			t.Setenv("HOME", tc.home)

			got, err := DefaultCredentialsPath()
			if err != nil {
				t.Fatalf("DefaultCredentialsPath() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("DefaultCredentialsPath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCheckPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX パーミッション検査は Windows ではスキップ")
	}

	tests := []struct {
		name    string
		setup   func(t *testing.T, path string)
		wantErr error
	}{
		{
			name: "not_exist",
			setup: func(_ *testing.T, _ string) {
				// 何もしない (ファイルを作らない)
			},
			wantErr: ErrCredentialsNotFound,
		},
		{
			name: "mode_0600",
			setup: func(t *testing.T, path string) {
				writeFileWithMode(t, path, "x", 0o600)
			},
			wantErr: nil,
		},
		{
			name: "mode_0400",
			setup: func(t *testing.T, path string) {
				writeFileWithMode(t, path, "x", 0o400)
			},
			wantErr: nil,
		},
		{
			name: "mode_0644",
			setup: func(t *testing.T, path string) {
				writeFileWithMode(t, path, "x", 0o644)
			},
			wantErr: ErrPermissionsTooOpen,
		},
		{
			name: "mode_0640_group_read",
			setup: func(t *testing.T, path string) {
				writeFileWithMode(t, path, "x", 0o640)
			},
			wantErr: ErrPermissionsTooOpen,
		},
		{
			name: "mode_0604_other_read",
			setup: func(t *testing.T, path string) {
				writeFileWithMode(t, path, "x", 0o604)
			},
			wantErr: ErrPermissionsTooOpen,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "credentials.toml")
			tc.setup(t, path)

			err := CheckPermissions(path)
			switch {
			case tc.wantErr == nil && err != nil:
				t.Errorf("CheckPermissions() error = %v, want nil", err)
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Errorf("CheckPermissions() error = %v, want errors.Is(_, %v)", err, tc.wantErr)
			}
		})
	}
}

func TestLoadCredentials_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.toml")

	_, err := LoadCredentials(path)
	if !errors.Is(err, ErrCredentialsNotFound) {
		t.Errorf("LoadCredentials() error = %v, want errors.Is(_, ErrCredentialsNotFound)", err)
	}
}

func TestLoadCredentials_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.toml")

	want := &Credentials{
		APIKey:            "key-001",
		APISecret:         "secret-002",
		AccessToken:       "token-003",
		AccessTokenSecret: "token-secret-004",
	}
	if err := SaveCredentials(path, want); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("os.Stat() error = %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("file mode = %o, want 0600", got)
		}
	}

	got, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if *got != *want {
		t.Errorf("LoadCredentials() = %+v, want %+v", got, want)
	}
}

// TestLoadCredentials_PermissionsWarning は log.SetOutput をグローバルに差し替えるため、
// 同パッケージ内で t.Parallel() を使うテストを追加する際は本テストと衝突しないように注意する。
// 現状 (M4 時点) は他テストも非並行なので問題なし。
func TestLoadCredentials_PermissionsWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX パーミッション検査は Windows ではスキップ")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.toml")

	want := &Credentials{APIKey: "k", APISecret: "s", AccessToken: "t", AccessTokenSecret: "ts"}
	if err := SaveCredentials(path, want); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("os.Chmod() error = %v", err)
	}

	var buf bytes.Buffer
	origWriter := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(origWriter)
		log.SetFlags(origFlags)
	})

	got, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v (want nil, only warning)", err)
	}
	if *got != *want {
		t.Errorf("LoadCredentials() = %+v, want %+v", got, want)
	}
	if !strings.Contains(buf.String(), "warning") {
		t.Errorf("expected log to contain %q, got %q", "warning", buf.String())
	}
}

func TestLoadCredentials_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.toml")

	// 構文不正な TOML を書く (セクションが閉じていない)
	if err := os.WriteFile(path, []byte("[xapi\napi_key = \"x\"\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	_, err := LoadCredentials(path)
	if err == nil {
		t.Errorf("LoadCredentials() error = nil, want decode error")
	}
	if errors.Is(err, ErrCredentialsNotFound) {
		t.Errorf("LoadCredentials() error = ErrCredentialsNotFound, want decode error")
	}
}

func TestSaveCredentials_CreatesDir(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "x", "credentials.toml")

	c := &Credentials{APIKey: "k"}
	if err := SaveCredentials(path, c); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}

	if runtime.GOOS != "windows" {
		dirInfo, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatalf("os.Stat(dir) error = %v", err)
		}
		if got := dirInfo.Mode().Perm(); got != 0o700 {
			t.Errorf("dir mode = %o, want 0700", got)
		}

		fileInfo, err := os.Stat(path)
		if err != nil {
			t.Fatalf("os.Stat(file) error = %v", err)
		}
		if got := fileInfo.Mode().Perm(); got != 0o600 {
			t.Errorf("file mode = %o, want 0600", got)
		}
	}
}

func TestSaveCredentials_NilReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.toml")

	if err := SaveCredentials(path, nil); err == nil {
		t.Errorf("SaveCredentials(_, nil) error = nil, want non-nil")
	}
}

// writeFileWithMode はテスト用ヘルパ: 指定 mode でファイルを作成する。
// umask の影響を避けるため WriteFile 後に Chmod を実行する。
func writeFileWithMode(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("os.Chmod() error = %v", err)
	}
}
