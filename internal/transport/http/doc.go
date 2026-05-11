// Package http は MCP サーバーを HTTP transport で提供するための薄いラッパーを
// 提供する。
//
// mark3labs/mcp-go の Streamable HTTP server を内包し、LWA (Lambda Web Adapter)
// 互換のシンプルな net/http.Server として起動できる。
//
// シグナル監視 (SIGTERM / SIGINT) は呼び出し側 (cobra / main) が
// signal.NotifyContext で行い、その ctx を Run に渡す責務とする。
// 本パッケージは ctx ベースのライフサイクル管理のみを扱う。
//
// 認証 middleware の接続は internal/authgate に委ねる (M16 以降)。
package http
