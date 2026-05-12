# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`x` は X (旧 Twitter) API v2 の CLI と Remote MCP サーバーを 1 バイナリで提供する Go プロジェクト (OSS, MIT)。
最終ユースケースは Claude Code Routines から MCP コネクター経由で前日 Liked Posts を取得し、Backlog に技術調査課題を自動起票する。

設計方針: **CLI がコア / MCP はその薄いラッパー**。
スペックは `docs/specs/x-spec.md` (v1.0.0 Approved) に確定済み。実装フェーズの記録は `plans/x-roadmap.md` と `plans/x-m??-*.md` に分割されている。

## Common Commands

すべて mise でツールチェイン管理 (`go 1.26.1` 固定)。

```bash
# テスト (race 検知、cache 無効化)
go test -race -count=1 ./...
go test -race -count=1 ./internal/xapi/...       # 単一パッケージ
go test -race -count=1 -run TestNewOAuth1Config ./internal/xapi/   # 単一テスト

# Lint (v2 設定、default: none + 8 linters)
golangci-lint run ./...
# ローカル sandbox では cache パス変更が必要なケースあり:
#   GOLANGCI_LINT_CACHE=$TMPDIR/golangci-cache golangci-lint run ./...

# Vet
go vet ./...

# Build (ローカル)
go build -o /tmp/x ./cmd/x

# ldflags 注入確認
go build -ldflags="-X github.com/youyo/x/internal/version.Version=v0.0.1 \
  -X github.com/youyo/x/internal/version.Commit=$(git rev-parse --short HEAD) \
  -X github.com/youyo/x/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o /tmp/x ./cmd/x

# GoReleaser スナップショット (リリース前の最終検証)
goreleaser check                              # yaml 構文
goreleaser release --snapshot --clean         # tarball + docker image (Docker daemon 必要)
goreleaser release --snapshot --clean --skip docker,docker_manifest   # Docker 無し

# CI と同じワークフローのローカル lint
actionlint .github/workflows/release.yml
```

## Architecture

レイヤー間は一方向依存。下層は上層を知らない。

```
cmd/x/main.go            ── エントリ。run() int で os.Exit から分離 (テスト可能)
                            error → exit code 写像を一元化 (errors.Is で xapi 番兵判定)
   │
   ▼
internal/cli/            ── Cobra ベースのサブコマンド層
   │  root.go            NewRootCmd() factory。サブコマンドを AddCommand で接続
   │  me.go / liked.go / configure.go / version.go / mcp.go
   │  auth_loader.go     env > credentials.toml の優先順位ロード (CLI モード)
   │  mcp_auth.go        env-only ロード + 4 store backend factory (MCP モード)
   │
   ▼ (CLI モード)                                ▼ (MCP モード)
internal/xapi/                              internal/mcp/ + internal/transport/http/
   client.go             HTTP client        server.go     mark3labs/mcp-go ラッパー
   oauth1.go             OAuth 1.0a 署名     tools_me.go   get_user_me ツール
   users.go / likes.go   X API 呼び出し      tools_likes.go get_liked_tweets ツール
   types.go              DTO                                ↑ tool は内部で xapi.Client を使う
   errors.go             番兵 + ExitCodeFor              transport/http/server.go
                                                         Streamable HTTP + graceful 停止
                                                                ↓
                                                       internal/authgate/  (HTTP middleware)
                                                         gate.go         New(mode, opts...)
                                                         none.go         passthrough
                                                         apikey.go       Bearer + constant-time
                                                         idproxy.go      OIDC session
                                                         store_*.go      memory/sqlite/redis/dynamodb

internal/config/         ── XDG 準拠の設定ローダー (CLI モードのみ)
   xdg.go / loader_cli.go / credentials.go / guard.go

internal/app/            ── exit code 定数 (0=success / 1=generic / 2=arg / 3=auth / 4=perm / 5=not_found)
internal/version/        ── Version / Commit / Date 変数 (ldflags 注入)
```

### CLI モード vs MCP モードの責務分離 (spec §11 不変条件)

- **CLI モード** (`x me`, `x liked list`, `x configure` 等): `LoadCredentialsFromEnvOrFile` で **env > credentials.toml** を解決
- **MCP モード** (`x mcp`): `loadMCPCredentials` で **env のみ**。`credentials.toml` を絶対に読まない (Lambda 不変インフラ前提)
- この分離はテスト (`TestLoadMCPCredentials_IgnoresFile`) で負系統 pin 済み

### authgate の Option パターン

`authgate.New(mode, opts...)` は variadic Option で各モード固有の設定を注入する。
**authgate パッケージは環境変数を一切直読しない** — env 解決は `internal/cli/mcp_auth.go` (M24) の責務。
4 store backend (`memory/sqlite/redis/dynamodb`) は `WithIDProxyStore(idproxy.Store)` で単一接続点に統一。

## Conventions

### パッケージ doc コメント

各パッケージで `// Package xxx ...` 形式の doc を**1 ファイルだけ**に書く (`revive: package-comments` ルール対策)。
パッケージ doc 集約先:
- `internal/cli/root.go` / `internal/config/config.go` / `internal/version/version.go`
- `internal/xapi/oauth1.go` / `internal/mcp/doc.go` / `internal/transport/http/doc.go` / `internal/authgate/doc.go`
新規ファイル追加時にパッケージ doc を書くと違反になるので注意。

### Cobra テストパターン

```go
cmd := NewRootCmd()
buf := &bytes.Buffer{}
cmd.SetOut(buf)
cmd.SetErr(buf)
cmd.SetArgs([]string{"version", "--no-json"})
err := cmd.Execute()
```

- `NewRootCmd()` は factory なのでグローバル状態なし、テストごとに新規生成
- 各サブコマンドは httptest 注入のため `newXxxClient` パッケージ var + interface でスワップ可能 (M9/M10 で確立)

### TDD 厳守

各マイルストーンは **Red → Green → Refactor** で進める。テスト先行で構造化し、`go test -race -count=1` を常に green に保つ。

### コミット規約

- Conventional Commits (日本語メッセージ)
- 各 commit のフッターに対応する `Plan: plans/x-m??-*.md` を記載
- 単一論理単位 = 1 commit (実装本体と roadmap 更新は別 commit に分離)
- 例: `feat(xapi): GetUserMe と User/Tweet/Meta DTO を追加`

### Exit code 写像

`cmd/x/main.go` の `run()` 内 switch で `errors.Is` を使って一元判定:
- `xapi.ErrAuthentication` → 3
- `xapi.ErrPermission` → 4
- `xapi.ErrNotFound` → 5
- `cli.ErrInvalidArgument` → 2
- `isArgumentError` (Cobra "unknown command/flag" 接頭辞文字列マッチ) → 2
- その他 → 1

**`isArgumentError` は英語ロケール依存** (Cobra v1 に型付きエラーがないため)。CI は `LANG=C` を env トップレベルで固定している (`.github/workflows/ci.yml`)。

### 設定ファイルとシークレット

- 非機密設定: `${XDG_CONFIG_HOME:-~/.config}/x/config.toml` (0644)
- CLI 用シークレット: `${XDG_DATA_HOME:-~/.local/share}/x/credentials.toml` (**0600 強制**)
- `config.toml` にシークレット系キー (api_key 等) を書くと `guard.CheckConfigNoSecrets` がエラー終了
- MCP モードはファイル一切読まず env のみ (SSM Parameter Store → Lambda env 想定)

## Release Flow

- タグ push `v*` → `.github/workflows/release.yml` 起動 → GoReleaser で darwin/linux × amd64/arm64 ビルド + GitHub Releases + ghcr.io image + Homebrew tap (`youyo/homebrew-tap`) 更新
- Homebrew tap への push は GitHub App token (`APP_ID` + `APP_PRIVATE_KEY` secrets) で短期発行 (`tibdex/github-app-token@v2.1.0`)
- Dockerfile は **GoReleaser 専用**の単純 COPY 版 (prebuilt バイナリ + distroless/static-debian12:nonroot)。ローカルで `docker build` するには `goreleaser release --snapshot --clean` で artifact 生成が必要
- CHANGELOG は Keep a Changelog 形式。リリース手順は `README.md` の "Release procedure (maintainer)" に書面化済み

## devflow / Plans

- `plans/x-roadmap.md` が Layer 1 (28 マイルストーン進捗管理)
- `plans/x-m??-<slug>.md` が Layer 2 (マイルストーン別詳細計画 + TDD 設計 + Mermaid 図 + ハンドオフ)
- 仕様変更時は `docs/specs/x-spec.md` を先に更新し、ADR を追記してからコード変更
