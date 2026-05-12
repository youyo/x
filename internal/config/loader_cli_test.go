package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/youyo/x/internal/config"
)

func TestLoadCLI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		writeFile bool   // false の場合はファイルを作成しない
		body      string // writeFile=true の場合のファイル内容
		wantCfg   *config.CLIConfig
		wantErr   bool
		errSubstr string // wantErr=true 時、エラーメッセージに含まれることを期待する部分文字列
	}{
		{
			name:      "file_not_found",
			writeFile: false,
			wantCfg:   config.DefaultCLIConfig(),
		},
		{
			name:      "empty_file",
			writeFile: true,
			body:      "",
			wantCfg:   config.DefaultCLIConfig(),
		},
		{
			name:      "partial_file",
			writeFile: true,
			body: `
[cli]
output = "ndjson"
`,
			wantCfg: func() *config.CLIConfig {
				c := config.DefaultCLIConfig()
				c.CLI.Output = "ndjson"
				return c
			}(),
		},
		{
			name:      "mixed_section",
			writeFile: true,
			body: `
[cli]
output = "ndjson"

[liked]
default_max_pages = 200
`,
			wantCfg: func() *config.CLIConfig {
				c := config.DefaultCLIConfig()
				c.CLI.Output = "ndjson"
				c.Liked.DefaultMaxPages = 200
				return c
			}(),
		},
		{
			name:      "full_file",
			writeFile: true,
			body: `
[cli]
output = "table"
log_level = "debug"

[liked]
default_max_results = 50
default_max_pages = 10
default_tweet_fields = "id,text"
default_expansions = "author_id,referenced_tweets.id"
default_user_fields = "username"
`,
			wantCfg: &config.CLIConfig{
				CLI: config.CLISection{
					Output:   "table",
					LogLevel: "debug",
				},
				Liked: config.LikedSection{
					DefaultMaxResults:  50,
					DefaultMaxPages:    10,
					DefaultTweetFields: "id,text",
					DefaultExpansions:  "author_id,referenced_tweets.id",
					DefaultUserFields:  "username",
				},
			},
		},
		{
			name:      "invalid_toml",
			writeFile: true,
			body:      "[cli\noutput = \"json\"\n", // 角括弧未閉
			wantErr:   true,
			errSubstr: "decode",
		},
		{
			name:      "unknown_keys_ignored",
			writeFile: true,
			body: `
[cli]
foo = "bar"
`,
			wantCfg: config.DefaultCLIConfig(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			if tc.writeFile {
				if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
					t.Fatalf("failed to write fixture: %v", err)
				}
			}

			got, err := config.LoadCLI(path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("LoadCLI(%q) expected error, got nil (cfg=%#v)", path, got)
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("LoadCLI(%q) err = %q, want substring %q", path, err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadCLI(%q) returned unexpected error: %v", path, err)
			}
			if !reflect.DeepEqual(got, tc.wantCfg) {
				t.Fatalf("LoadCLI(%q) mismatch:\n got = %#v\nwant = %#v", path, got, tc.wantCfg)
			}
		})
	}
}
