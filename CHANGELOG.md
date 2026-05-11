# Changelog

このプロジェクトの変更履歴を記録する。フォーマットは [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に準拠し、バージョニングは [Semantic Versioning](https://semver.org/lang/ja/) に従う。

## [Unreleased]

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

[Unreleased]: https://github.com/youyo/x/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/youyo/x/releases/tag/v0.1.0
