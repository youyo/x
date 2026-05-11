package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckConfigNoSecrets(t *testing.T) {
	tests := []struct {
		name        string
		write       bool
		content     string
		wantErrIs   error
		wantSubstrs []string
	}{
		{
			name:      "file_not_exist",
			write:     false,
			wantErrIs: nil,
		},
		{
			name:  "clean_config",
			write: true,
			content: `
[cli]
output = "json"
log_level = "info"

[liked]
default_max_results = 100
default_max_pages = 50
default_tweet_fields = "id,text"
default_expansions = "author_id"
default_user_fields = "username,name"
`,
			wantErrIs: nil,
		},
		{
			name:  "xapi_section_present",
			write: true,
			content: `
[xapi]
api_key = "leak"
`,
			wantErrIs:   ErrSecretInConfig,
			wantSubstrs: []string{"xapi"},
		},
		{
			name:  "top_level_api_key",
			write: true,
			content: `
api_key = "leak"
`,
			wantErrIs:   ErrSecretInConfig,
			wantSubstrs: []string{"api_key"},
		},
		{
			name:  "nested_access_token_under_cli",
			write: true,
			content: `
[cli]
output = "json"
access_token = "leak"
`,
			wantErrIs:   ErrSecretInConfig,
			wantSubstrs: []string{"cli.access_token"},
		},
		{
			name:  "multiple_secrets",
			write: true,
			content: `
[xapi]
api_key = "leak1"
api_secret = "leak2"
access_token = "leak3"
access_token_secret = "leak4"
`,
			wantErrIs: ErrSecretInConfig,
			wantSubstrs: []string{
				"xapi",
				"xapi.api_key",
				"xapi.api_secret",
				"xapi.access_token",
				"xapi.access_token_secret",
			},
		},
		{
			name:  "case_insensitive_section",
			write: true,
			content: `
[XAPI]
API_KEY = "leak"
`,
			wantErrIs:   ErrSecretInConfig,
			wantSubstrs: []string{"xapi"},
		},
		{
			name:  "invalid_toml",
			write: true,
			content: `
[cli
output = "json"
`,
			wantErrIs:   nil, // ErrSecretInConfig ではないが、エラーは返る
			wantSubstrs: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			if tc.write {
				if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
					t.Fatalf("os.WriteFile() error = %v", err)
				}
			}

			err := CheckConfigNoSecrets(path)

			// invalid_toml は特殊扱い (デコードエラーになるはず、ErrSecretInConfig ではない)
			if tc.name == "invalid_toml" {
				if err == nil {
					t.Errorf("CheckConfigNoSecrets() error = nil, want decode error")
				}
				if errors.Is(err, ErrSecretInConfig) {
					t.Errorf("CheckConfigNoSecrets() error = ErrSecretInConfig, want decode error")
				}
				return
			}

			if tc.wantErrIs == nil {
				if err != nil {
					t.Errorf("CheckConfigNoSecrets() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErrIs) {
				t.Errorf("CheckConfigNoSecrets() error = %v, want errors.Is(_, %v)", err, tc.wantErrIs)
			}
			for _, sub := range tc.wantSubstrs {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error message %q does not contain %q", err.Error(), sub)
				}
			}
		})
	}
}

func TestCheckConfigNoSecrets_DoesNotLeakValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	secret := "ABSOLUTELY-NOT-IN-ERROR-MESSAGE"
	content := "[xapi]\napi_key = \"" + secret + "\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	err := CheckConfigNoSecrets(path)
	if !errors.Is(err, ErrSecretInConfig) {
		t.Fatalf("CheckConfigNoSecrets() error = %v, want ErrSecretInConfig", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error message leaks secret value: %q", err.Error())
	}
}
