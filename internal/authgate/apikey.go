package authgate

import (
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"strings"
)

// ErrAPIKeyMissing は [NewAPIKey] / [New] が ModeAPIKey で空のキーを受け取った際に返すエラーである。
// 環境変数未設定や CLI 層からの空文字渡しを早期に弾く目的で利用する。errors.Is で判定すること。
var ErrAPIKeyMissing = errors.New("authgate: api key is required for apikey mode")

// bearerPrefix は Authorization ヘッダで期待する scheme プレフィックスである。
// RFC 6750 §2.1 に従い、本パッケージでは scheme 名は case-insensitive で照合し、
// 末尾の空白は厳密に 1 文字を要求する。
const bearerPrefix = "Bearer "

// wwwAuthenticate は 401 応答に付与する WWW-Authenticate ヘッダ値である。
// RFC 6750 §3 に従い realm のみを返し、`error="invalid_token"` 等の細分化は
// 行わない (実装単純化 + token 有無の漏洩防止)。
const wwwAuthenticate = `Bearer realm="x-mcp"`

// APIKey は Authorization: Bearer トークンを共有シークレットと
// [crypto/subtle.ConstantTimeCompare] で定数時間比較する [Middleware] 実装である。
//
// spec §11 の `X_MCP_AUTH=apikey` モードに対応する。共有シークレットは [NewAPIKey]
// 経由でしか設定できず、本パッケージは環境変数を直接読まない (責務は CLI 層が持つ)。
type APIKey struct {
	key string
}

// NewAPIKey は与えられた共有シークレット key で [APIKey] を生成する。
// key が空文字の場合 [ErrAPIKeyMissing] を返し、middleware は nil となる。
func NewAPIKey(key string) (*APIKey, error) {
	if key == "" {
		return nil, ErrAPIKeyMissing
	}
	return &APIKey{key: key}, nil
}

// Wrap は Authorization: Bearer <token> を検証し、シークレット一致時のみ
// next に処理を委譲する http.Handler を返す。不一致 / ヘッダ欠落時は
// 401 Unauthorized と `WWW-Authenticate: Bearer realm="x-mcp"` を返す。
func (a *APIKey) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authorize(r) {
			writeUnauthorized(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorize は Authorization ヘッダから Bearer トークンを取り出し、
// 共有シークレットと定数時間比較する。一致時のみ true を返す。
func (a *APIKey) authorize(r *http.Request) bool {
	authz := r.Header.Get("Authorization")
	if len(authz) <= len(bearerPrefix) {
		// プレフィックス長以下なら token 部が必ず空 (もしくはヘッダ未設定)。
		return false
	}
	if !strings.EqualFold(authz[:len(bearerPrefix)], bearerPrefix) {
		return false
	}
	token := authz[len(bearerPrefix):]
	// subtle.ConstantTimeCompare は長さが異なれば 0 を返す。
	// spec §10 は長さ秘匿までは要求していないため、そのまま利用する。
	return subtle.ConstantTimeCompare([]byte(token), []byte(a.key)) == 1
}

// writeUnauthorized は 401 応答を書き出す。すべての失敗ケースで同一の応答を返し、
// 失敗理由の差異 (token 不正 / ヘッダ欠落) が外部から識別できないようにする。
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", wwwAuthenticate)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, "unauthorized\n")
}
