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
| 最終更新 | 2026-05-14 (M29-M36 追加キューイング) |
| ステータス | 🚧 v0.4.0 開発完了 (M29) / v0.5.0〜v0.8.0 計画中 |
| スペック | `docs/specs/x-spec.md` (v1.0.0 Approved) |
| マイルストーン総数 | 36 (M29-M36 追加、細粒度方針) |

## Current Focus

M28 までの全 28 マイルストーンが完了 (v0.3.0 リリース準備完了)。次フェーズとして **readonly API 包括サポート (M29-M36)** を計画中。

**Phase I 優先順**:
1. **M29**: Posts Lookup + Note Tweet + Social Signals (v0.4.0) ✅ 完了
2. **M30**: Search Recent + Thread コマンド (v0.5.0) ← 次の着手対象
3. **M31**: User Timelines (v0.5.0)
4. **M32**: Users Extended (v0.6.0)
5. **M33**: Lists (v0.6.0)
6. **M34**: Spaces + Trends (v0.7.0)
7. **M35**: DM Read (v0.7.0、Pro 推奨)
8. **M36**: MCP v2 Tools (v0.8.0、CLI M29-M35 全完了後)

**残タスク (ユーザー手動)**:
1. GitHub remote (`https://github.com/youyo/x`) 設定 + repository secrets (`HOMEBREW_TAP_GITHUB_TOKEN`) 登録
2. `git push origin main`
3. v0.1.0 / v0.2.0 / v0.3.0 タグ push + Actions 完走確認 (任意)

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

#### M2: CI 軽量基盤 + Lint + Dockerfile ✅ 完了 (commit: 41ae9ad)
- [x] `.github/workflows/ci.yml` (lint + test + build + docker on PR/push, LANG=C)
- [x] `.golangci.yml` (v2 形式、default:none + 8 linters + gofumpt formatter)
- [x] `Dockerfile` (multi-stage, alpine builder → distroless/static-debian12:nonroot)
- [x] `.dockerignore`
- 完了: ローカルで lint 0 issues / 25 テスト pass / docker build 成功 (11.7MB) / `x:dev version` JSON 出力動作 / `--build-arg` ldflags 注入動作
- TDD 観点: M3 以降のテスト追加時に常時 lint+race+coverage を強制

#### M3: `internal/config` XDG loader (非機密設定のみ) ✅ 完了 (commit: 2b2a804)
- [x] `internal/config/xdg.go` (Dir() / DataDir() / DefaultCLIConfigPath() + ErrHomeNotResolved)
- [x] `internal/config/config.go` (CLIConfig + CLISection + LikedSection + DefaultCLIConfig())
- [x] `internal/config/loader_cli.go` (LoadCLI(path) + applyDefaults ゼロ値補完)
- 完了: BurntSushi/toml v1.6.0 追加、16 テストケース pass、計 41 テスト全 pass、lint 0 issues
- TDD: t.Setenv + t.TempDir() で XDG パスを差し替えるテーブル駆動テスト

#### M4: `internal/config` credentials.toml + guard ✅ 完了 (commit: 38bb199)
- [x] `internal/config/credentials.go` (Credentials 型 + LoadCredentials + SaveCredentials + CheckPermissions + tmp+rename 原子書き換え)
- [x] `internal/config/guard.go` (CheckConfigNoSecrets, 大文字小文字非依存、値漏洩防止)
- 完了: perm 0700/0600 強制、11 新規テスト、計 50+ テスト全 pass、lint 0 issues、シークレット値漏洩テストで保証
- API: Credentials{APIKey/APISecret/AccessToken/AccessTokenSecret}、ErrCredentialsNotFound/ErrPermissionsTooOpen/ErrSecretInConfig

### Phase B: X API クライアント

#### M5: `internal/xapi/oauth1.go` (dghubble/oauth1 ラッパー) ✅ 完了 (commit: 17d1c4f)
- [x] OAuth 1.0a HMAC-SHA1 署名 (`NewOAuth1Config` + `NewHTTPClient`)
- [x] httptest 経由で Authorization ヘッダ検証 (7 テスト)
- 完了: dghubble/oauth1 v0.7.3 追加、Credentials→Config マッピング、nil creds 安全動作、RFC 5849 oauth_version OPTIONAL 扱い

#### M6: `internal/xapi/client.go` (retry + rate-limit aware HTTP) ✅ 完了 (commit: 93b8558)
- [x] Client.Do 内 retry (exp backoff: base 2s, factor 2, max 30s, max 3 retry)、context-aware sleep
- [x] x-rate-limit-remaining/reset パース → Response.RateLimit (Raw フラグで枯渇判定誤動作防止)
- [x] APIError + 番兵エラー (ErrAuthentication/Permission/NotFound/RateLimit) + ExitCodeFor(err) で 0/1/3/4/5
- 完了: 22 テスト (httptest + 未公開 DI オプション)、計 70+ テスト全 pass、lint 0 issues

#### M7: `internal/xapi/users.go` (GetUserMe + DTO) ✅ 完了 (commit: bc2aff5)
- [x] types.go: User/Tweet/Meta/Includes/ErrorResponse + UserPublicMetrics
- [x] GetUserMe(ctx, opts...) + WithUserFields クエリオプション
- 完了: 16 新規テスト (httptest + envelope + JSON unmarshal)、計 80+ テスト全 pass、lint 0 issues
- 留意: User.ID の json タグは "id"、MCP 層 (M17) で "user_id" にリネームが必要

#### M8: `internal/xapi/likes.go` (ListLikedTweets + ページネーション) ✅ 完了 (commit: 2272f0b)
- [x] types.go 拡張: Tweet.Entities / PublicMetrics / ReferencedTweets, Includes.Tweets, 関連 DTO (TweetURL/Tag/Mention/Annotation/PublicMetrics/Entities/ReferencedTweet)
- [x] `ListLikedTweets(ctx, userID, opts...)` シングルページ (path escape + 全 Option クエリ反映 + envelope decode)
- [x] `EachLikedPage(ctx, userID, fn, opts...)` callback 形式の next_token 自動辿り iterator
- [x] `WithMaxPages(n)` 上限 (default 50) — 到達時は正常終了
- [x] ページ間 rate-limit aware sleep (remaining ≤ 2 で reset まで待機, 最大 15min, ページ間最小 200ms, stale reset は 200ms フォールバック)
- [x] Option 関数群: WithStartTime/WithEndTime (RFC3339 UTC, ナノ秒なし), WithMaxResults (0=no-op), WithPaginationToken, WithTweetFields, WithExpansions, WithLikedUserFields, WithMaxPages
- 完了: 18 新規テスト (likes_test.go) + 3 拡張テスト (types_test.go), 計 100+ テスト全 pass, lint 0 issues, vet clean
- 留意: WithMaxResults(0) は no-op (CLI 層 M10/M11 が default 100 を必ずセットする責務)。WithLikedUserFields は GetUserMe 用 WithUserFields とは型が異なる別関数。max_pages 到達時の truncated シグナル未提供 (将来 M11 で必要なら戻り値拡張)

### Phase C: CLI v0.1.0

#### M9: CLI `x me` ✅ 完了
- [x] `internal/cli/me.go` (newMeCmd factory + meClient interface + newMeClient var-swap for test)
- [x] `internal/cli/auth_loader.go` (LoadCredentialsFromEnvOrFile + ErrCredentialsMissing が xapi.ErrAuthentication を Unwrap)
- [x] `internal/cli/root.go` (AddCommand(newMeCmd()))
- [x] `cmd/x/main.go` run() の error → exit code 写像に xapi.ErrAuthentication / ErrPermission / ErrNotFound を追加
- 完了: JSON / human 出力 (--no-json) で動作、認証情報欠落 → exit 3、401 → exit 3、404 → exit 5。18 新規テスト全 pass、計 118+ テスト、lint 0 issues
- 留意: env > file は **ファイル単位** 優先順位 (タスク指示準拠、フィールド単位部分上書きは将来 M12 で再評価)。meClient interface + newMeClient 関数変数で httptest baseURL 注入を可能化。`xapi.WithBaseURL` を利用

#### M10: CLI `x liked list` 基本フラグ ✅ 完了
- [x] `internal/cli/liked.go` (newLikedCmd + newLikedListCmd + likedClient interface + newLikedClient var-swap for test)
- [x] `internal/cli/errors.go` (ErrInvalidArgument 番兵)
- [x] `--user-id` / `--start-time` / `--end-time` / `--max-results` / `--pagination-token` / `--no-json`
- [x] JSON 出力 (default, `{data, includes, meta}` 全体) + human 出力 (`--no-json` で 1 行/ツイート、改行 / タブ正規化 + 80 ルーン truncate)
- [x] `cmd/x/main.go` の switch に `cli.ErrInvalidArgument → ExitArgumentError (2)` を追加
- 完了: 21 新規テスト全 pass、計 140+ テスト、lint 0 issues、`/tmp/x liked list --max-results 999` で exit 2 確認
- 留意: `--user-id` 未指定 → GetUserMe で self の ID を解決 (D-2)。`WithMaxResults` は常に呼ぶ (default=100 を 0 に流さない D-3)。JSON 出力は `*xapi.LikedTweetsResponse` 全体を出して MCP `get_liked_tweets` のスキーマと整合 (D-4)

#### M11: CLI `x liked list` 拡張 ✅ 完了 (commit: 8903589)
- [x] `--since-jst <YYYY-MM-DD>` / `--yesterday-jst` (LoadLocation→FixedZone フォールバック)
- [x] `--all` + `--max-pages` (default 50) + EachLikedPage 統合 + likedAggregator
- [x] NDJSON 出力 (`--ndjson`, SetEscapeHTML(false), --all+NDJSON はストリーミング)
- [x] tweet/expansion/user fields のカスタマイズフラグ + splitCSV trim + 空要素除外
- 完了: 18 新規テスト、計 41+ CLI テスト、優先順位 yesterday-jst > since-jst > start/end、--no-json と --ndjson 排他 (exit 2)
- 留意: ハードコードのデフォルト値は M12 で config.toml [liked] 連携に移管予定

#### M12: CLI `x configure` + config.toml [liked] 連携 ✅ 完了 (commit: ab5ebdc)
- [x] 対話モード (XDG パス 2 ファイル生成、term.ReadPassword で TTY echo オフ + 非 TTY フォールバック)
- [x] `--print-paths` / `--check` (JSON + --no-json human フォーマット)
- [x] credentials.toml 既存時の保護 (上書き確認 `[y/N]` プロンプト)
- [x] config.toml [liked] 連携: loadLikedDefaults() でハードコード定数を置換
- 完了: 12 新規テスト (configure 11 + liked config 連携 1)、coverage 85.6%、golangci-lint 0 issues
- 留意: x completion は cobra が自動提供 (HasSubCommands 真) のため別途実装不要

### Phase D: v0.1.0 リリース

#### M13: README / CHANGELOG / LICENSE / GoReleaser ✅ 完了 (commit: d077bcd)
- [x] `README.md` (英) / `README.ja.md` (日) - 12 セクション + 双方向リンク
- [x] `CHANGELOG.md` (Keep a Changelog v1.1.0、[0.1.0] - 2026-05-12)
- [x] `LICENSE` (MIT, Copyright 2026 Naoto Ishizawa / Heptagon)
- [x] `.goreleaser.yaml` (darwin/linux × amd64/arm64 + Homebrew tap + ghcr.io multi-arch)
- 完了: `goreleaser release --snapshot --clean --skip docker` で 4 platform binary + tarball 生成、ldflags 注入確認、テスト全 pass

#### M14: GitHub Actions release.yml + v0.1.0 タグ ✅ workflow 完了 (commit: fbb2df8)
- [x] `.github/workflows/release.yml` (tag push → GoReleaser + Homebrew + ghcr.io)
- [x] actionlint pass、`goreleaser check` pass、既存テスト全 pass、lint 0 issues
- [ ] `v0.1.0` タグ作成 ← **GitHub remote 設定後にユーザー手動で実施 (本マイルストーンでは扱わない)**
- 完了条件: workflow 配置完了 / 将来タグ push 時に GitHub Releases へ成果物投稿 + `brew install youyo/tap/x` 動作

**v0.1.0 タグ push 手順 (将来手動実施)**:

GitHub remote (`https://github.com/youyo/x`) を設定後、以下のコマンドで実行する。

```bash
# 0. 必須 secrets を repository に登録 (Settings > Secrets and variables > Actions)
#    - HOMEBREW_TAP_GITHUB_TOKEN: youyo/homebrew-tap 宛 PAT (repo scope)
#    GITHUB_TOKEN は GitHub Actions が自動付与するので登録不要

# 1. リポジトリを GitHub に push (初回のみ)
git remote add origin https://github.com/youyo/x.git
git push -u origin main

# 2. v0.1.0 タグを作成・push
git tag -a v0.1.0 -m "v0.1.0: 初回リリース (CLI v0.1.0)"
git push origin v0.1.0

# 3. GitHub Actions の `release` ワークフローが走ることを確認
gh run watch  # または https://github.com/youyo/x/actions

# 4. 完了確認
gh release view v0.1.0  # 4 platform tarball + checksums.txt
brew install youyo/tap/x && x version
docker pull ghcr.io/youyo/x:v0.1.0 && docker run --rm ghcr.io/youyo/x:v0.1.0 version
```

### Phase E: MCP コア

#### M15: MCP サーバー雛形 + transport/http ✅ 完了
- [x] `internal/mcp/server.go` (`NewServer(client, ver)` ファクトリ, mark3labs/mcp-go v0.52.0)
- [x] `internal/mcp/doc.go` (パッケージドキュメンテーション)
- [x] `internal/transport/http/server.go` (Streamable HTTP + graceful shutdown, LWA 互換, Option パターン)
- [x] `internal/transport/http/doc.go`
- 完了: 空サーバーで `initialize` リクエストに 200 + serverInfo 応答 (JSON / SSE 両対応テスト)、context cancel での graceful shutdown 30s、ReadHeaderTimeout 10s 設定、calc 8 新規テスト全 pass、計 全パッケージ pass、lint 0 issues
- 留意: tools 登録は M17 (`get_user_me`) / M18 (`get_liked_tweets`) で実装。authgate middleware フックは M16 で `Option` 追加予定 (`WithHandlerMiddleware` 想定)。cobra サブコマンド `x mcp` の追加は M24

#### M16: authgate 基盤 + none モード ✅ 完了 (commit: 12efddb)
- [x] `internal/authgate/gate.go` (Middleware interface)
- [x] `internal/authgate/none.go` (passthrough)
- [x] transport の WithHandlerMiddleware Option + `/healthz` バイパス
- 完了: middleware framework 確立、認証 OFF モードで動作

#### M17: MCP tool `get_user_me` ✅ 完了 (commit: a3a3934)
- [x] `internal/mcp/tools_me.go`
- [x] xapi 呼び出し統合 (`user_id` リネーム対応)
- [x] JSON 出力
- 完了: mark3labs/mcp-go の client で `tools/call name=get_user_me` 動作

#### M18: MCP tool `get_liked_tweets` ✅ 完了 (commit: 1620ef5)
- [x] `internal/mcp/tools_likes.go`
- [x] 全パラメータ受け付け (user_id/start_time/end_time/since_jst/yesterday_jst/max_results/all/max_pages/tweet_fields/expansions/user_fields)
- [x] バリデーション + JST 優先順位 (`yesterday_jst > since_jst > start/end_time`)
- 完了: mock X API に対して `tools/call name=get_liked_tweets` が E2E で動作

### Phase F: MCP 認証 (idproxy 全 store backend)

#### M19: authgate apikey ✅ 完了 (commit: d42719c)
- [x] `internal/authgate/apikey.go` (Bearer + `subtle.ConstantTimeCompare`)
- [x] 環境変数 `X_MCP_API_KEY` 必須化
- 完了: 正しい Bearer で通過、間違いで 401、空で 401 (TDD 3 ケース)

#### M20: authgate idproxy 基盤 + memory store ✅ 完了 (commit: 35be1f1)
- [x] `internal/authgate/idproxy.go` (`idproxy.New` + `Wrap`)
- [x] memory store backend (デフォルト)
- [x] 環境変数読み込み (OIDC_ISSUER / CLIENT_ID / CLIENT_SECRET / COOKIE_SECRET / EXTERNAL_URL)
- 完了: idproxy memory store で session 発行 → MCP tools 呼び出し成功

#### M21: idproxy sqlite store ✅ 完了 (commit: 7b50dd8)
- [x] `internal/authgate/store_sqlite.go` (`modernc.org/sqlite` pure Go)
- [x] スキーマ migrate
- [x] `STORE_BACKEND=sqlite` / `SQLITE_PATH` 環境変数
- 完了: 起動再起動でセッション復元、`go test -race` 通過

#### M22: idproxy redis store ✅ 完了 (commit: 491bb30)
- [x] `internal/authgate/store_redis.go` (`github.com/redis/go-redis/v9`)
- [x] TTL 自動失効 (`EX` オプション)
- [x] `STORE_BACKEND=redis` / `REDIS_URL` 環境変数
- 完了: redis E2E (session/authcode/token がパス間で復元される)

#### M23: idproxy dynamodb store (Lambda 想定) ✅ 完了 (commit: 2ffa62c)
- [x] `internal/authgate/store_dynamodb.go` (`aws-sdk-go-v2`)
- [x] ConsistentRead で session/authcode 強整合性
- [x] TTL カラム (DynamoDB ネイティブ TTL + アプリ側期限切れチェック二重化)
- [x] `STORE_BACKEND=dynamodb` / `DYNAMODB_TABLE_NAME` / `AWS_REGION` 環境変数
- 完了: 4 store backend 全完成、全 store operation テスト pass

### Phase G: CLI 統合 + v0.2.0 リリース

#### M24: CLI `x mcp` サブコマンド + E2E ✅ 完了 (commit: 01f2a97)
- [x] `internal/cli/mcp.go` (`--host` / `--port` / `--auth` / `--apikey-env` / `--path`)
- [x] 環境変数フォールバック (`X_MCP_*` / `OIDC_*` / `STORE_BACKEND` 等)
- [x] 起動時の auth モード切り分けロジック
- [x] E2E: httptest + mark3labs/mcp-go client で none/apikey/idproxy(memory) の 3 モード検証
- 完了: 3 モード × 2 tools で 6 シナリオ全 pass、v0.2.0 機能完成

#### M25: v0.2.0 README 追記 + CHANGELOG + タグ ✅ workflow / docs 完了 (commit: 939f132, 追補 837e2eb)
- [x] README に MCP セクション追加 (3 モード起動例) [英日]
- [x] CHANGELOG `[0.2.0] - 2026-05-12` セクション追加 + LOG_LEVEL 追記
- [ ] `v0.2.0` タグ作成 ← **GitHub remote 設定後にユーザー手動で実施**

### Phase H: lambroll examples + docs + v0.3.0

#### M26: examples/lambroll/ ファイル一式 ✅ 完了 (commit: 82edb21)
- [x] `function.json` (provided.al2023, arm64, LWA layer, SSM SecureString 注入)
- [x] `function_url.json` (AuthType=NONE)
- [x] `bootstrap` シェル (`exec ./x mcp --host 0.0.0.0 --port "${PORT:-8080}"`)
- [x] `.env.example` (SSM 参照テンプレ + lambroll/SSM 全環境変数のリファレンス)
- 完了: lambroll deploy で AWS にデプロイ可能 (実デプロイは別作業, dry-run で検証)

#### M27: examples/lambroll/README.md ✅ 完了 (commit: 1599f7f)
- [x] Step 1-6 デプロイ手順 (IAM → DynamoDB → SSM → OIDC → deploy → 動作確認) + Mermaid 図
- [x] OIDC プロバイダ設定例 (Google Workspace / Microsoft Entra ID)
- [x] トラブルシュート FAQ + コスト見積もり + クリーンアップ
- 完了: README だけで他者がデプロイ完走できる粒度

#### M28: docs/x-api.md + docs/routine-prompt.md + v0.3.0 リリース準備 ✅ 完了 (commit: 本コミット)
- [x] `docs/x-api.md` (OAuth 1.0a 認証メモ / エンドポイント / rate limit / Owned Reads 課金 / エラーレスポンス)
- [x] `docs/routine-prompt.md` (Claude Code Routines 用プロンプト雛形 + Backlog 課題テンプレ + Mermaid シーケンス図)
- [x] `CHANGELOG.md` `[0.3.0] - 2026-05-12` セクション追加 + compare リンク更新
- [x] `README.md` / `README.ja.md` Status 表 + Documentation セクション + Roadmap セクション削除
- [x] `examples/lambroll/README.md` 関連ドキュメント節を確定リンクに更新
- [ ] `v0.3.0` タグ作成 ← **GitHub remote 設定後にユーザー手動で実施 (本マイルストーンでは扱わない)**
- 完了: docs から Routines 設定が完結できる粒度、全 28 マイルストーン達成

### Phase I: readonly API 包括サポート (v0.4.0〜v0.8.0)

> 除外: Streaming 系 (1 ショット CLI 設計と非整合) / OAuth 2.0 PKCE 専用 (Bookmarks 等) / Bearer Token 必須 (search/all / counts 系) / Enterprise 専用 (compliance / activity / webhooks)
> OAuth 1.0a User Context で利用可能な readonly エンドポイント約 35 件を 8 M に分割。

#### M29: Posts Lookup / Social Signals + Note Tweet 既定 + liked 下限補正 ✅ 完了
- [x] T1: `internal/xapi/types.go` — `Tweet.NoteTweet *NoteTweet` / `Tweet.ConversationID string` / `NoteTweet` 型追加
- [x] T2: `internal/xapi/tweets.go` 新規 — `GetTweet` / `GetTweets` + `TweetLookupOption` 群 + `TweetLookupError`
- [x] T3: `internal/xapi/tweets.go` 拡張 — `GetLikingUsers` / `GetRetweetedBy` / `GetQuoteTweets`
- [x] T4: `internal/cli/tweet.go` 新規 — `tweet get` (URL→ID 自動変換) / `tweet liking-users` / `tweet retweeted-by` / `tweet quote-tweets`
- [x] T5: `internal/cli/root.go` — `AddCommand(newTweetCmd())` + TestRootHelpShowsTweet
- [x] T6: `internal/cli/liked.go` 改修 — `note_tweet` 既定追加 / `--max-results<5` 補正 / `--all` × 1..4 拒否 / `writeLikedHuman` note_tweet 優先
- [x] T7 (Refactor): `internal/xapi/pagination.go` — `computeInterPageWait` を共通化
- [x] T8 (検証 + Docs): test/lint/vet 全 pass / spec §6 / docs/x-api.md / README 英日 / CHANGELOG [0.4.0] 追記
- 📄 詳細: [plans/x-m29-posts-lookup.md](./x-m29-posts-lookup.md)

#### M30: Search Recent + Thread コマンド ⏳ 未着手
- [ ] T1: `internal/xapi/tweets.go` 拡張 — `SearchRecent` + `EachSearchPage` + `SearchOption` 群
- [ ] T2: `internal/cli/tweet.go` 拡張 — `tweet search` (JST 系フラグ / --all / --ndjson)
- [ ] T3: `internal/cli/tweet.go` 拡張 — `tweet thread` (--author-only, CLI 層 AuthorID フィルタ)
- [ ] T4 (検証 + Docs): search/thread テスト / `x tweet search` / `x tweet thread --author-only` 実機 / docs/x-api.md Tier 要件 / CHANGELOG [0.5.0]
- 📄 詳細: [plans/x-m30-search-thread.md](./x-m30-search-thread.md)

#### M31: User Timelines ⏳ 未着手
- [ ] T1: `internal/xapi/timeline.go` 新規 — `GetUserTweets` / `GetUserMentions` / `GetHomeTimeline` + `EachTimelinePage`
- [ ] T2: `internal/cli/timeline.go` 新規 — `timeline tweets` / `timeline mentions` / `timeline home`
- [ ] T3: `internal/cli/root.go` — `AddCommand(newTimelineCmd())`
- [ ] T4 (検証 + Docs): test / `x timeline tweets <ID>` / `x timeline home --since-jst` 実機 / CHANGELOG
- 📄 詳細: [plans/x-m31-timelines.md](./x-m31-timelines.md)

#### M32: Users Extended ⏳ 未着手
- [ ] T1: `internal/xapi/users.go` 拡張 — `GetUser` / `GetUsers` / `GetUserByUsername` / `GetUsersByUsernames` / `SearchUsers` / `GetFollowing` / `GetFollowers` / `GetBlocking` / `GetMuting` + `EachUserGraphPage`
- [ ] T2: `internal/cli/user.go` 新規 — `user get` / `user followers` / `user following` / `user blocking` / `user muting` / `user search`
- [ ] T3: `internal/cli/root.go` — `AddCommand(newUserCmd())`
- [ ] T4 (検証 + Docs): test / `x user get @youyo` / `x user followers` 実機 / CHANGELOG
- 📄 詳細: [plans/x-m32-users-extended.md](./x-m32-users-extended.md)

#### M33: Lists ⏳ 未着手
- [ ] T1: `internal/xapi/lists.go` 新規 — `GetList` / `GetListTweets` / `GetListMembers` / `GetOwnedLists` / `GetListMemberships` / `GetFollowedLists` / `GetPinnedLists`
- [ ] T2: `internal/cli/list.go` 新規 — `list get` / `list tweets` / `list members` / `list owned` / `list followed` / `list memberships`
- [ ] T3: `internal/cli/root.go` — `AddCommand(newListCmd())`
- [ ] T4 (検証 + Docs): test / `x list tweets <ID>` / `x list owned` 実機 / CHANGELOG
- 📄 詳細: [plans/x-m33-lists.md](./x-m33-lists.md)

#### M34: Spaces + Trends ⏳ 未着手
- [ ] T1: `internal/xapi/spaces.go` 新規 + `internal/xapi/trends.go` 新規
- [ ] T2: `internal/cli/space.go` 新規 + `internal/cli/trends.go` 新規
- [ ] T3: `internal/cli/root.go` — AddCommand × 2
- [ ] T4 (検証 + Docs): test / `x space search "AI"` / `x trends get 1118370` 実機 / CHANGELOG
- 📄 詳細: [plans/x-m34-spaces-trends.md](./x-m34-spaces-trends.md)

#### M35: DM Read (Pro 推奨) ⏳ 未着手
- [ ] T1: `internal/xapi/dm.go` 新規 — `GetDMEvents` / `GetDMConversation` / `GetDMWithUser`
- [ ] T2: `internal/cli/dm.go` 新規 — `dm list` / `dm conversation`
- [ ] T3: `internal/cli/root.go` — `AddCommand(newDMCmd())`
- [ ] T4 (検証 + Docs): test / `x dm list` 実機 (Pro 環境) / docs に Tier 制限明記 / CHANGELOG
- 📄 詳細: [plans/x-m35-dm-read.md](./x-m35-dm-read.md)

#### M36: MCP v2 Tools (CLI M29-M35 の薄いラッパー) ⏳ 未着手
- [ ] `internal/mcp/tools_tweet.go` — get_tweet / get_tweets / get_liking_users / get_retweeted_by / get_quote_tweets
- [ ] `internal/mcp/tools_search.go` — search_recent_tweets / get_tweet_thread
- [ ] `internal/mcp/tools_timeline.go` — get_user_tweets / get_user_mentions / get_home_timeline
- [ ] `internal/mcp/tools_users.go` — get_user / get_user_by_username / get_user_following / get_user_followers
- [ ] `internal/mcp/tools_lists.go` — get_list / get_list_tweets
- [ ] `internal/mcp/tools_misc.go` — search_spaces / get_trends
- [ ] test / docs/routine-prompt.md 更新 / CHANGELOG [0.8.0]
- 📄 詳細: [plans/x-m36-mcp-v2-tools.md](./x-m36-mcp-v2-tools.md)

## Architecture Decisions (spec 引き継ぎ + 追加)

| # | 決定 | 理由 | 日付 |
|---|------|------|------|
| 1-10 | (spec §5 ADR を参照) | - | 2026-05-12 |
| 11 | idproxy の 4 store backend 全部サポート (memory/sqlite/redis/dynamodb) | ローカル開発=sqlite、軽量サーバー=redis、Lambda=dynamodb、テスト=memory の使い分けを 1 バイナリで完結 | 2026-05-12 |
| 12 | M2 で軽量 CI を先行整備、M14 で release.yml に拡張 | TDD 駆動で各 M を CI 守護する。release は v0.1.0 直前に切り出して責務分離 | 2026-05-12 |
| 13 | M5-M8 で xapi を完成させてから M9-M12 で CLI を被せる | クライアントとプレゼンテーションの責務分離。`internal/xapi` のテスト容易性を最大化 | 2026-05-12 |
| 14 | MCP コア (M15-M18) と認証 (M19-M23) を独立 Phase に分割 | tools 実装と middleware 実装の関心事を分離。M19 以降の各 store backend を TDD で独立検証 | 2026-05-12 |
| 15 | readonly API の段階的サポート (M29-M36) | Full-archive search / Streaming / Bookmarks は OAuth 1.0a または 1 ショット CLI と非整合なため除外。MCP は CLI 全完了後の M36 で薄いラッパーとして一括追加。DM は Basic tier レート制限が厳しく Pro 推奨と明記 | 2026-05-14 |

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
| 2026-05-12 | 完了 | M16-M28 を一括完了マーク化 (commit hash 追記)。全 28 マイルストーン達成、v0.3.0 文書セット (docs/x-api.md + docs/routine-prompt.md + CHANGELOG + README 英日) を整備済み。残タスクはユーザー手動の git push + タグ push のみ |
| 2026-05-14 | 追加 | M29-M36 (readonly API 包括サポート) をキューイング — Posts Lookup+Social Signals / Search+Thread / Timelines / Users Extended / Lists / Spaces+Trends / DM Read / MCP v2 Tools の 8 段階。Streaming・Bookmarks・Bearer 専用・Enterprise 専用は除外。ADR #15 追加。 |

---

