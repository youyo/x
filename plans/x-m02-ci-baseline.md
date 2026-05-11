# M2: CI 軽量基盤 + Lint + Dockerfile

> Layer 2: M2 マイルストーン詳細計画。Layer 1 ロードマップは [plans/x-roadmap.md](./x-roadmap.md) を参照。
> 前マイルストーン: [plans/x-m01-bootstrap.md](./x-m01-bootstrap.md) (commit e83b70d)

## 1. Overview

| 項目 | 値 |
|---|---|
| マイルストーン | M2 |
| ステータス | Planned |
| バージョン | 0.0.2 (CI baseline) |
| 依存 | M1 (Cobra スケルトン + 25 テスト) |
| 後続マイルストーン | M3 (`internal/config` XDG loader) |
| 対象モジュール | `github.com/youyo/x` |
| Go バージョン | 1.26.1 (mise 管理) |
| golangci-lint | v2.x (v2 系の `.golangci.yml` フォーマット) |
| 追加コード依存 | なし (CI / lint / Docker は外部ツール設定のみ) |
| 検証方式 | DoD (Definition of Done) ベース。ファイル新規作成 + 既存テスト維持 |
| 対象ファイル数 | 4 ファイル新規 (`.github/workflows/ci.yml` / `.golangci.yml` / `Dockerfile` / `.dockerignore`) |
| LOC 目安 | ~250 (設定ファイルが主) |
| 参考実装 | logvalet / kintone (同 author の OSS。Distroless + golangci-lint + GitHub Actions の構成例) |

### M2 で**やること**
- `.github/workflows/ci.yml` — Push (main) / PR 両トリガーで `lint` / `test` / `build` / `docker` 4 ジョブ
- `.golangci.yml` — Go の標準 linter セット + revive / gocritic 等のシンプル構成 (v2 系フォーマット)
- `Dockerfile` — Multi-stage build (`golang:1.26.1-alpine` → `gcr.io/distroless/static-debian12:nonroot`)、`ARG VERSION/COMMIT/DATE` → ldflags 注入
- `.dockerignore` — `.git/` / `dist/` / `*.test` / IDE 設定ファイル等

### M2 で**やらないこと** (将来マイルストーン)
- `goreleaser` (→ M13 で `.goreleaser.yaml` を追加)
- `release.yml` (タグ Push → GoReleaser → ghcr/Homebrew) (→ M14)
- `Dockerfile` の Production チューニング (multi-arch / Lambda 用 `provided.al2023`) (→ M14 / M23)
- カバレッジレポート / codecov 統合 (M3 でテスト群が増えた後で再検討)
- `Makefile` (現状は `go test` / `golangci-lint run` / `docker build` のコマンドが短いので不要)

## 2. Goal

### 機能要件
- [ ] PR / Push (main) で GitHub Actions が起動し、4 ジョブ (`lint` / `test` / `build` / `docker`) が全 green
- [ ] `golangci-lint run ./...` がローカルでも CI でも違反 0
- [ ] `go test -race -count=1 ./...` が CI で pass (M1 の 25 テストを維持)
- [ ] `go build ./...` が CI で成功 (バイナリ生成は dist 不要、コンパイル確認のみ)
- [ ] `docker build -t x:dev .` が成功し、イメージサイズ < 50MB (distroless static-debian12 ベース)
- [ ] `docker run --rm x:dev version` が JSON 出力 `{"version":"dev","commit":"none","date":"unknown"}` を返す
- [ ] `docker build` で `VERSION` / `COMMIT` / `DATE` を `--build-arg` 経由で ldflags 注入できる

### 非機能要件
- [ ] **LANG=C 環境変数を CI 全ジョブで固定** (M1 hand-off: `isArgumentError` が英語ロケール依存)
- [ ] CI 全体のランタイム ≤ 3 分 (キャッシュなし時) / ≤ 1 分 (キャッシュヒット時)
- [ ] golangci-lint のキャッシュ (`golangci/golangci-lint-action@v9` 内蔵) と Go モジュールキャッシュ (`actions/setup-go@v5` の `cache: true`) を有効化
- [ ] Dockerfile は `--no-cache` build でも 60 秒以内で完了 (alpine + distroless で最小依存)
- [ ] イメージは **nonroot 実行** (`gcr.io/distroless/static-debian12:nonroot`)、UID 65532
- [ ] `.dockerignore` で `.git/` を除外し、ビルドコンテキスト軽量化

## 3. 技術スタック

| カテゴリ | 採用 | 理由 |
|---|---|---|
| CI | GitHub Actions | OSS リポジトリ標準。Marketplace に `golangci/golangci-lint-action` / `actions/setup-go` が揃う |
| Lint | golangci-lint v2.x | logvalet/kintone と統一。v2 系で設定フォーマット刷新済み (`version: "2"`) |
| Linter セット (linters.enable) | govet / errcheck / ineffassign / staticcheck / unused / revive / gocritic / misspell | Go 標準 + コードスタイル一貫性。`gocyclo` 等の複雑度系は M3 以降に再検討 |
| Formatter セット (formatters.enable) | gofumpt | v2 では gofmt/gofumpt/gci/goimports は formatter として分離 |
| Container | Docker multi-stage | builder + distroless 構成は logvalet/kintone と統一 |
| Base image | `gcr.io/distroless/static-debian12:nonroot` | CGO=0 静的バイナリ前提。`nonroot` タグで UID 65532 実行、CVE 攻撃面最小化 |
| Build image | `golang:1.26.1-alpine` | mise.toml の Go 1.26.1 と pin。alpine で依存最小 |

### golangci-lint v2.x 採用理由
- ローカルにすでに v2.11.4 がインストール済み (mise 経由)
- v2 系は `linters.default` で baseline を宣言し、個別 enable/disable できる
- v1 形式の `.golangci.yml` は v2 では deprecate のみで動作するが、新規ファイルなので最初から v2 形式で書く

## 4. ファイル設計

### 4.1 `.github/workflows/ci.yml`

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read
  pull-requests: read  # golangci-lint-action が PR コメント用に要求 (only-new-issues 機能)

env:
  GO_VERSION: "1.26.1"
  LANG: "C"  # M1 hand-off: isArgumentError が英語ロケール依存

jobs:
  lint:
    name: Lint (golangci-lint)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v9
        with:
          version: v2.11
          args: --timeout=5m

  test:
    name: Test (race)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true
      - run: go test -race -count=1 ./...

  build:
    name: Build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true
      - run: go build ./...

  docker:
    name: Docker build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3
      - name: Build image
        uses: docker/build-push-action@v6
        with:
          context: .
          push: false
          tags: x:ci
          build-args: |
            VERSION=ci
            COMMIT=${{ github.sha }}
            DATE=${{ github.run_started_at }}
          load: true
      - name: Smoke test image
        run: |
          docker run --rm x:ci version
```

**設計判断**:
- ジョブ並列度: `lint` / `test` / `build` / `docker` 4 ジョブ独立並列。`docker` は他に依存しない (CI 完走時間短縮)
- `LANG=C` を `env:` トップレベルに置き、全ジョブで継承
- `golangci/golangci-lint-action@v9` (v9 系で golangci-lint v2 設定を正式サポート、v6/v7/v8 は v1 系のみ) は内蔵キャッシュあり、`only-new-issues: true` は M1 互換性のため一旦オフ (M2 で全違反 0 にする方針)
- `DATE` 注入には `github.run_started_at` (GitHub Actions が提供する ISO8601 タイムスタンプ) を使用。`repository.updated_at` はリポジトリ最終更新時刻でビルド時刻ではないため誤り (advisor 指摘)
- `docker/build-push-action@v6` で `load: true` にしてローカル daemon にイメージを反映し、`docker run x:ci version` を smoke test
- `permissions: contents: read` で最小権限

### 4.2 `.golangci.yml`

```yaml
version: "2"

run:
  timeout: 5m
  tests: true

linters:
  default: none
  enable:
    - errcheck      # 戻り値のエラーチェック漏れ検出
    - govet         # go vet 標準解析
    - ineffassign   # 無意味な代入検出
    - staticcheck   # SA/ST 系の高品質静的解析 (gosimple/stylecheck を含む)
    - unused        # 使われていない関数/変数/定数検出
    - revive        # golint 後継、可読性ルール
    - gocritic      # ベストプラクティス・パフォーマンス警告
    - misspell      # 英語綴り誤り検出 (技術用語辞書あり)

  settings:
    revive:
      rules:
        - name: exported            # 公開シンボルに doc コメント必須
        - name: package-comments    # パッケージドキュメント必須
        - name: var-naming
        - name: error-return
        - name: error-strings
        - name: error-naming
        - name: indent-error-flow
        - name: unexported-return
        - name: range
        - name: receiver-naming
        - name: time-naming
        - name: errorf
        - name: increment-decrement
    gocritic:
      enabled-tags:
        - diagnostic
        - performance
        - style
      disabled-checks:
        - hugeParam    # 大きな構造体の値渡し警告 (本プロジェクトでは小さい構造体中心なので off)
        - rangeValCopy # range の値コピー警告 (DTO の DeepCopy 警告と重複)

  exclusions:
    rules:
      - path: _test\.go
        linters:
          - errcheck    # テスト内の意図的なエラー無視を許容
          - gocritic    # テストヘルパーの命名規則を緩和
      - path: cmd/
        linters:
          - revive
        text: "package-comments"   # cmd/x/main.go は doc.go 不要

formatters:
  enable:
    - gofumpt       # gofmt の strict 版 (v2 では formatters セクションに分離)
  settings:
    gofumpt:
      module-path: github.com/youyo/x

issues:
  max-issues-per-linter: 0
  max-same-issues: 0
```

**設計判断**:
- v2 系の `linters.default: none` で全 OFF → `enable` で明示的に追加 (将来の linter 追加で意図せず警告が増えないように)
- `staticcheck` は gosimple / stylecheck を含むため重複 enable しない (v2 系の挙動)
- `revive` のルールは `golint` 後継として最重要 12 個を選定。`exported` で doc コメント必須化は M1 既存コードの慣例と一致
- `gocritic` の `hugeParam` / `rangeValCopy` は将来 DTO 設計時の偽陽性を避けるため off
- テストファイルでは `errcheck` / `gocritic` を緩和 (`SetOut(buf)` 等のエラー無視を許容)
- `cmd/x/main.go` の `package main` には doc コメント (`// Command x ...`) があるため `package-comments` は除外不要だが、念のため `cmd/` 配下で例外化
- **`gofumpt` は v2 系で `formatters.enable` に配置** (`linters.enable` ではない。`gci` / `gofmt` / `gofumpt` / `goimports` の 4 つは v2 で formatter として再分類された。advisor + Context7 v2 公式 docs より確認)

### 4.3 `Dockerfile`

```dockerfile
# syntax=docker/dockerfile:1.7

# --- builder stage ---
FROM golang:1.26.1-alpine AS builder

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

WORKDIR /src

# 依存だけ先に取得してキャッシュ効率化
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO=0 で完全静的バイナリ。distroless static にデプロイ可能。
ENV CGO_ENABLED=0 GOOS=linux

RUN go build \
    -trimpath \
    -ldflags "-s -w \
        -X github.com/youyo/x/internal/version.Version=${VERSION} \
        -X github.com/youyo/x/internal/version.Commit=${COMMIT} \
        -X github.com/youyo/x/internal/version.Date=${DATE}" \
    -o /out/x ./cmd/x

# --- final stage ---
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/x /x

USER nonroot:nonroot
ENTRYPOINT ["/x"]
```

**設計判断**:
- `# syntax=docker/dockerfile:1.7` で BuildKit の機能フラグを最新化 (キャッシュ最適化、`COPY --link` 等)
- ビルダーは `alpine` (CGO 不要なので `bullseye` 不要)
- `go mod download` を `COPY . .` より前に実行 → 依存変更がない限りキャッシュヒット
- `CGO_ENABLED=0` で完全静的バイナリ生成、`distroless/static-debian12:nonroot` に乗る
- `-trimpath` で絶対パス情報をバイナリから除去 (再現性 + プライバシー)
- `-ldflags "-s -w"` でデバッグシンボル削除 (バイナリサイズ削減)
- `nonroot` タグ + `USER nonroot:nonroot` で UID 65532 実行 (CVE 対策)
- `ENTRYPOINT ["/x"]` だけにし、`CMD` は付けない (引数なしで `x` を呼ぶと Cobra デフォルト help が出る)

### 4.4 `.dockerignore`

```
# Git
.git/
.gitignore
.gitattributes

# Build artifacts
/x
/dist/
*.test
*.out
coverage.out

# IDE / editor
.idea/
.vscode/
*.swp
.DS_Store

# Documentation / plans (build に不要)
docs/
plans/
*.md

# CI 設定 (build に不要)
.github/
.golangci.yml

# Go workspace (vendor 不使用)
go.work
go.work.sum
vendor/
```

**設計判断**:
- `.git/` 除外でビルドコンテキスト軽量化 (commit SHA は `--build-arg COMMIT` で渡す)
- `docs/` `plans/` `*.md` 除外でビルドキャッシュの誤無効化を防止 (README 編集だけで Docker キャッシュが飛ばない)
- `.github/` `.golangci.yml` 除外で CI 設定変更がビルドに影響しない
- `vendor/` は将来 vendor 化した場合のガード

## 5. TDD / 検証戦略

CI / Lint / Docker は外部ツールなので、ユニットテストの Red → Green → Refactor ではなく **DoD ベース** で検証する。

### 5.1 検証手順 (ローカル)

```bash
# 1. lint (既存コード違反 0 を確認)
cd /Users/youyo/src/github.com/youyo/x
golangci-lint run ./...

# 2. test (M1 の 25 テスト維持)
go test -race -count=1 ./...

# 3. build (コンパイル確認)
go build ./...

# 4. Docker build + smoke test
docker build -t x:dev .
docker run --rm x:dev version
# 期待: {"version":"dev","commit":"none","date":"unknown"}

# 5. Docker build w/ build args
docker build \
  --build-arg VERSION=v0.0.2 \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  --build-arg DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t x:dev .
docker run --rm x:dev version
# 期待: VERSION/COMMIT/DATE が注入されている

# 6. イメージサイズ確認
docker images x:dev --format "{{.Size}}"
# 期待: < 50MB
```

### 5.2 lint 違反対応方針

`revive` の `exported` ルールにより M1 既存コードで違反が出る場合:
- 違反 0 になるよう既存ファイルにドキュメンテーションコメント追加 (M1 で既に整備済みのはず)
- どうしても合理化できない違反は `//nolint:revive` をピンポイントで適用 (理由コメント必須)

`gofumpt` 違反対応:
- `gofumpt -l ./...` で違反ファイル一覧 → `gofumpt -w ./...` で自動修正

### 5.3 CI green 確認方針

- ローカルで 5.1 全 step pass → `git push` で main にプッシュ (実 push は本マイルストーンの範囲外。CI 動作確認は次マイルストーン以降の PR で実証)
- 本マイルストーンでは **コミットまで** で完了とし、CI green 確認は M3 着手時の PR で行う方針 (M2 のコミット内で `.github/workflows/ci.yml` が走ることはないため)

## 6. リスク評価

| リスク | 影響 | 緩和策 |
|---|---|---|
| `revive: exported` で M1 既存コードに違反が出る | 中 | M1 で全公開シンボルに doc コメント整備済み。万一抜けがあれば即座に追記 |
| `gofumpt` で既存コードに違反が出る | 中 | `gofumpt -w ./...` で自動修正。`gofmt` 標準と差分が小さいため軽微 |
| `gocritic` 偽陽性 | 中 | `hugeParam` / `rangeValCopy` を最初から off。他の偽陽性は `//nolint:gocritic` をピンポイント |
| Docker `--platform linux/arm64` Mac で動作確認できない | 低 | デフォルト `linux/amd64` で確認 (CI の ubuntu-latest と同じ)。multi-arch は M14 (release.yml) で対応 |
| `golangci-lint-action` のバージョン互換性 | 高 (解決済) | **v9 以降で golangci-lint v2 系設定を正式サポート** (v6/v7/v8 は v1 系のみ)。Context7 公式 docs で確認済み |
| Distroless image の glibc 非互換 | 低 | CGO_ENABLED=0 で完全静的バイナリ → glibc / musl 一切不要 |
| `LANG=C` 設定漏れ | 高 | `env:` をトップレベルに配置して全ジョブ継承。`isArgumentError` の言語依存は M1 既知制約 |
| GitHub Actions の secrets 漏洩 | 低 | 本マイルストーンで secrets は使わない。`X_API_KEY` 等は M9+ で必要 |
| Docker キャッシュが効かない (CI が毎回フル build) | 低 | `docker/build-push-action@v6` の `cache-from`/`cache-to` は M14 で導入。M2 ではキャッシュなしでも 60 秒以内目標 |

## 7. M3 への引き継ぎ事項

### 確立されたパターン
- **CI 構造**: `lint` / `test` / `build` / `docker` 4 ジョブ並列。新規ジョブは独立 job で追加
- **lint policy**: M2 で baseline 確立。M3 以降は違反 0 を維持
- **環境変数**: `LANG=C` は今後も必須 (cobra エラーメッセージ判定が英語ロケール依存)
- **Docker**: `Multi-stage builder + distroless:nonroot` パターンを踏襲。M14 で multi-arch / ghcr.io push を追加

### 制約 (継承)
- M1: `isArgumentError` が英語ロケール依存 → `LANG=C` で固定
- M1: コミット粒度 = 単一論理単位 = 1 コミット

### 次マイルストーン (M3) で必要な情報
- `internal/config/` の追加で linter violations が増えないよう M2 の `.golangci.yml` を引き継ぐ
- `internal/config/xdg.go` で `os.UserConfigDir()` を使う場合は `errcheck` が要求するエラーチェックを忘れない
- M3 で BurntSushi/toml を追加する際、`gocritic` の `regexpMust` 等を新たに踏まないよう注意

## 8. 完了判定 (DoD)

- [ ] `.github/workflows/ci.yml` 作成 (lint/test/build/docker 4 ジョブ + LANG=C)
- [ ] `.golangci.yml` 作成 (v2 形式、enable 一覧確定)
- [ ] `Dockerfile` 作成 (multi-stage + distroless:nonroot + ldflags 注入)
- [ ] `.dockerignore` 作成
- [ ] ローカルで `golangci-lint run ./...` 違反 0
- [ ] ローカルで `go test -race -count=1 ./...` pass (M1 の 25 テスト維持)
- [ ] ローカルで `go build ./...` 成功
- [ ] ローカルで `docker build -t x:dev .` 成功 (60 秒以内)
- [ ] ローカルで `docker run --rm x:dev version` が JSON 出力
- [ ] ローカルで `docker images x:dev` のサイズ < 50MB
- [ ] `--build-arg VERSION/COMMIT/DATE` の注入が `docker run x:dev version` に反映
- [ ] commit (push しない)
  - 例: `chore(ci): GitHub Actions CI と golangci-lint と Dockerfile を追加`
  - フッターに `Plan: plans/x-m02-ci-baseline.md`
