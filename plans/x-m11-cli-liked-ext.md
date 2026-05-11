# M11 詳細計画: CLI `x liked list` 拡張フラグ

> Layer 2: M11 マイルストーンの詳細実装計画。
> 親ロードマップ: [plans/x-roadmap.md](./x-roadmap.md)
> スペック: [docs/specs/x-spec.md](../docs/specs/x-spec.md) §6 `x liked list` フル仕様 / §10 rate-limit aware ページング / §11 `[liked]` デフォルト
> 前マイルストーン: M10 (commit `9931b01`) — likedClient interface / newLikedClient swap / `--user-id` `--start-time` `--end-time` `--max-results` `--pagination-token` `--no-json` の 6 フラグ実装済

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M11 |
| 範囲 | `internal/cli/liked.go` (既存編集) / `internal/cli/liked_test.go` (既存編集) のみ。`cmd/x/main.go` 変更なし (exit 写像は M10 で完了済) |
| 前提 | M10 (commit `9931b01`)、`xapi.EachLikedPage` / `WithMaxPages` / `WithTweetFields` / `WithExpansions` / `WithLikedUserFields` (M8 commit `2272f0b`) |
| 完了条件 | `httptest` mock で `x liked list --since-jst 2026-05-11` が JST 0:00-23:59 を UTC 変換してクエリ送出 / `--yesterday-jst` が当日基準で前日に変換 / `--all` で next_token を辿る (2-3 ページ繰り返し) / `--max-pages` で上限制御 / `--ndjson` で 1 ツイート 1 行 JSON / `--tweet-fields` `--expansions` `--user-fields` がデフォルト値・カスタム値ともクエリに反映 / `--no-json` と `--ndjson` 同時指定で exit 2。`go test -race -count=1 ./...` 全 pass (M1-M10 既存 21+ テスト維持)、`golangci-lint run ./...` 0 issues、`go vet ./...` clean、`go build -o /tmp/x ./cmd/x` 成功 |
| TDD | Red → Green → Refactor 厳守 |

## Scope

### In Scope

1. **`internal/cli/liked.go`** (既存編集)
   - 新規追加フラグ (8 個):
     | フラグ | 型 | デフォルト | 説明 |
     |---|---|---|---|
     | `--since-jst` | string | `""` | JST 日付 `YYYY-MM-DD`。当日 0:00-23:59 を UTC RFC3339 に変換して `--start-time` / `--end-time` を上書き |
     | `--yesterday-jst` | bool | `false` | JST 前日に対する `--since-jst` 等価。`--since-jst` より優先 |
     | `--all` | bool | `false` | next_token を自動辿って全件取得 (xapi.EachLikedPage を使う) |
     | `--max-pages` | int | `50` | `--all` 時の最大ページ数 (spec §6 / §10 / §11) |
     | `--ndjson` | bool | `false` | 1 ツイート 1 行 JSON で出力 |
     | `--tweet-fields` | string | `"id,text,author_id,created_at,entities,public_metrics"` | csv (spec §11 `[liked].default_tweet_fields`) |
     | `--expansions` | string | `"author_id"` | csv (spec §11 `[liked].default_expansions`) |
     | `--user-fields` | string | `"username,name"` | csv (spec §11 `[liked].default_user_fields`) |
   - `likedClient` interface 拡張: `EachLikedPage` メソッド追加
     ```go
     type likedClient interface {
         GetUserMe(ctx context.Context, opts ...xapi.UserFieldsOption) (*xapi.User, error)
         ListLikedTweets(ctx context.Context, userID string, opts ...xapi.LikedTweetsOption) (*xapi.LikedTweetsResponse, error)
         EachLikedPage(ctx context.Context, userID string, fn func(*xapi.LikedTweetsResponse) error, opts ...xapi.LikedTweetsOption) error
     }
     ```
     `*xapi.Client` は既に M8 で実装済みなので `newLikedClient` factory はそのまま (ファクトリ本体の変更不要)。
   - 新規内部ヘルパ:
     - `parseJSTDate(s string) (start, end time.Time, err error)`
       - JST `Asia/Tokyo` の `YYYY-MM-DD` を `time.ParseInLocation("2006-01-02", s, jst)` で UTC 変換
       - `start = 当日 0:00 JST → UTC`、`end = 翌日 0:00 JST - 1秒 → UTC` (= 当日 23:59:59 JST)
       - パース失敗時は `fmt.Errorf("%w: --since-jst: %v", ErrInvalidArgument, err)` を返却
       - JST ロケーションは `time.LoadLocation("Asia/Tokyo")` で取得。LoadLocation エラー時は `time.FixedZone("JST", 9*3600)` にフォールバック (advisor 補足)
     - `decideOutputMode(noJSON, ndjson bool) (likedOutputMode, error)`
       - 排他チェック: 両 true → `ErrInvalidArgument` wrap
       - `ndjson=true` → `likedOutputModeNDJSON`
       - `noJSON=true` → `likedOutputModeHuman`
       - 両 false → `likedOutputModeJSON` (default)
     - `likedOutputMode` 型 (`int` 定数 3 種: JSON / Human / NDJSON)
     - `splitCSV(s string) []string` — `strings.Split(s, ",")` の各要素を `strings.TrimSpace` してから空文字列を除外
   - `RunE` ロジック更新 (順序):
     1. **バリデーション** (認証ロードより先, plans D-7 維持):
        - `--max-results` 1..100 (既存 M10)
        - `--since-jst` 非空なら parseJSTDate
        - `--no-json` と `--ndjson` 同時指定で `ErrInvalidArgument`
        - `--yesterday-jst` 指定時は `parseJSTDate(time.Now().In(jst).AddDate(0,0,-1).Format("2006-01-02"))` 相当を内部生成
        - `--start-time` / `--end-time` パース (既存 M10)
     2. **時間窓の優先順位** (spec §6 + ハンドオフ確定):
        - `--yesterday-jst` true → `since-jst` 計算結果で startT / endT を上書き
        - else `--since-jst` 非空 → 上書き
        - else `--start-time` / `--end-time` (M10 の挙動を維持)
     3. 認証ロード → クライアント生成 (既存)
     4. self 解決 (既存)
     5. **オプション組み立て**:
        - `xapi.WithMaxResults(maxResults)` (既存)
        - `xapi.WithStartTime` / `xapi.WithEndTime` (時間窓セット時)
        - `xapi.WithPaginationToken` (`--pagination-token` 非空かつ `--all=false`、`--all=true` 時は警告ログを stderr に出して無視)
        - `xapi.WithTweetFields(splitCSV(tweetFields)...)` (空文字なら省略)
        - `xapi.WithExpansions(splitCSV(expansions)...)` (空文字なら省略)
        - `xapi.WithLikedUserFields(splitCSV(userFields)...)` (空文字なら省略)
        - `--all` 時のみ `xapi.WithMaxPages(maxPages)` を追加
     6. **取得**:
        - `--all=false` (default): `client.ListLikedTweets(ctx, targetUserID, opts...)`
        - `--all=true`: `client.EachLikedPage(ctx, targetUserID, callback, opts...)`
          - callback の挙動は出力モードで決定:
            - NDJSON: 各ページの Data を 1 ツイート 1 行 JSON で即時出力 (ストリーミング)
            - JSON / human: ページを集約して最後に出力 (集約器: `aggregateLikedPages`)
     7. **出力**:
        - `likedOutputModeJSON` (default): 集約済みレスポンスを単一 JSON で出力
        - `likedOutputModeHuman` (`--no-json`): 既存 `writeLikedHuman`
        - `likedOutputModeNDJSON` (`--ndjson`): 1 ツイートずつ `json.Encoder.Encode(tw)` で出力 (`SetEscapeHTML(false)` は不要、デフォルトの挙動を維持)
   - 集約器ヘルパ:
     - `aggregateLikedPages` は `EachLikedPage` の callback として `*LikedTweetsResponse` を受け、内部スライスに data / includes.users / includes.tweets を append し、最後の meta を保持する struct
     - `--all=true` 時の `meta` フィールドは最後のページの meta を出力する (next_token は出てくれば残るが、最終ページなら next_token は空のはず)
   - NDJSON ストリーミング:
     - `--all=true` + `--ndjson` の場合: callback 内で各ツイートを即時 `Encoder.Encode` する。集約しない (メモリ効率)。
     - `--all=false` + `--ndjson`: 単一ページ取得後、Data の各要素を順に `Encode`
   - 既存テストとの互換性:
     - **既存 21 テスト全 pass を維持** (M11 では `--no-json` 既定挙動 / JSON 既定挙動を一切変更しない)
     - M10 テストは追加フラグを指定しないので新フラグのデフォルト値で動作する (parseJSTDate スキップ、tweet-fields デフォルト値はクエリに反映されるので `TestLikedList_MaxResultsInQuery` の `strings.Contains(qs[0], "max_results=50")` 検査には影響なし、ただし `TestLikedList_TimeWindowInQuery` のクエリ assert は contains ベースなので影響なし)
   - 既存テストへの影響:
     - tweet.fields / expansions / user.fields のデフォルト値がクエリに反映されるため、M10 テストのクエリには `tweet.fields=...` 等が増える。`strings.Contains` ベースの assert は影響なし、ただし完全一致比較があれば修正必要 → M10 では `strings.Contains` のみで検査しているため影響なし。

2. **`internal/cli/liked_test.go`** (既存編集) — 新規テスト 15-20 件追加
   既存 21 テストはそのまま残し、以下を新規追加:
   - `TestLikedList_SinceJST_QueryReflectsUTC` — `--since-jst 2026-05-12` で start_time=2026-05-11T15:00:00Z & end_time=2026-05-12T14:59:59Z がクエリに含まれる
   - `TestLikedList_YesterdayJST_QueryReflectsUTC` — `--yesterday-jst` で「実行時刻 JST 前日」が正しく反映される (固定 clock 不要、test 実行時刻基準で範囲を含むこと検査)
   - `TestLikedList_SinceJST_OverridesStartEnd` — `--since-jst` と `--start-time`/`--end-time` 同時指定で since-jst が優先 (advisor 確定)
   - `TestLikedList_YesterdayJST_OverridesSinceJST` — yesterday-jst が since-jst を上書き
   - `TestLikedList_SinceJST_InvalidFormat` — `--since-jst notadate` で `ErrInvalidArgument`
   - `TestLikedList_All_FollowsNextToken` — 3 ページ続けた後 `next_token=""` で停止することを検証
   - `TestLikedList_All_MaxPagesCap` — `--max-pages=2` で 2 ページで停止
   - `TestLikedList_All_AggregatedJSON` — `--all` JSON 出力で全ページのデータが集約されている
   - `TestLikedList_All_Human` — `--all --no-json` で全ページの human 行が出力される
   - `TestLikedList_NDJSON_SinglePage` — `--ndjson` で 1 ツイート 1 行 JSON
   - `TestLikedList_NDJSON_AllStreaming` — `--all --ndjson` でストリーミング (全ツイートが NDJSON 形式で出る)
   - `TestLikedList_NDJSON_EmptyResponse` — 0 件で空 stdout
   - `TestLikedList_NoJSON_NDJSON_Mutex` — `--no-json --ndjson` 同時指定で `ErrInvalidArgument`
   - `TestLikedList_TweetFields_CustomCSV` — `--tweet-fields id,text` でクエリ `tweet.fields=id,text`
   - `TestLikedList_Expansions_CustomCSV` — `--expansions author_id,referenced_tweets.id`
   - `TestLikedList_UserFields_CustomCSV` — `--user-fields username,name,verified`
   - `TestLikedList_DefaultFieldsInQuery` — フラグ未指定でも default が反映される (`tweet.fields=id,text,...`)
   - `TestLikedListHelp_ShowsExtFlags` — `--help` に `--since-jst` `--yesterday-jst` `--all` `--max-pages` `--ndjson` `--tweet-fields` `--expansions` `--user-fields` が表示

### Out of Scope

- **config.toml 連携**: M12 (`x configure`) で実装。M11 では spec §11 のデフォルト値をハードコード
- **rate-limit ヘッダ追従の追加実装**: xapi.EachLikedPage が既に実装済 (M8)
- **`x liked count` 等のサブコマンド追加**: 範囲外
- **MCP 経由のフラグ反映**: M15-M17 で別途

## Decisions

- **D-1: `--no-json` と `--ndjson` の排他**: 同時指定で `ErrInvalidArgument` (exit 2)。優先度は不要、ハンドオフ確定通り
- **D-2: `--since-jst` の優先順位**: `--yesterday-jst` > `--since-jst` > `--start-time`/`--end-time`。spec §6 「since-jst は start/end を上書き」+ ハンドオフ確定
- **D-3: JST タイムゾーン取得**: `time.LoadLocation("Asia/Tokyo")` を試み、失敗時 (zoneinfo 無し環境) は `time.FixedZone("JST", 9*3600)` にフォールバック。Linux/macOS 開発環境では LoadLocation が成功するが、Lambda 等の minimal 環境保険
- **D-4: `--since-jst` の終了時刻**: JST 0:00:00 + 24h - 1s = JST 23:59:59 → UTC 14:59:59。spec §8 フロー1 例と一致
- **D-5: `--all` 時の `--pagination-token`**: 警告して無視。両指定はユーザの誤用と判断、停止せず動作継続 (spec §6 に明示無いが、`--all` の意図はトークン管理を CLI に任せること)。warning は `cmd.ErrOrStderr()` に 1 行出力
- **D-6: NDJSON ストリーミング (--all時)**: メモリ効率のため集約せず即時出力。ただし途中エラー時は既出力分は捨てない (stdout は append-only)。Error は exit code に反映
- **D-7: `--all=false` + NDJSON**: 単一ページ取得後 Data を 1 行ずつ Encode。集約構造は使わない (`[]Tweet` のループのみ)
- **D-8: 集約器 `--all=true` + JSON** (advisor 指摘 #2 反映): 全ページの `Data` を append、includes.users / includes.tweets も append (重複排除しない、X API がページ間で同じユーザーを返す可能性はあるが MCP/CLI スキーマでの一意化責任は呼び出し側)。meta は **再構築**: `ResultCount = len(aggregated.Data)`、`NextToken = ""` (集約後は必ず空)。最後のページの meta をそのまま流すと「全体件数か最終ページ件数か」が曖昧になり MCP consumer を誤導するため
- **D-9: csv split のホワイトスペース処理**: `--tweet-fields "id, text "` のような入力で空白を許容する。`strings.TrimSpace` + 空要素除外
- **D-10: `--tweet-fields=""` 明示空文字**: 「フィールド指定なし (X API デフォルトに任せる)」と解釈し、X API クエリに `tweet.fields` を含めない。`pflag.StringVar` のデフォルト値とユーザ指定空文字の区別は付かないが、用途上「デフォルト or 空白」のいずれも実害なし
- **D-11: ファクトリは変更不要**: `*xapi.Client` が `EachLikedPage` を既に持つので `newLikedClient` は変更不要。likedClient interface に EachLikedPage シグネチャを追加するだけで satisfy する
- **D-12: NDJSON の HTML エスケープ** (advisor 指摘 #3): NDJSON 出力時は `json.Encoder.SetEscapeHTML(false)` を呼ぶ。X tweet text は `<`, `>`, `&` を頻繁に含む (URL / hashtag / RT 記号) ため、デフォルトの `<` 等エスケープは NDJSON consumer に不要な変換コストを強いる。NDJSON 慣習に従い raw 出力する。既存の単一 JSON 出力 (`--no-json=false` default) は M10 互換性のため `json.NewEncoder().Encode(resp)` のままで HTML エスケープ有効を維持
- **D-13: `--all` + `--pagination-token` 警告先** (advisor 指摘): `cmd.ErrOrStderr()` に出力。stdout は JSON/NDJSON pipe 用途で keepclean

## Risks

- **R-1: 既存 M10 テストの破壊**: tweet.fields/expansions/user.fields のデフォルト値が常にクエリに反映されるため、M10 テストの URL クエリ assert に影響する可能性 → 既存テストは全て `strings.Contains` ベースで個別フラグを検査しているため影響なし (再確認済み)
- **R-2: yesterday-jst の境界条件**: 実行時刻が JST 0:00 直前/直後で挙動が変わる。テストは固定 clock を注入できないので「実行時刻 - 24h ± 数秒」範囲で検査する
- **R-3: parseJSTDate の zoneinfo 依存**: Lambda 等で `Asia/Tokyo` が利用できない可能性 → D-3 のフォールバック
- **R-4: EachLikedPage の rate-limit sleep**: テストで遅延が発生しない様、xapi 層で `c.sleep` 関数を差し替え不要なテストデータ (next_token あり 2-3 ページ完結) を使う。max_pages=2 等で早期停止する場合は sleep は呼ばれない
- **R-5: M10 テストの fixture 影響**: `newLikedTestServer` のデフォルトハンドラは 1 ページのみ。`--all` テストではカスタム handler で next_token を返すサーバを作る

## Test Plan

### Red 期待動作 (実装前に書く失敗テスト)

1. `TestLikedList_SinceJST_QueryReflectsUTC` — pass 前は `--since-jst` フラグ自体が `unknown flag` → fail
2. `TestLikedList_All_FollowsNextToken` — pass 前は `--all` フラグなし → fail
3. `TestLikedList_NDJSON_SinglePage` — pass 前は `--ndjson` フラグなし → fail
4. ...

### Green 実装

- `parseJSTDate` を実装
- `decideOutputMode` を実装
- `likedClient` interface に `EachLikedPage` 追加
- `RunE` ロジック書き換え
- 集約器 / NDJSON streaming 出力ヘルパ追加

### Refactor

- 重複コード除去 (timeWindow 解決を関数に切り出し)
- マジック値の定数化 (default fields 文字列)

## Quality Gates

- `go test -race -count=1 ./...` 全 pass (既存 + 新規 15-20 テスト)
- `golangci-lint run ./...` 0 issues
- `go vet ./...` clean
- `go build -o /tmp/x ./cmd/x` 成功
- M10 既存 21 テスト全 pass (互換性維持)

## Commit Message (Conventional Commits / 日本語)

```
feat(cli): x liked list に JST 日付ヘルパ・全件取得・NDJSON 等の拡張フラグを追加

- --since-jst / --yesterday-jst で JST 日付を UTC RFC3339 範囲に変換
- --all + --max-pages で next_token 自動辿り (xapi.EachLikedPage 利用)
- --ndjson で 1 ツイート 1 行 JSON 出力 (single page / all 共にストリーミング)
- --tweet-fields / --expansions / --user-fields で API レスポンスフィールドをカスタマイズ
- --no-json と --ndjson は排他 (同時指定で exit 2)
- spec §11 [liked] のデフォルト値をハードコード (config.toml 連携は M12)

Plan: plans/x-m11-cli-liked-ext.md
```
