# Changelog

このプロジェクトの変更履歴を記録する。フォーマットは [Keep a Changelog](https://keepachangelog.com/ja/1.1.0/) に準拠し、バージョニングは [Semantic Versioning](https://semver.org/lang/ja/) に従う。

## [Unreleased]

## [0.8.0] - 2026-05-15 (draft)

Phase I (readonly API 包括サポート) の最終マイルストーン。M29-M35 で追加した CLI 機能を MCP tools として一括公開する (M36)。18 個の readonly tools を追加し、Remote MCP からも tweet lookup / search / timeline / users / lists / spaces / trends にアクセスできるようになった。**「CLI が主 / MCP は薄いラッパー」** という既存方針を踏襲し、xapi 層を直接呼び出すだけのハンドラ実装に留めている。MCP モードは引き続き env-only で動作 (`credentials.toml` 読まない、spec §11 不変条件)。

### Added

#### MCP tools 18 個追加 (M36)

- **Tweet 系** (`internal/mcp/tools_tweet.go`): `get_tweet` / `get_tweets` / `get_liking_users` / `get_retweeted_by` / `get_quote_tweets`
- **Search 系** (`internal/mcp/tools_search.go`): `search_recent_tweets` / `get_tweet_thread`
  - `search_recent_tweets`: JST ヘルパ (`yesterday_jst` / `since_jst`) + `all` + `max_pages` + 全 fields
  - `get_tweet_thread`: 2 段呼び出し (GetTweet → SearchRecent) + `author_only` フィルタ
- **Timeline 系** (`internal/mcp/tools_timeline.go`): `get_user_tweets` / `get_user_mentions` / `get_home_timeline`
  - `user_id` 省略時は `GetUserMe` で self 解決 (CLI と同じ振る舞い)
- **Users 系** (`internal/mcp/tools_users.go`): `get_user` / `get_user_by_username` / `get_user_following` / `get_user_followers`
- **Lists 系** (`internal/mcp/tools_lists.go`): `get_list` / `get_list_tweets`
- **Misc 系** (`internal/mcp/tools_misc.go`): `search_spaces` / `get_trends`

#### MCP server.go の RegisterTool 拡張

- `NewServer` 内に 6 つの `registerToolXxx(s, client)` 呼び出しを追加 (`tools_tweet` / `tools_search` / `tools_timeline` / `tools_users` / `tools_lists` / `tools_misc`)
- `tools/list` が全 20 ツール (既存 2 + 新規 18) を返すように

#### MCP `get_liked_tweets` の T0 修正 (M29 handoff)

- `internal/mcp/tools_likes.go` の `likedDefaultTweetFields` に `note_tweet` を追加
- CLI M29 で `liked list` の既定 tweet.fields に `note_tweet` を追加したが、MCP モードは `config.toml` を読まないため自動波及せず、本 M36 着手時の最初のタスクとして対応 (spec §11 不変条件)

#### Routine プロンプトの拡張 (`docs/routine-prompt.md`)

- 「3.1 拡張ユースケース」を追加: M36 で増えた tools を Routine から活用する代表パターンを 5 例掲載 (`get_tweet_thread` / `search_recent_tweets` + JST / `get_user_tweets` / `get_list_tweets` / `get_trends`)

### Design Decisions (M36)

- **D-1 1 ハンドラ 1 ツール**: `NewGetXxxHandler(client)` factory + `registerToolXxx(s, client)` ペア (tools_me.go / tools_likes.go と同じパターン)
- **D-2 Default values 最小**: `likedDefaultTweetFields` 以外には MCP 固有の default 配列を作らない (env-only 制約)
- **D-3 URL → ID 解決は MCP 層では行わない**: MCP は ID 文字列を素直に受ける。URL 解決は CLI 層 (extractTweetID 等) の責務
- **D-4 user_id 省略時の self 解決**: timeline 3 ツールのみ実装 (`get_user_tweets` / `get_user_mentions` / `get_home_timeline`)。tweet 系 / user 系 / list 系では明示必須
- **D-5 出力スキーマ**: xapi の Response 型をそのまま StructuredContent + TextContent (JSON) として返す
- **D-6 JST 優先順位**: 4 ツール (`search_recent_tweets` / `get_user_tweets` / `get_user_mentions` / `get_home_timeline`) で `yesterday_jst > since_jst > start/end_time` (M18 と統一)
- **D-7 max_results バリデーション**: `search_recent_tweets` / `get_tweet_thread` で `< 10` はエラー (CLI 側の自動補正は実装せず、MCP では誤入力を弾く)

### Compatibility

- v0.7.0 以前の CLI / MCP 機能は完全後方互換
- 既存 MCP tools (`get_user_me` / `get_liked_tweets`) のスキーマ・引数仕様に変更なし
- `tools/list` の戻り値は新規 18 ツールを含むため、ハードコード件数で aborts するクライアントは更新が必要
- `credentials.toml` を絶対に読まない不変条件は維持 (`TestLoadMCPCredentials_IgnoresFile` で pin 済)

## [0.7.0] - 2026-05-15 (draft)

X API v2 の Spaces 系 5 エンドポイントと Trends 系 2 エンドポイントを `x space` / `x trends` サブコマンドとして CLI に追加する (M34)。Spaces はアクティブな Space (live/scheduled) のみ取得可能。SearchSpaces は X API がページネーション非対応のため `--all` を提供しない。Trends は 2 endpoint でパラメータ名 (`max_trends` / `personalized_trend.fields`) と返却フィールドが異なるため、xapi 層では Option 型を分離した。
続いて Direct Messages Read エンドポイント群 (events list / lookup / conversation / with) を `x dm` サブコマンドとして追加した (M35)。Basic tier では DM 系のレート制限が極端に厳しい (約 1 req / 24h) ため **Pro tier 推奨**。取得可能なのは直近 30 日以内のイベントのみ。あわせて `expansions=attachments.media_keys` の silent drop を防ぐため `xapi.Includes` 構造体に `Media []Media` フィールドを追加した。

### Added

#### `x dm` サブコマンド (M35)
- `x dm list` — 認証ユーザーの DM イベント一覧 (`GET /2/dm_events`)。`--event-types MessageCreate,ParticipantsJoin,ParticipantsLeave` の CSV ホワイトリスト指定 (case-sensitive)、`--max-results 1..100`、`--all` で `pagination_token` 自動辿り、`--no-json` / `--ndjson` 対応
- `x dm get <eventID>` — 単一 DM イベント (`GET /2/dm_events/:event_id`)。eventID は 1..19 桁数値のみ受ける
- `x dm conversation <conversationID>` — 特定会話の DM イベント (`GET /2/dm_conversations/:id/dm_events`)。conversation ID は `<userA>-<userB>` (1on1) / `group:<id>` (グループ) / 数値の全バリアントを許容
- `x dm with <ID|@username|URL>` — 特定ユーザーとの 1on1 DM (`GET /2/dm_conversations/with/:participant_id/dm_events`)。`@username` / X URL は `GetUserByUsername` で ID 解決
- **重要 Tier 制約**: Basic tier ($100/月) では DM 系のレート制限が約 1 req / 24h と極端に厳しく事実上使用不可。**Pro tier ($5,000/月) 以上を推奨**。取得可能なのは直近 30 日以内のイベントのみ。X App の DM read permission が必要
- `--user-id` フラグは X API 認証ユーザー固定のため非公開 (M35 D-14)

#### `xapi` パッケージ拡張 (M35)
- 4 新規 DM メソッド: `Client.GetDMEvents` / `GetDMEvent` / `GetDMConversation` / `GetDMWithUser`
- 3 新規 DM iterator: `EachDMEventsPage` / `EachDMConversationPage` / `EachDMWithUserPage`
- 2 新規 Option 型 (DM 用途別分離、M35 D-1):
  - `DMLookupOption` — `WithDMLookupDMEventFields` / `WithDMLookupExpansions` / `WithDMLookupUserFields` / `WithDMLookupTweetFields` / `WithDMLookupMediaFields` (GetDMEvent 専用)
  - `DMPagedOption` — `WithDMPagedMaxResults` / `WithDMPagedPaginationToken` / `WithDMPagedEventTypes` / 各種 fields + `WithDMPagedMaxPages` (paged 3 endpoint 共通)
- 新規 DTO: `DMEvent` / `DMAttachments` / `DMEventResponse` (単一) / `DMEventsResponse` (配列)
- `event_types` は CSV クエリ (`event_types=MessageCreate,ParticipantsJoin`) で送信、CLI 層でホワイトリスト検証 (M35 D-3 / D-4)
- paged 3 endpoint はレスポンス型が完全一致するため、内部 `fetchDMEventsPage` + `eachDMEventsPaged` で DRY 化 (M35 D-2)

#### `Includes.Media` の追加 (M35 D-5)
- `internal/xapi/types.go` の `Includes` 構造体に `Media []Media` フィールドを追加 (advisor 反映、silent drop 防止)
- 新規 `Media` DTO: `MediaKey` / `Type` / `URL` / `PreviewImageURL` / `DurationMs` / `Width` / `Height` / `AltText`
- これにより likes / timeline / list tweets / space tweets / dm 等の `expansions=attachments.media_keys` + `media.fields=...` が機能するようになる (従来は Unmarshal で捨てられていた)
- 既存 struct literal は名前付きフィールド初期化のため破壊的変更なし

#### `x space` サブコマンド (M34)
- `x space get <ID|URL>` — 単一 Space 取得 (`GET /2/spaces/:id`)。位置引数は英数字 ID または `https://(x|twitter).com/i/spaces/<ID>` URL を判別 (`extractSpaceID`)
- `x space by-ids --ids <csv>` — 複数 Space バッチ取得 (`GET /2/spaces?ids=`、1..100)。`--ids` は `MarkFlagRequired` で必須化、位置引数なし
- `x space search <query>` — キーワード検索 (`GET /2/spaces/search`)。`--state live/scheduled/all`、`--max-results 1..100`。X API はページネーション非対応のため `--all` 非提供 (M34 D-2)
- `x space by-creator --ids <csv>` — 作成者 ID 指定取得 (`GET /2/spaces/by/creator_ids`、1..100)
- `x space tweets <ID|URL>` — Space 内 Tweet (`GET /2/spaces/:id/tweets`)。`--max-results 1..100`、`--all` で `pagination_token` 自動辿り、`--no-json` / `--ndjson` 対応
- 注: アクティブな Space (live / scheduled) のみ取得可能。終了済み Space は X API 仕様で取得不可
- 除外: `GET /2/spaces/:id/buyers` は OAuth 2.0 PKCE 専用のため対象外

#### `x trends` サブコマンド (M34)
- `x trends get <woeid>` — WOEID 指定トレンド (`GET /2/trends/by/woeid/:woeid`)。`--max-trends 1..50` (X API パラメータ名は `max_trends`、`max_results` ではない)、`--trend-fields trend_name,tweet_count`
- `x trends personal` — パーソナライズトレンド (`GET /2/users/personalized_trends`)。認証ユーザー固定のため `--user-id` 非公開 (M34 D-7)。`--personalized-trend-fields` (X API パラメータ名は `personalized_trend.fields`、`trend.fields` ではない)
- 代表的な WOEID: 1118370 (東京) / 23424856 (日本) / 1 (全世界)

#### `xapi` パッケージ拡張 (M34)
- 5 新規 Spaces メソッド: `Client.GetSpace` / `GetSpaces` / `SearchSpaces` / `GetSpacesByCreatorIDs` / `GetSpaceTweets`
- 1 新規 Spaces iterator: `EachSpaceTweetsPage`
- 2 新規 Trends メソッド: `Client.GetTrends` / `GetPersonalizedTrends`
- 3 新規 Option 型 (Spaces 用途別分離、M34 D-1):
  - `SpaceLookupOption` — `WithSpaceLookupSpaceFields` / `WithSpaceLookupExpansions` / `WithSpaceLookupUserFields` / `WithSpaceLookupTopicFields` (GetSpace / GetSpaces / GetSpacesByCreatorIDs 共通)
  - `SpaceSearchOption` — `WithSpaceSearchMaxResults` / `WithSpaceSearchState` / 各種 fields (SearchSpaces 専用、将来の next_token サポートに備え独立型として確保)
  - `SpaceTweetsOption` — `WithSpaceTweetsMaxResults` / `WithSpaceTweetsPaginationToken` / 各種 fields + `WithSpaceTweetsMaxPages` (paged)
- 2 新規 Option 型 (Trends 用途別分離、M34 D-4):
  - `TrendWoeidOption` — `WithTrendWoeidMaxTrends` (1..50) / `WithTrendWoeidTrendFields`
  - `TrendPersonalOption` — `WithTrendPersonalFields`
- 新規 DTO: `Space` / `SpaceResponse` (単一) / `SpacesResponse` (配列) / `SpaceTweetsResponse` (Tweet 配列、space tweets 専用) / `Trend` (union DTO、両 endpoint のフィールド集約) / `TrendsResponse`
- SearchSpaces は X API が next_token を返さないことを docs.x.com で検証済 (2026-05-15、M34 D-2)
- Trends 2 endpoint のパラメータ名・上限・fields 値の差分を Option 型分離で吸収 (M34 D-4 / D-18 / D-19)

### Compatibility

- v0.6.0 以前の CLI / MCP 機能は完全後方互換
- 設定ファイル `config.toml` への変更なし

## [0.6.0] - 2026-05-14 (draft)

X API v2 の Users Extended エンドポイント群 (lookup / search / graph / blocking / muting) を `x user` サブコマンドとして CLI に追加する (M32)。9 endpoint をカバーし、`blocking` / `muting` は X API 仕様で self only のため、CLI 側で `--user-id` フラグ自体を公開しない設計とした。続いて Lists エンドポイント群 (lookup / tweets / members / owned / followed / memberships / pinned) を `x list` サブコマンドとして追加した (M33)。`pinned` は X API 仕様で self only のため `--user-id` フラグを公開しない (M32 D-5 と同パターン)。

### Added

#### `x list` サブコマンド (M33)
- `x list get <ID|URL>` — 単一 List 取得。位置引数は数値 ID または `https://(x|twitter).com/i/lists/<NUM>` URL を判別 (`extractListID`)
- `x list tweets <ID|URL>` — List のツイート (`GET /2/lists/:id/tweets`)。`--max-results 1..100`、`--all` で `pagination_token` 自動辿り、`--no-json` / `--ndjson` 対応
- `x list members <ID|URL>` — List メンバー (`GET /2/lists/:id/members`)。出力はユーザー配列、`--no-json` で `id=... username=... name=...` 形式
- `x list owned [<ID|@username|URL>]` — 所有 List 一覧 (`GET /2/users/:id/owned_lists`)。位置引数省略時は self、`@username`/URL 指定時は `GetUserByUsername` で ID 解決後に List 呼び出し (2 API call、M33 D-10)
- `x list followed [<ID|@username|URL>]` — フォロー中 List 一覧 (`GET /2/users/:id/followed_lists`)
- `x list memberships [<ID|@username|URL>]` — 参加 List 一覧 (`GET /2/users/:id/list_memberships`)
- `x list pinned` — ピン留め List 一覧 (`GET /2/users/:id/pinned_lists`)。X API 仕様で self only のため **`--user-id` フラグは非登録** (M33 D-7)、`GetUserMe` で self を必ず解決
- 共通フラグ: `--max-results 1..100`、`--pagination-token`、`--all` + `--max-pages` (default 50)、`--no-json` / `--ndjson` (排他)、`--list-fields` / `--user-fields` / `--tweet-fields` / `--expansions` / `--media-fields` (endpoint ごとに必要な分のみ登録)
- `pinned_lists` はページネーション非サポート (X API 仕様)

#### `xapi` パッケージ拡張 (M33)
- 7 新規メソッド: `Client.GetList` / `GetListTweets` / `GetListMembers` / `GetOwnedLists` / `GetListMemberships` / `GetFollowedLists` / `GetPinnedLists`
- 5 新規 iterator: `EachListTweetsPage` / `EachListMembersPage` / `EachOwnedListsPage` / `EachListMembershipsPage` / `EachFollowedListsPage`
- 2 新規 Option 型 (用途別分離、M33 D-1):
  - `ListLookupOption` — `WithListLookupListFields` / `WithListLookupExpansions` / `WithListLookupUserFields` (GetList / GetPinnedLists で再利用、M33 D-5)
  - `ListPagedOption` — `WithListPagedMaxResults` / `WithListPagedPaginationToken` / 各種 fields + expansions + `WithListPagedMaxPages` (paged 5 endpoint で共通)
- 新規 DTO: `List` (id/name/private/owner_id/member_count/follower_count/created_at) / `ListResponse` (単一) / `ListsResponse` (List 配列、owned/memberships/followed/pinned) / `ListTweetsResponse` (Tweet 配列、list tweets 専用)
- `GetListMembers` のレスポンス型は既存 `UsersResponse` (M32) を再利用 (M33 D-4)
- 全 5 paged endpoint で X API クエリパラメータ `pagination_token` を統一採用 (X API docs で next_token 表記が混在する members も pagination_token で送信、テストで pin、M33 D-2)
- `computeInterPageWait(rl, threshold)` (M29) を再利用 (threshold=2、他系統と同値)

#### `x user` サブコマンド (M32)
- `x user get [<ID|@username|URL>]` — 単一ユーザー lookup。位置引数は ID/`@username`/URL を自動判別 (`extractUserRef`)
- `x user get --ids <csv>` — 数値 ID バッチ lookup (1..100、`GET /2/users?ids=`)
- `x user get --usernames <csv>` — username バッチ lookup (1..100、`GET /2/users/by?usernames=`)
- `x user search <query>` — ユーザー検索 (`GET /2/users/search`)。`--max-results 1..1000` (X API docs 確認済)、`--all` で `next_token` 自動辿り
- `x user following [<ID|@username|URL>]` / `x user followers [<ID|@username|URL>]` — フォロー / フォロワー一覧。`--user-id` 省略時は self (`GetUserMe`)、`@username`/URL 指定時は `GetUserByUsername` で ID 解決後に graph 呼び出し (2 API call)
- `x user blocking` / `x user muting` — 自アカのブロック/ミュート一覧。X API 仕様で self only のため **`--user-id` フラグは非登録**、`GetUserMe` で self を必ず解決 (M32 D-5)
- 共通フラグ: `--max-results` (search/graph 共に 1..1000)、`--pagination-token`、`--all` + `--max-pages` (default 50)、`--no-json` / `--ndjson` (排他)、`--user-fields` / `--expansions` / `--tweet-fields`
- `--ids` / `--usernames` / 位置引数の三者排他 (M32 D-8)。複数指定は `ErrInvalidArgument` (exit 2)

#### `xapi` パッケージ拡張 (M32)
- 9 新規メソッド: `Client.GetUser` / `GetUsers` / `GetUserByUsername` / `GetUsersByUsernames` / `SearchUsers` / `GetFollowing` / `GetFollowers` / `GetBlocking` / `GetMuting`
- 5 新規 iterator: `EachSearchUsersPage` / `EachFollowingPage` / `EachFollowersPage` / `EachBlockingPage` / `EachMutingPage`
- 3 新規 Option 型 (用途別分離、M32 D-1):
  - `UserLookupOption` — `WithUserLookupUserFields` / `WithUserLookupExpansions` / `WithUserLookupTweetFields`
  - `UserSearchOption` — `WithUserSearchMaxResults` / `WithUserSearchNextToken` / `WithUserSearchUserFields` 等 (pagination キーは `next_token`、graph と異なる、M32 D-3)
  - `UserGraphOption` — `WithUserGraphMaxResults` / `WithUserGraphPaginationToken` / `WithUserGraphUserFields` 等
- 新規 DTO: `UserResponse` (単一) / `UsersResponse` (配列) / `UserLookupError` (partial error、`TweetLookupError` と同形だが別型)
- M29 で抽出した `computeInterPageWait(rl, threshold)` を user search / graph iterator で再利用 (threshold=2、likes/search/timeline と同値)
- graph 4 endpoint は `fetchUserGraphPage` / `eachUserGraphPage` で DRY、search は `fetchUserSearchPage` で別実装 (pagination パラメータ名の違いを吸収)

### Compatibility

- v0.5.0 以前の CLI / MCP 機能は完全後方互換
- 設定ファイル `config.toml` への変更なし
- `blocking` / `muting` / `list pinned` は X API 仕様で OAuth 1.0a 認証ユーザーの self のみ参照可能 (本リポジトリは OAuth 1.0a 専用のため問題なし)

## [0.5.0] - 2026-05-14 (draft)

X API v2 の `search/recent` エンドポイントを CLI に追加し、過去 7 日間のキーワード検索とスレッド (会話) 取得をサポートする (M30)。さらに User Timelines 3 エンドポイント (ユーザーの Post / メンション / 認証ユーザーのホーム) を `x timeline` サブコマンドとして追加した (M31)。`search/recent` は **X API v2 Basic tier 以上** が必要で、Free tier では `403` (`exit 4`) が返る。User Timelines は Free tier でも利用可能。

### Added

#### `x timeline` サブコマンド (M31)
- `x timeline tweets [<ID>]` — ユーザーの Post タイムライン (`GET /2/users/:id/tweets`)。`--user-id` 省略時は self (`GetUserMe`)、`--exclude retweets,replies` 対応
- `x timeline mentions [<ID>]` — ユーザーへのメンション (`GET /2/users/:id/mentions`)。`--user-id` 省略時は self。**`--exclude` は X API 仕様で非サポートのためフラグ自体を非登録** (M31 D-9)
- `x timeline home` — 認証ユーザーのホームタイムライン (`GET /2/users/:id/timelines/reverse_chronological`)。X API 仕様で認証ユーザー固定のため **`--user-id` フラグは非登録**、`GetUserMe` で self を必ず解決する (M31 D-4)
- 共通フラグ: `--max-results` (`tweets`/`mentions` は 5..100 下限補正、`home` は 1..100 そのまま)、時間窓 (`--yesterday-jst` / `--since-jst` / `--start-time` / `--end-time` の優先順位 liked と同形)、`--since-id` / `--until-id` (時間窓と独立、X API 仕様で併用可)、`--all` + `--max-pages` (default 50)、`--no-json` / `--ndjson` (排他)、`--tweet-fields` / `--expansions` / `--user-fields` / `--media-fields`
- `tweets` / `mentions` の `--max-results 1..4` は X API 下限 (5) に補正して `[:n]` で truncate。`--all` 併用時は `ErrInvalidArgument` (exit 2) で拒否

#### `xapi` パッケージ拡張 (M31)
- 新規ファイル `internal/xapi/timeline.go`
- 新規 DTO: `TimelineResponse` (Data / Includes / Meta)
- 新規メソッド: `Client.GetUserTweets` / `GetUserMentions` / `GetHomeTimeline` + `EachUserTweetsPage` / `EachUserMentionsPage` / `EachHomeTimelinePage`
- 新規 Option: `TimelineOption` (`WithTimelineMaxResults` / `WithTimelineStartTime` / `WithTimelineEndTime` / `WithTimelineSinceID` / `WithTimelineUntilID` / `WithTimelinePaginationToken` / `WithTimelineExclude` / `WithTimelineTweetFields` / `WithTimelineExpansions` / `WithTimelineUserFields` / `WithTimelineMediaFields` / `WithTimelineMaxPages`)
- M29 で抽出した `computeInterPageWait(rl, threshold)` を `Each*TimelinePage` でも再利用 (timeline threshold = 2、likes / search と同値)
- `WithTimelineExclude` は `tweets` / `home` のみ有効。`mentions` で渡すと X API が 400 を返す可能性があるため godoc で明示

#### `x tweet search` (M30)
- `x tweet search <query>` — X API v2 `GET /2/tweets/search/recent` のラッパ
- `query` は X 検索演算子をそのまま受け付ける (`from:user` / `lang:ja` / `conversation_id:<id>` / `-is:retweet` 等)
- 時間窓フラグ: `--start-time` / `--end-time` (RFC3339 UTC) と `--since-jst YYYY-MM-DD` / `--yesterday-jst` (liked list と同じ優先順位: yesterday-jst > since-jst > start/end)
- ページネーション: `--all` + `--max-pages` (default 50) で next_token を自動辿る
- 出力: 既定 JSON / `--no-json` (1 行/ツイート、note_tweet.text 優先) / `--ndjson` (line-delimited、`--all` 時はストリーミング)
- `--max-results 1..9` は X API 下限 (10) に補正して応答を `[:n]` で truncate。`--all` 併用時は `ErrInvalidArgument` (exit 2) で拒否

#### `x tweet thread` (M30)
- `x tweet thread <ID|URL>` — ツイートのスレッド (会話) を取得
- 内部動作 (2 リクエスト消費): `GetTweet(id)` で `conversation_id` を取得 → `SearchRecent(query="conversation_id:<convID>")` で構成ツイート取得
- `--author-only` — ルートツイートの作者発言のみに絞る (CLI 層フィルタ)
- 出力: 既定 JSON / `--no-json` / `--ndjson`。JSON / Human は `created_at` 昇順、NDJSON は X API 順 (新しい順) のままストリーミング
- `--all` で複数ページ集約、`--max-pages` で上限制御

#### `xapi` パッケージ拡張 (M30)
- 新規 DTO: `SearchResponse` (Data / Includes / Meta / Errors)
- 新規メソッド: `Client.SearchRecent`, `Client.EachSearchPage`
- 新規 Option: `SearchOption` (`WithSearchMaxResults` / `WithSearchStartTime` / `WithSearchEndTime` / `WithSearchPaginationToken` / `WithSearchTweetFields` / `WithSearchExpansions` / `WithSearchUserFields` / `WithSearchMediaFields` / `WithSearchMaxPages`)
- M29 で抽出した `computeInterPageWait(rl, threshold)` を `EachSearchPage` で再利用 (search 用 threshold = 2)
- `TweetLookupError` を `SearchResponse.Errors` でも再利用 (partial error 互換)

### Compatibility

- v0.4.0 以前の CLI / MCP 機能は完全後方互換
- 設定ファイル `config.toml` への変更なし
- `search/recent` は **Basic tier 以上必須** で、Free tier では使用不可 (`docs/x-api.md` 参照)
- User Timelines 3 エンドポイントは Free tier でも利用可能 (M31)
- `x timeline home` は OAuth 1.0a 必須 (本リポジトリは OAuth 1.0a 専用のため制約に該当しない)

## [0.4.0] - 2026-05-14

X API v2 の Posts Lookup と Social Signals エンドポイントを CLI に追加し、Note Tweet (ロングツイート) を既定で取得・表示するように改修する (M29)。`x liked list --max-results 1..4` も X API の下限値 (5) を自動補正することで使い勝手を改善した。

### Added

#### `x tweet` サブコマンド (M29)
- `x tweet get [ID|URL]` — 単一ツイート取得。`https://x.com/<u>/status/<id>` 形式の URL からも ID を自動抽出する (mobile / `i/web/status` / `statuses/` 旧形式 / クエリ・fragment 付き対応)
- `x tweet get --ids ID1,ID2,...` — 1..100 件の一括取得 (`GET /2/tweets?ids=...`)。X API の partial error (一部 ID 未取得) は `errors` フィールドに含めて返却し、`--no-json` 時は stderr に warning を出力
- `x tweet liking-users <ID|URL>` — そのツイートにいいねしたユーザー一覧 (`GET /2/tweets/:id/liking_users`)
- `x tweet retweeted-by <ID|URL>` — リツイートしたユーザー一覧 (`GET /2/tweets/:id/retweeted_by`)
- `x tweet quote-tweets <ID|URL>` — 引用ツイート一覧 (`GET /2/tweets/:id/quote_tweets`)。`--exclude retweets,replies` フラグ対応
- すべて `--no-json` (human 形式) / 既定 JSON / 詳細フラグ (`--tweet-fields` / `--expansions` / `--user-fields` / `--media-fields`) をサポート

#### `xapi` パッケージ拡張 (M29)
- `Tweet.NoteTweet *NoteTweet` / `Tweet.ConversationID string` フィールド追加
- 新規 DTO: `NoteTweet`, `TweetResponse`, `TweetsResponse`, `TweetLookupError`, `UsersByTweetResponse`, `QuoteTweetsResponse`
- 新規メソッド: `Client.GetTweet`, `GetTweets`, `GetLikingUsers`, `GetRetweetedBy`, `GetQuoteTweets`
- 新規 Option: `TweetLookupOption` (`WithGetTweetFields` / `WithGetTweetExpansions` / `WithGetTweetUserFields` / `WithGetTweetMediaFields`)、`UsersByTweetOption`、`QuoteTweetsOption`
- `internal/xapi/pagination.go` 新規: rate-limit aware ページ間待機 `computeInterPageWait` を共通化 (M30 SearchRecent 等で再利用予定)

### Changed

- **`x liked list` 既定 `tweet.fields` に `note_tweet` を追加** — ロングツイートの完全本文を既定で取得する。`config.toml [liked] default_tweet_fields` の既定値も同様に更新 (`id,text,author_id,created_at,entities,public_metrics,note_tweet`)
- **`x liked list --no-json` 出力で `note_tweet.text` 優先** — 非空ならそれを 80 ルーン truncate 表示し、`tw.text` (短縮版) より優先する。JSON / NDJSON 出力は両方含めて後方互換維持
- **`x liked list --max-results 1..4` の下限補正** — `--all=false` 時は X API に 5 を投げて応答を `[:n]` で絞る。`--all=true` 時は `ErrInvalidArgument` (exit 2) で拒否し UX 混乱を防ぐ

### Compatibility

- JSON / NDJSON 出力は完全後方互換 (`note_tweet` は `omitempty` のため、ロングツイート以外では従来通り出力に含まれない)
- v0.3.0 の CLI / MCP 機能は全て動作する
- 設定ファイル `config.toml [liked] default_tweet_fields` のユーザー上書き値は引き続き優先 (組み込みデフォルトのみ変更)

## [0.3.0] - 2026-05-12

AWS Lambda Function URL + Lambda Web Adapter での Remote MCP デプロイサンプルと、Claude Code Routines 連携のための文書セットを追加。本バージョンで「CLI → MCP → 公開配布」の 3 フェーズ計画 (28 マイルストーン) が完了する。Go コードの変更はなく、`examples/` と `docs/` への純粋な追加のみ。

### Added

#### `examples/lambroll/` — AWS Lambda デプロイサンプル
- `function.json` (`provided.al2023`, arm64, Lambda Web Adapter Layer, SSM SecureString 注入) (M26)
- `function_url.json` (`AuthType=NONE`、認証は idproxy 側に集約) (M26)
- `bootstrap` シェル (`exec ./x mcp --host 0.0.0.0 --port "${PORT:-8080}"`) (M26)
- `.env.example` (lambroll が参照する env と SSM 経由 env の全リファレンス) (M26)
- `README.md` (Step 1-6 デプロイ手順 + Mermaid 図 + IAM/DynamoDB/SSM/OIDC セットアップ + トラブルシュート FAQ + コスト見積もり + クリーンアップ) (M27)

#### `docs/` — 開発者向け補足ドキュメント
- `docs/x-api.md` — X API v2 OAuth 1.0a 認証手順 / エンドポイント / Rate Limit / 課金 (Owned Reads `$0.001`/Tweet) / エラーレスポンスを集約 (M28)
- `docs/routine-prompt.md` — Claude Code Routines に貼り付ける推奨プロンプト雛形と Backlog (HEP_ISSUES) 課題テンプレ + Mermaid シーケンス図 (M28)

### Compatibility
- v0.1.0 / v0.2.0 の CLI / MCP 機能は完全後方互換
- 本バージョンは **追加のみ**、CLI / MCP の挙動変更なし
- Go コード変更なし (`examples/` と `docs/` の純粋な追加)

## [0.2.0] - 2026-05-12

Remote MCP サーバーをリリース。Claude Code Routines や他の MCP クライアントから X (旧 Twitter) API v2 の Liked Posts を読み出せる Streamable HTTP サーバーが `x mcp` サブコマンドとして提供される。Lambda Web Adapter での AWS Lambda デプロイ想定 (実デプロイサンプルは v0.3.0)。

### Added

#### MCP サブコマンド
- `x mcp` — Streamable HTTP MCP サーバーを起動 (M24):
  - `--host` (default `127.0.0.1`, env: `X_MCP_HOST`)
  - `--port` (default `8080`, env: `X_MCP_PORT`)
  - `--path` (default `/mcp`, env: `X_MCP_PATH`)
  - `--auth idproxy|apikey|none` (default `idproxy`, env: `X_MCP_AUTH`)
  - `--apikey-env <name>` (default `X_MCP_API_KEY`) — apikey モード時に shared secret を保持する env 変数名
  - SIGINT/SIGTERM 受信で graceful shutdown
  - MCP モードは credentials.toml を一切読まず、シークレットは環境変数のみから取得 (spec §11 不変条件)

#### MCP Tools (mark3labs/mcp-go Streamable HTTP)
- `get_user_me` — 自分の `user_id` / `username` / `name` を返す (M17)
- `get_liked_tweets` — Liked Posts を取得 (M18):
  - 入力: `user_id` / `start_time` / `end_time` / `since_jst` / `yesterday_jst` / `max_results` / `all` / `max_pages` / `tweet_fields` / `expansions` / `user_fields`
  - `all=true` で next_token を自動辿り、rate-limit aware に集約結果を一括返却
  - `yesterday_jst > since_jst > start/end_time` の優先順位を CLI と統一

#### 認証 (authgate)
- `none` モード — 認証なし (ローカル開発専用, M16)
- `apikey` モード — Bearer token、constant-time 比較 (M19)
- `idproxy` モード — OIDC + cookie session (M20-M23)
- idproxy ストアバックエンド 4 種 (`STORE_BACKEND` で切替):
  - `memory` (default, テスト/一時用途, M20)
  - `sqlite` (`SQLITE_PATH`, `modernc.org/sqlite` pure Go, ローカル開発, M21)
  - `redis` (`REDIS_URL`, `go-redis/v9`, 軽量サーバー, M22)
  - `dynamodb` (`DYNAMODB_TABLE_NAME` / `AWS_REGION`, `aws-sdk-go-v2`, Lambda マルチコンテナ, M23)

#### Transport
- Streamable HTTP サーバー (`internal/transport/http`, M15)
- `GET /healthz` — LWA / Lambda 死活確認用 (`200 ok\n`, 認証 middleware バイパス, M16)
- graceful shutdown (ListenAndServe 並行起動 → ctx 終了で `Shutdown` 呼び出し)

### 環境変数 (新規)

| 名前 | 用途 |
|---|---|
| `X_MCP_HOST` | MCP bind host |
| `X_MCP_PORT` | MCP bind port |
| `X_MCP_PATH` | MCP エンドポイント prefix |
| `X_MCP_AUTH` | 認証モード (`idproxy` / `apikey` / `none`) |
| `X_MCP_API_KEY` | apikey モードの shared secret 値 |
| `OIDC_ISSUER` | idproxy OIDC Issuer (カンマ区切りで複数可) |
| `OIDC_CLIENT_ID` | idproxy OIDC Client ID (カンマ区切り) |
| `OIDC_CLIENT_SECRET` | idproxy OIDC Client Secret |
| `COOKIE_SECRET` | idproxy セッション暗号 (hex 32B+) |
| `EXTERNAL_URL` | idproxy 外部 URL |
| `STORE_BACKEND` | idproxy ストア (`memory` / `sqlite` / `redis` / `dynamodb`) |
| `SQLITE_PATH` | sqlite DB ファイルパス |
| `REDIS_URL` | Redis 接続 URL |
| `DYNAMODB_TABLE_NAME` | DynamoDB テーブル名 |
| `AWS_REGION` | AWS リージョン (Lambda / dynamodb 時) |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` (default `info`) |

### Security
- MCP モードはシークレットを環境変数のみから読み込み (Lambda 不変性前提、`credentials.toml` を一切読まない)
- apikey モードの shared secret は `subtle.ConstantTimeCompare` で比較
- `/healthz` は middleware バイパスだが、ペイロードは固定文字列のみ (情報漏洩なし)

### Compatibility
- v0.1.0 の CLI 機能は完全後方互換 (`x version` / `x me` / `x liked list` / `x configure` / `x completion`)
- 新規追加された `x mcp` サブコマンドは独立しており既存ユーザーへの影響なし

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

[Unreleased]: https://github.com/youyo/x/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/youyo/x/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/youyo/x/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/youyo/x/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/youyo/x/releases/tag/v0.1.0
