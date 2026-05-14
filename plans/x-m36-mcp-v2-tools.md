# M36: MCP v2 Tools (CLI M29-M35 の薄いラッパー)

## Overview

| 項目 | 値 |
|------|---|
| ステータス | ✅ 完了 (2026-05-15) |
| 対象 v リリース | v0.8.0 |
| Phase | I: readonly API 包括サポート (第 8 回、MCP 対応 / Phase I 最終マイルストーン) |
| 依存 | **M29-M35 (CLI 全完了済)**, M15-M18 (既存 MCP 基盤) |
| 主要対象ファイル | `internal/mcp/tools_tweet.go` / `tools_search.go` / `tools_timeline.go` / `tools_users.go` / `tools_lists.go` / `tools_misc.go` (全新規) + `internal/mcp/tools_likes.go` (T0 修正) + `internal/mcp/server.go` (RegisterTool 拡張) |

## Goal

M29-M35 で追加した CLI コマンドに対応する **18 個** の MCP tools を追加する。CLI が主 / MCP は薄いラッパーという既存方針を踏襲し、xapi 層のメソッドを直接呼び出す。新たな store backend や設定ローダーは追加しない。Routine からは `get_tweet` / `search_recent_tweets` / `get_user_tweets` 等で X 上の任意の readonly データを参照できるようになる。

## Design Decisions (M35→M36 handoff 含む)

- **D-1 1 ハンドラ 1 ツール**: tools_me.go / tools_likes.go と同じく `NewGetXxxHandler(client)` factory + `registerToolXxx(s, client)` ペア
- **D-2 Default values**: 各ツールは Default を「ハンドラ最小限」に抑える。CLI の loadLikedDefaults 相当のものは tools_likes.go の `likedDefaultTweetFields` 以外には作らない (MCP モードは env のみで file 読まない不変条件のため)。default を強制したい場合は引数省略時に xapi のデフォルト動作 (フィールド省略) に任せる
- **D-3 自前 argString/argBool/argInt/argStringSliceOrDefault は tools_likes.go の package-private ヘルパを再利用** (同パッケージなのでアクセス可)
- **D-4 user_id 省略時の self 解決**: timeline 3 ツールのみ implement (CLI と同じ振る舞い)。tweet 系・user 系・list 系では明示必須
- **D-5 URL→ID 解決は MCP 層では行わない**: MCP は ID 文字列を素直に受ける。URL 解決は CLI 層 (extractTweetID 等) の責務
- **D-6 出力スキーマ**: 各ツールは xapi の Response 型をそのまま StructuredContent + TextContent (JSON) として返す。MCP 仕様の "user_id" リネームは get_user_me のみ既存実装 (互換のため温存) で、他は xapi.User.ID の "id" タグそのまま (DTO 化しない、CLI と同じ JSON 出力)
- **D-7 JST 系**: `search_recent_tweets` / `get_user_tweets` / `get_user_mentions` / `get_home_timeline` の 4 ツールに `start_time` / `end_time` / `since_jst` / `yesterday_jst` を実装。優先順位は M18 と同じ (yesterday_jst > since_jst > start/end)。共通ヘルパ `parseJSTDate` / `yesterdayJSTRange` を再利用
- **D-8 max_results 下限補正**: `search_recent_tweets` / `get_tweet_thread` で `max_results < 10` 時に API min (10) に補正して `truncateTo` で出力件数を縮める処理は **MCP では行わない** (UX が複雑化するため)。代わりに `max_results >= 10` をバリデーション、`< 10` はエラー (CLI 側で `--all` と組み合わせる時の挙動再現は CLI が責務)
- **D-9 tweet_thread**: CLI と同じく 2 段呼び出し (GetTweet → SearchRecent)。`--author-only` 相当の `author_only` パラメータを実装。**CLI JSON 出力と整合させるため最終出力を `created_at` 昇順 sort** する (MCP 出力は JSON / Structured で CLI JSON モードと等価)。author_only フィルタは aggregator の `add()` callback でページ単位に適用してメモリ効率を保つ
- **D-10 全件取得 (all)**: liked_tweets と同じ `all` / `max_pages` セマンティクスで search / timeline / following / followers / list_tweets に対応。各々独自の aggregator を内部実装 (CLI 層と同じ独立実装方針 = D-5 in M35)
- **D-11 ファイル単位 commit**: T1-T6 を 1 ファイル = 1 commit にして diff レビューを容易に

## T0 (M29 handoff): tools_likes.go の likedDefaultTweetFields に note_tweet 追加

CLI M29 で `liked list` の既定 tweet.fields に `note_tweet` を追加したが、MCP モードは config.toml を読まないため自動波及しない。本 M36 着手時に最初に対応する。

- `internal/mcp/tools_likes.go:29` の `likedDefaultTweetFields` に `"note_tweet"` を追加
- 既存テストは generic な assertion (部分一致) のため、追加後も pass する想定

## T1 — tools_tweet.go (5 tools)

| Tool | 引数 (param: type) | xapi 呼び出し |
|------|---|---|
| `get_tweet` | `tweet_id: string (req)`, `tweet_fields: []string`, `expansions: []string`, `user_fields: []string`, `media_fields: []string` | `GetTweet(ctx, id, opts...)` |
| `get_tweets` | `tweet_ids: []string (req, 1-100)`, `tweet_fields`, `expansions`, `user_fields`, `media_fields` | `GetTweets(ctx, ids, opts...)` |
| `get_liking_users` | `tweet_id: string (req)`, `max_results (1-100)`, `pagination_token`, `user_fields`, `expansions`, `tweet_fields` | `GetLikingUsers(ctx, id, opts...)` |
| `get_retweeted_by` | 同上 | `GetRetweetedBy(ctx, id, opts...)` |
| `get_quote_tweets` | `tweet_id`, `max_results (1-100)`, `pagination_token`, `exclude: []string`, `tweet_fields`, `expansions`, `user_fields`, `media_fields` | `GetQuoteTweets(ctx, id, opts...)` |

## T2 — tools_search.go (2 tools)

| Tool | 引数 |
|------|---|
| `search_recent_tweets` | `query: string (req)`, `start_time`, `end_time`, `since_jst`, `yesterday_jst`, `max_results (10-100)`, `all`, `max_pages`, `pagination_token`, `tweet_fields`, `expansions`, `user_fields`, `media_fields` |
| `get_tweet_thread` | `tweet_id: string (req)`, `author_only: bool`, `max_results (10-100)`, `all`, `max_pages`, `pagination_token`, `tweet_fields`, `expansions`, `user_fields` |

`get_tweet_thread` のロジック:
1. `GetTweet(id, fields=conversation_id,author_id,created_at)` で root tweet を取得
2. `SearchRecent(query="conversation_id:<conv_id>", ...)` でスレッド構成ツイートを取得
3. `author_only=true` の場合 `tweet.AuthorID == rootAuthor` でフィルタ
4. `all=true` で aggregator により全件集約 (sort は NDJSON と無関係なので CLI とは異なり MCP は時系列昇順 sort も省略 → API order のまま返す。D-9)

## T3 — tools_timeline.go (3 tools)

| Tool | 引数 |
|------|---|
| `get_user_tweets` | `user_id` (省略可, self 解決), `max_results (5-100)`, `start_time`, `end_time`, `since_jst`, `yesterday_jst`, `since_id`, `until_id`, `pagination_token`, `exclude: []string`, `all`, `max_pages`, `tweet_fields`, `expansions`, `user_fields`, `media_fields` |
| `get_user_mentions` | 同上 (exclude なし) |
| `get_home_timeline` | 同上 (exclude=replies/retweets のみ) |

## T4 — tools_users.go (4 tools)

| Tool | 引数 |
|------|---|
| `get_user` | `user_id (req)`, `user_fields`, `expansions`, `tweet_fields` |
| `get_user_by_username` | `username (req)`, `user_fields`, `expansions`, `tweet_fields` |
| `get_user_following` | `user_id (req)`, `max_results (1-1000)`, `pagination_token`, `all`, `max_pages`, `user_fields`, `expansions`, `tweet_fields` |
| `get_user_followers` | 同上 |

## T5 — tools_lists.go (2 tools)

| Tool | 引数 |
|------|---|
| `get_list` | `list_id (req)`, `list_fields`, `expansions`, `user_fields` |
| `get_list_tweets` | `list_id (req)`, `max_results (1-100)`, `pagination_token`, `all`, `max_pages`, `list_fields`, `tweet_fields`, `user_fields`, `expansions`, `media_fields` |

## T6 — tools_misc.go (2 tools)

| Tool | 引数 |
|------|---|
| `search_spaces` | `query: string (req)`, `state: string (live/scheduled/all)`, `max_results (1-100)`, `space_fields`, `expansions`, `user_fields`, `topic_fields` |
| `get_trends` | `woeid: int (req)`, `max_trends (10-50)`, `trend_fields` |

## T7 — server.go RegisterTool 全 18 ツール登録

`NewServer` 内に `registerToolXxx(s, client)` を追加 (既存 me / likes と同じパターン)。

## T8 — 検証 + Docs 更新

- `go test -race -count=1 ./...` 全 pass (新規 70+ ケース想定)
- `GOLANGCI_LINT_CACHE=$TMPDIR/golangci-cache golangci-lint run ./...` 0 issues
- `go vet ./...` 0
- `docs/routine-prompt.md`: 新ツールの使用例 (`get_tweet` / `search_recent_tweets` / `get_user_tweets`) 追記
- `README.md` / `README.ja.md`: MCP ツール表に 18 新ツールを追加
- `CHANGELOG.md`: `[0.8.0]` セクション追加
- `plans/x-roadmap.md`: M36 ✅ 完了マーク + Current Focus 更新

## Completion Criteria

- 全 18 ツールが MCP client (mark3labs/mcp-go テストクライアント) で `tools/list` に表示される
- `tools/call name=get_tweet` が httptest E2E で成功する
- `tools/call name=search_recent_tweets` のバリデーション・JST 優先順位がテストで pin される
- TestLoadMCPCredentials_IgnoresFile が引き続き pass する (新ツールが file 読まない不変条件)
- `docs/routine-prompt.md` に新ツールの使用例が記載されている

## 既存方針との整合

- **CLI が主 / MCP は薄いラッパー**: xapi 層のメソッドを直接呼び出す。CLI の RunE と同じロジックを重複させない
- **env のみ**: `loadMCPCredentials` (M24) を引き続き使用。`credentials.toml` は絶対に読まない (Lambda 不変インフラ前提)
- **パッケージ doc は書かない**: 既存 `internal/mcp/doc.go` に集約済み
- **エラー番兵の再利用**: 新しい exit code は追加しない。既存 `xapi.ErrAuthentication` / `ErrPermission` / `ErrNotFound` で対応

## Risks

| リスク | 影響 | 対策 |
|---|---|---|
| 18 ツール一気に追加でコードレビューが困難 | 中 | T1-T6 を 1 ファイル = 1 commit で分割 |
| JST 系ヘルパの重複 | 低 | tools_likes.go の private 関数を再利用 (同パッケージ) |
| max_results の API 仕様差異 (timeline=5-100, following=1-1000 等) | 中 | 各ツール定義に gomcp.Min/Max で明示 |
| aggregator の重複実装 | 中 | 各 tool ファイル内に独立 aggregator (CLI 層と同じ "独立実装" 方針) |
