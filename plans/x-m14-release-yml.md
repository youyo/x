# M14: GitHub Actions release.yml + v0.1.0 タグ準備 詳細実装計画

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M14 (Phase D: v0.1.0 リリース) |
| スペック | `docs/specs/x-spec.md` §13 (Release Strategy) |
| 前提 | M13 完了 (commit 442fe68) — `.goreleaser.yaml` / README / CHANGELOG / LICENSE 整備済み |
| 参考実装 | `youyo/kintone:.github/workflows/release.yml` (docker + Homebrew), `youyo/logvalet:.github/workflows/release.yml` (Homebrew のみ) |
| ステータス | 計画中 → 実装へ |
| 作成日 | 2026-05-12 |

## Goal

タグ `v*` の push をトリガに、GoReleaser で以下を自動実行する CI ワークフローを整備する:

1. darwin/linux × amd64/arm64 のバイナリビルド + tarball
2. GitHub Releases へ成果物 (tarball + checksums.txt) を投稿
3. `youyo/homebrew-tap` リポジトリへ Formula を自動 PR / commit
4. `ghcr.io/youyo/x:{version, latest}` の multi-arch Docker image を push

⚠️ **このマイルストーンでは workflow ファイルの作成までを行う。実タグの push と CI 起動は GitHub remote を設定した将来時点で実施する** (本リポジトリは現状ローカルのみ)。

## Scope

### In scope

- `.github/workflows/release.yml` の新規作成
- ローカルでの YAML 構文 / actionlint 検証
- `goreleaser check` で `.goreleaser.yaml` との整合性を担保
- ロードマップへの「v0.1.0 タグ push 手順 (将来手動実施)」追記
- CHANGELOG `[0.1.0]` 日付の確認 (2026-05-12 で確定済み)

### Out of scope (将来)

- 実 `v0.1.0` タグの `git tag` / `git push` (リポジトリの remote 設定後にユーザー手動で実施)
- GitHub repository への push 作業全般
- Homebrew tap (`youyo/homebrew-tap`) 側の作成 (タップリポジトリは既存想定)
- GitHub Apps token 経路 (kintone/logvalet 流) — 本リポジトリは PAT で開始
- M15 以降の MCP 関連ジョブ追加

## 設計決定

### D-1: トリガは `tags: ['v*']` の push に限定

ci.yml は main への push / PR で走る。release.yml は **タグ push のみ** に限定し、責務分離する。これは kintone/logvalet と同じパターン。

### D-2: HOMEBREW_TAP_GITHUB_TOKEN は PAT (Personal Access Token) で開始

kintone/logvalet は GitHub Apps token 生成 (tibdex/github-app-token) を使うが、本リポジトリ単体では Apps 設定が未整備のため:

- **v0.1.0 では PAT (`HOMEBREW_TAP_GITHUB_TOKEN` secret) で運用開始**
- 将来 Apps 移行は別マイルストーンで実施 (差し替えは workflow 1 行変更で済む)

PAT の権限: `repo` scope (youyo/homebrew-tap への push)。

### D-3: ghcr.io 認証は `GITHUB_TOKEN` (デフォルト)

- `permissions: packages: write` を付与すれば追加 secret 不要
- `docker/login-action@v3` の `password: ${{ secrets.GITHUB_TOKEN }}` でログイン

### D-4: actions のバージョン pinning 方針

kintone/logvalet は SHA pinning (Dependabot 連携想定) しているが、本リポジトリ v0.1.0 では **メジャータグ pin** (`@v4` / `@v5` / `@v6` / `@v3`) で開始。

理由:
- セキュリティリスクは低い (公式 actions のみ使用)
- Dependabot 設定が未整備
- メジャータグ pin は GoReleaser 公式ドキュメントの推奨形式と整合
- 将来 SHA pin に切り替えるなら別マイルストーンで `pinact` 等を導入

### D-5: setup-go の go-version 指定方法

- ci.yml は `GO_VERSION: "1.26.1"` を env で固定
- release.yml は **`go-version-file: go.mod`** を使用 (kintone/logvalet と同じ)

理由:
- go.mod は `go 1.26.1` で ci.yml の `GO_VERSION` と一致 (現状)
- GoReleaser の `before.hooks` で `go mod tidy` が走るため go.mod が信頼源として最も安全
- go.mod のバージョン更新時に release.yml の env を忘れず更新する保守コストを排除
- 万一 ci.yml と go.mod がドリフトしてもビルド再現性は `go.mod` を信頼源として保持される

### D-6: QEMU + buildx の併用

`.goreleaser.yaml` の dockers セクションで `use: buildx` + `--platform=linux/{amd64,arm64}` を指定しているため:

- `docker/setup-qemu-action@v3` で multi-arch エミュレーション有効化
- `docker/setup-buildx-action@v3` で buildx builder セットアップ

両方が必要 (kintone と同じ構成)。

### D-7: v0.1.0 タグ push 作業の文書化方針

タグ push は本マイルストーンでは実施しないが、将来作業のために手順書をロードマップ M14 セクションにインライン記載する。形式は以下:

```bash
# 1. リポジトリを GitHub に push (初回のみ)
git remote add origin https://github.com/youyo/x.git
git push -u origin main

# 2. v0.1.0 タグを作成・push
git tag -a v0.1.0 -m "v0.1.0: 初回リリース (CLI v0.1.0)"
git push origin v0.1.0

# 3. GitHub Actions の `release` ワークフローが走ることを確認
gh run watch  # または https://github.com/youyo/x/actions
```

## ファイル設計

### `.github/workflows/release.yml`

```yaml
# リリースワークフロー
#
# トリガー: v* タグ push のみ。
# ジョブ: goreleaser 単一ジョブ。darwin/linux × amd64/arm64 バイナリ + tarball、
#         GitHub Releases 投稿、Homebrew tap 更新、ghcr.io multi-arch image push。
#
# 必須 secrets:
#   - GITHUB_TOKEN: GitHub Releases 投稿 + ghcr.io login (デフォルト)
#   - HOMEBREW_TAP_GITHUB_TOKEN: youyo/homebrew-tap への push 用 PAT (repo scope)
#
# permissions:
#   - contents: write   GitHub Releases 投稿に必要
#   - packages: write   ghcr.io へのイメージ push に必要

name: release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write
  packages: write

jobs:
  goreleaser:
    name: GoReleaser
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0  # 全タグ参照 (changelog 生成に必要)

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true

      - name: Set up QEMU (multi-arch docker)
        uses: docker/setup-qemu-action@v3

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Login to ghcr.io
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Run GoReleaser
        uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}
```

## TDD / 検証

### 自動検証 (CI 相当の事前確認)

1. **`goreleaser check`** — `.goreleaser.yaml` が正しいことを確認 (M13 で pass 済み、M14 でも回帰しないことを担保)
2. **`actionlint .github/workflows/release.yml`** — yml 構文 + 既知 actions の引数チェック
3. **`go test ./...`** — 既存 Go コードを壊さない (M14 はファイル追加のみのため変化しないことを確認)
4. **`go vet ./...`** — 0 warning
5. **`golangci-lint run`** — 0 issue (yml は対象外だが念のため回帰確認)

### 手動検証 (将来タグ push 時)

- GitHub Actions の `release` ワークフローが green になる
- `gh release view v0.1.0` で 4 platform tarball + checksums.txt 確認
- `brew install youyo/tap/x && x version` が動作
- `docker pull ghcr.io/youyo/x:v0.1.0 && docker run --rm ghcr.io/youyo/x:v0.1.0 version`

## 実装ステップ

1. **検証ツール準備確認**
   - `which goreleaser actionlint` — どちらもインストール済み (mise 配下)
2. **`.github/workflows/release.yml` 作成**
   - 上記 yaml 設計をそのまま書き出し
3. **構文検証**
   - `actionlint .github/workflows/release.yml`
   - `actionlint .github/workflows/ci.yml` (既存も同時検証)
4. **goreleaser 設定検証**
   - `goreleaser check`
5. **既存 Go コード回帰確認**
   - `go test ./...`
   - `go vet ./...`
   - `golangci-lint run` (任意)
6. **ロードマップ更新**
   - M14 チェック項目を [x] に変更
   - 「v0.1.0 タグ push 手順 (将来手動実施)」を M14 セクションに追記
7. **commit**
   - `chore(ci): v0.1.0 リリース用 GitHub Actions workflow を追加`
   - フッターに `Plan: plans/x-m14-release-yml.md`

## リスクと緩和策

| リスク | 影響 | 緩和策 |
|---|---|---|
| GoReleaser v6 と `goreleaser-action@v6` のバージョン乖離 | 中 | `args: release --clean` のみ使用、`version: latest` で goreleaser CLI 最新を採用 |
| HOMEBREW_TAP_GITHUB_TOKEN secret が未設定で release 失敗 | 中 | ロードマップ手順書に必須 secret 一覧を記載 (PAT 生成手順含む) |
| ghcr.io への push 権限不足 | 低 | `permissions: packages: write` を明示。GITHUB_TOKEN は自動付与 |
| タグ push 後に CI が動かない (workflow 未配置) | 高 | **タグ push は workflow を main に push してから実施** することを手順書に明記 |
| actionlint が新しい action のメジャー bump に追従していない | 低 | エラーは警告レベル。CI で再検証可能。M14 では既知 v3/v4/v5/v6 のみ使用 |
| go-version-file の go.mod が toolchain ディレクティブを持つ場合の挙動 | 低 | `actions/setup-go@v5` は `go-version-file` で go ディレクティブを読む。toolchain は別管理 |

## 完了条件 (Definition of Done)

- [ ] `.github/workflows/release.yml` を作成し、actionlint が pass する
- [ ] `goreleaser check` が pass する (回帰なし)
- [ ] `go test ./... && go vet ./...` が pass する (既存テスト数を維持)
- [ ] ロードマップの M14 完了マーク + タグ push 手順書追記
- [ ] commit 完了 (Plan フッター付き)
- [ ] v0.1.0 タグの実 push は将来作業として明記し、本マイルストーンでは行わない

## 次のマイルストーン (M15) へのハンドオフ予定

- `internal/mcp/` 配下のパッケージを新規作成。`server.go` で mark3labs/mcp-go の `server.NewMCPServer` ファクトリを公開
- `internal/transport/http/` 配下に Streamable HTTP transport を新設
- 既存 cobra root (`internal/cli/root.go`) に `mcp` サブコマンドを追加する設計を M24 で行うため、M15 は `internal/` 純粋実装に集中
- パッケージ docs ルール: `// Package mcp implements ...` 形式の doc.go を必須化
- TDD: `server_test.go` で `initialize` リクエストの response shape を契約テストする
