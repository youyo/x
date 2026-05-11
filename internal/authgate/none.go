package authgate

import "net/http"

// None は認証を行わない passthrough な [Middleware] 実装である。
//
// ローカル開発・テスト・LWA / function URL 等の別レイヤーで認可を担保するケースで
// 利用する。本番環境では使用しないこと。
type None struct{}

// Wrap は next をそのまま返し、認証チェックを一切行わない。
//
// next が nil の場合の挙動は呼び出し側 (transport/http) で next != nil を保証する
// 責務とし、本関数では nil チェックを行わない (Go 標準 middleware 慣習に合わせる)。
func (n *None) Wrap(next http.Handler) http.Handler {
	return next
}
