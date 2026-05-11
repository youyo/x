// Package xapi は X (Twitter) API v2 のクライアントを提供する。
//
// OAuth 1.0a (HMAC-SHA1) 署名は dghubble/oauth1 のラッパーを介して付与する。
// HTTP retry / rate-limit handling は client.go (M6 以降) で実装する。
package xapi

import (
	"context"
	"net/http"

	"github.com/dghubble/oauth1"

	"github.com/youyo/x/internal/config"
)

// NewOAuth1Config は config.Credentials を dghubble/oauth1.Config に変換する。
//
// マッピング:
//   - Credentials.APIKey    → oauth1.Config.ConsumerKey
//   - Credentials.APISecret → oauth1.Config.ConsumerSecret
//
// AccessToken / AccessTokenSecret は oauth1.Token として別途扱われるため
// 本関数の返り値には含まれない (NewHTTPClient 内部で oauth1.NewToken に渡す)。
//
// creds が nil の場合は空文字列 4 つを持つゼロ値 Credentials として扱う
// (panic させず、利用時に X API 側で 401 を返させる方針)。
func NewOAuth1Config(creds *config.Credentials) *oauth1.Config {
	c := safeCredentials(creds)
	return oauth1.NewConfig(c.APIKey, c.APISecret)
}

// NewHTTPClient は ctx と config.Credentials から OAuth 1.0a 署名済み *http.Client を返す。
//
// 用途: M6 (`client.go`) で X API v2 の各エンドポイントに対するリクエストに
// 利用する。返却される Client はリクエストごとに oauth_nonce / oauth_timestamp /
// oauth_signature を再計算し Authorization ヘッダに付与する。
//
// ctx は dghubble/oauth1.Config.Client にそのまま渡す。通常は context.Background()
// で良い。transport カスタマイズが必要になった場合の挙動は M6 で再確認する。
//
// creds が nil の場合は空 Credentials として扱う (NewOAuth1Config と同方針)。
func NewHTTPClient(ctx context.Context, creds *config.Credentials) *http.Client {
	c := safeCredentials(creds)
	cfg := oauth1.NewConfig(c.APIKey, c.APISecret)
	token := oauth1.NewToken(c.AccessToken, c.AccessTokenSecret)
	return cfg.Client(ctx, token)
}

// safeCredentials は nil の場合に空 Credentials を返すヘルパ。
//
// 公開シンボルを panic させない設計判断 (D-2) のための内部関数。
func safeCredentials(creds *config.Credentials) *config.Credentials {
	if creds == nil {
		return &config.Credentials{}
	}
	return creds
}
