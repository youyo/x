package authgate

import (
	"errors"
	"fmt"
	"net/http"
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
	// ModeIDProxy は OIDC ベースの session 認証モード。本番想定。M20 で実装する。
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
	apiKey string
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

// New は指定された [Mode] に対応する [Middleware] を返す。
//
// M19 までで [ModeNone] / [ModeAPIKey] を実装済み。[ModeIDProxy] および
// その他の値 (空文字を含む) は [ErrUnsupportedMode] でラップされたエラーを返す。
// [ModeAPIKey] では [WithAPIKey] による共有シークレット指定が必須であり、
// 未指定 / 空文字の場合 [ErrAPIKeyMissing] を返す。
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
		return nil, fmt.Errorf("%w: %q (not yet implemented)", ErrUnsupportedMode, string(mode))
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedMode, string(mode))
	}
}
