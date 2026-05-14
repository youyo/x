# ロードマップ更新プラン: readonly API 包括サポート (M29〜M36)

## Context

ユーザー要望: 「参照系 (GET) の X API v2 を基本的にすべてサポートする。write/更新系は不要。readonly で。」

researcher による網羅調査 (docs.x.com + X Developers Community) の結果、X API v2 には **60+ の GET エンドポイント** が存在。既実装は以下 2 件のみ:

- `GET /2/users/me` (M9)
- `GET /2/users/:id/liked_tweets` (M8/M10/M11)

### 除外対象 (今回実装しない)

| 理由 | エンドポイント |
|------|---------------|
| **Streaming** (1 ショット CLI 設計と非整合) | `search/stream`, `sample/stream`, `sample10/stream`, `activity/stream` |
| **OAuth 1.0a 非対応 + Bearer Token 必須** | `search/all`, `counts/recent`, `counts/all`, `usage/tweets`, `compliance/jobs` |
| **OAuth 2.0 PKCE 専用** | `bookmarks` 系, `spaces/:id/buyers` |
| **Enterprise 専用** | `compliance/*`, `activity/*`, `webhooks`, `sample10/stream` |
| **ドキュメント整備が薄い** | `news/*` (Limited), `communities/*` (将来検討) |

### 実装対象の総数

OAuth 1.0a User Context で利用可能な readonly エンドポイント: **約 35 件** (Basic tier 以上)

---

## マイルストーン分割方針

```
v0.4.0: M29 (Posts Lookup + note_tweet + Social Signals)
v0.5.0: M30 (Search + Thread) + M31 (Timelines)
v0.6.0: M32 (Users Extended) + M33 (Lists)
v0.7.0: M34 (Spaces + Trends) + M35 (DM Read, Pro 推奨)
v0.8.0: M36 (MCP v2 全 tools — CLI M29-M35 の薄いラッパー)
```

---

## M29: Posts Lookup + Note Tweet 既定 + Social Signals + liked 改善

### Meta

| 項目 | 値 |
|---|---|
| M 番号 | M29 |
| タイトル | Posts Lookup / Social Signals + Note Tweet 既定 + liked 下限補正 |
| 対象 v リリース | v0.4.0 |
| Phase | I: readonly API 包括サポート (第 1 回) |
| 依存 | M5-M8 (xapi 基盤), M10-M11 (liked list), M12 (config [liked]) |

### Goal

`x tweet get <ID|URL>` / `x tweet get --ids ID1,ID2,...` でツイートを直接取得し、 `x tweet liking-users`, `x tweet retweeted-by`, `x tweet quote-tweets` で Social Signals を確認。`liked list` の note_tweet 既定取得と `--max-results<5` 補正も本 M に含める。

### 対象エンドポイント

| API | 説明 |
|-----|------|
| `GET /2/tweets/:id` | 単一 Post 取得 (URL→ID 自動変換) |
| `GET /2/tweets` | 最大 100 件バッチ取得 (`--ids` フラグ) |
| `GET /2/tweets/:id/liking_users` | いいねしたユーザー一覧 |
| `GET /2/tweets/:id/retweeted_by` | RT したユーザー一覧 |
| `GET /2/tweets/:id/quote_tweets` | 引用ツイート一覧 |

### Tasks

- [ ] **T1**: `internal/xapi/types.go` — `Tweet.NoteTweet *NoteTweet` / `Tweet.ConversationID string` / `NoteTweet` 型追加、テスト 3 ケース
- [ ] **T2**: `internal/xapi/tweets.go` 新規 — `GetTweet(ctx, id, opts...)` + `GetTweets(ctx, ids[], opts...)` + `TweetOption` 群、httptest テスト (200/401/404/batch)
- [ ] **T3**: `internal/xapi/tweets.go` 拡張 — `GetLikingUsers(ctx, tweetID, opts...)` + `GetRetweetedBy` + `GetQuoteTweets` + `UsersByTweetOption` / `QuoteTweetsOption`、テスト追加
- [ ] **T4**: `internal/cli/tweet.go` 新規 — `newTweetCmd()` / `newTweetGetCmd()` / `newTweetSocialCmd()` (liking-users/retweeted-by/quote-tweets)、URL→ID 抽出 (`extractTweetID`)、`tweetClient` interface + `newTweetClient` package var
- [ ] **T5**: `internal/cli/root.go` — `AddCommand(newTweetCmd())` 1 行追加
- [ ] **T6**: `internal/cli/liked.go` 改修 — `config.LikedSection.DefaultTweetFields` に `note_tweet` 追加、`--max-results<5` を 5 に補正して CLI 側 `resp.Data[:n]` で絞る、`writeLikedHuman` を `note_tweet.text` 優先に変更
- [ ] **T7** (Refactor): `internal/xapi/pagination.go` 抽出 (EachLikedPage の rate-limit sleep を共通化)
- [ ] **T8** (検証 + Docs): `go test -race ./...` pass、lint 0、`x tweet get <ID>` / `x tweet get <URL>` 実機、`liked list --max-results 1` 実機、spec §6 / x-api.md / CHANGELOG [0.4.0] 更新

### Completion Criteria

- `go test -race -count=1 ./...` 全 pass (最低 25 新規テスト)
- `golangci-lint run ./...` 0 issues
- `x tweet get <URL>` で note_tweet.text 表示
- `x liked list --max-results 1` で API エラーなし 1 件表示
- `x tweet liking-users <ID>` でいいねユーザー一覧表示

---

## M30: Search Recent + Thread コマンド

### Meta

| 項目 | 値 |
|---|---|
| M 番号 | M30 |
| タイトル | tweet search / thread コマンド (search/recent + conversation_id) |
| 対象 v リリース | v0.5.0 |
| Phase | I: readonly API 包括サポート (第 2 回) |
| 依存 | M29 (GetTweet, ConversationID フィールド, pagination 共通化) |
| Tier 要件 | **Basic 以上** (`search/recent` は Free 非対応) |

### Goal

`x tweet search <query>` (過去 7 日検索) と `x tweet thread <ID>` (スレッド全体取得) を追加。

### 対象エンドポイント

| API | 説明 |
|-----|------|
| `GET /2/tweets/search/recent` | 過去 7 日間のキーワード検索 |
| `GET /2/tweets/:id` (conv_id 取得) + `GET /2/tweets/search/recent?query=conversation_id:XXX` | スレッド取得 |

### Tasks

- [ ] **T1**: `internal/xapi/tweets.go` 拡張 — `SearchRecent(ctx, query, opts...)` + `EachSearchPage` (rate-limit aware, pagination 共通化から流用) + `SearchOption` 群 (start/end time / max_results / expansions / fields / next_token / max_pages)
- [ ] **T2**: `internal/cli/tweet.go` 拡張 — `newTweetSearchCmd()` (positional query + JST 系フラグ + --all / --ndjson / --no-json)、JST 優先順位 = liked と同規約
- [ ] **T3**: `internal/cli/tweet.go` 拡張 — `newTweetThreadCmd()` (--author-only を CLI 層で `AuthorID` フィルタ、D-3 方式)
- [ ] **T4** (検証 + Docs): `x tweet search "query" --yesterday-jst`、`x tweet thread <ID> --author-only` 実機、docs/x-api.md に search/recent Tier 要件と conversation_id 演算子追記、CHANGELOG [0.5.0] draft

### Completion Criteria

- search/thread テスト最低 15 ケース
- `x tweet search "from:USER" --max-results 10` 動作 (Basic tier)
- `x tweet thread <ID> --author-only` でスレッドのみ表示

---

## M31: User Timelines

### Meta

| 項目 | 値 |
|---|---|
| M 番号 | M31 |
| タイトル | User Timelines (tweets / mentions / home) |
| 対象 v リリース | v0.5.0 |
| Phase | I: readonly API 包括サポート (第 3 回) |
| 依存 | M29 (xapi 基盤 / Tweet DTO) |

### Goal

`x timeline tweets <ID>` / `x timeline mentions <ID>` / `x timeline home` でユーザータイムラインを取得。

### 対象エンドポイント

| API | 説明 | max_results |
|-----|------|-------------|
| `GET /2/users/:id/tweets` | User's tweets | 5〜100 |
| `GET /2/users/:id/mentions` | User's mentions | 5〜100 |
| `GET /2/users/:id/timelines/reverse_chronological` | Home timeline (認証ユーザー) | 1〜100 |

### Tasks

- [ ] **T1**: `internal/xapi/timeline.go` 新規 — `GetUserTweets` / `GetUserMentions` / `GetHomeTimeline` + `TimelineOption` 群 + `EachTimelinePage`
- [ ] **T2**: `internal/cli/timeline.go` 新規 — `newTimelineCmd()` / `newTimelineTweetsCmd()` / `newTimelineMentionsCmd()` / `newTimelineHomeCmd()`、`timelineClient` interface + `newTimelineClient` package var
- [ ] **T3**: `internal/cli/root.go` — `AddCommand(newTimelineCmd())`
- [ ] **T4** (検証 + Docs): 実機 + テスト最低 15 ケース + CHANGELOG

### Completion Criteria

- `x timeline tweets <ID> --no-json` で時系列ツイート表示
- `x timeline home --since-jst 2026-05-10` で JST 範囲フィルタ動作

---

## M32: Users Extended (lookup / followers / following / blocking / muting)

### Meta

| 項目 | 値 |
|---|---|
| M 番号 | M32 |
| タイトル | Users Extended — lookup / graph / blocking / muting |
| 対象 v リリース | v0.6.0 |
| Phase | I: readonly API 包括サポート (第 4 回) |
| 依存 | M7 (GetUserMe), M29 (User DTO) |

### Goal

`x user get <ID|@username>` / `x user followers` / `x user following` / `x user blocking` / `x user muting` / `x user search <query>` でユーザー情報を取得。

### 対象エンドポイント

| API | 説明 |
|-----|------|
| `GET /2/users/:id` | 単一ユーザー ID 取得 |
| `GET /2/users` | バッチ取得 (max 100) |
| `GET /2/users/by/username/:username` | @username 指定取得 |
| `GET /2/users/by` | 複数 username バッチ |
| `GET /2/users/search` | キーワード検索 |
| `GET /2/users/:id/following` | フォロー中一覧 |
| `GET /2/users/:id/followers` | フォロワー一覧 |
| `GET /2/users/:id/blocking` | ブロック中 (自分のみ) |
| `GET /2/users/:id/muting` | ミュート中 (自分のみ) |

注意: `blocking` / `muting` は自アカウントのみ参照可能 (`--user-id` デフォルト=自分)。

### Tasks

- [ ] **T1**: `internal/xapi/users.go` 拡張 — `GetUser` / `GetUsers` / `GetUserByUsername` / `GetUsersByUsernames` / `SearchUsers` / `GetFollowing` / `GetFollowers` / `GetBlocking` / `GetMuting` + `UserGraphOption` 群 + `EachUserGraphPage`
- [ ] **T2**: `internal/cli/user.go` 新規 — `newUserCmd()` / 各サブコマンド factory、`userClient` interface + `newUserClient`
- [ ] **T3**: `internal/cli/root.go` — `AddCommand(newUserCmd())`
- [ ] **T4** (検証 + Docs): テスト最低 20 ケース + 実機 + CHANGELOG

### Completion Criteria

- `x user get @youyo` でユーザー情報表示
- `x user followers --no-json` でフォロワー一覧
- `x user blocking` で自分のブロックリスト表示

---

## M33: Lists

### Meta

| 項目 | 値 |
|---|---|
| M 番号 | M33 |
| タイトル | Lists — lookup / tweets / members / owned / followed |
| 対象 v リリース | v0.6.0 |
| Phase | I: readonly API 包括サポート (第 5 回) |
| 依存 | M29 (Tweet DTO), M32 (User DTO 安定化後) |

### Goal

`x list get <ID>` / `x list tweets <ID>` / `x list members <ID>` / `x list owned` / `x list followed` / `x list memberships` でリスト操作を行う。

### 対象エンドポイント

| API | 説明 |
|-----|------|
| `GET /2/lists/:id` | List 詳細 |
| `GET /2/lists/:id/tweets` | List のツイート一覧 |
| `GET /2/lists/:id/members` | List メンバー一覧 |
| `GET /2/users/:id/owned_lists` | 所有 List 一覧 |
| `GET /2/users/:id/list_memberships` | 参加 List 一覧 |
| `GET /2/users/:id/followed_lists` | フォロー中 List 一覧 |
| `GET /2/users/:id/pinned_lists` | ピン留め List 一覧 |

### Tasks

- [ ] **T1**: `internal/xapi/lists.go` 新規 — `GetList` / `GetListTweets` / `GetListMembers` / `GetOwnedLists` / `GetListMemberships` / `GetFollowedLists` / `GetPinnedLists` + `ListOption` 群
- [ ] **T2**: `internal/cli/list.go` 新規 — `newListCmd()` / 各サブコマンド factory、`listClient` interface
- [ ] **T3**: `internal/cli/root.go` — `AddCommand(newListCmd())`
- [ ] **T4** (検証 + Docs): テスト最低 15 ケース + 実機 + CHANGELOG

### Completion Criteria

- `x list tweets <ID> --no-json` でリストのツイート表示
- `x list owned` で自分の所有リスト一覧表示

---

## M34: Spaces + Trends

### Meta

| 項目 | 値 |
|---|---|
| M 番号 | M34 |
| タイトル | Spaces lookup / search + Trends |
| 対象 v リリース | v0.7.0 |
| Phase | I: readonly API 包括サポート (第 6 回) |
| 依存 | M29 (xapi 基盤) |
| 注意 | Spaces はアクティブな Space のみ取得可 (終了済みは不可) |

### Goal

`x space get <ID>` / `x space search <query>` でアクティブ Space を取得。`x trends get <woeid>` / `x trends personal` でトレンドを確認。

### 対象エンドポイント

| API | 説明 |
|-----|------|
| `GET /2/spaces/:id` | Space 詳細 |
| `GET /2/spaces` | 複数 Space バッチ |
| `GET /2/spaces/search` | キーワード検索 |
| `GET /2/spaces/by/creator_ids` | 作成者 ID 指定 |
| `GET /2/spaces/:id/tweets` | Space 内 Tweet |
| `GET /2/trends/by/woeid/:id` | WOEID 指定トレンド (東京=1118370) |
| `GET /2/users/personalized_trends` | パーソナライズトレンド |

### Tasks

- [ ] **T1**: `internal/xapi/spaces.go` 新規 + `internal/xapi/trends.go` 新規
- [ ] **T2**: `internal/cli/space.go` 新規 + `internal/cli/trends.go` 新規
- [ ] **T3**: `internal/cli/root.go` — AddCommand × 2
- [ ] **T4** (検証 + Docs): テスト最低 12 ケース + CHANGELOG

### Completion Criteria

- `x space search "AI" --no-json` でアクティブ Space 検索
- `x trends get 1118370 --no-json` で東京トレンド表示

---

## M35: DM Read (Pro 推奨)

### Meta

| 項目 | 値 |
|---|---|
| M 番号 | M35 |
| タイトル | Direct Messages Read (Pro 推奨) |
| 対象 v リリース | v0.7.0 |
| Phase | I: readonly API 包括サポート (第 7 回) |
| 依存 | M29 (xapi Client) |
| Tier 要件 | Basic では 1 回/24h 程度の厳しいレート制限あり。**実用的な利用には Pro 推奨** |

### Goal

`x dm list` / `x dm conversation <ID>` で DM を取得。Pro 環境での実機検証を前提。

### 対象エンドポイント

| API | 説明 |
|-----|------|
| `GET /2/dm_events` | 全 DM イベント |
| `GET /2/dm_events/:id` | 単一 DM イベント |
| `GET /2/dm_conversations/:id/dm_events` | 会話のイベント |
| `GET /2/dm_conversations/with/:participant_id/dm_events` | 特定ユーザーとの DM |

### Tasks

- [ ] **T1**: `internal/xapi/dm.go` 新規 — `GetDMEvents` / `GetDMConversation` / `GetDMWithUser` + `DMOption` 群
- [ ] **T2**: `internal/cli/dm.go` 新規 — `newDMCmd()` / `newDMLisCmd()` / `newDMConversationCmd()`
- [ ] **T3**: `internal/cli/root.go` — `AddCommand(newDMCmd())`
- [ ] **T4** (検証 + Docs): テスト最低 10 ケース、docs に Basic tier の DM 制限を明記、CHANGELOG

### Completion Criteria

- `x dm list --no-json` で直近 DM 一覧 (Pro 環境で実機検証)
- docs/x-api.md に DM の tier 制限が明記されている

---

## M36: MCP v2 Tools (CLI M29-M35 の薄いラッパー)

### Meta

| 項目 | 値 |
|---|---|
| M 番号 | M36 |
| タイトル | MCP v2 Tools — M29-M35 CLI の MCP tool 対応 |
| 対象 v リリース | v0.8.0 |
| Phase | I: readonly API 包括サポート (第 8 回、MCP 対応) |
| 依存 | M29-M35 (CLI 全完了後), M15-M18 (既存 MCP 基盤) |

### Goal

M29-M35 で追加した CLI コマンドに対応する MCP tools を追加。CLI が主でMCP は薄いラッパー (既存方針)。

### 対象 MCP Tools (予定)

| Tool 名 | 対応 CLI | 対応 API |
|---------|----------|----------|
| `get_tweet` | `tweet get` | `GET /2/tweets/:id` |
| `get_tweets` | `tweet get --ids` | `GET /2/tweets` |
| `get_liking_users` | `tweet liking-users` | `GET /2/tweets/:id/liking_users` |
| `get_retweeted_by` | `tweet retweeted-by` | `GET /2/tweets/:id/retweeted_by` |
| `get_quote_tweets` | `tweet quote-tweets` | `GET /2/tweets/:id/quote_tweets` |
| `search_recent_tweets` | `tweet search` | `GET /2/tweets/search/recent` |
| `get_tweet_thread` | `tweet thread` | search/recent + conversation_id |
| `get_user_tweets` | `timeline tweets` | `GET /2/users/:id/tweets` |
| `get_user_mentions` | `timeline mentions` | `GET /2/users/:id/mentions` |
| `get_home_timeline` | `timeline home` | `GET /2/users/:id/timelines/reverse_chronological` |
| `get_user` | `user get` | `GET /2/users/:id` |
| `get_user_by_username` | `user get @username` | `GET /2/users/by/username/:username` |
| `get_user_following` | `user following` | `GET /2/users/:id/following` |
| `get_user_followers` | `user followers` | `GET /2/users/:id/followers` |
| `get_list` | `list get` | `GET /2/lists/:id` |
| `get_list_tweets` | `list tweets` | `GET /2/lists/:id/tweets` |
| `search_spaces` | `space search` | `GET /2/spaces/search` |
| `get_trends` | `trends get` | `GET /2/trends/by/woeid/:id` |

### Tasks

- [ ] `internal/mcp/tools_tweet.go` — get_tweet / get_tweets / get_liking_users / get_retweeted_by / get_quote_tweets
- [ ] `internal/mcp/tools_search.go` — search_recent_tweets / get_tweet_thread
- [ ] `internal/mcp/tools_timeline.go` — get_user_tweets / get_user_mentions / get_home_timeline
- [ ] `internal/mcp/tools_users.go` — get_user / get_user_by_username / get_user_following / get_user_followers
- [ ] `internal/mcp/tools_lists.go` — get_list / get_list_tweets
- [ ] `internal/mcp/tools_misc.go` — search_spaces / get_trends
- [ ] MCP tools の各 テスト + docs/routine-prompt.md 更新
- [ ] README / CHANGELOG [0.8.0]

### Completion Criteria

- MCP client で `get_tweet`, `search_recent_tweets` が動作
- `docs/routine-prompt.md` に新ツールの使用例が記載
- `TestLoadMCPCredentials_IgnoresFile` パターンが新ツールに適用されている

---

## ロードマップ更新内容 (承認後に実行)

### 1. Meta セクション修正

```diff
- | 最終更新 | 2026-05-12 (M28 完了) |
- | ステータス | ✅ 完了 (全 28 マイルストーン) |
- | マイルストーン総数 | 28 (細粒度方針) |
+ | 最終更新 | 2026-05-14 (M29-M36 追加キューイング) |
+ | ステータス | 🚧 v0.3.0 完了 / v0.4.0〜v0.8.0 計画中 |
+ | マイルストーン総数 | 36 (M29-M36 追加、細粒度方針) |
```

### 2. Current Focus 差し替え

全 28 完了の記述を以下に変更:

```
M28 までの全 28 マイルストーンが完了 (v0.3.0 リリース準備完了)。
次フェーズとして readonly API 包括サポート (M29-M36) を計画中。

優先順:
1. M29: Posts Lookup + Note Tweet + Social Signals (v0.4.0)
2. M30: Search + Thread (v0.5.0)
3. M31: User Timelines (v0.5.0)
4. M32: Users Extended (v0.6.0)
5. M33: Lists (v0.6.0)
6. M34: Spaces + Trends (v0.7.0)
7. M35: DM Read (v0.7.0, Pro 推奨)
8. M36: MCP v2 Tools (v0.8.0, CLI 全完了後)
```

### 3. Progress セクション末尾に Phase I を追記

上記各 M29-M36 の定義を roadmap 形式で追記する。

### 4. Architecture Decisions に追加

```
| 15 | readonly API の段階的サポート (M29-M36) | Full-archive search / Streaming / Bookmarks は OAuth 1.0a または 1 ショット CLI と非整合なため除外。MCP は CLI 完全完了後の M36 で薄いラッパーとして一括追加 | 2026-05-14 |
```

### 5. Changelog に追記

```
| 2026-05-14 | 追加 | M29-M36 (readonly API 包括サポート) をキューイング — Posts/Search/Timeline/Users/Lists/Spaces/DM/MCP の 8 段階 |
```

---

## 作成する詳細ファイル (承認後)

| ファイル | 概要 |
|---------|------|
| `plans/x-m29-posts-lookup.md` | Posts Lookup + Social Signals + note_tweet 改善 |
| `plans/x-m30-search-thread.md` | Search Recent + Thread コマンド |
| `plans/x-m31-timelines.md` | User Timelines |
| `plans/x-m32-users-extended.md` | Users lookup / followers / blocking / muting |
| `plans/x-m33-lists.md` | Lists |
| `plans/x-m34-spaces-trends.md` | Spaces + Trends |
| `plans/x-m35-dm-read.md` | DM Read |
| `plans/x-m36-mcp-v2-tools.md` | MCP v2 全 Tools |

---

## 適用範囲 (Edit/Write 対象)

- `plans/x-roadmap.md` (Meta / Current Focus / Progress / Architecture Decisions / Changelog の各セクション更新)
- `plans/x-m29-posts-lookup.md` 〜 `plans/x-m36-mcp-v2-tools.md` (8 ファイル新規作成)

ソースコード (`internal/**`, `cmd/**`, `docs/**`) は編集しない。

## Verification

```bash
# ロードマップに M29-M36 が追加されている
grep -E "M2[9]|M3[0-6]" plans/x-roadmap.md | wc -l

# 詳細ファイル 8 件が存在する
ls plans/x-m29-*.md plans/x-m30-*.md plans/x-m31-*.md plans/x-m32-*.md plans/x-m33-*.md plans/x-m34-*.md plans/x-m35-*.md plans/x-m36-*.md

# ソースコードは無変更
git status -- internal/ cmd/ docs/
```

## Next Action

承認後:
1. `plans/x-roadmap.md` を更新
2. `plans/x-m29-posts-lookup.md` 〜 `plans/x-m36-mcp-v2-tools.md` を新規作成
3. M29 実装着手 → `/devflow:plan plans/x-m29-posts-lookup.md` → `/devflow:cycle` または `/devflow:implement`
