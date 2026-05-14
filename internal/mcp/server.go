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
// 構築時に tool capability を宣言した上で、登録済み tools を順次差し込む:
//   - M17: get_user_me (registerToolMe)
//   - M18: get_liked_tweets (registerToolLikes)
//
// 引数:
//   - client: X API 呼び出しに利用する xapi.Client。nil でも登録自体は成功する
//     (ハンドラ実行時にしか参照されないため)。実運用では非 nil を渡すこと。
//   - version: server.version として initialize レスポンスに反映される。
//     ldflags 注入される binary version (internal/version) を渡すことを想定する。
func NewServer(client *xapi.Client, version string) *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer(
		ServerName,
		version,
		mcpserver.WithToolCapabilities(true),
	)
	registerToolMe(s, client)
	registerToolLikes(s, client)
	registerToolTweet(s, client)
	registerToolSearch(s, client)
	return s
}
