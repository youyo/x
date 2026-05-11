package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/youyo/x/internal/config"
)

func TestConfigDir(t *testing.T) {
	// t.Setenv を使うため Parallel 不可 (Go 標準 testing の制約)。

	tests := []struct {
		name      string
		xdgValue  string
		xdgSet    bool
		homeValue string
		wantSfx   string // 期待結果の末尾サブパス (HOME を一時パスにするため絶対値比較は別途)
	}{
		{
			name:      "xdg_set",
			xdgValue:  "/tmp/xdg",
			xdgSet:    true,
			homeValue: "/tmp/home",
			wantSfx:   filepath.Join("/tmp/xdg", "x"),
		},
		{
			name:      "xdg_unset",
			xdgSet:    false,
			homeValue: "/tmp/home",
			wantSfx:   filepath.Join("/tmp/home", ".config", "x"),
		},
		{
			name:      "xdg_empty",
			xdgValue:  "",
			xdgSet:    true,
			homeValue: "/tmp/home",
			wantSfx:   filepath.Join("/tmp/home", ".config", "x"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.xdgSet {
				t.Setenv("XDG_CONFIG_HOME", tc.xdgValue)
			} else {
				// 親プロセスから引き継いだ環境変数を空文字でマスクし、
				// 同時に t.Setenv に「ここで設定し、テスト終了時に復元」させる。
				// Go の t.Setenv は元値を保存して復元するので、unset 相当の検証には
				// "" を入れるしかない。loader 側で len(s) == 0 を未設定として扱う設計のため許容。
				t.Setenv("XDG_CONFIG_HOME", "")
			}
			t.Setenv("HOME", tc.homeValue)

			got, err := config.Dir()
			if err != nil {
				t.Fatalf("ConfigDir() returned error: %v", err)
			}
			if got != tc.wantSfx {
				t.Fatalf("ConfigDir() = %q, want %q", got, tc.wantSfx)
			}
		})
	}
}

func TestDataDir(t *testing.T) {
	// t.Setenv を使うため Parallel 不可 (Go 標準 testing の制約)。

	tests := []struct {
		name      string
		xdgValue  string
		xdgSet    bool
		homeValue string
		want      string
	}{
		{
			name:      "xdg_set",
			xdgValue:  "/tmp/xdg-data",
			xdgSet:    true,
			homeValue: "/tmp/home",
			want:      filepath.Join("/tmp/xdg-data", "x"),
		},
		{
			name:      "xdg_unset",
			xdgSet:    false,
			homeValue: "/tmp/home",
			want:      filepath.Join("/tmp/home", ".local", "share", "x"),
		},
		{
			name:      "xdg_empty",
			xdgValue:  "",
			xdgSet:    true,
			homeValue: "/tmp/home",
			want:      filepath.Join("/tmp/home", ".local", "share", "x"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.xdgSet {
				t.Setenv("XDG_DATA_HOME", tc.xdgValue)
			} else {
				t.Setenv("XDG_DATA_HOME", "")
			}
			t.Setenv("HOME", tc.homeValue)

			got, err := config.DataDir()
			if err != nil {
				t.Fatalf("DataDir() returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("DataDir() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDefaultCLIConfigPath(t *testing.T) {
	// t.Setenv を使うため Parallel 不可。

	t.Run("xdg_set_returns_config_toml_under_xdg", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
		t.Setenv("HOME", "/tmp/home")

		got, err := config.DefaultCLIConfigPath()
		if err != nil {
			t.Fatalf("DefaultCLIConfigPath() returned error: %v", err)
		}
		want := filepath.Join("/tmp/xdg", "x", "config.toml")
		if got != want {
			t.Fatalf("DefaultCLIConfigPath() = %q, want %q", got, want)
		}
	})

	t.Run("suffix_is_config_toml", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "/tmp/home")

		got, err := config.DefaultCLIConfigPath()
		if err != nil {
			t.Fatalf("DefaultCLIConfigPath() returned error: %v", err)
		}
		if !strings.HasSuffix(got, filepath.Join("x", "config.toml")) {
			t.Fatalf("DefaultCLIConfigPath() = %q, want suffix %q", got, filepath.Join("x", "config.toml"))
		}
	})
}
