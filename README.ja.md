# x — X (Twitter) API CLI & Remote MCP

Read this in: [English](README.md) | 日本語

[![CI](https://github.com/youyo/x/actions/workflows/ci.yml/badge.svg)](https://github.com/youyo/x/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.26-blue.svg)](go.mod)
[![Latest Release](https://img.shields.io/github/v/release/youyo/x?include_prereleases)](https://github.com/youyo/x/releases)

X (旧 Twitter) API v2 を扱うための単一バイナリ Go 製 CLI。Claude Code Routines を使った「前日に Like した Post → Backlog 課題化」自動化基盤の土台として設計されている。

設計方針は **「CLI がコア、MCP はその薄いラッパー」**。`v0.3.0` から AWS Lambda Function URL 用デプロイサンプル (`examples/lambroll/`) と Claude Code Routines 用プロンプト雛形 (`docs/routine-prompt.md`) も提供する。

## ステータス

`v0.3.0` で 3 フェーズ計画 (CLI → MCP → 公開配布) が完了。リリース履歴:

| バージョン | スコープ |
|---------|-------|
| `v0.1.0` | CLI: `x version` / `x me` / `x liked list` / `x configure` / `x completion` |
| `v0.2.0` | Remote MCP サーバー (`x mcp --auth idproxy\|apikey\|none`) と `get_user_me` / `get_liked_tweets` tools、加えて 4 種類の `idproxy` ストアバックエンド (memory / sqlite / redis / dynamodb) |
| `v0.3.0` (本リリース) | `examples/lambroll/` AWS Lambda Function URL デプロイサンプル + Claude Code Routines プロンプト雛形 (`docs/routine-prompt.md`) + X API v2 リファレンス (`docs/x-api.md`) |

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

## CLI レシピ集

### `x me` の典型例

```bash
# JSON (デフォルト) — jq でパイプ
x me | jq -r '.username'

# シェルスクリプト用に human-readable な 1 行
x me --no-json
# → id=12345 username=yourname name=Your Name
```

### Liked Post の取得

```bash
# シングルページ (最大 100 件)
x liked list --max-results 100

# 日時範囲指定 (UTC RFC3339)
x liked list \
  --start-time 2026-05-10T00:00:00Z \
  --end-time   2026-05-10T23:59:59Z

# JST 前日 (UTC レンジへ自動換算、全ページ取得)
x liked list --yesterday-jst --all

# JST 任意日
x liked list --since-jst 2026-05-10 --all

# LLM のコンテキストにパイプするときの暴走防止
x liked list --yesterday-jst --all --max-pages 5

# カスタムフィールド (公開メトリクス + entities など)
x liked list --yesterday-jst --all \
  --tweet-fields "id,text,author_id,created_at,public_metrics,entities" \
  --expansions   "author_id" \
  --user-fields  "username,name,verified"

# NDJSON ストリーミング (1 tweet / 1 行) を jq / xargs / LLM に流し込む
x liked list --yesterday-jst --all --ndjson \
  | jq -r '"- [\(.text | gsub("\n"; " ") | .[0:80])](https://x.com/i/web/status/\(.id))"'
```

### 設定の確認・検証

```bash
x configure --print-paths
# {
#   "config":      "/home/you/.config/x/config.toml",
#   "credentials": "/home/you/.local/share/x/credentials.toml",
#   "data_dir":    "/home/you/.local/share/x"
# }

x configure --check
# credentials.toml のパーミッション (0600) と
# config.toml にシークレットが混入していないかを検証する。
```

### Claude Code との連携

```bash
# ローカル Claude Code セッションに一時的に追加
echo '{"x":{"command":"x","args":["mcp","--auth","none","--host","127.0.0.1","--port","18080"]}}' \
  > ~/.config/claude/mcp.json
```

恒久的にリモート利用したい場合は `examples/lambroll/` でデプロイし、Function URL を Claude Code Routines のコネクターに登録する — [`docs/routine-prompt.md`](docs/routine-prompt.md) を参照。

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

### MCP クライアントレシピ

#### `curl` から (生 Streamable HTTP / JSON-RPC 2.0)

```bash
# 1) initialize ハンドシェイク
curl -sS -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
    "protocolVersion":"2025-03-26",
    "capabilities":{},
    "clientInfo":{"name":"curl","version":"1.0"}}}'

# 2) tools 一覧
curl -sS -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'

# 3) get_user_me 呼び出し
curl -sS -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call",
       "params":{"name":"get_user_me","arguments":{}}}'

# 4) get_liked_tweets 呼び出し (前日 JST、全ページ)
curl -sS -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call",
       "params":{"name":"get_liked_tweets",
         "arguments":{"yesterday_jst":true,"all":true,"max_pages":5}}}'
```

`--auth apikey` の場合は各リクエストに `-H "Authorization: Bearer <token>"` を追加する。

#### Claude Code Routines から

1. `examples/lambroll/` でデプロイ ([`examples/lambroll/README.md`](examples/lambroll/README.md) 参照)
2. Claude Code Routines でデプロイした Function URL をコネクターとして登録
3. [`docs/routine-prompt.md`](docs/routine-prompt.md) のプロンプト雛形を使用 — 前日 Liked 取得、技術判定、重複検出付きで Backlog 課題を起票する一連の指示を含む

#### mark3labs/mcp-go から (Go クライアント)

```go
import "github.com/mark3labs/mcp-go/client"

c, _ := client.NewStreamableHttpClient("http://127.0.0.1:8080/mcp")
c.Start(ctx)
defer c.Close()

_, _ = c.Initialize(ctx, mcp.InitializeRequest{...})
result, _ := c.CallTool(ctx, mcp.CallToolRequest{
    Params: mcp.CallToolParams{
        Name:      "get_liked_tweets",
        Arguments: map[string]any{"yesterday_jst": true, "all": true},
    },
})
```

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

## トラブルシュート

| 症状 | 原因 | 対処 |
|---|---|---|
| `x me` が exit 3 (「クレデンシャル不足」) | `X_API_*` env 未設定かつ `credentials.toml` 不在 | `x configure` で `credentials.toml` 生成、または `export X_API_KEY=...` 等。`x configure --check` で検証可 |
| `x me` が exit 3 (`401 Unauthorized`) | X Developer Portal でトークン再発行された | Developer Portal でトークンを再発行し `x configure` をやり直す |
| `x liked list` が exit 4 (Permission) | `403`: アカウント凍結または Read scope 不足 | Developer App の権限に少なくとも **Read** が含まれているか確認 |
| `x liked list --all` がとても遅い | レートリミットに当たり `x-rate-limit-reset` まで sleep 中 | 想定動作。必要なら `--max-pages 5` で上限を切る |
| `x liked list` の `meta.result_count = 0` | `start_time` / `end_time` の窓に Like が無い、または期間内に Like していない | 時間フィルタなしの `x liked list` で接続性を先に確認 |
| `x mcp` が起動直後に exit 3 (「X_MCP_API_KEY が必要」) | `--auth apikey` 指定だが `X_MCP_API_KEY` 未設定 | `X_MCP_API_KEY=$(openssl rand -hex 32)` をセットして再起動 |
| MCP クライアントが `/mcp` から `401` を受ける | `Authorization` ヘッダの誤り、または `idproxy` cookie 失効 | apikey: `Bearer <token>` を確認。idproxy: ブラウザで `EXTERNAL_URL` にアクセスして再認証 |
| MCP クライアントが特定パスで `404` | サーバーは既定で `/mcp` のみマウント | 既定パスを使うか `--path` / `X_MCP_PATH` を明示 |
| 設定 / クレデンシャルファイルの場所が分からない | 実行時に XDG パスを解決 | `x configure --print-paths` で確認 |
| Routines コネクターが接続を拒否される | Function URL 非公開、または `EXTERNAL_URL` と OIDC コールバック登録が不一致 | [`examples/lambroll/README.md`](examples/lambroll/README.md) のトラブルシュート節を参照 |

## ドキュメント

| ドキュメント | 用途 |
|---|---|
| [`docs/specs/x-spec.md`](docs/specs/x-spec.md) | プロダクト仕様書 (Approved v1.0.0) |
| [`docs/x-api.md`](docs/x-api.md) | X API v2 OAuth 1.0a + レート制限 + Owned Reads 課金リファレンス |
| [`docs/routine-prompt.md`](docs/routine-prompt.md) | Claude Code Routines 用プロンプト雛形 (前日 Like → Backlog 課題化) |
| [`examples/lambroll/README.md`](examples/lambroll/README.md) | AWS Lambda Function URL デプロイ手順 (lambroll + LWA) |
| [`CHANGELOG.md`](CHANGELOG.md) | リリース履歴 |
| [`plans/x-roadmap.md`](plans/x-roadmap.md) | マイルストーン分解 (全 28 マイルストーン完了) |

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

`v0.1.0` / `v0.2.0` / `v0.3.0` のいずれのタグも本リポジトリからはまだ push していない。上記手順はその意図を文書化したもの。

## コントリビュート

Issue / Pull Request 歓迎。本プロジェクトは以下に従う:

- **Conventional Commits** (日本語コミットメッセージ可。メインメンテナが日本語話者のため)
- **テスト駆動開発** (Red → Green → Refactor) — エージェント駆動の開発フローは [`CLAUDE.md`](CLAUDE.md) を参照
- **`config.toml` にシークレットを書かない** — ローダーが拒否する

## ライセンス

[MIT](LICENSE) — Copyright (c) 2026 Naoto Ishizawa / Heptagon
