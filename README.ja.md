# x — X (Twitter) API CLI & Remote MCP

Read this in: [English](README.md) | 日本語

[![CI](https://github.com/youyo/x/actions/workflows/ci.yml/badge.svg)](https://github.com/youyo/x/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.26-blue.svg)](go.mod)
[![Latest Release](https://img.shields.io/github/v/release/youyo/x?include_prereleases)](https://github.com/youyo/x/releases)

X (旧 Twitter) API v2 を扱うための単一バイナリ Go 製 CLI。Claude Code Routines を使った「前日に Like した Post → Backlog 課題化」自動化基盤の土台として設計されている。

設計方針は **「CLI がコア、MCP はその薄いラッパー」**。`v0.2.0` から Remote MCP サーバーが利用可能になった。AWS Lambda デプロイサンプルは `v0.3.0` で対応予定。

## ステータス

`v0.2.0` で **Remote MCP サーバー** をリリース。リリース履歴:

| バージョン | スコープ |
|---------|-------|
| `v0.1.0` | CLI: `x version` / `x me` / `x liked list` / `x configure` / `x completion` |
| `v0.2.0` (本リリース) | Remote MCP サーバー (`x mcp --auth idproxy\|apikey\|none`) と `get_user_me` / `get_liked_tweets` tools、加えて 4 種類の `idproxy` ストアバックエンド (memory / sqlite / redis / dynamodb) |
| `v0.3.0` (予定) | `examples/lambroll/` AWS Lambda デプロイサンプル + Claude Code Routines プロンプト雛形 |

詳細仕様は [`docs/specs/x-spec.md`](docs/specs/x-spec.md) を参照。

## 機能

- **`x me`** — `GET /2/users/me` で自分の `user_id` / `username` を取得
- **`x liked list`** — `GET /2/users/:id/liked_tweets` で Liked Post を取得
  - JST 日付ヘルパ (`--since-jst`, `--yesterday-jst`) で自動的に UTC に変換
  - `--all` モードで `next_token` を自動辿り (rate-limit aware: `x-rate-limit-remaining` / `x-rate-limit-reset` ヘッダ追従)
  - NDJSON ストリーミング出力 (`--ndjson`) — 他ツールへのパイプ向け
  - `tweet.fields` / `expansions` / `user.fields` のカスタマイズ
- **`x configure`** — 対話形式で XDG 準拠の設定 / 認証情報ファイルを生成
- **`x mcp`** — Streamable HTTP MCP サーバーを起動 (Claude Code Routines / MCP クライアント接続用)
  - 3 種類の認証モード: `none` (ローカル開発専用) / `apikey` (Bearer token) / `idproxy` (OIDC + cookie session)
  - 4 種類の `idproxy` ストアバックエンド: `memory` / `sqlite` / `redis` / `dynamodb`
  - MCP tools: `get_user_me`, `get_liked_tweets`
  - `GET /healthz` で Lambda Web Adapter / k8s liveness probe に対応
  - SIGINT/SIGTERM 受信で graceful shutdown
- **`x version`** — ビルド情報 (バージョン / コミット / ビルド日時) の表示
- **`x completion`** — bash / zsh / fish / powershell 4 シェルの補完スクリプト生成 (Cobra 標準)
- **OAuth 1.0a** 静的トークン認証 (user context)
- **XDG Base Directory Specification 準拠** — 非機密設定とシークレットを別ファイルに分離
- **安定した exit code** (`0` / `1` / `2` / `3` / `4` / `5`) でスクリプト連携可能

## インストール

### Homebrew

```bash
brew install youyo/tap/x
```

### `go install`

```bash
go install github.com/youyo/x/cmd/x@latest
```

### Docker

```bash
docker pull ghcr.io/youyo/x:latest
docker run --rm ghcr.io/youyo/x:latest version
```

### GitHub Releases

[Releases ページ](https://github.com/youyo/x/releases) から OS / arch に合った tarball をダウンロードし、`x` バイナリを `$PATH` に配置する。

### ソースから

```bash
git clone https://github.com/youyo/x.git
cd x
go build -o x ./cmd/x
./x version
```

## クイックスタート

### 1. X API クレデンシャルを取得

**OAuth 1.0a User Context** が有効化された X (Twitter) Developer App が必要。詳細は [X API クレデンシャルの取得方法](#x-api-クレデンシャルの取得方法) を参照。

最終的に 4 つのシークレットが手元に揃う:

- `X_API_KEY` (consumer key)
- `X_API_SECRET` (consumer secret)
- `X_ACCESS_TOKEN`
- `X_ACCESS_TOKEN_SECRET`

### 2. 設定

対話モード (XDG 準拠パスに保存、`credentials.toml` は `chmod 0600`):

```bash
x configure
```

または環境変数で直接指定:

```bash
export X_API_KEY=...
export X_API_SECRET=...
export X_ACCESS_TOKEN=...
export X_ACCESS_TOKEN_SECRET=...
```

### 3. 動作確認

```bash
x me
# {"id":"12345","username":"yourname","name":"Your Name"}
```

### 4. 前日の Liked Post を取得

```bash
x liked list --yesterday-jst --all
```

NDJSON でパイプしたい場合:

```bash
x liked list --yesterday-jst --all --ndjson | jq -r '.id + " " + .text'
```

## クイックスタート (MCP サーバー)

`x mcp` サブコマンドは [Streamable HTTP](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports) MCP サーバーを起動する。3 種類の認証モードに対応する。**MCP モードではシークレットは必ず環境変数から読み込み**、`credentials.toml` は一切読まない。

エンドポイント (認証モードに依存しない):

- `POST /mcp` — MCP Streamable HTTP エントリポイント (認証あり)
- `GET /healthz` — liveness probe (常に `200 ok` を返却、認証バイパス)

### ローカル開発 (`--auth none`)

```bash
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
  x mcp --auth none --host 127.0.0.1 --port 8080
```

### 共有 API Key (`--auth apikey`)

```bash
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
X_MCP_API_KEY=$(openssl rand -hex 32) \
  x mcp --auth apikey --host 0.0.0.0 --port 8080
```

クライアントは `Authorization: Bearer ${X_MCP_API_KEY}` を送る必要がある。比較は constant-time (`subtle.ConstantTimeCompare`)。`--apikey-env` フラグは shared secret を保持する **環境変数名** を指定する (default: `X_MCP_API_KEY`)。

### OIDC + cookie session (`--auth idproxy`, デフォルト)

[`github.com/youyo/idproxy`](https://github.com/youyo/idproxy) を利用する。永続化バックエンドは `STORE_BACKEND` で選択する:

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

ストアバックエンド:

| `STORE_BACKEND` | 必須環境変数 | 用途 |
|---|---|---|
| `memory` (default) | — | 単体テスト・一時的なローカル開発 |
| `sqlite` | `SQLITE_PATH` (default `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db`) | 単一プロセスのローカル開発 (`modernc.org/sqlite`, pure Go) |
| `redis` | `REDIS_URL` (例: `redis://localhost:6379/0`) | 軽量サーバー、TTL ネイティブ (`go-redis/v9`) |
| `dynamodb` | `DYNAMODB_TABLE_NAME`, `AWS_REGION` | Lambda マルチコンテナ、`ConsistentRead` (`aws-sdk-go-v2`) |

### 利用可能な MCP tools

| Tool | 説明 |
|---|---|
| `get_user_me` | OAuth 1.0a ユーザーの `{ user_id, username, name }` を返す。 |
| `get_liked_tweets` | Liked Posts を全ページング (`all=true`, `max_pages`, rate-limit aware) 込みで返す。`user_id` / `start_time` / `end_time` / `since_jst` / `yesterday_jst` / `max_results` / `tweet_fields` / `expansions` / `user_fields` を受け取る。JST ヘルパの優先順位は `yesterday_jst > since_jst > start_time/end_time` (CLI と統一)。 |

## 設定

### ファイル配置 (XDG Base Directory Specification 準拠)

| 種別 | パス | パーミッション |
|------|------|-------------|
| 非機密設定 | `${XDG_CONFIG_HOME:-~/.config}/x/config.toml` | `0644` |
| シークレット (CLI 用) | `${XDG_DATA_HOME:-~/.local/share}/x/credentials.toml` | `0600` |

`config.toml` にシークレットを書き込もうとした場合はロード時に **明示的に拒否** される (`ErrSecretInConfig`)。両ファイルを別パスに分離することで、シークレットファイルの `.gitignore` 化を容易にし、誤コミット事故を防止する。

### ロード優先順位

CLI モード (`x me`, `x liked list`, `x configure`):

1. CLI フラグ
2. 環境変数
3. `credentials.toml` (シークレットのみ)
4. `config.toml` (非機密のみ)
5. 組み込みデフォルト

MCP モード (`x mcp`):

1. CLI フラグ (`--host` / `--port` / `--path` / `--auth` / `--apikey-env`)
2. 環境変数 (シークレット含む)
3. 組み込みデフォルト

**MCP モードは `config.toml` も `credentials.toml` も一切読まない** — シークレットは必ず環境変数経由で渡す。Lambda / コンテナデプロイの決定性を担保するための不変条件である。

### `config.toml` 例

```toml
# CLI のデフォルト値。シークレットは書かない。
[cli]
output = "json"           # json | ndjson | table
log_level = "info"

[liked]
default_max_results = 100
default_max_pages = 50
default_tweet_fields = "id,text,author_id,created_at,entities,public_metrics"
default_expansions   = "author_id"
default_user_fields  = "username,name"
```

### `credentials.toml` 例 (`chmod 0600`)

```toml
[xapi]
api_key             = "..."
api_secret          = "..."
access_token        = "..."
access_token_secret = "..."
```

### 環境変数 (X API、CLI / MCP 共通)

| 名前 | 用途 | 必須 |
|------|---------|------|
| `X_API_KEY` | OAuth 1.0a consumer key | はい |
| `X_API_SECRET` | OAuth 1.0a consumer secret | はい |
| `X_ACCESS_TOKEN` | OAuth 1.0a access token | はい |
| `X_ACCESS_TOKEN_SECRET` | OAuth 1.0a access token secret | はい |
| `XDG_CONFIG_HOME` | 設定ディレクトリ上書き (CLI のみ) | いいえ |
| `XDG_DATA_HOME` | データディレクトリ上書き (CLI のみ) | いいえ |

環境変数が指定されている場合、ファイルベースのクレデンシャルより優先される。

### MCP サーバー環境変数 (v0.2.0+、MCP モード専用)

| 名前 | 用途 | デフォルト / 必須 |
|------|---------|--------------------|
| `X_MCP_HOST` | bind host | `127.0.0.1` |
| `X_MCP_PORT` | bind port | `8080` |
| `X_MCP_PATH` | MCP エンドポイント prefix | `/mcp` |
| `X_MCP_AUTH` | `idproxy` / `apikey` / `none` | `idproxy` |
| `X_MCP_API_KEY` | apikey モードの shared secret **値** (`Authorization: Bearer ...` と比較) | `--auth apikey` 時必須 |
| `OIDC_ISSUER` | idproxy OIDC issuer (カンマ区切りで複数可) | `--auth idproxy` 時必須 |
| `OIDC_CLIENT_ID` | idproxy OIDC client ID (`OIDC_ISSUER` と整合させてカンマ区切り) | `--auth idproxy` 時必須 |
| `OIDC_CLIENT_SECRET` | idproxy OIDC client secret | issuer 依存 |
| `COOKIE_SECRET` | idproxy セッション暗号鍵 (hex, 32B+) | `--auth idproxy` 時必須 |
| `EXTERNAL_URL` | idproxy 外部 URL | `--auth idproxy` 時必須 |
| `STORE_BACKEND` | `memory` / `sqlite` / `redis` / `dynamodb` | `memory` |
| `SQLITE_PATH` | sqlite DB ファイルパス | `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db` |
| `REDIS_URL` | Redis 接続 URL | `STORE_BACKEND=redis` 時必須 |
| `DYNAMODB_TABLE_NAME` | DynamoDB テーブル名 | `STORE_BACKEND=dynamodb` 時必須 |
| `AWS_REGION` | AWS リージョン | Lambda / `STORE_BACKEND=dynamodb` 時必須 |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` | `info` |

## X API クレデンシャルの取得方法

1. [X Developer Portal](https://developer.x.com/) にサインインする。
2. Project + App を作成する (個人用 Liked Post 取得には Free tier で十分)。
3. App 設定で **OAuth 1.0a User Context** を `Read` 権限で有効化する。
4. **Consumer Keys** を生成 (`X_API_KEY`, `X_API_SECRET`)。
5. 同ユーザーで **Access Token and Secret** を生成 (`X_ACCESS_TOKEN`, `X_ACCESS_TOKEN_SECRET`)。
6. `x configure` を実行して値を入力するか、環境変数として export する。

> Rate limit: Free tier では `GET /2/users/me` と `GET /2/users/:id/liked_tweets` のいずれも **15 分あたり 75 リクエスト**。`x` は `--all` モード時に `x-rate-limit-remaining` / `x-rate-limit-reset` ヘッダを参照して 429 を回避する。Owned Reads は執筆時点で約 `$0.001/Tweet` の従量課金なので、`--max-pages` でコスト上限を必ず設定すること。

## Exit code

| Code | 意味 |
|------|------|
| `0` | 成功 |
| `1` | 一般エラー |
| `2` | 引数 / バリデーションエラー (不正なフラグ組み合わせ、不正な値) |
| `3` | 認証エラー (X API `401`、クレデンシャル欠落) |
| `4` | 権限エラー (X API `403`) |
| `5` | 見つからない (X API `404`) |

## ロードマップ

- **`v0.3.0`** — `examples/lambroll/`: AWS Lambda + Function URL + Lambda Web Adapter デプロイサンプル + Claude Code Routines プロンプト雛形 (`docs/routine-prompt.md`)

マイルストーン分解は [`plans/x-roadmap.md`](plans/x-roadmap.md) を、リリース履歴は [`CHANGELOG.md`](CHANGELOG.md) を参照。

## 開発

本リポジトリは [`mise`](https://mise.jdx.dev/) でツールチェイン管理を行っている。

```bash
mise install                # Go 1.26.x をインストール

# テスト実行 (race + coverage)
go test -race ./...

# Lint (golangci-lint v2)
golangci-lint run

# ローカルバイナリビルド
go build -o x ./cmd/x

# Snapshot リリース (upload なし、Docker daemon 不要)
goreleaser release --snapshot --clean --skip docker,docker_manifest
```

CI は `main` への push および PR ごとに lint / test / build / docker を実行する。詳細は [`.github/workflows/ci.yml`](.github/workflows/ci.yml) を参照。

### リリース手順 (メンテナ向け)

リリースはタグ駆動。`vX.Y.Z` タグが push されると [`.github/workflows/release.yml`](.github/workflows/release.yml) が GoReleaser を実行し、Homebrew formula と Docker イメージを自動公開する。

```bash
git checkout main && git pull
git status                                # working tree は clean であること
grep "^## \[X.Y.Z\]" CHANGELOG.md         # CHANGELOG に対応セクションがあることを確認
git tag vX.Y.Z
git push origin vX.Y.Z
```

リリースワークフローは続けて以下を実行する:

- `goreleaser` (darwin / linux × amd64 / arm64 の tarball + checksums)
- GitHub Releases アセットの公開
- `youyo/homebrew-tap` formula の更新
- `ghcr.io/youyo/x:X.Y.Z` / `:latest` への Docker イメージ push

`v0.2.0` については本リポジトリからまだタグを push していない。上記手順はその意図を文書化したもの。

## コントリビュート

Issue / Pull Request 歓迎。本プロジェクトは以下に従う:

- **Conventional Commits** (日本語コミットメッセージ可。メインメンテナが日本語話者のため)
- **テスト駆動開発** (Red → Green → Refactor) — エージェント駆動の開発フローは [`CLAUDE.md`](CLAUDE.md) を参照
- **`config.toml` にシークレットを書かない** — ローダーが拒否する

## ライセンス

[MIT](LICENSE) — Copyright (c) 2026 Naoto Ishizawa / Heptagon
