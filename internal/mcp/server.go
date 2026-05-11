package mcp

import (
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/youyo/x/internal/xapi"
)

// ServerName は MCP サーバーが initialize レスポンスで返す server.name である。
// バイナリ名 (cmd/x) およびスペック §5 と整合させる。
const ServerName = "x"

// NewServer は X API CLI 用の MCP サーバーを構築する。
//
// 本マイルストーン (M15) では tools 登録を行わず、tool capability のみを
// 宣言した空サーバーを返す。tools (get_user_me / get_liked_tweets) は
// 後続マイルストーン (M17 / M18) で別ファイルの登録関数経由で差し込む。
//
// 引数:
//   - client: X API 呼び出しに利用する xapi.Client。M15 時点では未使用だが、
//     M17 以降で tool handler に注入するためシグネチャに含めておく。
//   - version: server.version として initialize レスポンスに反映される。
//     ldflags 注入される binary version (internal/version) を渡すことを想定する。
func NewServer(client *xapi.Client, version string) *mcpserver.MCPServer {
	// client は M17 以降で使用する。Go の未使用関数引数は idiomatic であり
	// linter も flag しないため discard は不要。
	_ = client
	return mcpserver.NewMCPServer(
		ServerName,
		version,
		mcpserver.WithToolCapabilities(true),
	)
}
