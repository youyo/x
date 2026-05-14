package mcp_test

import (
	"testing"

	mcpinternal "github.com/youyo/x/internal/mcp"
	"github.com/youyo/x/internal/xapi"
)

// TestNewServer_NotNil は NewServer が nil でない MCPServer を返すことを確認する。
// 現時点で xapi.Client は未使用のため nil を渡してテストする
// (M17 でクライアント注入後に契約を更新予定)。
func TestNewServer_NotNil(t *testing.T) {
	t.Parallel()

	var client *xapi.Client
	s := mcpinternal.NewServer(client, "1.0.0")
	if s == nil {
		t.Fatal("expected non-nil MCPServer")
	}
}

// TestNewServer_VersionFormats はバージョン文字列のフォーマットに依存せず
// NewServer が成功することを確認する。
// (dev / リリースバージョンの両方で panic しないこと)
func TestNewServer_VersionFormats(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		version string
	}{
		{"dev", "dev"},
		{"semver", "1.2.3"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := mcpinternal.NewServer(nil, tc.version)
			if s == nil {
				t.Fatalf("expected non-nil MCPServer for version %q", tc.version)
			}
		})
	}
}

// TestServerName は公開定数 ServerName が "x" であることを確認する。
// バイナリ名 (cmd/x) とスペック §5 に整合するための回帰防止テスト。
func TestServerName(t *testing.T) {
	t.Parallel()

	if mcpinternal.ServerName != "x" {
		t.Errorf("ServerName = %q, want %q", mcpinternal.ServerName, "x")
	}
}

// TestNewServer_RegistersAllTools は NewServer が全 20 ツール
// (M17 / M18 既存 2 + M36 新規 18) を登録することを pin する (advisor 指摘)。
//
// 各ツールが MCP 仕様の `tools/list` で見えるよう ListTools() に名前が存在することを検証。
func TestNewServer_RegistersAllTools(t *testing.T) {
	t.Parallel()

	want := []string{
		// Existing (M17, M18)
		"get_user_me",
		"get_liked_tweets",
		// M36 T1: tools_tweet.go
		"get_tweet",
		"get_tweets",
		"get_liking_users",
		"get_retweeted_by",
		"get_quote_tweets",
		// M36 T2: tools_search.go
		"search_recent_tweets",
		"get_tweet_thread",
		// M36 T3: tools_timeline.go
		"get_user_tweets",
		"get_user_mentions",
		"get_home_timeline",
		// M36 T4: tools_users.go
		"get_user",
		"get_user_by_username",
		"get_user_following",
		"get_user_followers",
		// M36 T5: tools_lists.go
		"get_list",
		"get_list_tweets",
		// M36 T6: tools_misc.go
		"search_spaces",
		"get_trends",
	}

	s := mcpinternal.NewServer(nil, "test")
	registered := s.ListTools()
	if got := len(registered); got != len(want) {
		t.Errorf("registered tool count = %d, want %d", got, len(want))
	}
	for _, name := range want {
		if _, ok := registered[name]; !ok {
			t.Errorf("tool %q is not registered", name)
		}
	}
}
