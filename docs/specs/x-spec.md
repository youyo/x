# Product Spec: x — X (Twitter) API CLI & Remote MCP

## Context

毎日、前日に X (旧Twitter) でLikeしたPostの中から「技術的に検証すべき内容」を抽出し、Backlog (HEP_ISSUES) に課題化したい。重複は自動でまとめる。実行基盤は Anthropic 公式の **Claude Code Routines**（research preview）で、Routines のコネクターとして使える Remote MCP（Streamable HTTP）が必要。既存の X MCP はすべて stdio ローカル用なので、Remote MCP を **Go で新規実装**する。

設計方針: 「**CLI がコア、MCP はその薄いラッパー**」。logvalet / kintone と同じ構造で、`github.com/youyo/x` リポジトリに単一バイナリ `x` を OSS として公開する。

## Meta

| 項目 | 値 |
|---|---|
| バージョン | 1.0.0 |
| 作成日 | 2026-05-12 |
| 最終更新 | 2026-05-12 |
| ステータス | Approved |
| モジュール名 | `github.com/youyo/x` |
| バイナリ名 | `x` |

## 1. Overview

### 解決する課題
日々 X で Like した Post の中に埋もれている「あとで調べたい技術ネタ」が積み上がり、確実に検証されないまま流れていく。手動でメモ化・課題化する手間が大きい。

### ターゲットユーザー
- 一次ユーザー: 自分 (Naoto / Heptagon)
- セカンダリ: 同じく X 上で技術キュレーションをしている個人開発者 / 小チーム (OSS 公開後)

### 既存代替手段の課題
- 既存 X MCP は **すべて stdio ローカル用** で Claude Code Routines のコネクターには使えない
- Twitter Bookmark / Like を Notion 等に転記する SaaS はあるが、Backlog 連携 + 技術判定 + 重複排除を一気通貫に行う既製品はない

## 2. Goals / Non-goals

### 3ヶ月後のゴール
- Claude Code Routines により毎朝 JST 8:00 に自動実行され、前日 Liked から技術トピック単位で Backlog 課題が生成されている状態
- `x` バイナリが Homebrew Tap (`youyo/tap`) から `brew install` 可能
- OSS リポジトリとして公開、README が英日両言語

### 成功指標 (KPI)
| 指標 | 目標値 |
|---|---|
| Routine 成功率 (週単位) | ≥ 95% |
| 1課題あたり手動修正コスト | ≤ 1 min |
| 重複課題率 (新規課題のうち事実上の重複) | ≤ 5% |
| Lambda コールドスタート | ≤ 500ms (p95) |

### 意図的にスコープ外
- 投稿系操作 (post / retweet / DM) — Read 専用で開始
- Bookmark 取得 — Liked のみ
- 多言語 UI / i18n
- Web フロントエンド・GUI
- リアルタイム Webhook / Filtered Stream
- Backlog 以外のチケットシステム連携
- **本リポジトリでの本番 Lambda デプロイ実作業** — examples/ に lambroll サンプル一式を置くまで。実 AWS リソース作成は別作業

## 3. Scope

### MVP (フェーズ1) に含むもの
- CLI: `x me`, `x liked list`, `x mcp`, `x configure`, `x version`, `x completion`
- MCP tools: `get_user_me`, `get_liked_tweets`
- OAuth 1.0a 静的トークンによる X API v2 アクセス
- 設定: 環境変数 + `~/.config/x/config.toml` (XDG 準拠)
- MCP 認証: **idproxy + OIDC**（default）/ **API Key**（共有シークレット） / **none**（ローカルのみ）の 3 モード切替
- examples/lambroll: 公開配布用デプロイサンプル一式 (logvalet 流用)
- GoReleaser + Homebrew Tap + Docker (ghcr.io)
- GitHub Actions: lint / test / tag リリース

### フェーズ2 以降の展望
- Bookmark API (`/2/users/:id/bookmarks`) 対応
- 検索系 tools (`search_recent_tweets`)
- Routine プロンプトを `x` リポジトリにテンプレ同梱
- ユーザー別 OAuth 2.0 PKCE フロー (他人のアカウントを叩く需要が出たら)
- ユーザー別 OAuth 2.0 PKCE のトークン保管も同じ Store interface (memory/sqlite/redis/dynamodb) に乗せて、idproxy セッションと統一管理

## 4. Distribution & Execution Model

### 配布形態
**単一 Go バイナリ** (CLI)。MCP サーバーはそのサブコマンド `x mcp` として起動する。

### 起動方法

```bash
# CLI
x me
x liked list --start-time 2026-05-11T15:00:00Z --end-time 2026-05-12T14:59:59Z

# MCP (ローカル開発)
x mcp --host 127.0.0.1 --port 8080 --auth none

# MCP (Lambda 本番想定)
# bootstrap → exec x mcp --host 0.0.0.0 --port "${PORT:-8080}"
```

### インストール方法

| 経路 | コマンド |
|---|---|
| Homebrew | `brew install youyo/tap/x` |
| go install | `go install github.com/youyo/x/cmd/x@latest` |
| Docker | `docker pull ghcr.io/youyo/x:latest` |
| GitHub Releases | tar.gz バイナリ DL |
| Lambda | examples/lambroll/ を参照 (lambroll deploy) |

### バックグラウンドプロセス
**なし。** MCP サーバーは Streamable HTTP の同期サーバーとして単独起動。

## 5. Architecture

### 主要コンポーネント

```
cmd/x/main.go
  └─ Kong パーサー (logvalet と同じ)
internal/
  app/        — exit code 定数 (0/1/2/3/4/5)
  cli/        — Kong コマンド struct: Auth / Me / Liked / MCP / Configure / Version / Completion
  config/     — XDG 準拠 loader:
                  ~/.config/x/config.toml          (非機密のみ, シークレット混入は拒否)
                  ~/.local/share/x/credentials.toml (CLI 用シークレット, perm 0600)
                  CLI モード: flag > env > credentials > config > default
                  MCP モード: flag > env > default (ファイル不使用)
  xapi/       — X API v2 クライアント (OAuth 1.0a 署名)
    client.go      — http.Client wrap + signing
    oauth1.go      — HMAC-SHA1 署名 (dghubble/oauth1 利用)
    users.go       — GetUserMe
    likes.go       — ListLikedTweets (ページネーション iterator)
    types.go       — Tweet / User / Meta DTO
  mcp/        — mark3labs/mcp-go ラッパー
    server.go      — NewServer(client, ver)
    tools_me.go    — get_user_me
    tools_likes.go — get_liked_tweets
  authgate/   — MCP 着信認証 (idproxy / apikey / none) のミドルウェア切替
    idproxy.go         — idproxy.New + Wrap, memory/sqlite/redis/dynamodb の 4 store backend をサポート
    store_memory.go    — メモリ実装 (デフォルト, テスト用)
    store_sqlite.go    — sqlite 実装 (modernc.org/sqlite, ローカル開発向け)
    store_redis.go     — redis 実装 (go-redis/v9, 軽量サーバー向け)
    store_dynamodb.go  — DynamoDB 実装 (aws-sdk-go-v2, Lambda マルチコンテナ向け)
    apikey.go          — Bearer token 定数比較 (subtle.ConstantTimeCompare)
    none.go            — passthrough
  transport/  — Lambda Web Adapter 対応 HTTP server (logvalet と同じ)
  version/    — Version 定数 (ldflags 注入)
```

### プロセスモデル
**単一プロセス・単一バイナリ。** Lambda 上では Lambda Web Adapter Layer (arm64 v27) が HTTP を bootstrap に橋渡しする。

### アーキテクチャ決定記録 (ADR)

| # | 決定 | 理由 | 却下した選択肢 |
|---|------|------|-------------|
| 1 | CLI コア + MCP は薄いラッパー | テスト容易性・ローカル動作確認・OSS としての汎用性 | MCP 専用バイナリ (柔軟性が低い) |
| 2 | Go + mark3labs/mcp-go v0.49+ | logvalet/kintone と同じ SDK、Streamable HTTP 対応 | TypeScript (参考実装あるが、既存資産が Go) |
| 3 | OAuth 1.0a 静的トークン | 自分のデータ取得のみ。期限なし。実装最小 | OAuth 2.0 PKCE (将来必要なら追加) |
| 4 | CLI パーサーに **Cobra** (kong から変更, 2026-05-12) | (i) `__complete` 動的補完が標準機能ビルトイン、(ii) viper 統合で env > flag > config 階層解決が宣言的、(iii) Go コミュニティのデファクト (kubectl/gh/hugo/docker) で OSS リーチ最大化、(iv) ヘルプ/マンページ/補完の標準化 | kong (logvalet/kintone と統一感は失うが OSS 公開時のリーチを優先) |
| 5 | MCP 着信認証は idproxy 既定・API Key 切替可 | OIDC で人が叩く時と CI/Routine から API Key で叩く時の両立 | idproxy 単独 (機械実行が面倒) / API Key 単独 (権限管理が雑) |
| 6 | dghubble/oauth1 を利用 | Go 標準的な OAuth1 実装、保守も継続 | 自前実装 (車輪の再発明) |
| 7 | デプロイは lambroll + LWA (examples/) | logvalet と同パターン、SAM/CDK より軽量 | AWS SAM / CDK / Serverless Framework |
| 8 | このリポジトリでは Lambda 実デプロイをしない | examples/ にサンプル一式を置くまでが責務範囲 | 完全自動デプロイまで含める (スコープ過大) |
| 9 | シークレットを設定ファイルに書かない (CLI は credentials.toml 専用ファイル / MCP は環境変数のみ) | 設定ファイル誤コミット事故防止、Lambda 不変性、責務分離 | 単一 config.toml に全部書く (事故リスク高) / 環境変数のみ (CLI 体験が悪化) |
| 10 | XDG_CONFIG_HOME / XDG_DATA_HOME に分離 | XDG 仕様準拠、Linux/macOS 慣習、シークレットを `~/.local/share/` に隔離可能 | `~/.config/x/` 一本 (シークレット同居) |
| 11 | idproxy の 4 store backend を全部サポート (memory/sqlite/redis/dynamodb) | ローカル開発=sqlite、軽量サーバー=redis、Lambda=dynamodb、テスト=memory の使い分けを 1 バイナリで完結。OSS 利用者が自由に選べる | dynamodb 単独 (ローカル不便) / memory 単独 (永続性なし) |

## 6. Interfaces & Contracts

### CLI サブコマンド

```
x me [--json]
  自分の user_id / username を出力
  exit: 0 success, 3 auth-error

x liked list
  --user-id <id>            (default: me)
  --start-time <RFC3339>    (UTC)
  --end-time <RFC3339>      (UTC)
  --max-results <1-100>     (default: 100)
                            (M29: 1..4 のとき X API には 5 を投げて応答を [:n] に絞る。
                             --all との併用は ErrInvalidArgument で拒否)
  --pagination-token <s>    (オプション: 続きから取得)
  --all                     (next_token を自動辿って全件取得)
  --max-pages <int>         (--all 時の最大ページ数, default: 50)
  --tweet-fields <csv>      (default: id,text,author_id,created_at,entities,public_metrics,note_tweet)
  --expansions <csv>        (default: author_id)
  --user-fields <csv>       (default: username,name)
  --json                    (NDJSON 出力)
  --since-jst <YYYY-MM-DD>  (簡易: 指定日 JST 0:00〜23:59 を UTC 変換)
  --yesterday-jst           (簡易: JST 前日)
  ※ --no-json 出力時、note_tweet.text が非空なら truncated text より優先表示する (M29)

x tweet get [ID|URL]
  --ids <csv>               (1..100 件のバッチ取得、引数と排他)
  --tweet-fields <csv>      (default: id,text,author_id,created_at,entities,public_metrics,note_tweet,conversation_id)
  --expansions <csv>        (default: author_id)
  --user-fields <csv>       (default: username,name)
  --media-fields <csv>      (default: 空)
  --no-json                 (1 ツイート 1 行 / note_tweet.text 優先表示)
  ※ URL/数値 ID どちらも受理。partial error は --no-json 時 stderr に warning

x tweet liking-users <ID|URL>
  --max-results <1-100>     (default: 100)
  --pagination-token <s>
  --user-fields <csv>       (default: username,name)
  --expansions <csv>        / --tweet-fields <csv>
  --no-json                 (1 ユーザー 1 行)

x tweet retweeted-by <ID|URL>
  (liking-users と同じフラグ)

x tweet quote-tweets <ID|URL>
  --max-results <1-100>     (default: 100)
  --pagination-token <s>
  --exclude <csv>           (retweets / replies)
  --tweet-fields / --expansions / --user-fields / --media-fields / --no-json

x tweet search <query>      ※ M30、X API v2 Basic tier 以上必須 (Free は 403 → exit 4)
  --start-time <RFC3339>    (UTC、過去 7 日以内)
  --end-time <RFC3339>
  --since-jst <YYYY-MM-DD>  (JST 0:00-23:59 を UTC 変換、--start-time/--end-time より優先)
  --yesterday-jst           (JST 前日、最も高優先)
  --max-results <1-100>     (default: 100、1..9 は X API 下限 10 に補正して [:n] で slice)
  --pagination-token <s>
  --all                     (next_token 自動辿り、--max-results 1..9 とは併用不可)
  --max-pages <int>         (--all 時の上限ページ数, default: 50)
  --tweet-fields <csv>      (default: id,text,author_id,created_at,entities,public_metrics,note_tweet,conversation_id)
  --expansions <csv>        (default: author_id)
  --user-fields <csv>       (default: username,name)
  --media-fields <csv>      (default: 空)
  --no-json                 (1 ツイート 1 行 / note_tweet.text 優先表示)
  --ndjson                  (1 ツイート 1 行 JSON、--all 時はストリーミング、--no-json と排他)
  ※ query は X 検索演算子をサポート (from: / lang: / conversation_id: / -is:retweet 等)

x tweet thread <ID|URL>     ※ M30、スレッド (会話) 全体取得 (2 リクエスト消費)
  --author-only             (ルート author 以外を CLI 層でフィルタ)
  --max-results <1-100>     (default: 100、search と同じ下限補正規則)
  --pagination-token <s>
  --all                     (next_token 自動辿り)
  --max-pages <int>         (default: 50)
  --tweet-fields / --expansions / --user-fields / --no-json / --ndjson
  ※ 動作: GetTweet(id, tweet.fields=conversation_id) → SearchRecent(query=conversation_id:CONV)
  ※ conversation_id 欠落時は plain error (exit 1)
  ※ JSON / Human は created_at 昇順ソート、NDJSON は X API 順 (新しい順) のまま

x timeline tweets [<ID>]    ※ M31、GET /2/users/:id/tweets
  --user-id <id>            (default: 認証ユーザー、位置引数 <ID> 指定時は --user-id を上書き)
  --max-results <1-100>     (default: 100、5..100 の X API 仕様。1..4 は --all=false 時に 5 を投げて [:n]、--all=true 時は exit 2)
  --start-time <RFC3339>    / --end-time <RFC3339>
  --since-jst YYYY-MM-DD    / --yesterday-jst   (liked と同優先順位: yesterday-jst > since-jst > start/end)
  --since-id <s>            / --until-id <s>     (X API 仕様で時間窓と独立、併用可能)
  --pagination-token <s>     (--all 時は警告して無視)
  --all                     (next_token 自動辿り)
  --max-pages <int>         (default: 50)
  --exclude <csv>           (retweets / replies)
  --tweet-fields / --expansions / --user-fields / --media-fields / --no-json / --ndjson

x timeline mentions [<ID>]  ※ M31、GET /2/users/:id/mentions
  (tweets と同じフラグセット、ただし --exclude は X API 仕様で非対応のためフラグ未登録)
  --user-id <id>            (default: 認証ユーザー、位置引数 <ID> 指定時は --user-id を上書き)
  --max-results <1-100>     (5..100 の X API 仕様。1..4 補正規則は tweets と同じ)
  --start-time / --end-time / --since-jst / --yesterday-jst / --since-id / --until-id
  --pagination-token / --all / --max-pages
  --tweet-fields / --expansions / --user-fields / --media-fields / --no-json / --ndjson

x timeline home             ※ M31、GET /2/users/:id/timelines/reverse_chronological (OAuth 1.0a 必須)
  ※ X API 仕様で認証ユーザー固定のため --user-id フラグは公開しない (GetUserMe で self 自動解決)
  --max-results <1-100>     (default: 100、X API 下限 1 なので補正なし)
  --start-time / --end-time / --since-jst / --yesterday-jst / --since-id / --until-id
  --pagination-token / --all / --max-pages
  --exclude <csv>           (retweets / replies)
  --tweet-fields / --expansions / --user-fields / --media-fields / --no-json / --ndjson

x user get [<ID|@username|URL>]   ※ M32、GET /2/users/:id or /2/users/by/username/:username
  --ids <csv>               (数値 ID 1..100、GET /2/users?ids=)
  --usernames <csv>         (username 1..100、GET /2/users/by?usernames=)
  ※ 位置引数 / --ids / --usernames は三者排他
  --user-fields / --expansions / --tweet-fields / --no-json

x user search <query>       ※ M32、GET /2/users/search
  --max-results <1-1000>    (default: 100、X API 仕様 1..1000)
  --pagination-token <s>    (X API クエリでは next_token にマップ)
  --all                     (next_token 自動辿り)
  --max-pages <int>         (default: 50)
  --user-fields / --expansions / --tweet-fields / --no-json / --ndjson

x user following [<ID|@username|URL>]   ※ M32、GET /2/users/:id/following
  --user-id <id>            (default: 認証ユーザー、位置引数指定時は上書き)
  ※ @username / URL 位置引数は GetUserByUsername で ID 解決後に呼び出し (2 API call)
  --max-results <1-1000>    (default: 100)
  --pagination-token <s>    / --all / --max-pages <int>
  --user-fields / --expansions / --tweet-fields / --no-json / --ndjson

x user followers [<ID|@username|URL>]   ※ M32、GET /2/users/:id/followers
  (following と同じフラグセット)

x user blocking             ※ M32、GET /2/users/:id/blocking (self only)
  ※ X API 仕様で self のみ参照可能のため --user-id フラグは公開しない (GetUserMe で self 自動解決)
  --max-results <1-1000>    / --pagination-token / --all / --max-pages
  --user-fields / --expansions / --tweet-fields / --no-json / --ndjson

x user muting               ※ M32、GET /2/users/:id/muting (self only)
  (blocking と同じフラグセット、--user-id は同様に未公開)

x list get <ID|URL>         ※ M33、GET /2/lists/:id
  ※ 位置引数は数値 ID または https://(x|twitter).com/i/lists/<NUM> URL のみ
  --list-fields / --expansions / --user-fields / --no-json

x list tweets <ID|URL>      ※ M33、GET /2/lists/:id/tweets
  --max-results <1-100>     (default: 100)
  --pagination-token <s>    / --all / --max-pages <int>
  --tweet-fields / --user-fields / --expansions / --media-fields / --no-json / --ndjson

x list members <ID|URL>     ※ M33、GET /2/lists/:id/members
  --max-results <1-100>     (default: 100)
  --pagination-token <s>    / --all / --max-pages <int>
  --user-fields / --expansions / --tweet-fields / --no-json / --ndjson

x list owned [<ID|@username|URL>]   ※ M33、GET /2/users/:id/owned_lists
  --user-id <id>            (default: 認証ユーザー、位置引数指定時は上書き)
  ※ @username / URL 位置引数は GetUserByUsername で ID 解決後に呼び出し (2 API call)
  --max-results <1-100>     / --pagination-token / --all / --max-pages
  --list-fields / --user-fields / --expansions / --no-json / --ndjson

x list followed [<ID|@username|URL>]   ※ M33、GET /2/users/:id/followed_lists
  (owned と同じフラグセット)

x list memberships [<ID|@username|URL>]   ※ M33、GET /2/users/:id/list_memberships
  (owned と同じフラグセット)

x list pinned               ※ M33、GET /2/users/:id/pinned_lists (self only)
  ※ X API 仕様で self のみ参照可能のため --user-id フラグは公開しない (GetUserMe で self 自動解決)
  ※ X API はページネーション非対応のため --all / --pagination-token / --max-results は登録しない
  --list-fields / --expansions / --user-fields / --no-json

x space get <ID|URL>        ※ M34、GET /2/spaces/:id (アクティブな Space のみ取得可)
  ※ 位置引数は英数字 Space ID (例: 1OdJrXWaPVPGX) または https://(x|twitter).com/i/spaces/<ID> URL を判別
  --space-fields / --expansions / --user-fields / --topic-fields / --no-json

x space by-ids --ids <csv>  ※ M34、GET /2/spaces?ids= (1..100、バッチ取得)
  ※ --ids は MarkFlagRequired 必須、位置引数なし (cobra.NoArgs)
  --space-fields / --expansions / --user-fields / --topic-fields / --no-json

x space search <query>      ※ M34、GET /2/spaces/search
  ※ X API はページネーション非対応 (docs.x.com で検証済) のため --all を提供しない
  --state live|scheduled|all / --max-results 1..100
  --space-fields / --expansions / --user-fields / --topic-fields / --no-json

x space by-creator --ids <csv>   ※ M34、GET /2/spaces/by/creator_ids?user_ids= (1..100)
  ※ --ids は MarkFlagRequired 必須、位置引数なし
  --space-fields / --expansions / --user-fields / --topic-fields / --no-json

x space tweets <ID|URL>     ※ M34、GET /2/spaces/:id/tweets
  --max-results 1..100 / --pagination-token / --all + --max-pages
  --no-json / --ndjson (排他) / --tweet-fields / --user-fields / --expansions / --media-fields

x trends get <woeid>        ※ M34、GET /2/trends/by/woeid/:woeid
  ※ X API パラメータ名は max_trends (≠ max_results)、上限 50
  ※ WOEID 例: 1118370 (東京) / 23424856 (日本) / 1 (全世界)
  --max-trends 1..50 / --trend-fields trend_name,tweet_count / --no-json

x trends personal           ※ M34、GET /2/users/personalized_trends (認証ユーザー固定)
  ※ X API 認証ヘッダから自動解決のため --user-id を公開しない
  ※ X API パラメータ名は personalized_trend.fields (≠ trend.fields)
  --personalized-trend-fields trend_name,category,post_count,trending_since / --no-json

x mcp
  --host <addr>             (default: 127.0.0.1, Lambda は 0.0.0.0)
  --port <int>              (default: 8080)
  --auth idproxy|apikey|none (default: idproxy)
  --apikey-env <name>       (apikey モード時, default: X_MCP_API_KEY)
  --path <path>             (MCP エンドポイント prefix, default: /mcp)

x configure
  対話形式で以下を生成:
    - ~/.config/x/config.toml          (非機密設定, perm 0644)
    - ~/.local/share/x/credentials.toml (X API トークン, perm 0600)
  シークレット系のキーが config.toml に書かれている場合はエラー終了する

x configure --print-paths
  各ファイルパスを出力 (XDG 環境変数を解決して表示)

x configure --check
  credentials.toml のパーミッション・必須キーの存在をチェック

x version
x completion bash|zsh|fish|powershell  (Cobra 標準で 4 シェル無料対応)
```

### MCP Tools

#### `get_user_me`
- 入力: なし
- 出力: `{ "user_id": "...", "username": "...", "name": "..." }`
- 内部実装: `xapi.GetUserMe()` をそのまま JSON 化

#### `get_liked_tweets`
- 入力 (JSON Schema):
  ```jsonc
  {
    "user_id": "string (optional, default: me)",
    "start_time": "string (RFC3339, optional)",
    "end_time":   "string (RFC3339, optional)",
    "since_jst":  "string (YYYY-MM-DD, optional; start/end を上書き)",
    "yesterday_jst": "boolean (optional; true なら JST 前日)",
    "max_results": "integer 1-100 (default: 100)",
    "all": "boolean (default: false; true なら next_token を辿る)",
    "max_pages": "integer (default: 50; all=true 時の上限)",
    "tweet_fields": "string[] (optional)",
    "expansions":   "string[] (optional)",
    "user_fields":  "string[] (optional)"
  }
  ```
- 出力: `{ "data": [...Tweet], "includes": { "users": [...User] }, "meta": { "result_count": N, "next_token": "..." } }`
- `all=true` 時はクライアント側で集約した結果を一括返却

### エラーハンドリングポリシー
- すべてのエラーは JSON エンベロープ: `{ "error": { "code": "string", "message": "string", "details": {...} } }`
- exit code 規約 (logvalet と同じ):
  - 0 = success
  - 1 = generic error
  - 2 = argument / validation error
  - 3 = auth error (X API 401 / idproxy 401)
  - 4 = permission error (X API 403)
  - 5 = not found (X API 404)
- X API 429 (rate limit) → exponential backoff (max 3 retry, base 2s, max 30s)
- X API 5xx → 同様にリトライ
- **Rate-limit aware ページング** (`--all` / `all=true` 時):
  - レスポンスヘッダ `x-rate-limit-remaining` / `x-rate-limit-reset` を毎回パース
  - `remaining ≤ 2` になったら `reset` 時刻まで sleep（最大 15 分）してから次ページ取得
  - 429 受信時は `x-rate-limit-reset` (UNIX秒) があればそこまで sleep、無ければ exponential backoff
  - ページ間の最小待機: 200ms (バースト抑止)
  - 上限: `--max-pages <int>` (default: 50) で暴走防止

## 7. Storage & Data Model

このリポジトリ内では**永続ストレージなし**。

- 設定: `~/.config/x/config.toml` (XDG)
- トークン: 環境変数または config.toml
- examples/lambroll/ では idproxy 用 DynamoDB テーブルを使う想定だが、テーブル定義は examples 配下のドキュメントとして提供のみ

## 8. Runtime Flows

### フロー1: CLI `x liked list --yesterday-jst --all`

| 段階 | 処理 |
|---|---|
| 1 | flags 解析 / config load (env > toml) |
| 2 | yesterday-jst → start_time/end_time を UTC RFC3339 に変換 |
| 3 | xapi.GetUserMe (user_id 未指定時) |
| 4 | xapi.ListLikedTweets を next_token が無くなるまでループ |
| 5 | 429/5xx は backoff リトライ。401/403/404 は exit code 3/4/5 |
| 6 | NDJSON または整形済み JSON を stdout |

### フロー2: MCP `get_liked_tweets`

| 段階 | 処理 |
|---|---|
| 1 | LWA → HTTP → mark3labs/mcp-go ルーター |
| 2 | authgate ミドルウェア: idproxy.Wrap or apikey check |
| 3 | tool ハンドラが引数バリデーション |
| 4 | xapi.ListLikedTweets を呼び出し |
| 5 | mcp.NewToolResultJSON で返却 |
| 6 | エラーは `_meta.error` に code/message |

### フロー3: Claude Code Routine (毎朝 JST 8:00, 本リポジトリのスコープ外だが参考)

```
1. x.get_user_me → user_id
2. x.get_liked_tweets(yesterday_jst=true, all=true) → tweets[]
3. Claude が tweets を技術判定 + トピックグルーピング
4. logvalet.find_issues(project=HEP_ISSUES, keyword=topic) で重複チェック
5. 重複なければ logvalet.create_issue を発行
```

## 9. Ranking / Decision Logic

技術判定とグルーピングは **Claude (Routine プロンプト) 側の責務**で、本リポジトリでは扱わない。MCP は素のデータを返す。

## 10. Technical Constraints

### 必須技術スタック
- Go 1.26.x (logvalet/kintone と統一)
- `github.com/mark3labs/mcp-go` v0.49.0 以上
- `github.com/spf13/cobra` v1.x (CLI パーサ。kong から変更)
- `github.com/spf13/pflag` v1.x (cobra の transitive)
- `github.com/dghubble/oauth1` v0.7.x
- `github.com/youyo/idproxy` v0.4.2 以上
- 標準 net/http (外部 framework なし)

### Store backend optional 依存

| バックエンド | パッケージ | 備考 |
|---|---|---|
| memory | (なし) | 標準 map ベース、デフォルト |
| sqlite | `modernc.org/sqlite` | pure Go (CGO 不要)、ローカル開発向け |
| redis | `github.com/redis/go-redis/v9` | 軽量サーバー向け、TTL ネイティブ |
| dynamodb | `github.com/aws/aws-sdk-go-v2/service/dynamodb` | Lambda マルチコンテナ向け、ConsistentRead 使用 |

### 外部依存

| サービス / API | 用途 | 制約 |
|---|---|---|
| X API v2 `/2/users/me` | self user lookup | OAuth 1.0a user context, ~75req/15min |
| X API v2 `/2/users/:id/liked_tweets` | Liked Posts 取得 | OAuth 1.0a, 75req/15min, max_results=100, $0.001/件 (Owned Reads) |
| Claude Code Routines | Daily トリガー | research preview, 仕様変更可能性あり |
| Redis (任意) | idproxy store backend | TTL 自動失効、`STORE_BACKEND=redis` で有効化 |
| DynamoDB (任意) | idproxy store backend | Lambda マルチコンテナ向け、`STORE_BACKEND=dynamodb` で有効化 |

### セキュリティ / コンプライアンス
- **シークレットを設定ファイルに書かない方針**:
  - `config.toml` (XDG_CONFIG_HOME) には**非機密設定のみ**。シークレット項目を書こうとした場合は `x configure` がエラー終了する
  - CLI 用シークレットは `~/.local/share/x/credentials.toml` (XDG_DATA_HOME, **perm 0600**) に分離
  - MCP モード時はシークレットを**環境変数のみ**から読み込み (credentials.toml は読まない)
- `.gitignore` に `credentials.toml` / `.env` / `.env.*` を含める。`config.toml` 自体は安全になったのでテンプレ可
- API Key モードの shared secret は constant-time 比較 (`subtle.ConstantTimeCompare`)
- 起動時に credentials.toml のパーミッションを検査し、`0600` でなければ警告 (POSIX のみ)
- ログに credential を出さない (構造化ログ + フィルタ)
- CORS は MCP では原則 disable (Routines は同一 origin で叩かない前提)
- examples/lambroll/ の function.json は SSM Parameter Store 参照テンプレ (`{{ ssm '/x-mcp/...' }}`)

## 11. Configuration

### 設計原則

シークレットと非機密設定を**ファイルパスで分離する** (XDG Base Directory Specification 準拠):

| 種別 | 保存先 | 用途 | 備考 |
|---|---|---|---|
| 非機密設定 (CLI) | `${XDG_CONFIG_HOME:-~/.config}/x/config.toml` | 動作設定 (出力形式、デフォルト fields など) | テンプレ化可、コミット可（ただし `.gitignore` の挙動は別途） |
| シークレット (CLI) | `${XDG_DATA_HOME:-~/.local/share}/x/credentials.toml` | X API OAuth 1.0a トークン | **perm 0600**、git 管理外 |
| すべての設定 (MCP) | **環境変数のみ** | X API トークン、idproxy 設定、apikey | ファイル読み込みは行わない |

### 設定ロード優先順位

#### CLI モード (`x me`, `x liked list`, `x configure` 等)
1. CLI flag
2. 環境変数 (`X_*` / `OIDC_*` / etc.)
3. credentials.toml (シークレットのみ)
4. config.toml (非機密のみ)
5. 組み込みデフォルト

#### MCP モード (`x mcp`)
1. CLI flag (`--host` / `--port` / `--auth` 等の動作系のみ)
2. 環境変数 (シークレット含むすべて)
3. 組み込みデフォルト

**MCP モードでは config.toml / credentials.toml を一切読まない。** Lambda 等の不変インフラ前提を明示化し、ローカルファイルに依存しない構成に統一する。

### 環境変数 一覧

#### X API (CLI / MCP 共通)

| 名前 | 用途 | 必須 |
|---|---|---|
| `X_API_KEY` | Consumer Key | ✅ |
| `X_API_SECRET` | Consumer Secret | ✅ |
| `X_ACCESS_TOKEN` | OAuth 1.0a Access Token | ✅ |
| `X_ACCESS_TOKEN_SECRET` | OAuth 1.0a Access Token Secret | ✅ |

> CLI モードに限り `credentials.toml` で代用可。MCP モードでは環境変数必須。

#### MCP 動作 / 認証 (MCP モード専用)

| 名前 | 用途 | 必須 |
|---|---|---|
| `X_MCP_HOST` | bind host (CLI フラグ優先) | (default: 127.0.0.1) |
| `X_MCP_PORT` | bind port (CLI フラグ優先) | (default: 8080) |
| `X_MCP_PATH` | MCP エンドポイント prefix | (default: /mcp) |
| `X_MCP_AUTH` | `idproxy` / `apikey` / `none` | (default: idproxy) |
| `X_MCP_API_KEY` | apikey モードの共有トークン (この値が一致すれば許可) | apikey 時 |
| `OIDC_ISSUER` | idproxy 設定 (カンマ区切りで複数可) | idproxy 時 |
| `OIDC_CLIENT_ID` | idproxy 設定 (カンマ区切り) | idproxy 時 |
| `OIDC_CLIENT_SECRET` | idproxy 設定 | (Issuer 依存) |
| `COOKIE_SECRET` | idproxy セッション暗号 (hex 32B+) | idproxy 時 |
| `EXTERNAL_URL` | idproxy 外部 URL | idproxy 時 |
| `STORE_BACKEND` | `memory` / `sqlite` / `redis` / `dynamodb` | (default: memory) |
| `SQLITE_PATH` | sqlite DB ファイルパス (`STORE_BACKEND=sqlite` 時) | (default: `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db`) |
| `REDIS_URL` | Redis 接続 URL (例: `redis://localhost:6379/0`) | redis 時 |
| `DYNAMODB_TABLE_NAME` | idproxy ストア | dynamodb 時 |
| `AWS_REGION` | AWS SDK | Lambda 時 / dynamodb 時 |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` | (default: info) |

### `~/.config/x/config.toml` (非機密のみ)

```toml
# CLI の動作デフォルト。シークレットを書いてはならない。
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

`x configure` はこのファイルを生成する際、シークレット系のキー (`api_key` / `access_token` 等) が存在した場合は **保存を拒否してエラー終了**する。

### `~/.local/share/x/credentials.toml` (CLI 用シークレット, perm 0600)

```toml
[xapi]
api_key             = "..."
api_secret          = "..."
access_token        = "..."
access_token_secret = "..."
```

`x configure` 実行時:
1. ディレクトリを `0700`、ファイルを `0600` で作成
2. 既存ファイルのパーミッションが緩い場合は警告
3. 環境変数 (`XDG_DATA_HOME`) で配置先を上書き可

### MCP モード起動例

```bash
# ローカル開発 (auth=none)
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
  x mcp --auth none

# apikey モード
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
X_MCP_API_KEY=$(openssl rand -hex 32) \
  x mcp --auth apikey

# idproxy + DynamoDB (Lambda 想定)
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
OIDC_ISSUER=https://accounts.google.com \
OIDC_CLIENT_ID=... OIDC_CLIENT_SECRET=... \
COOKIE_SECRET=$(openssl rand -hex 32) \
EXTERNAL_URL=https://x-mcp.example.com \
STORE_BACKEND=dynamodb DYNAMODB_TABLE_NAME=x-mcp-idproxy AWS_REGION=ap-northeast-1 \
  x mcp --auth idproxy --host 0.0.0.0
```

## 12. Directory Structure

```
x/
├── cmd/x/
│   └── main.go
├── internal/
│   ├── app/                  # exit codes
│   ├── cli/
│   │   ├── root.go
│   │   ├── me.go
│   │   ├── liked.go
│   │   ├── mcp.go
│   │   ├── configure.go
│   │   ├── version.go
│   │   └── completion.go
│   ├── config/
│   │   ├── config.go         # CLIConfig / MCPConfig 型定義
│   │   ├── xdg.go            # XDG_CONFIG_HOME / XDG_DATA_HOME 解決
│   │   ├── loader_cli.go     # CLI: flag/env/credentials.toml/config.toml/default
│   │   ├── loader_mcp.go     # MCP: flag/env/default (ファイル不使用)
│   │   ├── credentials.go    # credentials.toml R/W (perm 0600 強制)
│   │   └── guard.go          # config.toml にシークレットが含まれていたら拒否
│   ├── xapi/
│   │   ├── client.go
│   │   ├── oauth1.go
│   │   ├── users.go
│   │   ├── likes.go
│   │   └── types.go
│   ├── mcp/
│   │   ├── server.go
│   │   ├── tools_me.go
│   │   └── tools_likes.go
│   ├── authgate/
│   │   ├── gate.go
│   │   ├── idproxy.go
│   │   ├── apikey.go
│   │   └── none.go
│   ├── transport/http/
│   │   └── server.go
│   └── version/
│       └── version.go
├── examples/
│   └── lambroll/
│       ├── function.json
│       ├── function_url.json
│       ├── bootstrap
│       ├── .env.example
│       └── README.md
├── docs/
│   ├── specs/
│   │   └── x-spec.md            # ← このスペックの正式配置先
│   ├── routine-prompt.md        # Claude Code Routines プロンプトの参考
│   └── x-api.md                 # X API メモ
├── plans/
├── .github/
│   └── workflows/
│       ├── ci.yml               # lint + test
│       └── release.yml          # tag → GoReleaser
├── .goreleaser.yaml
├── mise.toml
├── Dockerfile
├── .gitignore
├── .golangci.yml
├── go.mod
├── go.sum
├── README.md
├── README.ja.md
├── LICENSE                       # MIT
├── CHANGELOG.md
└── CLAUDE.md
```

## 13. Release Strategy

- **リリース形態**: 段階的 (v0.1.0 = CLI のみ → v0.2.0 = MCP idproxy → v0.3.0 = lambroll examples)
- **フェーズ1完了条件 (v0.1.0)**:
  - `x me`, `x liked list --yesterday-jst --all` がローカルで動作
  - `go test -race ./...` pass、`golangci-lint run` 違反 0
  - README (英日) と LICENSE 整備
- **フェーズ2完了条件 (v0.2.0)**:
  - `x mcp --auth idproxy|apikey|none` の 3 モードが動作
  - MCP tools `get_user_me`, `get_liked_tweets` が mark3labs/mcp-go 互換
  - GoReleaser によるタグリリース + Homebrew Tap 自動更新
- **フェーズ3完了条件 (v0.3.0)**:
  - examples/lambroll/ 一式で **README に書いてあるコマンドだけで Lambda にデプロイ可能**
  - Claude Code Routines に組み込むためのプロンプト雛形 (docs/routine-prompt.md)
- **移行計画**: 既存システム無し。完全新規

## 14. Surrounding Files

### README.md (英) / README.ja.md (日)
- 概要 / Quick Start (CLI / MCP / Lambda) / Configuration / Tools 一覧 / FAQ

### docs/x-api.md
- X API v2 OAuth 1.0a 認証メモ、rate limit、課金、scope

### docs/routine-prompt.md
- Claude Code Routines に貼り付けるプロンプト雛形（提供仕様、本リポジトリは MCP までで責務終わり）

### CLAUDE.md
- 開発フロー（TDD, devflow, conventional commits 日本語）
- AGENTS.md は作成しない（CLAUDE.md に統一）

### examples/lambroll/README.md
- AWS アカウント前提 / SSM Parameter Store のキー命名 / OIDC プロバイダ設定 / DynamoDB テーブル作成手順

## 15. Phased Implementation Plan

### フェーズ 1 — CLI コア (v0.1.0)
1. リポジトリ初期化: `go mod init github.com/youyo/x`, Kong / oauth1 / testify 追加
2. `internal/xapi`: OAuth1 署名 + GetUserMe + ListLikedTweets (table-driven test + httptest)
3. `internal/config`: env > toml > default ロード (テスト含む)
4. `internal/cli`: root + me + liked + version + configure + completion
5. `cmd/x/main.go`: Kong パーサ統合
6. GoReleaser, mise.toml, Dockerfile, .golangci.yml, GitHub Actions CI
7. README 英日 + LICENSE (MIT)

### フェーズ 2 — MCP ラッパー (v0.2.0)
8. `internal/mcp`: mark3labs/mcp-go で `get_user_me`, `get_liked_tweets` 実装 (テスト含む)
9. `internal/authgate`: idproxy / apikey / none の 3 モード切替
10. `internal/transport/http`: LWA 互換 HTTP server (graceful shutdown)
11. `internal/cli/mcp`: `x mcp` サブコマンド
12. Streamable HTTP の end-to-end テスト (httptest + mcp client)

### フェーズ 3 — 公開配布 (v0.3.0)
13. GitHub Actions release.yml: tag push → GoReleaser → Homebrew tap / ghcr.io
14. examples/lambroll/: function.json / bootstrap / .env.example / README
15. docs/routine-prompt.md: 動作確認済みプロンプト雛形

## Open Questions

(初版時点では全て解決済み)

### 解決済み判断 (2026-05-12)

- [x] OIDC Issuer: **Google と Entra ID の両方を例として記載** (idproxy のカンマ区切り複数 issuer 機能を利用)
- [x] AGENTS.md: **作成しない** (CLAUDE.md に統一)
- [x] Docker image: **distroless で統一** (logvalet/kintone と揃える)
- [x] `--all` ページング rate limit 対応: **必要** — `x-rate-limit-remaining`/`x-rate-limit-reset` ヘッダ追従 + `--max-pages` (default: 50) で暴走防止 (詳細は §6 / §10)
- [x] AWS SAM 版 examples: **並置しない** (lambroll 一本)
- [x] Routine プロンプトの「技術判定」基準: **docs に明文化しない** (プロンプト埋め込みのみ。本リポジトリは MCP までで責務終わり)

## Changelog

| 日時 | 内容 |
|---|---|
| 2026-05-12 | 初版ドラフト作成 (devflow:spec) |
| 2026-05-12 | Open Questions 6項目を確定 (OIDC Issuer 複数記載 / AGENTS.md なし / distroless / rate-limit ページング / lambroll 一本 / Routine 基準は本リポ外) |
| 2026-05-12 | 設定ファイル分離方針を確定: config.toml は非機密のみ・credentials.toml (XDG_DATA_HOME, perm 0600) に CLI シークレット隔離・MCP モードは環境変数のみ |
| 2026-05-12 | CLI パーサを **kong → cobra** に変更 (ADR #4 修正): OSS デファクト + `__complete` 標準補完 + viper 統合 + 4 シェル無料対応 |
| 2026-05-12 | idproxy の 4 store backend (memory/sqlite/redis/dynamodb) を全部サポート (ADR #11 追加): §5 architecture / §10 必須技術スタック / §11 環境変数 (`SQLITE_PATH`/`REDIS_URL`/`STORE_BACKEND` 拡張) / §3 フェーズ2展望 を更新 |

---

## Next Action (実装フェーズへ進む場合)

スペック承認後:

1. `docs/specs/x-spec.md` にこのスペックを正式配置
2. `/devflow:roadmap` を実行してマイルストーン分解 + M1 詳細計画を作成
3. 続いて `/devflow:implement` で M1 (CLI コア v0.1.0) から着手

**実装はまだ開始しません。** このスペックの内容・Open Questions について先にフィードバックをください。
