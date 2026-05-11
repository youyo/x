# M13: README/CHANGELOG/LICENSE/GoReleaser 詳細実装計画

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M13 (Phase D: v0.1.0 リリース準備) |
| スペック | `docs/specs/x-spec.md` §13 / §14 |
| 前提 | M1〜M12 完了 (af0436a) — v0.1.0 機能セット凍結 |
| ステータス | 計画中 → 実装へ |
| 作成日 | 2026-05-12 |

## Scope

### In scope
- `README.md` (英語)
- `README.ja.md` (日本語)
- `CHANGELOG.md` (Keep a Changelog 形式)
- `LICENSE` (MIT)
- `.goreleaser.yaml` (darwin/linux × amd64/arm64, Homebrew tap, ghcr.io multi-arch)
- 動作確認: `goreleaser check` / `goreleaser release --snapshot --clean`

### Out of scope (M14 以降)
- `.github/workflows/release.yml` 実装 → M14
- `v0.1.0` git tag の実作成 → M14
- Homebrew tap (`youyo/homebrew-tap`) リポジトリへの実 PR → M14
- ghcr.io への実 push → M14
- MCP 関連の README 章 → M25 で追記

## v0.1.0 機能セット (M12 完了時点で確定)

ユーザー向けに README/CHANGELOG で公表する CLI 機能:

| サブコマンド | 状態 | マイルストーン |
|---|---|---|
| `x version` (および `x --version` 短縮) | 完了 | M1 |
| `x me [--no-json]` | 完了 | M9 |
| `x liked list [全フラグ]` | 完了 | M10/M11/M12 |
| `x configure` (対話モード) | 完了 | M12 |
| `x configure --print-paths` / `--check` | 完了 | M12 |
| `x completion {bash,zsh,fish,powershell}` | 完了 | M1 (Cobra 標準) |

主要依存:
- `github.com/spf13/cobra` v1.10.2
- `github.com/BurntSushi/toml` v1.6.0
- `github.com/dghubble/oauth1` v0.7.3
- `golang.org/x/term` v0.43.0

## ファイル別設計

### 1. `README.md` (English, ~350行目安)

#### 構成
1. **Header** — タイトル / 1行サマリ / バッジ (CI / Release / License / Go version)
2. **Status** — v0.1.0 (CLI only). MCP server is planned for v0.2.0. examples/lambroll for v0.3.0
3. **Features** — 現状サポートの CLI 機能リスト (5 commands)
4. **Installation** — Homebrew (planned) / `go install` / Docker / GitHub Releases
5. **Quick Start** — `x configure` → `x me` → `x liked list --yesterday-jst --all`
6. **Configuration** — XDG paths, env vars, `config.toml` テンプレ, `credentials.toml` テンプレ
7. **Obtaining X API credentials** — X Developer Portal リンク手順
8. **Exit codes** — 0/1/2/3/4/5 の規約
9. **Roadmap** — v0.2.0 (MCP) / v0.3.0 (lambroll examples)
10. **Development** — `mise install` / `go test` / `golangci-lint run`
11. **Contributing** — Issue 歓迎 / Conventional Commits 日本語可
12. **License** — MIT, Naoto Ishizawa / Heptagon

#### バッジ
- CI ステータス: `https://github.com/youyo/x/actions/workflows/ci.yml`
- License: MIT shield
- Go version: 1.26.1
- Latest release: GitHub release shield

#### 注意点
- v0.1.0 は CLI のみであることを明示
- インストール方法に Homebrew は「planned (after v0.1.0 release)」表示
- `x mcp` サブコマンドは README に書かない (まだ未実装)

### 2. `README.ja.md` (日本語, ~350行)

`README.md` と同等構成・同等情報量。一次ターゲット (Naoto / Heptagon) は日本語ユーザーなので双方向リンクをヘッダに置く:
- 英語: "Read this in: English | [日本語](README.ja.md)"
- 日本語: "Read this in: [English](README.md) | 日本語"

### 3. `CHANGELOG.md` (Keep a Changelog v1.1.0 形式)

```markdown
# Changelog

このプロジェクトの変更履歴を記録する。フォーマットは [Keep a Changelog](https://keepachangelog.com/) に準拠し、バージョニングは [Semantic Versioning](https://semver.org/) に従う。

## [Unreleased]

## [0.1.0] - 2026-05-12

初回リリース。X (Twitter) API v2 の Liked Posts をローカル CLI で取得できる v0.1.0 を公開。MCP / Lambda 展開は v0.2.0 以降で対応予定。

### Added
- `x version` / `x --version` (M1)
- `x me [--no-json]` — self user_id / username の取得 (M9)
- `x liked list` — Liked Posts の取得 (M10/M11):
  - `--user-id` / `--start-time` / `--end-time` / `--max-results` / `--pagination-token`
  - `--since-jst <YYYY-MM-DD>` / `--yesterday-jst` (JST 日付ヘルパ)
  - `--all` + `--max-pages` (rate-limit aware ページネーション)
  - `--ndjson` (NDJSON ストリーミング出力)
  - `--tweet-fields` / `--expansions` / `--user-fields` のカスタマイズ
- `x configure` 対話モード (M12) — `~/.config/x/config.toml` と `~/.local/share/x/credentials.toml` を XDG 準拠で生成
- `x configure --print-paths` — 設定ファイルパスの表示
- `x configure --check` — credentials.toml のパーミッション・必須キー検証
- `x completion {bash,zsh,fish,powershell}` (Cobra 標準)
- XDG Base Directory Specification 準拠の設定ロード
- OAuth 1.0a 静的トークンによる X API v2 アクセス
- Rate-limit aware ページネーション (`x-rate-limit-remaining` / `x-rate-limit-reset` 追従)
- exit code 規約 (0/1/2/3/4/5)
- 配布: `go install` / Docker (`ghcr.io/youyo/x`) / GitHub Releases tar.gz / Homebrew tap (planned)

### Security
- 非機密設定 (`config.toml`) とシークレット (`credentials.toml`, perm 0600) をファイルレベルで分離
- `config.toml` にシークレットキーが含まれている場合は読み込み拒否

[Unreleased]: https://github.com/youyo/x/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/youyo/x/releases/tag/v0.1.0
```

### 4. `LICENSE` (MIT)

```
MIT License

Copyright (c) 2026 Naoto Ishizawa / Heptagon

Permission is hereby granted, free of charge, to any person obtaining a copy
... (標準 MIT 全文)
```

### 5. `.goreleaser.yaml`

kintone の goreleaser を参考にしつつ、x 向けに調整:

```yaml
version: 2

project_name: x

before:
  hooks:
    - go mod tidy
    - go test ./...

builds:
  - id: x
    main: ./cmd/x
    binary: x
    env:
      - CGO_ENABLED=0
    flags:
      - -trimpath
    ldflags:
      - -s -w
      - -X github.com/youyo/x/internal/version.Version={{ .Version }}
      - -X github.com/youyo/x/internal/version.Commit={{ .ShortCommit }}
      - -X github.com/youyo/x/internal/version.Date={{ .Date }}
    goos: [darwin, linux]
    goarch: [amd64, arm64]

archives:
  - id: default
    ids: [x]
    formats: [tar.gz]
    name_template: >-
      {{ .ProjectName }}_{{ .Version }}_
      {{- title .Os }}_
      {{- if eq .Arch "amd64" }}x86_64
      {{- else if eq .Arch "arm64" }}arm64
      {{- else }}{{ .Arch }}{{ end }}
    files:
      - LICENSE
      - README.md
      - README.ja.md
      - CHANGELOG.md

checksum:
  name_template: checksums.txt
  algorithm: sha256

snapshot:
  version_template: "{{ incpatch .Version }}-next"

changelog:
  sort: asc
  use: github
  groups:
    - title: Features
      regexp: '^.*feat[(\w)]*:+.*$'
      order: 0
    - title: Bug fixes
      regexp: '^.*fix[(\w)]*:+.*$'
      order: 1
    - title: Others
      order: 999
  filters:
    exclude:
      - '^docs:'
      - '^test:'
      - '^chore:'
      - merge

release:
  github:
    owner: youyo
    name: x
  draft: false
  prerelease: auto

brews:
  - name: x
    repository:
      owner: youyo
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_GITHUB_TOKEN }}"
    directory: Formula
    homepage: https://github.com/youyo/x
    description: "X (Twitter) API CLI and Remote MCP server"
    license: MIT
    commit_author:
      name: goreleaserbot
      email: bot@goreleaser.com
    install: |
      bin.install "x"
    test: |
      system "#{bin}/x", "version"

dockers:
  - image_templates:
      - "ghcr.io/youyo/x:{{ .Version }}-amd64"
    use: buildx
    goos: linux
    goarch: amd64
    dockerfile: Dockerfile
    build_flag_templates:
      - "--platform=linux/amd64"
      - "--label=org.opencontainers.image.source=https://github.com/youyo/x"
      - "--label=org.opencontainers.image.version={{ .Version }}"
      - "--label=org.opencontainers.image.licenses=MIT"
      - "--build-arg=VERSION={{ .Version }}"
      - "--build-arg=COMMIT={{ .ShortCommit }}"
      - "--build-arg=DATE={{ .Date }}"
  - image_templates:
      - "ghcr.io/youyo/x:{{ .Version }}-arm64"
    use: buildx
    goos: linux
    goarch: arm64
    dockerfile: Dockerfile
    build_flag_templates:
      - "--platform=linux/arm64"
      - "--label=org.opencontainers.image.source=https://github.com/youyo/x"
      - "--label=org.opencontainers.image.version={{ .Version }}"
      - "--label=org.opencontainers.image.licenses=MIT"
      - "--build-arg=VERSION={{ .Version }}"
      - "--build-arg=COMMIT={{ .ShortCommit }}"
      - "--build-arg=DATE={{ .Date }}"

docker_manifests:
  - name_template: "ghcr.io/youyo/x:{{ .Version }}"
    image_templates:
      - "ghcr.io/youyo/x:{{ .Version }}-amd64"
      - "ghcr.io/youyo/x:{{ .Version }}-arm64"
  - name_template: "ghcr.io/youyo/x:latest"
    image_templates:
      - "ghcr.io/youyo/x:{{ .Version }}-amd64"
      - "ghcr.io/youyo/x:{{ .Version }}-arm64"
```

#### 設計判断
- **before hook に `go test ./...` を含める** — logvalet 流。リリース時点でテストが全部通っていることを保証。kintone は test なしだが M13 では x のテストカバレッジが高いので含める。
- **goos: windows 除外** — kintone は windows 同梱だが、x の用途 (Lambda / macOS / Linux dev) では windows 不要。後で需要が出たら追加。
- **brews と dockers は v0.1.0 でも準備する** — `goreleaser release --snapshot --clean` はローカル実行で skip するため snapshot で安全。実 release は M14 で実行する。
- **`{{ .ShortCommit }}`** を ldflags 注入: 既存 Dockerfile 互換性 (Dockerfile も `git rev-parse --short HEAD` を期待)。
- **docker manifests** で multi-arch を組成: kintone と同じパターン。

## TDD アプローチ

本マイルストーンはコード変更を伴わない (ドキュメント + リリース設定のみ)。テスト駆動は適用せず、代わりに以下の検証を実施:

### 検証コマンド
1. `goreleaser check` — yaml 構文 OK
2. `goreleaser release --snapshot --clean` — 全 platform binary 生成 + brew/docker のテンプレ展開エラーなし
3. `markdown-lint` 相当の目視確認: 見出し階層 / コードブロック言語指定 / リンク healthy
4. `go test ./...` & `golangci-lint run` — 既存テスト・lint 違反 0 を維持 (コード変更なしなので不変のはず)

### Acceptance criteria
- [ ] `README.md` 存在、すべてのセクション (12 個) が記載
- [ ] `README.ja.md` 存在、英版と等価
- [ ] `CHANGELOG.md` 存在、`[0.1.0]` セクションがすべての M1-M12 機能を網羅
- [ ] `LICENSE` 存在、MIT 標準テンプレート、Copyright Naoto Ishizawa / Heptagon
- [ ] `.goreleaser.yaml` 存在、`goreleaser check` で構文 OK
- [ ] `goreleaser release --snapshot --clean --skip docker,docker_manifest` がローカルでエラーなく完走 (Docker daemon 不要)
- [ ] `dist/` に darwin_amd64 / darwin_arm64 / linux_amd64 / linux_arm64 の各バイナリが生成
- [ ] `dist/x_<version>_Darwin_x86_64.tar.gz` 等の archives が生成、中に LICENSE / README*.md / CHANGELOG.md が同梱
- [ ] git commit (chore(release): v0.1.0 リリース準備)

#### Docker 検証 (deferred to M14)
- ローカル動作確認では `--skip docker,docker_manifest` で binary/archive/brew のみ検証
- 完全な Docker (ghcr.io) 検証は M14 で release.yml 経由の実 push で行う
- 既存 Dockerfile (multi-stage で `go build` する logvalet 流) を維持。goreleaser passes prebuilt binary in context but the Dockerfile ignores it (rebuilds from source). 2x rebuild は v0.1.0 で許容、必要なら M14 で kintone 流 (COPY prebuilt binary) に最適化

## 実装手順

### Step 1: LICENSE
標準 MIT テンプレート。Copyright "(c) 2026 Naoto Ishizawa / Heptagon"。

### Step 2: CHANGELOG.md
Keep a Changelog 形式。M1-M12 で追加された機能を `[0.1.0]` の Added にまとめる。日付は 2026-05-12 (現在日付)。

### Step 3: README.md (英)
12 セクション構成。Quick Start は 3 ステップ (configure → me → liked list)。

### Step 4: README.ja.md (日)
英版の日本語版。同等情報量、双方向リンク。

### Step 5: .goreleaser.yaml
上記設計通り。`goreleaser check` で構文確認。

### Step 6: 動作確認
- `goreleaser check`
- `goreleaser release --snapshot --clean` (Docker daemon 不要なら `--skip docker,docker_manifest`)
- `go test ./...`
- `golangci-lint run` (touch しないが念のため)
- `dist/` 中身確認

### Step 7: git commit
`chore(release): v0.1.0 リリース準備 (README/CHANGELOG/LICENSE/GoReleaser)`
フッター: `Plan: plans/x-m13-release-prep.md`

## リスク

| リスク | 対策 |
|---|---|
| `goreleaser release --snapshot --clean` で Docker daemon が必要 | ローカル動作確認時は `--skip docker,docker_manifest` で binary/archive/brew のみ検証。Docker 実 push は M14 / 本リリース時に確認 |
| brews の `HOMEBREW_TAP_GITHUB_TOKEN` 未設定 | snapshot モードでは brew push がスキップされるため問題なし。M14 で release.yml に secret 注入 |
| go test が新規導入により遅い | 既存テストのみ、< 30s で完走想定 |
| 日付ハードコード (`2026-05-12`) | CHANGELOG はリリース日付なので問題なし。実タグ作成は M14 で行うため日付は M14 時点で確定する。本 PR では現在日付を使う |

## ハンドオフ → M14

- **GoReleaser config 既存**: `.goreleaser.yaml` 完成。M14 はこれを `release.yml` から呼び出すだけ。
- **Homebrew tap repo**: `youyo/homebrew-tap` リポジトリの存在前提。logvalet/kintone と共有なので既存のはず。M14 で `HOMEBREW_TAP_GITHUB_TOKEN` (GitHub App PAT) を release.yml で発行する必要あり。
- **ghcr.io 権限**: release.yml に `permissions: packages: write` を付ける。Docker login は `docker/login-action@v3` で `${{ secrets.GITHUB_TOKEN }}`。
- **v0.1.0 タグ**: M14 で `git tag v0.1.0` → push で release.yml トリガー。
- **CHANGELOG 日付**: 万一 M14 でリリースが翌日以降になった場合、CHANGELOG の `[0.1.0] - 2026-05-12` を実リリース日に更新する手間が発生。M14 では現在日付を確認し必要なら修正コミットを 1 つ挟む。
