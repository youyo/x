// Package mcp は X (旧 Twitter) API CLI 向けの MCP サーバーを提供する。
//
// mark3labs/mcp-go を基盤に「x」サーバーをファクトリ NewServer で構築する。
// 本パッケージは tool capability の宣言と最小限のメタ情報 (name / version) を
// 担い、登録済みの tools を順次差し込む設計を採る:
//
//   - tools_me.go    : get_user_me      (M17)
//   - tools_likes.go : get_liked_tweets (M18)
//
// 各 tool はファイル冒頭で `registerToolXxx(s, client)` パターンを取り、
// NewServer から呼び出される。ハンドラ生成はテスト容易性のため
// `NewXxxHandler(client) server.ToolHandlerFunc` として分離している。
//
// get_liked_tweets は spec §6 の 11 入力パラメータをすべて受け付け、時間窓は
// yesterday_jst > since_jst > start_time/end_time の優先順位で決定する。
// user_id 未指定時は GetUserMe で self を解決する (CLI 層と同型)。
// max_results / max_pages / tweet_fields / expansions / user_fields のハードコード
// デフォルト (spec §11) は tools_likes.go 内に固定値として保持し、MCP モードは
// config.toml を読まない方針 (spec §11) を遵守する。
//
// 認証 middleware は internal/authgate、HTTP transport は
// internal/transport/http に分離してそれぞれ責務を持つ。
package mcp
