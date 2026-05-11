// Package authgate は MCP サーバーの着信認証 middleware を提供する。
//
// スペック §11 に定義される 3 モード (none / apikey / idproxy) を切り替え可能な
// Middleware インターフェースを公開する:
//
//   - none:    認証を行わない passthrough。ローカル開発専用。
//   - apikey:  Bearer token を共有シークレットと定数時間比較する。CI / Routine
//     からの呼び出しを想定する。
//   - idproxy: OIDC ベースの session 認証。本番想定。memory / sqlite / redis /
//     dynamodb の 4 store backend を全てサポートする (M23 で全実装完了)。
//
// 本パッケージは [Middleware] interface と [New] ファクトリを公開し、モード値は
// [Mode] 型の定数 ([ModeNone] / [ModeAPIKey] / [ModeIDProxy]) として表現する。
//
// M23 までで 3 モード + 4 store backend を全実装済み。[ModeIDProxy] については
// M20 で memory store ([NewMemoryStore])、M21 で sqlite store ([NewSQLiteStore])、
// M22 で redis store ([NewRedisStore])、M23 で dynamodb store
// ([NewDynamoDBStore]) を順次実装した。sqlite store は POSIX 環境において起動時に
// メイン DB ファイルおよび WAL サイドカー (`-wal` / `-shm`) を 0600 へ chmod する
// (best-effort、Windows では静かに無視)。redis store は spec §11 の `REDIS_URL`
// (例: `redis://localhost:6379/0`) を [github.com/redis/go-redis/v9.ParseURL] で
// 解析し、TLS は `rediss://` スキームで自動有効化する。dynamodb store は spec §11 の
// `DYNAMODB_TABLE_NAME` / `AWS_REGION` を引数として受け取り、AWS SDK v2 の credential
// 解決は lazy (最初の API call 時) のためコンストラクタは AWS account / credential
// 無しでも成功する。session / authcode の Get は ConsistentRead=true 固定で、TTL は
// DynamoDB ネイティブ TTL + アプリ側二重チェックで保護する (上流実装)。また、
// 現状の idproxy 統合は `idproxy.Config.OAuth = nil` 固定で **ブラウザ cookie session
// 認証のみ** を提供し、OAuth 2.1 AS / Bearer JWT 検証は未対応である (機械実行は
// [ModeAPIKey] を利用すること)。サポート外のモードに対しては [ErrUnsupportedMode]
// でラップされたエラーを返す。空文字 "" もこのエラーで弾く方針とし、defaulting
// は呼び出し側 CLI 層 (M24) の責務とする。
//
// 各モード固有の設定 (apikey の共有シークレット、idproxy の OIDC 設定等) は [Option]
// 経由で注入し、本パッケージは環境変数を一切直接読まない。env 読み込みは CLI 層 M24
// の責務とする。
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
//
//	// idproxy モード (memory store デフォルト, ブラウザ cookie session)
//	mw, err := authgate.New(authgate.ModeIDProxy,
//	    authgate.WithOIDCIssuer(os.Getenv("OIDC_ISSUER")),
//	    authgate.WithOIDCClientID(os.Getenv("OIDC_CLIENT_ID")),
//	    authgate.WithOIDCClientSecret(os.Getenv("OIDC_CLIENT_SECRET")),
//	    authgate.WithCookieSecret(os.Getenv("COOKIE_SECRET")), // hex 32B+
//	    authgate.WithExternalURL(os.Getenv("EXTERNAL_URL")),
//	)
//	if err != nil { /* ErrIDProxyConfigInvalid 等を処理 */ }
//	srv := http.NewServer(mcp, http.WithHandlerMiddleware(mw.Wrap))
//
//	// idproxy モード (sqlite store, 単一ノード永続化)
//	store, err := authgate.NewSQLiteStore(os.Getenv("SQLITE_PATH"))
//	if err != nil { /* ErrSQLitePathRequired 等を処理 */ }
//	defer store.Close()
//	mw, err = authgate.New(authgate.ModeIDProxy,
//	    authgate.WithOIDCIssuer(os.Getenv("OIDC_ISSUER")),
//	    authgate.WithOIDCClientID(os.Getenv("OIDC_CLIENT_ID")),
//	    authgate.WithOIDCClientSecret(os.Getenv("OIDC_CLIENT_SECRET")),
//	    authgate.WithCookieSecret(os.Getenv("COOKIE_SECRET")),
//	    authgate.WithExternalURL(os.Getenv("EXTERNAL_URL")),
//	    authgate.WithIDProxyStore(store),
//	)
//
//	// idproxy モード (redis store, 軽量サーバー / 複数インスタンス向け)
//	store, err := authgate.NewRedisStore(os.Getenv("REDIS_URL"))
//	if err != nil { /* ErrRedisURLRequired 等を処理 */ }
//	defer store.Close()
//	mw, err = authgate.New(authgate.ModeIDProxy,
//	    authgate.WithOIDCIssuer(os.Getenv("OIDC_ISSUER")),
//	    authgate.WithOIDCClientID(os.Getenv("OIDC_CLIENT_ID")),
//	    authgate.WithOIDCClientSecret(os.Getenv("OIDC_CLIENT_SECRET")),
//	    authgate.WithCookieSecret(os.Getenv("COOKIE_SECRET")),
//	    authgate.WithExternalURL(os.Getenv("EXTERNAL_URL")),
//	    authgate.WithIDProxyStore(store),
//	)
//
//	// idproxy モード (dynamodb store, Lambda マルチコンテナ向け)
//	store, err := authgate.NewDynamoDBStore(
//	    os.Getenv("DYNAMODB_TABLE_NAME"),
//	    os.Getenv("AWS_REGION"),
//	)
//	if err != nil { /* ErrDynamoDBTableRequired / ErrDynamoDBRegionRequired 等を処理 */ }
//	defer store.Close()
//	mw, err = authgate.New(authgate.ModeIDProxy,
//	    authgate.WithOIDCIssuer(os.Getenv("OIDC_ISSUER")),
//	    authgate.WithOIDCClientID(os.Getenv("OIDC_CLIENT_ID")),
//	    authgate.WithOIDCClientSecret(os.Getenv("OIDC_CLIENT_SECRET")),
//	    authgate.WithCookieSecret(os.Getenv("COOKIE_SECRET")),
//	    authgate.WithExternalURL(os.Getenv("EXTERNAL_URL")),
//	    authgate.WithIDProxyStore(store),
//	)
package authgate
