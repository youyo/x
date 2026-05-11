package authgate

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/youyo/idproxy"
)

// Mode は authgate の認証モードを表す文字列型である。
// 値はスペック §11 の環境変数 X_MCP_AUTH と 1:1 対応する。
type Mode string

// 利用可能な認証モードの定数。spec §11 の X_MCP_AUTH 値と一致させる。
const (
	// ModeNone は認証を行わない passthrough モード。ローカル開発専用。
	ModeNone Mode = "none"
	// ModeAPIKey は Bearer token を共有シークレットと定数時間比較するモード。
	// CI / Routine からの呼び出しを想定する。M19 で実装済み。
	ModeAPIKey Mode = "apikey"
	// ModeIDProxy は OIDC ベースの session 認証モード。本番想定。M20 で memory store
	// 実装済み。sqlite / redis / dynamodb 用の Store は M21–M23 で追加予定。
	// 現状は idproxy.Config.OAuth = nil 固定でブラウザ cookie session 認証のみを提供し、
	// OAuth 2.1 AS / Bearer JWT 検証は別マイルストーンで対応する。
	ModeIDProxy Mode = "idproxy"
)

// ErrUnsupportedMode は [New] がサポート外モードを受け取った際に返すエラーである。
// 空文字 "" もこのエラーで弾く方針とし、defaulting は呼び出し側 CLI 層 (M24) の
// 責務とする。errors.Is で判定すること。
var ErrUnsupportedMode = errors.New("authgate: unsupported mode")

// Middleware は MCP handler を任意のロジックでラップする責務を表すインターフェースである。
//
// Wrap は next を受け取り、認証チェック等を行ったうえで next を呼び出す
// http.Handler を返す。実装は ServeHTTP 内で 401 / 403 を返すか、next.ServeHTTP に
// 処理を委譲するかを選択する。
//
// シグネチャは idproxy.Wrap (`func(http.Handler) http.Handler` 相当) と整合させ、
// 後続 M20 で idproxy 実装をそのまま差し込めるようにする。
type Middleware interface {
	// Wrap は next を装飾した http.Handler を返す。next == nil の場合の挙動は
	// 呼び出し側 (transport/http) で next != nil を保証する責務とし、本 interface
	// の実装では nil チェックを要求しない。
	Wrap(next http.Handler) http.Handler
}

// options は [New] に渡される設定値を集約する内部構造体。
// 各モード固有の設定は [Option] 経由で注入し、authgate パッケージは
// 環境変数を一切直接読まない (env 読み込みは CLI 層 M24 の責務)。
type options struct {
	// apiKey は [ModeAPIKey] 用の共有シークレット。
	apiKey string

	// idproxy 用フィールド群。すべて [ModeIDProxy] 専用。
	oidcIssuer       string
	oidcClientID     string
	oidcClientSecret string
	cookieSecret     string
	externalURL      string
	idproxyStore     idproxy.Store
	idproxyPrefix    string
}

// Option は [New] の挙動を変更する関数型オプション。
// モードに応じて必要な値を [Option] 経由で注入する。
type Option func(*options)

// WithAPIKey は [ModeAPIKey] 用に共有シークレット (Bearer 比較対象) を設定する。
// 空文字は [ErrAPIKeyMissing] として [New] から返される。
func WithAPIKey(key string) Option {
	return func(o *options) {
		o.apiKey = key
	}
}

// WithOIDCIssuer は [ModeIDProxy] 用に OIDC Issuer URL を設定する。
// カンマ区切りで複数の Issuer を指定可能 (spec §11 `OIDC_ISSUER` 準拠)。
// 各エントリは前後空白を trim される。複数指定する場合、Issuer / ClientID /
// ClientSecret は同インデックス位置でペアを成し、CSV エントリ数は完全一致が必須となる。
func WithOIDCIssuer(issuer string) Option {
	return func(o *options) {
		o.oidcIssuer = issuer
	}
}

// WithOIDCClientID は [ModeIDProxy] 用に OIDC Client ID を設定する。
// カンマ区切りで複数指定可能 (詳細は [WithOIDCIssuer] 参照)。
func WithOIDCClientID(clientID string) Option {
	return func(o *options) {
		o.oidcClientID = clientID
	}
}

// WithOIDCClientSecret は [ModeIDProxy] 用に OIDC Client Secret を設定する。
// カンマ区切りで複数指定可能 (詳細は [WithOIDCIssuer] 参照)。
// idproxy.Config.Validate により provider ごとに必須項目となるため、空文字エントリは
// [ErrIDProxyConfigInvalid] で reject される。
func WithOIDCClientSecret(clientSecret string) Option {
	return func(o *options) {
		o.oidcClientSecret = clientSecret
	}
}

// WithCookieSecret は [ModeIDProxy] 用に Cookie 暗号化シークレットを hex エンコード
// 文字列として設定する。decode 後 32 バイト以上である必要があり、満たさない場合
// [ErrIDProxyConfigInvalid] を返す (spec §11 `COOKIE_SECRET` 準拠)。
func WithCookieSecret(hexEncoded string) Option {
	return func(o *options) {
		o.cookieSecret = hexEncoded
	}
}

// WithExternalURL は [ModeIDProxy] 用に外部公開 URL を設定する。
// OAuth コールバック URL やメタデータの issuer として使用される。`https://` 始まりまたは
// `http://localhost` 系のいずれかを要求する (idproxy 側のバリデーション準拠)。
func WithExternalURL(externalURL string) Option {
	return func(o *options) {
		o.externalURL = externalURL
	}
}

// WithIDProxyStore は [ModeIDProxy] 用に永続化バックエンドを明示的に指定する。
// 未指定の場合、[NewMemoryStore] のデフォルト memory store が使用される。
// M21–M23 で追加される sqlite / redis / dynamodb 用ヘルパもこの Option 経由で渡す。
//
// 呼び出し側は不要になった時点で [idproxy.Store.Close] を呼ぶ責務を負う。
func WithIDProxyStore(store idproxy.Store) Option {
	return func(o *options) {
		o.idproxyStore = store
	}
}

// WithIDProxyPathPrefix は [ModeIDProxy] のパスプレフィックスを設定する。
// 例: `/auth` を指定すると `/auth/login`, `/auth/callback`, `/auth/.well-known/*` 等の
// パスで応答する。未指定の場合 (空文字) は idproxy のデフォルト (ルート直下) となる。
func WithIDProxyPathPrefix(prefix string) Option {
	return func(o *options) {
		o.idproxyPrefix = prefix
	}
}

// New は指定された [Mode] に対応する [Middleware] を返す。
//
// M20 までで 3 モード ([ModeNone] / [ModeAPIKey] / [ModeIDProxy]) を全実装済み。
// その他の値 (空文字を含む) は [ErrUnsupportedMode] でラップされたエラーを返す。
//
//   - [ModeAPIKey] では [WithAPIKey] による共有シークレット指定が必須であり、
//     未指定 / 空文字の場合 [ErrAPIKeyMissing] を返す。
//   - [ModeIDProxy] では [WithOIDCIssuer] / [WithOIDCClientID] /
//     [WithOIDCClientSecret] / [WithCookieSecret] / [WithExternalURL] の全 5 つが
//     必須となる。Store 未指定時はデフォルトの memory store が使用される。
//     設定不備や CSV 件数不一致は [ErrIDProxyConfigInvalid] でラップされる。
func New(mode Mode, opts ...Option) (Middleware, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	switch mode {
	case ModeNone:
		return &None{}, nil
	case ModeAPIKey:
		mw, err := NewAPIKey(o.apiKey)
		if err != nil {
			// 型付き nil をそのまま返すと interface としては non-nil になるため、
			// 明示的に nil Middleware を返す。
			return nil, err
		}
		return mw, nil
	case ModeIDProxy:
		mw, err := newIDProxy(context.Background(), o)
		if err != nil {
			return nil, err
		}
		return mw, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedMode, string(mode))
	}
}
