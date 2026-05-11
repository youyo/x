// Package authgate は MCP サーバーの着信認証 middleware を提供する。
//
// スペック §11 に定義される 3 モード (none / apikey / idproxy) を切り替え可能な
// Middleware インターフェースを公開する:
//
//   - none:    認証を行わない passthrough。ローカル開発専用。
//   - apikey:  Bearer token を共有シークレットと定数時間比較する。CI / Routine
//     からの呼び出しを想定する。
//   - idproxy: OIDC ベースの session 認証。本番想定。memory / sqlite / redis /
//     dynamodb の 4 store backend をサポートする。
//
// 本パッケージは [Middleware] interface と [New] ファクトリを公開し、モード値は
// [Mode] 型の定数 ([ModeNone] / [ModeAPIKey] / [ModeIDProxy]) として表現する。
//
// M19 までで [ModeNone] / [ModeAPIKey] を実装済み。[ModeIDProxy] は後続マイル
// ストーン (M20) で追加する。サポート外のモードに対しては [ErrUnsupportedMode]
// でラップされたエラーを返す。空文字 "" もこのエラーで弾く方針とし、defaulting
// は呼び出し側 CLI 層 (M24) の責務とする。
//
// 各モード固有の設定 (apikey の共有シークレット等) は [Option] 経由で注入し、
// 本パッケージは環境変数を一切直接読まない。env 読み込みは CLI 層 M24 の責務
// とする。
//
// transport/http パッケージとは循環依存を避けるため、Middleware の差し込みは
// `func(http.Handler) http.Handler` 型を介する。呼び出し側は次のように接続する:
//
//	// none モード (ローカル開発)
//	mw, _ := authgate.New(authgate.ModeNone)
//	srv := http.NewServer(mcp, http.WithHandlerMiddleware(mw.Wrap))
//
//	// apikey モード (CI / Routine 用)
//	apiKey := os.Getenv("X_MCP_API_KEY") // 読み込みは CLI 層の責務
//	mw, err := authgate.New(authgate.ModeAPIKey, authgate.WithAPIKey(apiKey))
//	if err != nil { /* ErrAPIKeyMissing 等を処理 */ }
//	srv := http.NewServer(mcp, http.WithHandlerMiddleware(mw.Wrap))
package authgate
