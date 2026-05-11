# M08 詳細計画: `internal/xapi/likes.go` — ListLikedTweets + ページネーション + rate-limit aware

> Layer 2: M8 マイルストーンの詳細実装計画。
> 親ロードマップ: [plans/x-roadmap.md](./x-roadmap.md)
> スペック: [docs/specs/x-spec.md](../docs/specs/x-spec.md) §6 `get_liked_tweets` / §10 Rate-limit aware ページング

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M8 |
| 範囲 | `internal/xapi/types.go` (Tweet 拡張), `internal/xapi/likes.go` (新規), `internal/xapi/likes_test.go` (新規), `internal/xapi/types_test.go` (拡張) |
| 前提 | M7 (users.go + types.go) commit `bc2aff5` |
| 完了条件 | `httptest` mock で 5 ページ next_token 辿り + rate-limit aware sleep + 429 retry + context cancel が pass、`go test -race ./...` 全件 pass、`golangci-lint run` 0 issues |
| TDD | Red → Green → Refactor 厳守 |

## Scope

### In Scope

1. **`internal/xapi/types.go`** (既存ファイルへ追記)
   - `TweetPublicMetrics` 型 (retweet_count / reply_count / like_count / quote_count / bookmark_count / impression_count)
   - `TweetEntities` 型 (urls / hashtags / mentions / annotations) — 最小スキーマ
   - `ReferencedTweet` 型 (type / id)
   - `Tweet` に `Entities` / `PublicMetrics` / `ReferencedTweets` フィールドを追加 (omitempty)
   - `Includes` に `Tweets []Tweet` を追加 (referenced_tweets.id expansion 用)

2. **`internal/xapi/likes.go`** (新規)
   - `ListLikedTweets(ctx, userID string, opts ...LikedTweetsOption) (*LikedTweetsResponse, error)` — シングルページ
   - `EachLikedPage(ctx, userID string, fn func(*LikedTweetsResponse) error, opts ...LikedTweetsOption) error` — 全ページ自動辿り (max_pages 上限 + rate-limit aware sleep + ページ間最小待機)
   - `LikedTweetsResponse` 型 (Data / Includes / Meta)
   - `LikedTweetsOption` 型 (`func(*likedTweetsConfig)`) + Option 関数群:
     - `WithStartTime(t time.Time)` (内部で `t.UTC().Format(time.RFC3339)` で `2026-05-12T00:00:00Z` 形式に固定。`RFC3339Nano` ではない)
     - `WithEndTime(t time.Time)` (同上)
     - `WithMaxResults(n int)` (1-100 を呼び出し側で検証はせず、X API に丸投げ。0 は no-op。**CLI 層 (M10/M11) が default 100 をセットする責務を負う**。spec §11 `default_max_results = 100` と整合)
     - `WithPaginationToken(token string)`
     - `WithTweetFields(fields ...string)`
     - `WithExpansions(exp ...string)`
     - `WithLikedUserFields(fields ...string)` (M7 の `WithUserFields` は GetUserMe 専用なので別名で公開)
     - `WithMaxPages(n int)` (EachLikedPage 専用、default 50)

3. **`internal/xapi/likes_test.go`** (新規)
   - シングルページ取得 (200 + 3 ツイート + meta)
   - クエリパラメータ検証 (start_time / end_time / max_results / pagination_token / tweet.fields / expansions / user.fields)
   - 401 → `ErrAuthentication`
   - 404 → `ErrNotFound`
   - context cancel
   - 不正 JSON → decode error (リトライしない)
   - userID の URL エスケープ (例: `42` → `/2/users/42/liked_tweets`)
   - EachLikedPage 全件辿り (3 ページ × 2 ツイート = 計 6 ツイート、最後の next_token 空で終了)
   - EachLikedPage max_pages=2 で打ち切り (3 ページ用意してあっても 2 ページで終了)
   - EachLikedPage rate-limit aware sleep (Remaining=1, Reset=未来 5 秒 → sleep されることを検証)
   - EachLikedPage ページ間最小待機 200ms (rate-limit 余裕がある場合)
   - EachLikedPage 中の 429 → Client.Do の既存 retry を利用 (server 側で 429→200 を返し正常完了)
   - EachLikedPage callback でエラー返却 → 即時中断
   - EachLikedPage context cancel → ctx.Err() 返却

4. **`internal/xapi/types_test.go`** (既存ファイルへ追記)
   - `Tweet.PublicMetrics` / `Tweet.Entities` / `Tweet.ReferencedTweets` の unmarshal 検証
   - `Includes.Tweets` の unmarshal 検証

### Out of Scope (M9 以降)

- CLI 層からの呼び出し → M10/M11
- `--yesterday-jst` / `--since-jst` の time 計算 → M11 (cli/liked.go)
- NDJSON 出力 → M11
- MCP tool 統合 → M18
- iter.Seq2 ベースのイテレータ (callback 形式に統一して KISS)

## Design Decisions

### D-1: Option パターンの型を **`func(*likedTweetsConfig)`** にする (M7 の `func(*url.Values)` 継承ではない)

理由:
- `WithMaxPages` は **EachLikedPage 専用** で URL クエリではなくクライアント側挙動の設定 → `url.Values` には乗らない
- `WithMaxResults` は数値、`WithStartTime` は `time.Time` 変換が必要 → 中間構造体経由が型安全
- M7 の `WithUserFields(...)` (`UserFieldsOption`) は GetUserMe 専用なので独立して残す
- M11/M18 で MCP/CLI 層から構造体経由で渡しやすい

中間構造体:
```go
type likedTweetsConfig struct {
    startTime       *time.Time
    endTime         *time.Time
    maxResults      int
    paginationToken string
    tweetFields     []string
    expansions      []string
    userFields      []string

    // EachLikedPage 専用
    maxPages int
}
```

`ListLikedTweets` は `maxPages` を無視。`EachLikedPage` のみ参照する。

### D-2: `EachLikedPage` は **callback 形式** (iter.Seq2 は使わない)

理由:
- Go 1.26.1 で iter.Seq2 自体は利用可能だが、エラー伝搬・early break の semantics が callback の方が単純
- 呼び出し側は CLI/MCP の薄いラッパーで「ページごとに NDJSON 出力 / 配列に append」が主用途 → callback で十分
- テストが書きやすい (`fn` が呼ばれた回数・引数を spy できる)

```go
func (c *Client) EachLikedPage(
    ctx context.Context,
    userID string,
    fn func(*LikedTweetsResponse) error,
    opts ...LikedTweetsOption,
) error
```

呼び出し側で全件配列にまとめたい場合は `var all []Tweet; err := c.EachLikedPage(ctx, id, func(p *LikedTweetsResponse) error { all = append(all, p.Data...); return nil })` のように書く。

### D-3: rate-limit aware sleep ロジック

仕様 (§10):
- レスポンスヘッダ `x-rate-limit-remaining` / `x-rate-limit-reset` を毎回パース → 既に Client.Do が `Response.RateLimit` に詰める
- `remaining ≤ 2` かつ `Raw=true` (実際にヘッダが来た) で `reset` 時刻まで sleep (最大 15 分)
- ページ間の最小待機: 200ms (バースト抑止)
- 429 自体は Client.Do の retry で吸収済み (M6)

実装 (advisor 指摘#1 反映: wait<=0 でも 200ms を最低保証):
```go
// 各ページ取得後 (next_token があれば次ページ前) に判定
wait := likesMinInterPageDelay // 既定 200ms (バースト抑止)
if rl := resp.RateLimit; rl.Raw && rl.Remaining >= 0 && rl.Remaining <= likesRateLimitThreshold && !rl.Reset.IsZero() {
    until := rl.Reset.Sub(c.now())
    if until > rateLimitMaxWait { // 15 min cap
        until = rateLimitMaxWait
    }
    if until > wait { // 200ms より長い場合のみ採用 (Reset が過去でも 200ms は確保)
        wait = until
    }
}
_ = c.sleep(ctx, wait)
```

定数 (advisor 指摘#2 反映: `rateLimitMaxWait` は `client.go` の既存定数を再利用):
```go
// likes.go では client.go の既存定数 `rateLimitMaxWait` (15 * time.Minute) を再利用する。
// likes 固有の定数のみ likes.go に定義する:
const (
    likesMinInterPageDelay  = 200 * time.Millisecond
    likesDefaultMaxPages    = 50
    likesRateLimitThreshold = 2 // remaining <= 2 で sleep 対象
)
```

テスト追加 (advisor 指摘#1 反映):
- `TestEachLikedPage_RateLimitStaleReset_FallsBackTo200ms`: Remaining=1, Reset=過去時刻 → sleep[0] = 200ms (rateLimitMaxWait の cap も Reset 過去では発火しない)

### D-4: max_pages の暴走防止

- `WithMaxPages(n)` で上書き可、default 50 (spec §10)
- `EachLikedPage` 内でカウンタを増やし、`pages >= maxPages` で **error 返却ではなく正常終了** (打ち切られた事実は呼び出し側で `next_token` の有無で判断可能だが、今回は単純に正常終了とする)
- M11 で CLI 層が警告ログを出すかどうかを決める (M8 では責務外)
- **将来拡張余地** (advisor 指摘#補足): 戻り値を `(pagesFetched int, truncated bool, err error)` に拡張する選択肢を残す。M8 では現行シグネチャ (`error` のみ) で進め、M11 で必要になれば追加する

### D-5: userID の URL エスケープ

- `url.PathEscape(userID)` で `/2/users/{id}/liked_tweets` の `{id}` 部分をエスケープ
- 数値 ID 想定だが、X API は将来 alphanumeric を返す可能性もあるため安全側で
- パスインジェクション防止

### D-6: クエリパラメータの結合方針

- `WithTweetFields("id","text")` → `tweet.fields=id,text` (M7 の `WithUserFields` と同じ csv 結合)
- `WithExpansions("author_id","referenced_tweets.id")` → `expansions=author_id,referenced_tweets.id`
- 空 slice は no-op
- 重複呼び出しは last-wins (`url.Values.Set` 相当)

### D-7: 内部レスポンス Envelope

```go
type likedTweetsEnvelope struct {
    Data     []Tweet  `json:"data,omitempty"`
    Includes Includes `json:"includes,omitempty"`
    Meta     Meta     `json:"meta,omitempty"`
}
```

`LikedTweetsResponse` は同じ shape を公開型として再定義:

```go
type LikedTweetsResponse struct {
    Data     []Tweet  `json:"data,omitempty"`
    Includes Includes `json:"includes,omitempty"`
    Meta     Meta     `json:"meta,omitempty"`
}
```

`Data` が `nil` (X API が `data` フィールドを返さなかった = 0 件) の場合は `len(resp.Data) == 0` で扱えるようにする。

### D-8: Tweet 拡張フィールドの最小範囲

スペック §6 のデフォルト `tweet_fields = "id,text,author_id,created_at,entities,public_metrics"` を踏まえ、M8 で以下を追加:

```go
type Tweet struct {
    ID                string             `json:"id"`
    Text              string             `json:"text"`
    AuthorID          string             `json:"author_id,omitempty"`
    CreatedAt         string             `json:"created_at,omitempty"`
    Entities          *TweetEntities     `json:"entities,omitempty"`
    PublicMetrics     *TweetPublicMetrics `json:"public_metrics,omitempty"`
    ReferencedTweets  []ReferencedTweet  `json:"referenced_tweets,omitempty"`
}

type TweetPublicMetrics struct {
    RetweetCount      int `json:"retweet_count"`
    ReplyCount        int `json:"reply_count"`
    LikeCount         int `json:"like_count"`
    QuoteCount        int `json:"quote_count"`
    BookmarkCount     int `json:"bookmark_count,omitempty"`
    ImpressionCount   int `json:"impression_count,omitempty"`
}

type TweetEntities struct {
    URLs        []TweetURL        `json:"urls,omitempty"`
    Hashtags    []TweetTag        `json:"hashtags,omitempty"`
    Mentions    []TweetMention    `json:"mentions,omitempty"`
    Annotations []TweetAnnotation `json:"annotations,omitempty"`
}

type TweetURL struct {
    Start       int    `json:"start"`
    End         int    `json:"end"`
    URL         string `json:"url"`
    ExpandedURL string `json:"expanded_url,omitempty"`
    DisplayURL  string `json:"display_url,omitempty"`
}

type TweetTag struct {
    Start int    `json:"start"`
    End   int    `json:"end"`
    Tag   string `json:"tag"`
}

type TweetMention struct {
    Start    int    `json:"start"`
    End      int    `json:"end"`
    Username string `json:"username"`
    ID       string `json:"id,omitempty"`
}

type TweetAnnotation struct {
    Start          int     `json:"start"`
    End            int     `json:"end"`
    Probability    float64 `json:"probability,omitempty"`
    Type           string  `json:"type,omitempty"`
    NormalizedText string  `json:"normalized_text,omitempty"`
}

type ReferencedTweet struct {
    Type string `json:"type"` // "retweeted" | "quoted" | "replied_to"
    ID   string `json:"id"`
}
```

### D-9: sleep 関数の差し替え (テスト容易性)

`Client.sleep` (M6 で `func(ctx context.Context, d time.Duration) error`) を再利用。
likes.go では `c.sleep(ctx, wait)` を直接呼ぶ。

テストで `recordedSleeper()` (client_test.go の既存ヘルパ) を再利用し、ページ間 sleep の duration 系列を spy する。

### D-10: ページ間待機の判定順序

1. callback (`fn`) を呼ぶ
2. callback が error → return
3. `meta.next_token` が空文字列 → return nil (正常終了)
4. ページ数カウンタ +1 → max_pages に達した → return nil
5. rate-limit aware sleep (remaining <= 2 なら reset まで wait、それ以外なら 200ms)
6. 次ページの token を `pagination_token` に設定して loop 継続

## Implementation Plan

### Step 1: types.go 拡張 (Red → Green)

1.1 `types_test.go` に追加テスト (Red):
- `TestTweet_Unmarshal_WithExtendedFields`: entities / public_metrics / referenced_tweets を含む JSON が Tweet にデコードできる
- `TestIncludes_Unmarshal_WithTweets`: `includes.tweets` がデコードできる
- `TestTweetPublicMetrics_Unmarshal`: 各カウントが入る

1.2 `types.go` の Tweet を拡張 + 新型を追加 (Green)

### Step 2: likes.go シングルページ (Red → Green)

2.1 `likes_test.go` を作成 (Red):
- `TestListLikedTweets_HitsCorrectEndpoint`: GET /2/users/42/liked_tweets
- `TestListLikedTweets_Success`: 200 + 3 ツイートのデコード
- `TestListLikedTweets_UserIDPathEscape`: `42` がそのままパスに入る + 特殊文字 (例: スラッシュ含み) のエスケープ検証
- `TestListLikedTweets_QueryParams`: 全 Option がクエリに反映
- `TestListLikedTweets_StartTimeFormat_RFC3339`: `WithStartTime(2026-05-12 00:00:00 UTC)` → クエリ値 `start_time=2026-05-12T00:00:00Z` (RFC3339, ナノ秒なし) を文字列比較で明示検証 (advisor 指摘#4)
- `TestListLikedTweets_EndTimeFormat_RFC3339`: 同上 (end_time)
- `TestListLikedTweets_401`: ErrAuthentication
- `TestListLikedTweets_404`: ErrNotFound
- `TestListLikedTweets_ContextCanceled`: context.Canceled
- `TestListLikedTweets_InvalidJSON`: decode error + リトライしない

2.2 `likes.go` 実装 (Green):
- `LikedTweetsOption` / `likedTweetsConfig` / 各 With 関数
- `ListLikedTweets` (envelope decode + APIError 伝搬)
- 内部関数 `buildLikedTweetsURL(userID, cfg)` (CSV 結合 / URL escape)

### Step 3: EachLikedPage (Red → Green)

3.1 `likes_test.go` に追加テスト (Red):
- `TestEachLikedPage_MultiPage_FullTraversal`: 3 ページ × 2 件 → callback 3 回呼ばれる、計 6 件
- `TestEachLikedPage_MaxPages_Truncates`: max_pages=2 で 2 ページで終了
- `TestEachLikedPage_RateLimitSleep`: Remaining=1, Reset=5s 後 → sleep[0] ≈ 5s
- `TestEachLikedPage_InterPageDelay`: rate-limit 余裕がある場合 sleep[0] = 200ms
- `TestEachLikedPage_RateLimitStaleReset_FallsBackTo200ms`: Remaining=1, Reset=過去 → sleep[0] = 200ms (advisor 指摘#1)
- `TestEachLikedPage_CallbackError_Aborts`: callback で error 返却 → 即中断 (server call 1 回のみ)
- `TestEachLikedPage_ContextCanceled`: 初回前 cancel で context.Canceled
- `TestEachLikedPage_PaginationTokenForwarded`: 2 ページ目以降のリクエストに `pagination_token` が付く
- `TestEachLikedPage_429_RetryThenSuccess`: Client.Do の retry が効くこと
- `TestEachLikedPage_ReferencedTweetsExpansion_IntegratesIncludesTweets`: `WithExpansions("referenced_tweets.id")` 指定 + mock が `includes.tweets` を返す → callback で `resp.Includes.Tweets` が読める (advisor 指摘#3)

3.2 `likes.go` の `EachLikedPage` 実装 (Green):
- ループ + max_pages カウンタ + rate-limit aware sleep + ページ間最小待機
- callback への `*LikedTweetsResponse` 渡し
- next_token 空 or max_pages 到達で正常終了

### Step 4: Refactor

4.1 共通化:
- `buildLikedTweetsURL` を unexported に
- `decodeLikedTweetsResponse` を unexported に (envelope decode)
- 定数命名 `likesRateLimitMaxWait` etc.

4.2 doc コメント整備:
- 公開 API には日本語 doc コメント必須
- パッケージ doc は oauth1.go のみ (重複禁止)

### Step 5: 動作確認

```bash
go test -race -count=1 ./internal/xapi/...
go test -race -count=1 ./...
golangci-lint run ./...
go vet ./...
```

## Risks

| リスク | 緩和策 |
|---|---|
| EachLikedPage で 5xx/429 が enthusiastic に retry されて max_pages を超える | Client.Do の retry は単一ページ内のみ。max_pages はページ単位で適用されるため独立して機能する |
| rate-limit ヘッダの解釈ミス (秒精度) | `recordedSleeper()` で sleep duration を捕捉し、±1s 許容で検証 |
| `data` フィールドが省略されたレスポンス (0 件) | `Data []Tweet` の `omitempty` + `len(resp.Data) == 0` で扱う |
| Tweet.CreatedAt の型変更 (string → time.Time) | M8 では維持 (string)。CLI 層 (M11) で必要なら変換 |
| iter.Seq2 採用判断 | 採用せず callback。Future-proofing 不要 (CLI/MCP は callback で困らない) |
| max_pages 到達時のシグナル不足 | 戻り値で示さず正常終了。CLI 層で `cfg.maxPages` と実回数を比較したい場合は別途 metric 追加を検討 (M11 で再考) |
| Tweet 拡張による既存テスト (M7) の break | `omitempty` で M7 の最小 JSON に対するテストは影響なし |

## TDD チェックリスト

- [ ] Step 1.1 Red: types_test.go の追加テストが赤
- [ ] Step 1.2 Green: types.go 拡張で types_test.go 全 pass
- [ ] Step 2.1 Red: likes_test.go シングルページテストが赤 (likes.go 未作成)
- [ ] Step 2.2 Green: likes.go 実装でシングルページテスト全 pass
- [ ] Step 3.1 Red: EachLikedPage テストが赤
- [ ] Step 3.2 Green: EachLikedPage 実装で全 pass
- [ ] Step 4 Refactor: doc コメント整備、定数命名、lint 0 issues
- [ ] Step 5: `go test -race ./...` 全 pass、`golangci-lint run` 0 issues

## Completion Criteria

- [ ] `internal/xapi/types.go` に Tweet 拡張 + TweetPublicMetrics / TweetEntities / ReferencedTweet / Includes.Tweets 追加済み
- [ ] `internal/xapi/likes.go` に ListLikedTweets / EachLikedPage / 各 Option 関数が公開
- [ ] `internal/xapi/likes_test.go` で 15+ テストケース pass (シングルページ 8 + ページネーション 7)
- [ ] `internal/xapi/types_test.go` に 3 つの追加テスト pass
- [ ] `go test -race -count=1 ./...` 全件 pass
- [ ] `golangci-lint run ./...` 0 issues
- [ ] git commit (Conventional Commits 日本語 + Plan フッター)

## Next Milestone (M9 CLI `x me`) への引き継ぎ事項

- `ListLikedTweets` / `EachLikedPage` のシグネチャ確定 → CLI 層からそのまま呼べる
- LikedTweetsOption の型確定 → CLI フラグから option への変換パターン
- rate-limit aware sleep は xapi 層で完結 → CLI 層は max_pages フラグを `WithMaxPages` に渡すだけ
- `ExitCodeFor(err)` で 401/403/404 を 3/4/5 に写像する既存パターンを継続利用
