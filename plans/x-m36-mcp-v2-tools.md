# M36: MCP v2 Tools (CLI M29-M35 の薄いラッパー)

## Overview

| 項目 | 値 |
|------|---|
| ステータス | 未着手 |
| 対象 v リリース | v0.8.0 |
| Phase | I: readonly API 包括サポート (第 8 回、MCP 対応) |
| 依存 | **M29-M35 (CLI 全完了後)**, M15-M18 (既存 MCP 基盤) |
| 主要対象ファイル | `internal/mcp/tools_tweet.go` / `tools_search.go` / `tools_timeline.go` / `tools_users.go` / `tools_lists.go` / `tools_misc.go` (全新規) |

## Goal

M29-M35 で追加した CLI コマンドに対応する MCP tools を追加する。CLI が主でMCP は薄いラッパーという既存方針を踏襲し、xapi 層のメソッドを直接呼び出す。

## 追加 MCP Tools

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

## Tasks (TDD: Red → Green → Refactor)

- [ ] **T1**: `internal/mcp/tools_tweet.go` — `get_tweet` / `get_tweets` / `get_liking_users` / `get_retweeted_by` / `get_quote_tweets` の 5 ツール。既存 M17/M18 の実装パターンを踏襲 (`mcp.NewTool` + `RegisterTool` + `tools_me_test.go` / `tools_likes_test.go` パターン)
- [ ] **T2**: `internal/mcp/tools_search.go` — `search_recent_tweets` / `get_tweet_thread` の 2 ツール。`yesterday_jst` / `since_jst` / `start_time` / `end_time` パラメータ + JST 優先順位 (M18 と同じバリデーションロジック)
- [ ] **T3**: `internal/mcp/tools_timeline.go` — `get_user_tweets` / `get_user_mentions` / `get_home_timeline` の 3 ツール。`user_id` パラメータ (省略時は GetUserMe で self 解決)
- [ ] **T4**: `internal/mcp/tools_users.go` — `get_user` / `get_user_by_username` / `get_user_following` / `get_user_followers` の 4 ツール
- [ ] **T5**: `internal/mcp/tools_lists.go` — `get_list` / `get_list_tweets` の 2 ツール
- [ ] **T6**: `internal/mcp/tools_misc.go` — `search_spaces` / `get_trends` の 2 ツール
- [ ] **T7**: `internal/mcp/server.go` — 全 18 ツールを `srv.RegisterTool` で登録
- [ ] **T8**: 全 6 ファイルのテスト — `TestLoadMCPCredentials_IgnoresFile` パターンが新ツールにも適用されていること確認 (MCP モードはファイル読まない不変条件の pin テスト)
- [ ] **T9** (検証 + Docs): MCP client で `get_tweet` / `search_recent_tweets` 動作確認 (E2E)、`docs/routine-prompt.md` に新ツールの使用例追記、`README.md` / `README.ja.md` MCP ツール表更新、`CHANGELOG.md` `[0.8.0]` セクション追加

## Completion Criteria

- 全 18 ツールが MCP client で `tools/list` に表示される
- `tools/call name=get_tweet` が動作する (httptest E2E)
- `tools/call name=search_recent_tweets` が動作する (Basic tier 環境)
- `TestLoadMCPCredentials_IgnoresFile` 相当のテストが新ツールファイルに存在する
- `docs/routine-prompt.md` に新ツールの利用例が記載されている

## 既存方針との整合

- **CLI が主 / MCP は薄いラッパー**: xapi 層のメソッドを直接呼び出す。CLI の RunE と同じロジックを重複させない
- **env のみ**: `loadMCPCredentials` (M24) を引き続き使用。`credentials.toml` は絶対に読まない (Lambda 不変インフラ前提)
- **パッケージ doc は書かない**: 既存 `internal/mcp/doc.go` に集約済み
- **エラー番兵の再利用**: 新しい exit code は追加しない。既存 `xapi.ErrAuthentication` / `ErrPermission` / `ErrNotFound` で対応

## 既存 MCP ツールの追従更新 (M29 からの handoff)

M29 で CLI `liked list` の既定 tweet.fields に `note_tweet` を追加したが、MCP モードは spec §11 不変条件
により config.toml を読まないため、`internal/mcp/tools_likes.go` の `likedDefaultTweetFields` には影響しない。
本 M36 着手時に以下を併せて更新する:

- [ ] **T0 (handoff)**: `internal/mcp/tools_likes.go:29` の `likedDefaultTweetFields` に `note_tweet` を追加し、
      Routines → MCP 経由でもロングツイートの完全本文が既定で取得されるようにする。テスト (M18 既存) は
      assertion を generic に保ったまま動作するはずだが、念のため expected 値を含めた diff チェックを追加

---

> このファイルはプレースホルダーです。
> 詳細な実装計画は `/devflow:plan` 実行時に生成されます。
