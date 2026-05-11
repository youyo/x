# Roadmap: x — X (Twitter) API CLI & Remote MCP

> Layer 1: プロジェクト全体ロードマップ。マイルストーン一覧と Current Focus を管理する。
> M1 詳細計画は [plans/x-m01-bootstrap.md](./x-m01-bootstrap.md) を参照。

## Context

毎日、前日に X (旧 Twitter) で Like した Post の中から「技術的に検証すべき内容」を抽出し、Backlog (HEP_ISSUES) に課題化する。実行基盤は Anthropic 公式の Claude Code Routines (research preview)。既存の X MCP はすべて stdio ローカル用なので、Remote MCP (Streamable HTTP) を Go で新規実装する。

設計方針: **「CLI コア + MCP は薄いラッパー」**。logvalet / kintone と同じ構造で、`github.com/youyo/x` リポジトリに単一バイナリ `x` を OSS 公開する。スペックは `docs/specs/x-spec.md` (v1.0.0 Approved) に確定済み。

## Meta

| 項目 | 値 |
|---|---|
| ゴール | Routines → X MCP → Backlog の自動化基盤を OSS として公開する |
| 成功基準 | (1) v0.1.0: CLI でローカル動作 (2) v0.2.0: MCP 3 モード認証で動作 (3) v0.3.0: lambroll サンプル + docs |
| 制約 | TDD 必須 / Go 1.26.x / mark3labs/mcp-go / **cobra (kong から変更)** / idproxy 4 store / OSS 公開 |
| 対象リポジトリ | `/Users/youyo/src/github.com/youyo/x` |
| 作成日 | 2026-05-12 |
| 最終更新 | 2026-05-12 |
| ステータス | 進行中 (M1 詳細計画完了、実装未着手) |
| スペック | `docs/specs/x-spec.md` (v1.0.0 Approved) |
| マイルストーン総数 | 28 (細粒度方針) |

## Current Focus

- **マイルストーン**: M1 (リポジトリ初期化 + Kong スケルトン + `x version`)
- **直近の完了**: ロードマップ作成 + M1 詳細計画作成
- **次のアクション**: 本計画承認後、`/devflow:implement` で M1 着手

## Spec Update Required (本ロードマップ作成時の確定追加事項)

ユーザー要望により、ExitPlanMode 後に `docs/specs/x-spec.md` を以下のように更新する:

1. **§11 環境変数表**: `STORE_BACKEND` の選択肢を `memory / sqlite / redis / dynamodb` に拡張
2. **§11 環境変数表**: sqlite/redis 用環境変数を追記
   - `SQLITE_PATH` (sqlite 時、default: `~/.local/share/x/idproxy.db`)
   - `REDIS_URL` (redis 時、例: `redis://localhost:6379/0`)
3. **§10 外部依存**: sqlite (modernc.org/sqlite, pure Go) / redis (go-redis/redis/v9) を追記 (どちらも optional)
4. **§3 フェーズ2 展望**: 「ユーザー別 OAuth 2.0 PKCE トークンも同じ Store interface に乗せる」を追記
5. **§5 Architecture**: `internal/authgate/idproxy.go` の説明に「memory / sqlite / redis / dynamodb の 4 store backend をサポート」と明記
6. **ADR #11 追加**: idproxy の 4 store backend を全部サポートする理由 (ローカル開発=sqlite、軽量サーバー=redis、Lambda=dynamodb、テスト=memory) を記載
7. **ADR #4 修正 (kong → cobra)**: CLI パーサを **cobra** に変更。理由は (i) `__complete` による動的補完が標準機能でビルトイン (Kong は自前実装)、(ii) viper 統合で env > flag > config の階層解決が宣言的、(iii) Go コミュニティのデファクト (kubectl/gh/hugo/docker) で OSS 公開時のコントリビュータ獲得に有利、(iv) ヘルプ・マンページ・補完の標準化。却下: kong (logvalet/kintone と統一感は失うが、OSS リーチを優先)
8. **§10 必須技術スタック**: `github.com/alecthomas/kong` を `github.com/spf13/cobra v1.x` に置換
9. **§6 CLI**: `x completion {bash,zsh,fish,powershell}` を Cobra 標準の出力に揃える (`x` バイナリは PowerShell も無料で対応可能)

## Progress

### Phase A: 基盤整備

#### M1: リポジトリ初期化 + Cobra スケルトン + `x version` ✅ 完了 (commit: e83b70d)
- [x] go mod init / mise.toml / .gitignore
- [x] `internal/app` (exit code 6 個)
- [x] `internal/version` (ldflags 注入)
- [x] `internal/cli/root.go` (Cobra root + Version)
- [x] `internal/cli/version.go` (`x version` JSON/human)
- [x] Cobra 標準 completion (bash/zsh/fish/powershell) — 自前実装不要
- [x] `cmd/x/main.go` (Cobra Execute + exit code 伝播)
- 完了: `x version` / `x --version` / `x completion {bash,zsh,fish,powershell}` / `x __complete` 動作確認、25 テストケース全 pass

#### M2: CI 軽量基盤 + Lint + Dockerfile
- [ ] `.github/workflows/ci.yml` (lint + test on PR/push)
- [ ] `.golangci.yml`
- [ ] `Dockerfile` (multi-stage, distroless)
- [ ] `.dockerignore`
- 完了条件: CI が green、`docker build` 成功
- TDD 観点: M3 以降のテスト追加時に常時 lint+race+coverage を強制

#### M3: `internal/config` XDG loader (非機密設定のみ)
- [ ] `internal/config/xdg.go` (XDG_CONFIG_HOME / XDG_DATA_HOME 解決)
- [ ] `internal/config/loader_cli.go` (config.toml 読み込み, BurntSushi/toml)
- [ ] `internal/config/config.go` (CLIConfig 型定義)
- 完了条件: `[cli]` / `[liked]` セクションを読み込み、env override も動作
- TDD: シェルでない一時ディレクトリで XDG_CONFIG_HOME を上書きする table-driven テスト

#### M4: `internal/config` credentials.toml + guard
- [ ] `internal/config/credentials.go` (perm 0600 強制、R/W)
- [ ] `internal/config/guard.go` (config.toml にシークレット混入を拒否)
- 完了条件: パーミッション緩い credentials.toml に警告、シークレットが config.toml に書かれていたらエラー
- TDD: ファイル mode を `os.Chmod` で意図的に緩めて検査ロジックを試す

### Phase B: X API クライアント

#### M5: `internal/xapi/oauth1.go` (dghubble/oauth1 ラッパー + 署名検証)
- [ ] OAuth 1.0a HMAC-SHA1 署名
- [ ] Twitter 公式テストベクトルで署名一致
- 完了条件: `dghubble/oauth1.Config` ラッパーが標準テストケースで pass

#### M6: `internal/xapi/client.go` (retry + rate-limit aware HTTP)
- [ ] HTTP client (retry, exponential backoff, max 3 retry, max 30s)
- [ ] `x-rate-limit-remaining` / `x-rate-limit-reset` ヘッダパース
- [ ] エラーマッピング → exit code 3/4/5
- 完了条件: httptest で 429/5xx/401/403/404 の各シナリオを TDD で検証

#### M7: `internal/xapi/users.go` (GetUserMe + DTO)
- [ ] `Tweet` / `User` / `Meta` DTO 定義
- [ ] `GetUserMe(ctx)` 実装
- 完了条件: httptest mock で `/2/users/me` が users.go に渡る統合テスト pass

#### M8: `internal/xapi/likes.go` (ListLikedTweets + ページネーション)
- [ ] `ListLikedTweets(ctx, params)` シングルページ
- [ ] `--all` 用の next_token 自動辿り iterator
- [ ] `--max-pages` 上限
- [ ] ページ間 sleep (rate-limit remaining ≤ 2 で reset まで待機)
- 完了条件: 5 ページのモックレスポンスを全部辿り、429 → reset 待機 → 続きを取得する E2E テスト

### Phase C: CLI v0.1.0

#### M9: CLI `x me`
- [ ] `internal/cli/me.go`
- [ ] JSON / human 出力
- 完了条件: `x me` で自分の user_id 出力、--no-json で human

#### M10: CLI `x liked list` 基本フラグ
- [ ] `internal/cli/liked.go`
- [ ] `--user-id` / `--start-time` / `--end-time` / `--max-results` / `--pagination-token`
- [ ] JSON 出力
- 完了条件: 基本フラグでシングルページ取得が動作

#### M11: CLI `x liked list` 拡張
- [ ] `--since-jst <YYYY-MM-DD>` / `--yesterday-jst`
- [ ] `--all` + `--max-pages` (default 50)
- [ ] NDJSON 出力 (`--ndjson`)
- [ ] tweet/expansion/user fields のカスタマイズフラグ
- 完了条件: `x liked list --yesterday-jst --all` で前日分を全取得

#### M12: CLI `x configure` + `x completion` 拡張
- [ ] 対話モード (XDG パス 2 ファイル生成)
- [ ] `--print-paths` / `--check`
- [ ] credentials.toml 既存時の保護
- 完了条件: `x configure` で初期セットアップが完結し、`x configure --check` で構成検証可能

### Phase D: v0.1.0 リリース

#### M13: README / CHANGELOG / LICENSE / GoReleaser
- [ ] `README.md` (English) / `README.ja.md` (日本語)
- [ ] `CHANGELOG.md` (Keep a Changelog 形式)
- [ ] `LICENSE` (MIT)
- [ ] `.goreleaser.yaml` (darwin/linux × amd64/arm64, Homebrew tap, ghcr.io)
- 完了条件: `goreleaser release --snapshot --clean` 成功

#### M14: GitHub Actions release.yml + v0.1.0 タグ
- [ ] `.github/workflows/release.yml` (tag push → GoReleaser + Homebrew + ghcr.io)
- [ ] `v0.1.0` タグ作成
- 完了条件: GitHub Releases に成果物が掲載、`brew install youyo/tap/x` で動作確認

### Phase E: MCP コア

#### M15: MCP サーバー雛形 + transport/http
- [ ] `internal/mcp/server.go` (`NewServer(client, ver)` ファクトリ, mark3labs/mcp-go)
- [ ] `internal/transport/http/server.go` (Streamable HTTP + graceful shutdown, LWA 互換)
- 完了条件: 空サーバーが起動・`initialize` リクエストに応答

#### M16: authgate 基盤 + none モード
- [ ] `internal/authgate/gate.go` (Middleware interface)
- [ ] `internal/authgate/none.go` (passthrough)
- 完了条件: middleware framework が確立、認証 OFF モードで動作

#### M17: MCP tool `get_user_me`
- [ ] `internal/mcp/tools_me.go`
- [ ] xapi 呼び出し統合
- [ ] JSON 出力
- 完了条件: mark3labs/mcp-go の client で `tools/call name=get_user_me` が動作

#### M18: MCP tool `get_liked_tweets`
- [ ] `internal/mcp/tools_likes.go`
- [ ] 全パラメータ受け付け (user_id/start_time/end_time/since_jst/yesterday_jst/max_results/all/max_pages/tweet_fields/expansions/user_fields)
- [ ] バリデーション
- 完了条件: mock X API に対して `tools/call name=get_liked_tweets` が E2E で動作

### Phase F: MCP 認証 (idproxy 全 store backend)

#### M19: authgate apikey
- [ ] `internal/authgate/apikey.go` (Bearer + `subtle.ConstantTimeCompare`)
- [ ] 環境変数 `X_MCP_API_KEY` 必須化
- 完了条件: 正しい Bearer で通過、間違いで 401、空で 401 (TDD で 3 ケース)

#### M20: authgate idproxy 基盤 + memory store
- [ ] `internal/authgate/idproxy.go` (`idproxy.New` + `Wrap`)
- [ ] memory store backend (デフォルト)
- [ ] 環境変数読み込み (OIDC_ISSUER / CLIENT_ID / CLIENT_SECRET / COOKIE_SECRET / EXTERNAL_URL)
- 完了条件: ローカル起動で Google OIDC ログイン → セッション発行 → MCP tools 呼び出し成功

#### M21: idproxy sqlite store
- [ ] `internal/authgate/store_sqlite.go` (`modernc.org/sqlite` pure Go)
- [ ] スキーマ migrate (idproxy が要求する単一テーブル設計)
- [ ] `STORE_BACKEND=sqlite` / `SQLITE_PATH` 環境変数
- [ ] パーミッション 0600
- 完了条件: 起動再起動でセッション復元、`go test -race` 通過

#### M22: idproxy redis store
- [ ] `internal/authgate/store_redis.go` (`github.com/redis/go-redis/v9`)
- [ ] TTL 自動失効 (`EX` オプション)
- [ ] `STORE_BACKEND=redis` / `REDIS_URL` 環境変数
- 完了条件: redis CI コンテナで E2E (session/authcode/token がパス間で復元される)

#### M23: idproxy dynamodb store (Lambda 想定)
- [ ] `internal/authgate/store_dynamodb.go` (`aws-sdk-go-v2`)
- [ ] ConsistentRead で session/authcode 強整合性
- [ ] TTL カラム (DynamoDB ネイティブ TTL + アプリ側で期限切れチェック二重化)
- [ ] `STORE_BACKEND=dynamodb` / `DYNAMODB_TABLE_NAME` / `AWS_REGION` 環境変数
- 完了条件: LocalStack または DynamoDB Local で全 store operation テスト pass

### Phase G: CLI 統合 + v0.2.0 リリース

#### M24: CLI `x mcp` サブコマンド + E2E
- [ ] `internal/cli/mcp.go` (`--host` / `--port` / `--auth` / `--apikey-env` / `--path`)
- [ ] 環境変数フォールバック (`X_MCP_*` / `OIDC_*` / `STORE_BACKEND` 等)
- [ ] 起動時の auth モード切り分けロジック
- [ ] E2E: httptest + mark3labs/mcp-go client で none/apikey/idproxy(memory) の 3 モード検証
- 完了条件: 3 モード × 2 tools で 6 シナリオ全 pass

#### M25: v0.2.0 README 追記 + CHANGELOG + タグ
- [ ] README に MCP セクション追加 (3 モード起動例)
- [ ] CHANGELOG 更新
- [ ] `v0.2.0` タグ作成
- 完了条件: GitHub Releases に成果物掲載

### Phase H: lambroll examples + docs + v0.3.0

#### M26: examples/lambroll/ ファイル一式
- [ ] `function.json` (provided.al2023, arm64, LWA layer)
- [ ] `function_url.json` (AuthType=NONE, CORS なし)
- [ ] `bootstrap` シェル (`exec ./x mcp --host 0.0.0.0 --port "${PORT:-8080}"`)
- [ ] `.env.example` (SSM 参照テンプレ含む全環境変数)
- 完了条件: lambroll deploy で AWS にデプロイ可能 (実デプロイは別作業, dry-run で検証)

#### M27: examples/lambroll/README.md
- [ ] Step-by-step デプロイ手順 (アカウント前提 → SSM 投入 → DynamoDB テーブル作成 → IAM ロール → デプロイ)
- [ ] OIDC プロバイダ設定例 (Google / Entra ID)
- [ ] トラブルシュート FAQ
- 完了条件: README だけで他者がデプロイ完走できる粒度

#### M28: docs/x-api.md + docs/routine-prompt.md + v0.3.0 タグ
- [ ] `docs/x-api.md` (OAuth 1.0a 認証メモ / rate limit / 課金)
- [ ] `docs/routine-prompt.md` (Claude Code Routines 用プロンプト雛形)
- [ ] `CHANGELOG.md` 更新 + `v0.3.0` タグ
- 完了条件: docs から Routines 設定が完結できる

## Architecture Decisions (spec 引き継ぎ + 追加)

| # | 決定 | 理由 | 日付 |
|---|------|------|------|
| 1-10 | (spec §5 ADR を参照) | - | 2026-05-12 |
| 11 | idproxy の 4 store backend 全部サポート (memory/sqlite/redis/dynamodb) | ローカル開発=sqlite、軽量サーバー=redis、Lambda=dynamodb、テスト=memory の使い分けを 1 バイナリで完結 | 2026-05-12 |
| 12 | M2 で軽量 CI を先行整備、M14 で release.yml に拡張 | TDD 駆動で各 M を CI 守護する。release は v0.1.0 直前に切り出して責務分離 | 2026-05-12 |
| 13 | M5-M8 で xapi を完成させてから M9-M12 で CLI を被せる | クライアントとプレゼンテーションの責務分離。`internal/xapi` のテスト容易性を最大化 | 2026-05-12 |
| 14 | MCP コア (M15-M18) と認証 (M19-M23) を独立 Phase に分割 | tools 実装と middleware 実装の関心事を分離。M19 以降の各 store backend を TDD で独立検証 | 2026-05-12 |

## Risks (ロードマップ全体)

| リスク | 影響 | 対策 |
|---|---|---|
| Claude Code Routines が research preview のため仕様変更 | 高 | スペック §10 に明記。Routine プロンプトは docs に置くだけで本体に依存させない |
| X API 料金 ($0.001/Owned Read) 想定超え | 中 | `--max-pages` でハードリミット。日次 Routine 実行で 1 日 1 回想定なので問題なし |
| mark3labs/mcp-go の API 変更 | 中 | logvalet/kintone が v0.46/v0.49 で実績あり。v0.49+ pinned |
| idproxy v0.4.2 の sqlite/redis サポート状況 | 中 | M21/M22 着手時に idproxy のソースを再確認、必要なら PR を出す方針 |
| Lambda Cold Start (Go バイナリ + LWA で ~500ms 目標) | 中 | logvalet で実績あり。M23 で実機計測する |
| Homebrew Tap (`youyo/tap`) のメンテナンス | 低 | logvalet/kintone と同じく GoReleaser が自動更新 |

## Blockers

なし

## Changelog

| 日時 | 種別 | 内容 |
|------|------|------|
| 2026-05-12 | 作成 | ロードマップ初版作成 (28 マイルストーン、TDD 必須、細粒度方針) |
| 2026-05-12 | 反映 | ユーザー要望: CI を M2 先行、idproxy 4 store backend (memory/sqlite/redis/dynamodb) を M20-M23 で個別マイルストーン化 |
| 2026-05-12 | spec 影響 | spec §11 の `STORE_BACKEND` 拡張 / sqlite redis 環境変数追加 / §3 フェーズ2展望 / §10 外部依存 / §5 architecture / ADR #11 追加 を ExitPlanMode 後に実施 |
| 2026-05-12 | 変更 | CLI パーサを **kong → cobra** に変更 (ユーザー提案受諾)。理由: OSS デファクト + `__complete` 標準補完 + viper 統合 + ヘルプ/マンページ標準化。M1 詳細計画を Cobra ベースで全面刷新 (テストケース 30→21、LOC 800→530、completion.go 不要)。spec §10/§5 ADR #4 を ExitPlanMode 後に修正 |

---

