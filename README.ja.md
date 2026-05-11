# x — X (Twitter) API CLI & Remote MCP

Read this in: [English](README.md) | 日本語

[![CI](https://github.com/youyo/x/actions/workflows/ci.yml/badge.svg)](https://github.com/youyo/x/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.26-blue.svg)](go.mod)
[![Latest Release](https://img.shields.io/github/v/release/youyo/x?include_prereleases)](https://github.com/youyo/x/releases)

X (旧 Twitter) API v2 を扱うための単一バイナリ Go 製 CLI。Claude Code Routines を使った「前日に Like した Post → Backlog 課題化」自動化基盤の土台として設計されている。

設計方針は **「CLI がコア、MCP はその薄いラッパー」**。`v0.1.0` では CLI のみを提供し、Remote MCP サーバーと AWS Lambda 配布は後続リリースで対応する。

## ステータス

本リリースは **`v0.1.0` — CLI のみ** である。今後の予定:

| バージョン | スコープ |
|---------|-------|
| `v0.1.0` (本リリース) | CLI: `x version` / `x me` / `x liked list` / `x configure` / `x completion` |
| `v0.2.0` (予定) | Remote MCP サーバー (`x mcp --auth idproxy\|apikey\|none`) と `get_user_me` / `get_liked_tweets` tools |
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
- **`x version`** — ビルド情報 (バージョン / コミット / ビルド日時) の表示
- **`x completion`** — bash / zsh / fish / powershell 4 シェルの補完スクリプト生成 (Cobra 標準)
- **OAuth 1.0a** 静的トークン認証 (user context)
- **XDG Base Directory Specification 準拠** — 非機密設定とシークレットを別ファイルに分離
- **安定した exit code** (`0` / `1` / `2` / `3` / `4` / `5`) でスクリプト連携可能

## インストール

### Homebrew (`v0.1.0` リリース後に利用可能予定)

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

### 環境変数

| 名前 | 用途 | 必須 |
|------|---------|------|
| `X_API_KEY` | OAuth 1.0a consumer key | はい |
| `X_API_SECRET` | OAuth 1.0a consumer secret | はい |
| `X_ACCESS_TOKEN` | OAuth 1.0a access token | はい |
| `X_ACCESS_TOKEN_SECRET` | OAuth 1.0a access token secret | はい |
| `XDG_CONFIG_HOME` | 設定ディレクトリ上書き | いいえ |
| `XDG_DATA_HOME` | データディレクトリ上書き | いいえ |

環境変数が指定されている場合、ファイルベースのクレデンシャルより優先される。

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

- **`v0.2.0`** — Remote MCP サーバー: `x mcp --auth idproxy\|apikey\|none --host 0.0.0.0 --port 8080`
  - MCP tools: `get_user_me`, `get_liked_tweets`
  - [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go) (Streamable HTTP) で実装
  - `idproxy` ミドルウェア + 4 種類のストアバックエンド (memory / sqlite / redis / dynamodb) を切り替え可能
- **`v0.3.0`** — `examples/lambroll/`: AWS Lambda + Function URL + Lambda Web Adapter デプロイサンプル + Claude Code Routines プロンプト雛形 (`docs/routine-prompt.md`)

マイルストーン分解は [`plans/x-roadmap.md`](plans/x-roadmap.md) を参照。

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

## コントリビュート

Issue / Pull Request 歓迎。本プロジェクトは以下に従う:

- **Conventional Commits** (日本語コミットメッセージ可。メインメンテナが日本語話者のため)
- **テスト駆動開発** (Red → Green → Refactor) — エージェント駆動の開発フローは [`CLAUDE.md`](CLAUDE.md) を参照
- **`config.toml` にシークレットを書かない** — ローダーが拒否する

## ライセンス

[MIT](LICENSE) — Copyright (c) 2026 Naoto Ishizawa / Heptagon
