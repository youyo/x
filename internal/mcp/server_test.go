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
