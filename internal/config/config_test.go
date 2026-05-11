package config_test

import (
	"reflect"
	"testing"

	"github.com/youyo/x/internal/config"
)

func TestDefaultCLIConfig(t *testing.T) {
	t.Parallel()

	got := config.DefaultCLIConfig()
	want := &config.CLIConfig{
		CLI: config.CLISection{
			Output:   "json",
			LogLevel: "info",
		},
		Liked: config.LikedSection{
			DefaultMaxResults:  100,
			DefaultMaxPages:    50,
			DefaultTweetFields: "id,text,author_id,created_at,entities,public_metrics",
			DefaultExpansions:  "author_id",
			DefaultUserFields:  "username,name",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultCLIConfig() mismatch:\n got = %#v\nwant = %#v", got, want)
	}
}

func TestDefaultCLIConfig_ReturnsNewInstance(t *testing.T) {
	t.Parallel()

	// 呼び出しごとに別インスタンスが返ることを確認 (グローバル可変状態の防止)。
	a := config.DefaultCLIConfig()
	b := config.DefaultCLIConfig()
	if a == b {
		t.Fatalf("DefaultCLIConfig() returned the same pointer on consecutive calls")
	}

	a.CLI.Output = "modified"
	if b.CLI.Output == "modified" {
		t.Fatalf("DefaultCLIConfig() instances share state (mutation visible across calls)")
	}
}
