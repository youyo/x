# Changelog

このプロジェクトの変更履歴を記録する。フォーマットは [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に準拠し、バージョニングは [Semantic Versioning](https://semver.org/lang/ja/) に従う。

## [Unreleased]

## [0.3.0] - 2026-05-12

AWS Lambda Function URL + Lambda Web Adapter での Remote MCP デプロイサンプルと、Claude Code Routines 連携のための文書セットを追加。本バージョンで「CLI → MCP → 公開配布」の 3 フェーズ計画 (28 マイルストーン) が完了する。Go コードの変更はなく、`examples/` と `docs/` への純粋な追加のみ。

### Added

#### `examples/lambroll/` — AWS Lambda デプロイサンプル
- `function.json` (`provided.al2023`, arm64, Lambda Web Adapter Layer, SSM SecureString 注入) (M26)
- `function_url.json` (`AuthType=NONE`、認証は idproxy 側に集約) (M26)
- `bootstrap` シェル (`exec ./x mcp --host 0.0.0.0 --port "${PORT:-8080}"`) (M26)
- `.env.example` (lambroll が参照する env と SSM 経由 env の全リファレンス) (M26)
- `README.md` (Step 1-6 デプロイ手順 + Mermaid 図 + IAM/DynamoDB/SSM/OIDC セットアップ + トラブルシュート FAQ + コスト見積もり + クリーンアップ) (M27)

#### `docs/` — 開発者向け補足ドキュメント
- `docs/x-api.md` — X API v2 OAuth 1.0a 認証手順 / エンドポイント / Rate Limit / 課金 (Owned Reads `$0.001`/Tweet) / エラーレスポンスを集約 (M28)
- `docs/routine-prompt.md` — Claude Code Routines に貼り付ける推奨プロンプト雛形と Backlog (HEP_ISSUES) 課題テンプレ + Mermaid シーケンス図 (M28)

### Compatibility
- v0.1.0 / v0.2.0 の CLI / MCP 機能は完全後方互換
- 本バージョンは **追加のみ**、CLI / MCP の挙動変更なし
- Go コード変更なし (`examples/` と `docs/` の純粋な追加)

## [0.2.0] - 2026-05-12

Remote MCP サーバーをリリース。Claude Code Routines や他の MCP クライアントから X (旧 Twitter) API v2 の Liked Posts を読み出せる Streamable HTTP サーバーが `x mcp` サブコマンドとして提供される。Lambda Web Adapter での AWS Lambda デプロイ想定 (実デプロイサンプルは v0.3.0)。

### Added

#### MCP サブコマンド
- `x mcp` — Streamable HTTP MCP サーバーを起動 (M24):
  - `--host` (default `127.0.0.1`, env: `X_MCP_HOST`)
  - `--port` (default `8080`, env: `X_MCP_PORT`)
  - `--path` (default `/mcp`, env: `X_MCP_PATH`)
  - `--auth idproxy|apikey|none` (default `idproxy`, env: `X_MCP_AUTH`)
  - `--apikey-env <name>` (default `X_MCP_API_KEY`) — apikey モード時に shared secret を保持する env 変数名
  - SIGINT/SIGTERM 受信で graceful shutdown
  - MCP モードは credentials.toml を一切読まず、シークレットは環境変数のみから取得 (spec §11 不変条件)

#### MCP Tools (mark3labs/mcp-go Streamable HTTP)
- `get_user_me` — 自分の `user_id` / `username` / `name` を返す (M17)
- `get_liked_tweets` — Liked Posts を取得 (M18):
  - 入力: `user_id` / `start_time` / `end_time` / `since_jst` / `yesterday_jst` / `max_results` / `all` / `max_pages` / `tweet_fields` / `expansions` / `user_fields`
  - `all=true` で next_token を自動辿り、rate-limit aware に集約結果を一括返却
  - `yesterday_jst > since_jst > start/end_time` の優先順位を CLI と統一

#### 認証 (authgate)
- `none` モード — 認証なし (ローカル開発専用, M16)
- `apikey` モード — Bearer token、constant-time 比較 (M19)
- `idproxy` モード — OIDC + cookie session (M20-M23)
- idproxy ストアバックエンド 4 種 (`STORE_BACKEND` で切替):
  - `memory` (default, テスト/一時用途, M20)
  - `sqlite` (`SQLITE_PATH`, `modernc.org/sqlite` pure Go, ローカル開発, M21)
  - `redis` (`REDIS_URL`, `go-redis/v9`, 軽量サーバー, M22)
  - `dynamodb` (`DYNAMODB_TABLE_NAME` / `AWS_REGION`, `aws-sdk-go-v2`, Lambda マルチコンテナ, M23)

#### Transport
- Streamable HTTP サーバー (`internal/transport/http`, M15)
- `GET /healthz` — LWA / Lambda 死活確認用 (`200 ok\n`, 認証 middleware バイパス, M16)
- graceful shutdown (ListenAndServe 並行起動 → ctx 終了で `Shutdown` 呼び出し)

### 環境変数 (新規)

| 名前 | 用途 |
|---|---|
| `X_MCP_HOST` | MCP bind host |
| `X_MCP_PORT` | MCP bind port |
| `X_MCP_PATH` | MCP エンドポイント prefix |
| `X_MCP_AUTH` | 認証モード (`idproxy` / `apikey` / `none`) |
| `X_MCP_API_KEY` | apikey モードの shared secret 値 |
| `OIDC_ISSUER` | idproxy OIDC Issuer (カンマ区切りで複数可) |
| `OIDC_CLIENT_ID` | idproxy OIDC Client ID (カンマ区切り) |
| `OIDC_CLIENT_SECRET` | idproxy OIDC Client Secret |
| `COOKIE_SECRET` | idproxy セッション暗号 (hex 32B+) |
| `EXTERNAL_URL` | idproxy 外部 URL |
| `STORE_BACKEND` | idproxy ストア (`memory` / `sqlite` / `redis` / `dynamodb`) |
| `SQLITE_PATH` | sqlite DB ファイルパス |
| `REDIS_URL` | Redis 接続 URL |
| `DYNAMODB_TABLE_NAME` | DynamoDB テーブル名 |
| `AWS_REGION` | AWS リージョン (Lambda / dynamodb 時) |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` (default `info`) |

### Security
- MCP モードはシークレットを環境変数のみから読み込み (Lambda 不変性前提、`credentials.toml` を一切読まない)
- apikey モードの shared secret は `subtle.ConstantTimeCompare` で比較
- `/healthz` は middleware バイパスだが、ペイロードは固定文字列のみ (情報漏洩なし)

### Compatibility
- v0.1.0 の CLI 機能は完全後方互換 (`x version` / `x me` / `x liked list` / `x configure` / `x completion`)
- 新規追加された `x mcp` サブコマンドは独立しており既存ユーザーへの影響なし

## [0.1.0] - 2026-05-12

初回リリース。X (旧 Twitter) API v2 の Liked Posts をローカル CLI から取得できる v0.1.0 を公開する。MCP サーバー / Lambda 配布は v0.2.0 以降で対応予定。

### Added

#### CLI サブコマンド
- `x version` および `x --version` — ビルド情報 (Version / Commit / Date) を JSON / human で出力 (M1)
- `x me [--no-json]` — 自分の `user_id` / `username` を取得 (M9)
- `x liked list` — Liked Posts を取得 (M10 / M11):
  - `--user-id` / `--start-time` / `--end-time` / `--max-results` / `--pagination-token`
  - `--since-jst <YYYY-MM-DD>` / `--yesterday-jst` — JST 日付ヘルパ
  - `--all` + `--max-pages` (default 50) — rate-limit aware な next_token 自動ページング
  - `--ndjson` — NDJSON ストリーミング出力 (`--all` 時は逐次 flush)
  - `--tweet-fields` / `--expansions` / `--user-fields` — フィールドカスタマイズ
  - `--no-json` — 1 行 / ツイートの human 出力
- `x configure` 対話モード (M12):
  - `${XDG_CONFIG_HOME:-~/.config}/x/config.toml` (非機密設定, perm 0644)
  - `${XDG_DATA_HOME:-~/.local/share}/x/credentials.toml` (X API トークン, perm 0600)
  - 既存 credentials.toml は上書き確認 (`[y/N]`)
  - TTY echo オフ + 非 TTY フォールバック (`golang.org/x/term`)
- `x configure --print-paths` — 設定ファイル / データファイルパスを表示 (M12)
- `x configure --check` — credentials.toml のパーミッション・必須キーを検証 (M12)
- `x completion {bash,zsh,fish,powershell}` — Cobra 標準補完スクリプト生成 (M1)

#### 基盤
- XDG Base Directory Specification 準拠の設定ロード (`internal/config`, M3 / M4)
- OAuth 1.0a 静的トークンによる X API v2 アクセス (`internal/xapi`, M5)
- リトライ付き HTTP クライアント (exponential backoff: base 2s, factor 2, max 30s, max 3 retry, M6)
- Rate-limit aware ページネーション (`x-rate-limit-remaining` / `x-rate-limit-reset` 追従, 暴走防止 `--max-pages`, M8)
- `xapi.GetUserMe` / `xapi.ListLikedTweets` / `xapi.EachLikedPage` (M7 / M8)
- exit code 規約: 0=success / 1=generic / 2=argument / 3=auth / 4=permission / 5=not-found

#### 配布
- `go install github.com/youyo/x/cmd/x@latest`
- Docker イメージ `ghcr.io/youyo/x` (distroless/static-debian12:nonroot, multi-arch)
- GitHub Releases tar.gz バイナリ (darwin/linux × amd64/arm64)
- Homebrew tap (`youyo/homebrew-tap`) — v0.1.0 リリース以降に利用可能

### Security
- 非機密設定 (`config.toml`) とシークレット (`credentials.toml`, perm 0600) をファイルパスレベルで分離 (XDG_CONFIG_HOME / XDG_DATA_HOME)
- `config.toml` にシークレットキー (`api_key` / `access_token` 等) が含まれている場合は読み込みを拒否 (`ErrSecretInConfig`)
- 起動時に credentials.toml のパーミッションを検査し `0600` 以外は警告 (POSIX のみ)
- credentials.toml の書き換えは tmp + rename による原子置換 (`internal/config/credentials.go`)

[Unreleased]: https://github.com/youyo/x/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/youyo/x/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/youyo/x/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/youyo/x/releases/tag/v0.1.0
