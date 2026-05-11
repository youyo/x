# M18: MCP tool `get_liked_tweets` 実装計画

> Layer 2: M18 詳細計画。M17 (`get_user_me`) で確立した `registerToolXxx` + `NewXxxHandler` パターンを踏襲し、Liked ツイート取得 tool を追加する。

## Context

- スペック: `docs/specs/x-spec.md` §6 MCP tools `get_liked_tweets`、§11 `[liked]` defaults
- 入力 (JSON Schema, spec §6):
  ```jsonc
  {
    "user_id":      "string (optional, default: me)",
    "start_time":   "string (RFC3339, optional)",
    "end_time":     "string (RFC3339, optional)",
    "since_jst":    "string (YYYY-MM-DD, optional)",
    "yesterday_jst":"boolean (optional)",
    "max_results":  "integer 1-100 (default: 100)",
    "all":          "boolean (default: false)",
    "max_pages":    "integer (default: 50)",
    "tweet_fields": "string[] (optional)",
    "expansions":   "string[] (optional)",
    "user_fields":  "string[] (optional)"
  }
  ```
- 出力: `*xapi.LikedTweetsResponse` (json タグ `data` / `includes` / `meta` が spec §6 と一致)
- 既存資産:
  - `internal/xapi/likes.go`: `ListLikedTweets` / `EachLikedPage` / 各 `WithXxx` Option (M8 完了)
  - `internal/xapi/users.go`: `*Client.GetUserMe(ctx, ...)` (M7 完了, user_id 未指定時の self 解決用)
  - `internal/mcp/server.go`: `NewServer(client, version)` ファクトリ (M15)
  - `internal/mcp/tools_me.go`: M17 で確立した registerToolXxx + NewXxxHandler パターン
  - `internal/cli/liked.go`: CLI 層の `parseJSTDate` / `yesterdayJSTRange` / `loadLikedDefaults` / `likedAggregator` (参考実装)
  - `mark3labs/mcp-go` v0.52.0 (`WithString` / `WithBoolean` / `WithNumber` / `WithArray` + `WithStringItems` / `DefaultBool` / `DefaultNumber` / `Description` / `Min` / `Max`)

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M18 |
| ステータス | 進行中 |
| 作成日 | 2026-05-12 |
| 依存 | M17 (registerToolXxx パターン), M8 (ListLikedTweets/EachLikedPage), M7 (GetUserMe) |
| 後続 | M19 (authgate apikey) |

## Design Decisions

### D-1: パターン踏襲 — `registerToolLikes` + `NewGetLikedTweetsHandler`
M17 の `registerToolMe` + `NewGetUserMeHandler` と同型。ハンドラ生成は handler factory として export し、テスト容易性を維持する (httptest で X API モック → handler 呼び出し → IsError/StructuredContent 検証)。

### D-2: 出力 DTO は `*xapi.LikedTweetsResponse` を直接利用 (pointer 渡し)
spec §6 出力スキーマ `{ data, includes, meta }` は `LikedTweetsResponse` の json タグ (`data,omitempty` / `includes,omitempty` / `meta,omitempty`) と完全一致するため中間 DTO を作らない (M17 と異なり User の id/user_id リネーム問題は無し)。

- **StructuredContent 型決定**: `NewToolResultJSON(resp)` で `resp` は `*xapi.LikedTweetsResponse` をそのまま渡す (`ListLikedTweets` / 集約後の戻り値が pointer のため自然な流れ)。したがって `res.StructuredContent` は **pointer 型 `*xapi.LikedTweetsResponse`** で返る。テストの型アサートも `*xapi.LikedTweetsResponse` で行う (G-1)。
- `data` が nil の場合 (0 件ヒット) は `omitempty` で出力 JSON から消える。これは spec の「optional response body subset」と整合する (Tweet API も 0 件時 `data` キー欠落が観測される)。
- 注意: `Meta` / `Includes` は struct 型のため Go の `encoding/json` 標準挙動では空構造体でも JSON 出力に残る (`"meta": {}` / `"includes": {}`)。spec は明示禁止していないため許容する。テスト G-2 でも空 `meta` 存在を前提にアサートする。

### D-3: 11 入力パラメータの宣言
| name | 型 | 制約 / Default | mcp-go オプション |
|---|---|---|---|
| user_id | string | optional, default: me | `WithString("user_id", Description(...))` |
| start_time | string | RFC3339, optional | `WithString("start_time", Description("RFC3339 UTC"))` |
| end_time | string | RFC3339, optional | `WithString("end_time", Description("RFC3339 UTC"))` |
| since_jst | string | YYYY-MM-DD, optional | `WithString("since_jst", Description("YYYY-MM-DD JST"))` |
| yesterday_jst | boolean | optional, default false | `WithBoolean("yesterday_jst", DefaultBool(false), ...)` |
| max_results | number | 1-100, default 100 | `WithNumber("max_results", Min(1), Max(100), DefaultNumber(100))` |
| all | boolean | default false | `WithBoolean("all", DefaultBool(false), ...)` |
| max_pages | number | default 50 | `WithNumber("max_pages", Min(1), DefaultNumber(50))` |
| tweet_fields | string[] | optional | `WithArray("tweet_fields", WithStringItems())` |
| expansions | string[] | optional | `WithArray("expansions", WithStringItems())` |
| user_fields | string[] | optional | `WithArray("user_fields", WithStringItems())` |

`mcp-go` の `DefaultBool` / `DefaultNumber` は JSON Schema の `default` キーを設定するだけで、引数取得側 (`req.GetArguments()`) はデフォルト値を自動補完しない。**default 値の適用はハンドラ側の責務**。

### D-4: 時間窓決定の優先順位 (CLI と同じ)
spec §6 「since_jst は start/end を上書き」「yesterday_jst が true なら JST 前日」を踏襲。CLI 層 (`internal/cli/liked.go` L197-211) の挙動と完全一致させる:

```
yesterday_jst > since_jst > start_time/end_time
```

排他制約は設けず「上書き」セマンティクスとする (CLI と同じ)。

### D-5: 共通ヘルパは MCP 内に独立実装する
`parseJSTDate` / `yesterdayJSTRange` / `jstLocation` は CLI に既存だが、**M18 では MCP 層内に独立実装する** (M11 ハンドオフの方針通り)。

理由:
- CLI / MCP の配線は独立 (CLI が `internal/cli` を、MCP が `internal/mcp` を呼ぶ。直接互いに依存しない)
- 共通化するなら `internal/timeutil` または `internal/xapi/timeutil.go` への昇格が必要 — これは別マイルストーンでリファクタする
- M18 のスコープを最小化 (機能追加 + テスト)

実装方針:
- `internal/mcp/tools_likes.go` 内に `parseJSTDate` / `yesterdayJSTRange` / `jstLocation` を private 関数として置く
- 戻り値型・挙動は CLI 版と完全一致 (重複ロジック)。将来の昇格対象とコメントに明記

### D-6: user_id 未指定時の self 解決
spec §6 「user_id (optional, default: me)」を踏襲し、未指定または空文字列の場合は `client.GetUserMe(ctx)` を呼び出して `User.ID` を取得する (CLI L227-234 と同型)。

- GetUserMe 失敗時はそのままエラー (IsError=true)
- GetUserMe 内部で X API 認証エラーが出ると `xapi.ErrAuthentication` が返るが、handler では `err.Error()` のみ展開 (M17 と同じ。番兵エラーの細分は M19+ で考える)

### D-7: ハードコードデフォルト (spec §11) を MCP 層に固定値で置く
spec §11 [liked] の defaults は config.toml 由来だが MCP モードは `config.toml` を一切読まない (spec §11)。
- `max_results` = 100
- `max_pages` = 50
- `tweet_fields` = `["id","text","author_id","created_at","entities","public_metrics"]`
- `expansions` = `["author_id"]`
- `user_fields` = `["username","name"]`

MCP 層内に `const` / `var` で定義し、ハンドラ初期化時にデフォルト値として適用する。`mcp-go` の `DefaultNumber` は JSON Schema の hint のみで補完しないため、**ハンドラ側で `args[key]` の有無を判定して default を適用する**。

### D-8: `all=true` 時の集約は `likedAggregator` 同等ロジック
CLI の `runLikedAll` (L404-432) / `likedAggregator` (L441-472) と同じ:
- 全ページの Data / Includes.Users / Includes.Tweets を append (重複排除なし)
- Meta は再構築 (ResultCount = len(Data), NextToken = "")

MCP 層内に同等ロジックを独立実装 (D-5 と同様の方針)。

### D-9: 入力バリデーション
ハンドラ最上位で実施し、失敗時は `mcp.NewToolResultError(...)` で IsError=true として返す (protocol-level error は返さない):
1. `user_id`: 空文字列許容 (default: me)、型違反 (string 以外) → IsError
2. `start_time` / `end_time`: 非空なら RFC3339 パース、失敗で IsError
3. `since_jst`: 非空なら `YYYY-MM-DD` パース、失敗で IsError
4. `yesterday_jst`: 型違反 → IsError
5. `max_results`: 型違反 / 範囲外 (1-100 外) → IsError
6. `all`: 型違反 → IsError
7. `max_pages`: 型違反 / `<= 0` → IsError (xapi 側のデフォルト 50 をフォールバックさせるのではなく、明示的なバリデーション)
8. `tweet_fields` / `expansions` / `user_fields`: 型違反 (`[]any` でない or 要素が string でない) → IsError

JSON 数値は `req.GetArguments()` 経由で `float64` として届く点に注意 (Go の `encoding/json` 標準挙動)。`int` への変換は `int(v.(float64))` でキャストし、小数部があれば warning ではなく明示的に IsError とする (`max_results = 50.5` 等の異常入力を弾く)。

**小数部判定の実装**: `v != float64(int(v))` で判定する (文字列フォーマット経由ではない)。NaN / Inf も同条件で弾かれる。

### D-10: 関数長 / cyclomatic complexity 対策
ハンドラ本体は引数が 11 個 + 分岐が多いため、責務を分離する:
1. `buildLikedTweetsConfig(args map[string]any) (*likedTweetsCallConfig, error)`: 引数パース + 時間窓決定 + デフォルト適用
2. `resolveLikedUserID(ctx, client, configured string) (string, error)`: user_id 未指定 → GetUserMe
3. `runLikedSingle(ctx, client, userID, opts) (*xapi.LikedTweetsResponse, error)`: all=false
4. `runLikedAll(ctx, client, userID, opts) (*xapi.LikedTweetsResponse, error)`: all=true + 集約

`likedTweetsCallConfig` は private struct で 11 個のフラグを保持する:
```go
type likedTweetsCallConfig struct {
    userID       string
    startTime    time.Time // ゼロ値なら未設定
    endTime      time.Time
    maxResults   int
    all          bool
    maxPages     int
    tweetFields  []string
    expansions   []string
    userFields   []string
}
```

`buildLikedTweetsCallOptions(cfg *likedTweetsCallConfig) []xapi.LikedTweetsOption` で xapi opts に変換する。

### D-11: max_results / max_pages のデフォルト適用方針
- args に key 不在: spec デフォルト適用 (max_results=100, max_pages=50)
- args に key 存在で値が `null`: default 適用 (JSON null → Go では key 存在で値が `nil` interface。helper が「不在」として返す)
- args に key 存在で値が `0` 等の異常値: IsError (明示的な誤入力として扱う)

**ヘルパ実装ルール (advisor 指摘#2)**: `argString` / `argBool` / `argInt` / `argStringSlice` は冒頭で `v, ok := args[key]; if !ok || v == nil { return zero, false, nil }` を行う。これで JSON `null` を「key 不在」と同じく扱い、後続の型アサートで nil-interface panic を起こさない。

### D-12: tweet_fields / expansions / user_fields のデフォルト適用
- args に key 不在 (or `null` or 空配列): spec §11 のデフォルト配列を適用
- 明示的に空配列 `[]` を渡したい場合は xapi 側で no-op (WithTweetFields は空 slice で no-op) — つまり「明示的な空 = デフォルト適用」と同じ挙動になる。これは spec が明示禁止していない (CLI と同じ動作)

**Description 文言で明示 (advisor 指摘 non-blocking)**: tool / 各フィールドの `Description` に「empty array applies spec defaults」を 1 行入れる (例: "tweet.fields (default applied when omitted or empty)")。

### D-13: nil client 防御
`NewGetLikedTweetsHandler(nil)` も M17 と同様 panic させず IsError=true を返す (テストで `NewServer(nil, "test")` 互換)。

## Implementation Plan

### ファイル

1. **`internal/mcp/tools_likes.go`** (新規):
   - `const` / `var` ブロックで spec §11 デフォルト値 (max_results=100, max_pages=50, tweet_fields=[..], expansions=[..], user_fields=[..])
   - `type likedTweetsCallConfig struct { ... }`
   - `func registerToolLikes(s *mcpserver.MCPServer, client *xapi.Client)`: tool 宣言 (11 入力 + description) + `s.AddTool(tool, NewGetLikedTweetsHandler(client))`
   - `func NewGetLikedTweetsHandler(client *xapi.Client) mcpserver.ToolHandlerFunc`: ハンドラ factory
   - `func buildLikedTweetsCallConfig(args map[string]any) (*likedTweetsCallConfig, error)`: 引数パース
   - `func buildLikedTweetsCallOptions(cfg *likedTweetsCallConfig) []xapi.LikedTweetsOption`: xapi opts 構築
   - `func resolveLikedUserID(ctx context.Context, client *xapi.Client, configured string) (string, error)`: self 解決
   - `func runLikedSingle(ctx, client, userID, opts) (*xapi.LikedTweetsResponse, error)`: シングルページ
   - `func runLikedAll(ctx, client, userID, opts) (*xapi.LikedTweetsResponse, error)`: 全件 + 集約 + Meta 再構築
   - `func parseJSTDate(s string) (start, end time.Time, err error)`: CLI と同等 (M18 内に独立実装、将来昇格対象)
   - `func yesterdayJSTRange(now time.Time) (start, end time.Time, err error)`: CLI と同等
   - `func jstLocation() *time.Location`: CLI と同等
   - `func argString(args map[string]any, key string) (string, bool, error)`: 引数取得ヘルパ (型違反は error)
   - `func argBool(args map[string]any, key string) (bool, bool, error)`
   - `func argInt(args map[string]any, key string) (int, bool, error)`
   - `func argStringSlice(args map[string]any, key string) ([]string, bool, error)`

2. **`internal/mcp/server.go`** (更新):
   - `NewServer` 内に `registerToolLikes(s, client)` を追記 (M17 の `registerToolMe(s, client)` の直後)

3. **`internal/mcp/doc.go`** (更新):
   - パッケージ doc に `tools_likes.go: get_liked_tweets (M18)` を追記 (現状 "M18 以降" になっているのを「M18 完了」に明示化)

4. **`internal/mcp/tools_likes_test.go`** (新規):
   - 後述 TDD ケース

### TDD ケース (table-driven, httptest + handler 直接呼び出し)

#### Group A: 引数バリデーション (X API 呼び出し前に弾く)

1. **A-1**: `since_jst = "2026/05/11"` (不正フォーマット) → IsError=true
2. **A-2**: `start_time = "not-rfc3339"` → IsError=true
3. **A-3**: `max_results = 0` → IsError=true (範囲外)
4. **A-4**: `max_results = 101` → IsError=true (範囲外)
5. **A-5**: `max_results = "50"` (string 渡し) → IsError=true (型違反)
6. **A-6**: `tweet_fields = "id,text"` (string 渡し、spec は string[]) → IsError=true
7. **A-7**: nil client → IsError=true (panic しない)

#### Group B: 時間窓決定 (httptest で URL 検証)

8. **B-1a** (advisor 指摘#1: 決定論的に切り替え): `yesterdayJSTRange(fixedTime)` ヘルパを直接呼び出す unit test。固定時刻 (例: `2026-05-12T03:00:00+09:00`) を渡し、戻り値 start/end が `2026-05-11T00:00:00+09:00` / `2026-05-11T23:59:59+09:00` となることを確認。URL 検証は B-2 で代用 (httptest が JST 跨ぎ midnight でフレークする問題を回避)。
9. **B-2**: `since_jst = "2026-05-11"` → URL の `start_time = "2026-05-10T15:00:00Z"` / `end_time = "2026-05-11T14:59:59Z"`
10. **B-3**: `start_time = "2026-05-10T00:00:00Z"` / `end_time = "2026-05-10T23:59:59Z"` → URL に同値が反映
11. **B-4**: 優先順位 — `since_jst="2026-05-11"` + `start_time="2025-01-01T00:00:00Z"` の組合せで URL は since_jst の範囲が勝つ (yesterday_jst の優先順位確認は B-1a の unit test + B-2 のしくみで担保される)
12. **B-5**: 上の B-4 を `yesterday_jst=false` で確認 (yesterday_jst の有無で since_jst の動作が変わらないこと)

#### Group C: シングルページ取得 (all=false)

13. **C-1**: `user_id = "12345"` + 200 OK + 5 件 → `IsError=false` + `StructuredContent` が `*xapi.LikedTweetsResponse` 型 + `Data` len=5
14. **C-2**: `user_id` 未指定 → 内部で `/2/users/me` を呼び `data.id` を取得 → 後続の `/2/users/:id/liked_tweets` の path に反映 (httptest で 2 段階リクエストを順に応答)
15. **C-3**: TextContent (fallback) が JSON 文字列で `"data"` / `"includes"` / `"meta"` キーを含む

#### Group D: 全件取得 (all=true + 集約)

16. **D-1**: 3 ページ (P1 NextToken=t1, P2 NextToken=t2, P3 NextToken="") → 集約後 Data 件数 = 3 ページ合計, NextToken="" (再構築), ResultCount = 集約後の総件数
17. **D-2**: `all=true` + `max_pages=2` → 2 ページで打ち切り (3 ページ目に到達しない、URL 呼び出し回数 = 2)
18. **D-3**: `all=true` で 1 ページ目から NextToken="" → 1 ページのみ取得

#### Group E: クエリパラメータ反映

19. **E-1**: `tweet_fields=["id","text"]` / `expansions=["author_id"]` / `user_fields=["username"]` → URL の `tweet.fields=id,text` / `expansions=author_id` / `user.fields=username`
20. **E-2**: `tweet_fields` 未指定 → URL に spec §11 デフォルト (`id,text,author_id,created_at,entities,public_metrics`) が含まれる
21. **E-3**: `max_results=50` → URL に `max_results=50`

#### Group F: X API エラー → IsError

22. **F-1**: 401 Unauthorized → IsError=true + text に "auth" 系
23. **F-2**: 404 Not Found (user_id 不正) → IsError=true
24. **F-3**: GetUserMe (self 解決) で 401 → IsError=true

#### Group G: 出力スキーマ

25. **G-1**: `StructuredContent` が `*xapi.LikedTweetsResponse` 型 **pointer** で返ること (D-2 で `NewToolResultJSON(resp)` に pointer を渡すと決定)。型アサートは `*xapi.LikedTweetsResponse` で行う。
26. **G-2**: TextContent 生 JSON が spec §6 出力スキーマ (`data` / `includes` / `meta` キー) と一致 (`"user_id"` のような MCP リネームは行わない、xapi の json タグそのまま)。空 includes / meta は `{}` で残ることに注意 (omitempty + struct の標準挙動)。

### Helper 関数のテスト (任意、内部関数として export しない場合は package internal test で間接的にカバー)

- `parseJSTDate`: CLI 側の `parseJSTDate` と同等の挙動 (CLI 側で既にテスト済みのため、MCP 内では handler テスト経由で間接カバー)
- `yesterdayJSTRange`: 同上
- `argInt` / `argBool` / `argString` / `argStringSlice`: handler バリデーションテスト経由で間接カバー

最小限の export は避ける (テストファイル外からアクセス不要)。`package mcp_test` から到達できないが、テストケースで `IsError=true` を確認することで実質的にカバーされる。

## Open Questions

- (none) — spec §6 / §11 / M17 ハンドオフで全て確定

## Risks & Mitigation

| リスク | 対応 |
|---|---|
| ハンドラ関数が肥大化 (引数 11 個 + 分岐多) | D-10 のヘルパ分割 + 個別の単体ロジックを `likedTweetsCallConfig` に集約 |
| `parseJSTDate` / `yesterdayJSTRange` の CLI 重複 | D-5 で意図的に独立実装。コメントで「将来 `internal/timeutil` 昇格対象」と明記 |
| `xapi.LikedTweetsResponse` の `omitempty` で 0 件時 `data` キーが消える | spec が明示的に許容している (X API 自身も同挙動)。テストで「0 件 == data キー無し」を検証 |
| `mcp-go` の `DefaultBool` / `DefaultNumber` が引数取得側に補完しない仕様 | D-7/D-11 でハンドラ側にデフォルト適用ロジックを実装 |
| StructuredContent が pointer 型で返る (`*xapi.LikedTweetsResponse`) | テストでの型アサートを `*xapi.LikedTweetsResponse` で行う |
| JSON 数値が `float64` で届くため `int` 変換時に小数部丢失 | D-9 で「小数部があれば IsError」とする (`v - float64(int(v)) != 0` で判定) |

## Verification

実装完了後:
- `go test -race -count=1 ./...` 全 pass
- `golangci-lint run ./...` 0 issues (v2 形式維持)
- `go vet ./...` clean
- `go build -o /tmp/x ./cmd/x` 成功
- 既存 M1-M17 テストの非破壊性確認

## Commit Message Template

```
feat(mcp): get_liked_tweets ツールを追加 (全パラメータ + ページネーション + JST 優先順位)

M17 で確立した registerToolXxx + NewXxxHandler パターンを踏襲し、
spec §6 の 11 入力パラメータをすべて受け付ける get_liked_tweets MCP tool を実装。

主要決定:
- 出力は *xapi.LikedTweetsResponse をそのまま返却 (json タグが spec 出力スキーマと一致)
- 時間窓: yesterday_jst > since_jst > start_time/end_time (CLI と同優先順位)
- user_id 未指定時は GetUserMe で self 解決 (CLI と同型)
- ハードコードデフォルト (spec §11): max_results=100, max_pages=50,
  tweet_fields/expansions/user_fields は MCP 層に固定値
- parseJSTDate / yesterdayJSTRange は CLI と重複実装 (将来 internal/timeutil 昇格対象)
- ハンドラは buildLikedTweetsCallConfig / resolveLikedUserID / runLikedSingle/All で責務分割
- nil client / 不正引数 / API エラーはすべて IsError=true CallToolResult を返す

テスト: 20+ ケース (table-driven, httptest + handler 直接呼び出し)
- 引数バリデーション (型違反 / 範囲外 / フォーマット不正)
- 時間窓優先順位 (4 パターン)
- シングルページ / 全件取得 + 集約 / max_pages 打ち切り
- クエリパラメータ反映 / 401/404 → IsError
- 出力スキーマの spec 一致確認

Plan: plans/x-m18-mcp-get-liked-tweets.md
```
