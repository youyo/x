// Package mcp は X (旧 Twitter) API CLI 向けの MCP サーバー雛形を提供する。
//
// mark3labs/mcp-go を基盤に「x」サーバーをファクトリ NewServer で構築する。
// 本パッケージは tool capability の宣言と最小限のメタ情報 (name / version) を
// 担い、tool 登録 (get_user_me / get_liked_tweets) は後続マイルストーンで
// 別ファイルの登録関数経由で差し込む設計とする。
//
// 認証 middleware は internal/authgate、HTTP transport は
// internal/transport/http に分離してそれぞれ責務を持つ。
package mcp
