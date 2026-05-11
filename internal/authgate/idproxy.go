package authgate

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/youyo/idproxy"
)

// minCookieSecretBytes は CookieSecret の最小バイト数。
// spec §11 の `COOKIE_SECRET` 要件 (32B+) と idproxy.Config.Validate の制約に整合する。
const minCookieSecretBytes = 32

// ErrIDProxyConfigInvalid は [ModeIDProxy] の必須設定が欠落・不正値の際に
// [New] が返すエラーである。errors.Is で判定すること。
//
// このエラーがラップする原因は次のいずれか:
//   - Issuer / ClientID / ClientSecret / CookieSecret / ExternalURL の未指定
//   - CookieSecret が hex デコード不能、もしくは 32 バイト未満
//   - CSV エントリに空文字が含まれる (trim 後)
//   - [ErrIDProxyProvidersMismatch] (CSV 件数不一致)
//   - idproxy.Config.Validate の検証エラー (パッケージ内部で wrap)
var ErrIDProxyConfigInvalid = errors.New("authgate: idproxy config is invalid")

// ErrIDProxyProvidersMismatch は OIDC_ISSUER / OIDC_CLIENT_ID / OIDC_CLIENT_SECRET の
// CSV 件数が一致しない場合に返されるエラーである。
// 必ず [ErrIDProxyConfigInvalid] と共にラップされて返るため、両方で errors.Is が真になる。
var ErrIDProxyProvidersMismatch = errors.New(
	"authgate: oidc issuer/client_id/client_secret csv lengths do not match",
)

// IDProxy は [github.com/youyo/idproxy.Auth] をラップし、authgate の [Middleware] として
// 機能する型である。`Wrap` メソッドはそのまま idproxy.Auth.Wrap に委譲する。
//
// 本マイルストーン (M20) では `idproxy.Config.OAuth = nil` 固定で構築するため、
// 提供される認証フローは「ブラウザ cookie session」のみである。Bearer JWT 検証 /
// OAuth 2.1 AS エンドポイントは未対応。
type IDProxy struct {
	auth *idproxy.Auth
}

// Wrap は idproxy.Auth.Wrap を呼び、cookie session 認証 middleware を返す。
// 未認証リクエストは idproxy 内部のロジックに従い、login へのリダイレクト
// (ブラウザリクエスト) もしくは 401 応答 (API リクエスト) となる。
func (i *IDProxy) Wrap(next http.Handler) http.Handler {
	return i.auth.Wrap(next)
}

// newIDProxy は authgate 内部の [options] から [idproxy.Config] を組み立て、
// idproxy.New を呼んで [IDProxy] を生成する非エクスポート関数である。
//
// 設定不足 / 値の不正は [ErrIDProxyConfigInvalid] でラップして返す。CSV 件数不一致は
// [ErrIDProxyProvidersMismatch] を [ErrIDProxyConfigInvalid] でラップする。
//
// idproxy.Config.OAuth は本マイルストーンでは常に nil とし、ブラウザ cookie session
// 認証のみを有効化する。Store が未指定の場合は [NewMemoryStore] のデフォルトを使う。
func newIDProxy(ctx context.Context, o options) (*IDProxy, error) {
	cfg, err := buildIDProxyConfig(o)
	if err != nil {
		return nil, err
	}

	auth, err := idproxy.New(ctx, cfg)
	if err != nil {
		// idproxy 内部で発生した設定検証エラーも ErrIDProxyConfigInvalid でラップする。
		// ネットワーク系 (Discovery 失敗) も同種のラップで返す。
		return nil, fmt.Errorf("%w: %w", ErrIDProxyConfigInvalid, err)
	}
	return &IDProxy{auth: auth}, nil
}

// buildIDProxyConfig は options から idproxy.Config を組み立てる。
// authgate 層で先に弾けるバリデーション (必須項目、CSV pairing、CookieSecret hex 形式) を
// すべてここで実行する。idproxy.New に渡る前の早期 reject で、エラー文言を統一する。
func buildIDProxyConfig(o options) (idproxy.Config, error) {
	if o.oidcIssuer == "" {
		return idproxy.Config{}, fmt.Errorf("%w: %s", ErrIDProxyConfigInvalid, "oidc issuer is required")
	}
	if o.oidcClientID == "" {
		return idproxy.Config{}, fmt.Errorf("%w: %s", ErrIDProxyConfigInvalid, "oidc client_id is required")
	}
	if o.oidcClientSecret == "" {
		return idproxy.Config{}, fmt.Errorf("%w: %s", ErrIDProxyConfigInvalid, "oidc client_secret is required")
	}
	if o.externalURL == "" {
		return idproxy.Config{}, fmt.Errorf("%w: %s", ErrIDProxyConfigInvalid, "external_url is required")
	}
	if o.cookieSecret == "" {
		return idproxy.Config{}, fmt.Errorf("%w: %s", ErrIDProxyConfigInvalid, "cookie_secret is required")
	}

	cookieSecret, err := hex.DecodeString(o.cookieSecret)
	if err != nil {
		return idproxy.Config{}, fmt.Errorf("%w: cookie_secret must be hex: %w", ErrIDProxyConfigInvalid, err)
	}
	if len(cookieSecret) < minCookieSecretBytes {
		return idproxy.Config{}, fmt.Errorf(
			"%w: cookie_secret must be at least %d bytes (got %d)",
			ErrIDProxyConfigInvalid, minCookieSecretBytes, len(cookieSecret),
		)
	}

	providers, err := buildProviders(o.oidcIssuer, o.oidcClientID, o.oidcClientSecret)
	if err != nil {
		return idproxy.Config{}, err
	}

	store := o.idproxyStore
	if store == nil {
		store = NewMemoryStore()
	}

	cfg := idproxy.Config{
		Providers:    providers,
		ExternalURL:  o.externalURL,
		CookieSecret: cookieSecret,
		Store:        store,
		PathPrefix:   o.idproxyPrefix,
		OAuth:        nil, // M20 では OAuth 2.1 AS を無効化 (cookie session 認証のみ)
	}
	return cfg, nil
}

// buildProviders は CSV 形式の Issuer / ClientID / ClientSecret から
// idproxy.OIDCProvider のスライスを構築する。
//
// 規則:
//   - 各 CSV エントリは空白 trim 後に評価する
//   - trim 後に空文字となるエントリは [ErrIDProxyConfigInvalid] で reject
//   - 3 つの CSV のエントリ数は完全一致が必須 (不一致は [ErrIDProxyProvidersMismatch])
//   - 最低 1 エントリ必要
//   - インデックス位置同士でペアを成す (Issuer[i] と ClientID[i] と ClientSecret[i])
func buildProviders(issuerCSV, clientIDCSV, clientSecretCSV string) ([]idproxy.OIDCProvider, error) {
	issuers := splitCSV(issuerCSV)
	clientIDs := splitCSV(clientIDCSV)
	clientSecrets := splitCSV(clientSecretCSV)

	if len(issuers) == 0 {
		return nil, fmt.Errorf("%w: oidc issuer must have at least one entry", ErrIDProxyConfigInvalid)
	}
	if len(issuers) != len(clientIDs) || len(issuers) != len(clientSecrets) {
		return nil, fmt.Errorf(
			"%w: %w (issuer=%d, client_id=%d, client_secret=%d)",
			ErrIDProxyConfigInvalid, ErrIDProxyProvidersMismatch,
			len(issuers), len(clientIDs), len(clientSecrets),
		)
	}

	providers := make([]idproxy.OIDCProvider, 0, len(issuers))
	for i, iss := range issuers {
		if iss == "" {
			return nil, fmt.Errorf("%w: oidc issuer entry %d is empty", ErrIDProxyConfigInvalid, i)
		}
		if clientIDs[i] == "" {
			return nil, fmt.Errorf("%w: oidc client_id entry %d is empty", ErrIDProxyConfigInvalid, i)
		}
		if clientSecrets[i] == "" {
			return nil, fmt.Errorf("%w: oidc client_secret entry %d is empty", ErrIDProxyConfigInvalid, i)
		}
		providers = append(providers, idproxy.OIDCProvider{
			Issuer:       iss,
			ClientID:     clientIDs[i],
			ClientSecret: clientSecrets[i],
		})
	}
	return providers, nil
}

// splitCSV は CSV 文字列を strings.Split し、各エントリの前後空白を strip して返す。
// 空文字入力に対しては nil を返す。空白のみのエントリも空文字として保持する (上位で reject)。
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}
