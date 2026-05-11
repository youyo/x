# M25: v0.2.0 README 追記 + CHANGELOG + タグ準備 詳細実装計画

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M25 (Phase G: v0.2.0 リリース) |
| スペック | `docs/specs/x-spec.md` §13 / §14 |
| 前提 | M1〜M24 完了 (01f2a97) — v0.2.0 機能セット凍結 |
| ステータス | 計画中 → 実装へ |
| 作成日 | 2026-05-12 |

## 背景

M24 までで v0.2.0 の機能セットが完成した:

- `x mcp` サブコマンド (3 モード認証: none / apikey / idproxy)
- MCP tools `get_user_me` / `get_liked_tweets` (Streamable HTTP)
- idproxy 4 store backend (memory / sqlite / redis / dynamodb) 全サポート
- SIGINT/SIGTERM での graceful shutdown
- `/healthz` エンドポイント (LWA 健全性確認用、認証バイパス)

M25 はこれらをユーザー向けに公表するためのドキュメンテーション層 (README / CHANGELOG) の整備と、タグ push 手順の書面化が責務。**v0.2.0 タグの実 push は本マイルストーンでは行わない** (M14 と同方針、リモート未設定/CI 起動しない環境のため)。

## Scope

### In scope
- `CHANGELOG.md` に `## [0.2.0] - 2026-05-12` セクションを追加
- `README.md` (英語) に MCP セクション (Quick Start / Tools / Configuration / Status 表更新)
- `README.ja.md` (日本語) に MCP セクション (Quick Start / Tools / Configuration / Status 表更新)
- タグ push 手順の書面化 (CHANGELOG または README に「Release procedure」として記載 — 実施は将来)

### Out of scope
- `v0.2.0` git tag の実作成・push (将来手動)
- GitHub Releases の手動編集 (タグ push 後に GoReleaser が自動生成する)
- Homebrew tap PR 確認 (release.yml の責務)
- `examples/lambroll/` 一式 (M26 以降)
- `docs/routine-prompt.md` / `docs/x-api.md` (M28)

## v0.2.0 機能セット (M24 完了時点で確定)

ユーザー向けに README/CHANGELOG で公表する MCP 機能:

| 機能 | 状態 | マイルストーン |
|---|---|---|
| `x mcp` サブコマンド (`--host` / `--port` / `--path` / `--auth` / `--apikey-env`) | 完了 | M24 |
| MCP tool `get_user_me` | 完了 | M17 |
| MCP tool `get_liked_tweets` (全パラメータ + ページング + JST 優先順位) | 完了 | M18 |
| authgate `none` モード | 完了 | M16 |
| authgate `apikey` モード (Bearer token, constant-time 比較) | 完了 | M19 |
| authgate `idproxy` モード | 完了 | M20 |
| idproxy memory store | 完了 | M20 |
| idproxy sqlite store (modernc.org/sqlite, pure Go) | 完了 | M21 |
| idproxy redis store (go-redis/v9) | 完了 | M22 |
| idproxy dynamodb store (aws-sdk-go-v2, Lambda 想定) | 完了 | M23 |
| SIGINT/SIGTERM graceful shutdown | 完了 | M15 / M24 |
| `/healthz` エンドポイント (認証バイパス) | 完了 | M16 |

主要追加依存 (v0.1.0 から):

- `github.com/mark3labs/mcp-go` v0.49+
- `github.com/youyo/idproxy` v0.4.2+
- `modernc.org/sqlite` (sqlite backend, pure Go)
- `github.com/redis/go-redis/v9` (redis backend)
- `github.com/aws/aws-sdk-go-v2/service/dynamodb` (dynamodb backend)

## ファイル別設計

### 1. `CHANGELOG.md`

#### 変更点
- `## [Unreleased]` セクションは残す (空にする) — 0.2.0 セクションをその下に追加
- `## [0.2.0] - 2026-05-12` を新規追加
- `[Unreleased]` リンクを `v0.2.0...HEAD` に更新
- `[0.2.0]` リンク (`v0.1.0...v0.2.0` の compare URL) を末尾に追加
- `[0.1.0]` の既存セクション・リンクは変更しない

#### `## [0.2.0]` セクションの内容

```markdown
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
  - `sqlite` (`SQLITE_PATH`, modernc.org/sqlite pure Go, ローカル開発, M21)
  - `redis` (`REDIS_URL`, go-redis/v9, 軽量サーバー, M22)
  - `dynamodb` (`DYNAMODB_TABLE_NAME` / `AWS_REGION`, aws-sdk-go-v2, Lambda マルチコンテナ, M23)

#### Transport
- Streamable HTTP サーバー (`internal/transport/http`, M15)
- `GET /healthz` — LWA / Lambda 死活確認用 (`200 ok\n`, 認証 middleware バイパス, M16)
- graceful shutdown (ListenAndServe 並行起動 → ctx 終了で `Shutdown` 呼び出し)

### Environment variables (新規)

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

### Security
- MCP モードはシークレットを環境変数のみから読み込み (Lambda 不変性前提、`credentials.toml` を一切読まない)
- apikey モードの shared secret は `subtle.ConstantTimeCompare` で比較
- `/healthz` は middleware バイパスだが、ペイロードは固定文字列のみ (情報漏洩なし)

### Compatibility
- v0.1.0 の CLI 機能は完全後方互換 (`x version` / `x me` / `x liked list` / `x configure` / `x completion`)
- 新規追加された `x mcp` サブコマンドは独立しており既存ユーザーへの影響なし

[Unreleased]: https://github.com/youyo/x/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/youyo/x/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/youyo/x/releases/tag/v0.1.0
```

### 2. `README.md` (英語)

#### Status 表の更新

既存:

```
| `v0.1.0` (this release) | CLI: x version / x me / x liked list / x configure / x completion |
| `v0.2.0` (planned)      | Remote MCP server ... |
| `v0.3.0` (planned)      | examples/lambroll ... |
```

→

```
| `v0.1.0` | CLI: x version / x me / x liked list / x configure / x completion |
| `v0.2.0` (this release) | Remote MCP server (x mcp --auth idproxy|apikey|none) with get_user_me and get_liked_tweets tools |
| `v0.3.0` (planned) | examples/lambroll/ + Claude Code Routines prompt template |
```

冒頭の "Today, only the CLI is shipped (`v0.1.0`); the Remote MCP server and AWS Lambda distribution are scheduled for later releases." の段落も "The Remote MCP server is now available in `v0.2.0`; the AWS Lambda deployment sample is scheduled for `v0.3.0`." に書き換える (README.md L12 / README.ja.md L12 を**両方**確実に置換、advisor フィードバック #1)。

#### Features セクションに追記

`x configure` の下に追加:

- **`x mcp`** — Start a Streamable HTTP MCP server (Claude Code Routines / MCP client connectivity)
  - Three auth modes: `none` (local dev only), `apikey` (Bearer token), `idproxy` (OIDC + cookie session)
  - Four idproxy store backends: `memory` / `sqlite` / `redis` / `dynamodb`
  - MCP tools: `get_user_me`, `get_liked_tweets`
  - `GET /healthz` for Lambda Web Adapter / k8s liveness probes
  - Graceful shutdown on SIGINT/SIGTERM

#### "Quick Start (MCP server)" セクションを新規追加

Quick Start の末尾 (Configuration の直前) に挿入:

```markdown
## Quick Start (MCP server)

The `x mcp` subcommand starts a [Streamable HTTP](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports) MCP server. Three auth modes are available:

### Local development (`--auth none`)

```bash
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
  x mcp --auth none --host 127.0.0.1 --port 8080
```

Endpoints:

- `POST /mcp` — MCP Streamable HTTP (no auth)
- `GET /healthz` — liveness probe (always returns `200 ok`)

### Shared API Key (`--auth apikey`)

```bash
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
X_MCP_API_KEY=$(openssl rand -hex 32) \
  x mcp --auth apikey --host 0.0.0.0 --port 8080
```

Clients must send `Authorization: Bearer ${X_MCP_API_KEY}`. The comparison is constant-time (`subtle.ConstantTimeCompare`).

### OIDC + cookie session (`--auth idproxy`, default)

Uses [`github.com/youyo/idproxy`](https://github.com/youyo/idproxy). Choose a persistent store backend via `STORE_BACKEND`:

```bash
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
OIDC_ISSUER=https://accounts.google.com,https://login.microsoftonline.com/<tenant>/v2.0 \
OIDC_CLIENT_ID=<google-client-id>,<entra-client-id> \
OIDC_CLIENT_SECRET=<google-client-secret> \
COOKIE_SECRET=$(openssl rand -hex 32) \
EXTERNAL_URL=https://x-mcp.example.com \
STORE_BACKEND=dynamodb \
DYNAMODB_TABLE_NAME=x-mcp-idproxy \
AWS_REGION=ap-northeast-1 \
  x mcp --auth idproxy --host 0.0.0.0 --port 8080
```

Store backends:

| `STORE_BACKEND` | Required env vars | Use case |
|---|---|---|
| `memory` (default) | — | unit tests, ephemeral local dev |
| `sqlite` | `SQLITE_PATH` (default `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db`) | single-process local dev |
| `redis` | `REDIS_URL` | lightweight servers, native TTL |
| `dynamodb` | `DYNAMODB_TABLE_NAME`, `AWS_REGION` | Lambda multi-container, ConsistentRead |
```

#### "Available MCP tools" セクション (Quick Start の中の小節として)

```markdown
### Available MCP tools

| Tool | Description |
|---|---|
| `get_user_me` | Returns `{ user_id, username, name }` for the OAuth 1.0a user. |
| `get_liked_tweets` | Returns Liked posts with full pagination (`all=true`, `max_pages`, rate-limit aware). Accepts `user_id`, `start_time`/`end_time`, `since_jst`, `yesterday_jst`, `max_results`, `tweet_fields`, `expansions`, `user_fields`. |
```

#### Configuration セクションに MCP 用 env 表を追記

既存の "Environment variables" 表の下に、新規見出し "MCP server environment variables (v0.2.0+)" を作って先述の表を入れる (CHANGELOG と同内容)。Load priority の節も以下を追記:

```markdown
MCP mode (`x mcp`):

1. CLI flag (`--host` / `--port` / `--path` / `--auth` / `--apikey-env`)
2. Environment variable (including secrets)
3. Built-in default

**MCP mode never reads `config.toml` or `credentials.toml`** — all secrets must come from environment variables. This makes Lambda / container deployments deterministic.
```

#### Homebrew 注釈の整合 (advisor #2)

README.md L43 / README.ja.md L43:

```
### Homebrew (planned, available after `v0.1.0` is published)
```

v0.2.0 リリース後の論理状態に合わせ `(planned, available after `v0.1.0` is published)` 部分を削除する (見出しを `### Homebrew` のみに)。本文の `brew install youyo/tap/x` は変えない。日本語版の `(`v0.1.0` リリース後に利用可能予定)` も同様に削除。

#### Roadmap セクションの更新 (advisor #3)

`v0.2.0` の項目自体を **Roadmap セクションから削除**する (CHANGELOG にリリース履歴があるため二重記載は冗長)。Roadmap には未来リリースのみを残す: `v0.3.0` (lambroll examples + routine prompt template) の項目のみを残し、Roadmap 全体が `v0.3.0` 1 件になる。

### 3. `README.ja.md` (日本語)

`README.md` と完全に対称な変更を反映する。すべての見出しを日本語に統一しつつ、技術用語 (Streamable HTTP / OIDC / idproxy 等) はそのまま英語表記を維持する。コードブロック・env 表は日英共通の値なのでそのまま使う。

主な対応:

- Status 表 → 「ステータス」表に `v0.2.0` を本リリースに昇格
- 「機能」セクションに `x mcp` を追加
- 「クイックスタート (MCP サーバー)」セクションを新規追加 (auth=none / apikey / idproxy の 3 例)
- 「利用可能な MCP tools」表
- 「設定」セクションに MCP 用環境変数表を追加
- 「ロードマップ」の `v0.2.0` をリリース済みに更新

### 4. タグ push 手順の書面化

`README.md` の Development セクションに追記する。CHANGELOG はリリースノート専用に保ち、運用手順は README に集約する方針 (advisor #4: 入れ子コードフェンス問題を踏まえ、Markdown はファイル単体で正しくレンダリングされるよう **コードブロック 1 つ** にコメント (`#`) で見出し・補足を入れ込む形式とする。プラン内で見せた疑似マルチセクション形式はあくまで設計図であり、実ファイルでは bash コードブロック単体 + その下に通常テキストの注釈で構成する)。

実ファイルでの構成 (Markdown ネスト問題を回避):

```
### Release procedure (maintainer)

Releases are tag-driven. Once a `vX.Y.Z` tag is pushed, `.github/workflows/release.yml` runs GoReleaser and publishes Homebrew formula + Docker images automatically.

<single bash code block>
git checkout main && git pull
git status                                # working tree must be clean
grep "^## \[X.Y.Z\]" CHANGELOG.md         # CHANGELOG must have the new section
git tag vX.Y.Z
git push origin vX.Y.Z
</single bash code block>

The release workflow will:

- run goreleaser (darwin/linux × amd64/arm64 tarballs + checksums)
- publish GitHub Releases assets
- update the youyo/homebrew-tap formula
- push Docker images to ghcr.io/youyo/x:X.Y.Z and :latest

For `v0.2.0`, the tag has not been pushed yet from this repository; the procedure above documents the intent.
```

日本語 README にも対応する手順を追記する。

## 実装手順 (Red → Green → Refactor のうち Refactor 中心)

Markdown のみの変更のためテストは存在しないが、以下の順で確実に進める:

1. **CHANGELOG.md 編集**
   - `[Unreleased]` 直下に `## [0.2.0] - 2026-05-12` セクションを挿入
   - リンク参照を更新 (`[Unreleased]` / `[0.2.0]` / `[0.1.0]`)
   - markdown lint / 視認チェック

2. **README.md 編集 (英語)**
   - Status 表 更新
   - Features セクションに `x mcp` を追加
   - "Quick Start (MCP server)" セクション挿入
   - "Available MCP tools" 小節
   - Configuration セクションに MCP env 表 + load priority 追記
   - Roadmap で v0.2.0 をリリース済みに更新
   - Development セクションに "Release procedure (maintainer)" 追記

3. **README.ja.md 編集 (日本語)**
   - README.md と完全対称の編集を反映

4. **動作確認**
   - `go test -race -count=1 ./...` (Markdown 変更だが全 pass を確認)
   - `goreleaser check` (yaml 構文確認)
   - 手目検でリンク切れがないかチェック (相対パス・アンカー)

5. **コミット**
   - 1 commit に纏める: `chore(release): v0.2.0 リリース準備 (README MCP セクション + CHANGELOG)`
   - フッターに `Plan: plans/x-m25-release-v020.md`

6. **タグ push は実施しない** (本 M のスコープ外、将来手動)

## 検証

| 項目 | コマンド / 手順 | 期待結果 |
|---|---|---|
| Go テスト | `go test -race -count=1 ./...` | 全 pass (Markdown 変更だが念のため) |
| goreleaser check | `goreleaser check` | エラーなし |
| Markdown 視認 | `cat CHANGELOG.md` / `cat README.md` / `cat README.ja.md` | 構造が崩れていない、コードブロックが閉じている |
| リンク | アンカーリンク (`#configuration` 等) が壊れていない | 目視 |
| 一致性 | 英語版と日本語版で表構造・コードブロックが対称 | 目視 |

## リスク / 注意

1. **タグ push なし方針の徹底**: M14 と同じく実 push しない。CHANGELOG 上は `2026-05-12` 日付で確定して問題ない (リリース戦略の論理的確定日として記録)。
2. **CHANGELOG リンク URL**: `youyo/x` は OSS 公開予定リポ。compare URL は `https://github.com/youyo/x/compare/v0.1.0...v0.2.0` 形式に統一する。
3. **環境変数表の漏れ**: spec §11 と完全一致させる。`X_MCP_*` / `OIDC_*` / `COOKIE_SECRET` / `EXTERNAL_URL` / `STORE_BACKEND` / `SQLITE_PATH` / `REDIS_URL` / `DYNAMODB_TABLE_NAME` / `AWS_REGION` を網羅。
4. **`apikey-env` の説明**: `--apikey-env` は **env 変数名** を指定するもので、値ではない (spec の細部に注意、`X_MCP_API_KEY` は デフォルト名)。
5. **`idproxy` の OIDC_ISSUER カンマ区切り**: 仕様で「カンマ区切りで複数 issuer 可」と書かれているため、Google + Entra の 2 例を併記する (Open Questions で確定済み)。

## 完了条件

- [x] CHANGELOG.md に `## [0.2.0] - 2026-05-12` セクションが追加されている
- [x] README.md に MCP セクション (Quick Start / Tools / Configuration / Status / Roadmap 更新) が反映されている
- [x] README.ja.md に対称の MCP セクションが反映されている
- [x] Release procedure (タグ push 手順) が README / README.ja に書面化されている
- [x] `go test -race -count=1 ./...` 全 pass
- [x] `goreleaser check` エラーなし
- [x] commit が作成され、message に Plan フッター付き
- [x] `v0.2.0` タグ自体は push しない (将来手動)

## ハンドオフ (M26 へ)

- v0.2.0 README/CHANGELOG が確定し、次は examples/lambroll/ の作成 (M26)
- M26 が知るべき情報:
  - bootstrap script: `exec ./x mcp --host 0.0.0.0 --port "${PORT:-8080}"`
  - function.json: provided.al2023 + arm64 + LWA Layer (arm64 v27)
  - .env.example: SSM Parameter Store 参照テンプレ (`{{ ssm '/x-mcp/...' }}`) で全環境変数を列挙
  - README ファイル構成: AWS アカウント前提 / SSM 投入 / DynamoDB テーブル作成 / IAM ロール / デプロイ
