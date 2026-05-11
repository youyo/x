# M6 詳細計画: `internal/xapi/client.go` (retry + rate-limit aware HTTP client)

> Layer 2: M6 の実装詳細計画。Roadmap [plans/x-roadmap.md](./x-roadmap.md) Phase B、M5 (`oauth1.go`) の上に組む。
> スペック: [docs/specs/x-spec.md](../docs/specs/x-spec.md) §6 エラーハンドリング / §10 Rate-limit aware ページング

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M6 |
| タイトル | `internal/xapi/client.go` (retry + rate-limit aware HTTP client) |
| 前提マイルストーン | M1-M5 完了 (commits: e83b70d→17d1c4f) |
| 推定 LOC | 実装 ~260 行 / テスト ~340 行 |
| 推定テストケース | 14 ケース |
| 作成日 | 2026-05-12 |
| ステータス | 計画中 |

## 目的とスコープ

X API v2 への HTTP 通信における共通振る舞いを集約した **Client** 型を提供する。M5 (`NewHTTPClient`) で得られる署名済み `*http.Client` を内包し、その上に以下の責務を一段で載せる:

1. **リトライ**: 429 / 5xx で exponential backoff (base 2s, factor 2, max 30s, max 3 retry)
2. **Rate-limit ヘッダパース**: `x-rate-limit-remaining` / `x-rate-limit-reset` (UNIX 秒) / `x-rate-limit-limit` を構造化して返す
3. **エラー分類**: 401 → `ErrAuthentication` / 403 → `ErrPermission` / 404 → `ErrNotFound` / 429 (リトライ枯渇) → `ErrRateLimit`
4. **exit code マッピングヘルパ**: `ExitCodeFor(err) int` で M9+ の CLI 層が `internal/app` 定数に写像できる
5. **context キャンセル**: backoff 待機中も `ctx.Done()` で即座に中断

スコープ内:
- `internal/xapi/client.go` (Client / NewClient / Do / Response / RateLimitInfo / Option)
- `internal/xapi/errors.go` (APIError / 番兵エラー / ExitCodeFor)
- TDD: httptest + 注入可能 sleep / now で **テスト実行は秒オーダーにしない**

スコープ外:
- `users.go` (GetUserMe) / `likes.go` (ListLikedTweets) → M7-M8
- ページネーション iterator (rate-limit aware) → M8 で Client を組み込む
- exit code 数値そのものは `internal/app` パッケージで M1 確定済み (本マイルストーンでは循環依存を避けるため数値を直接埋め込む)

## ハンドオフ確認 (M5 → M6)

- `NewHTTPClient(ctx, creds) *http.Client` は **OAuth1 署名済み Transport を持つ Client** を返す。Client.Transport は `dghubble/oauth1` の `Transport` 型 (内部に `http.DefaultTransport` を持つ)。
- **Transport を上書きせず wrap する**: 上書きすると OAuth 署名が消える。本 M6 では Client.Transport は触らず、`xapi.Client.Do` 内で retry ループを構成する (Transport ラップは過剰設計)。
- パッケージ doc コメントは `internal/xapi/oauth1.go` の冒頭 1 箇所のみ。`client.go` / `errors.go` には書かない (golangci-lint v2 `revive: package-comments` 違反回避)。
- `internal/app/exit.go` に exit code 定数が既にある (`ExitAuthError=3` / `ExitPermissionError=4` / `ExitNotFoundError=5` / `ExitGenericError=1`)。**M6 は `internal/app` を import しない** (将来 `internal/app` が `internal/xapi` を import する可能性があり循環依存リスクのため)。代わりに数値を直接 `ExitCodeFor` 内に埋め込み、コメントで `internal/app` 定数と同期する旨を明示する。

## 設計判断

### D-1: Transport ラップではなく Client.Do 内 retry

選択肢:
- (A) `RoundTripper` をラップして retry を Transport 層で実装
- (B) `xapi.Client.Do(req) (*Response, error)` メソッド内で retry ループを書く

**採用: (B)**

理由:
- (A) は body の `io.ReadCloser` を retry のたびに再生成する必要があり (`req.GetBody` か `bytes.Buffer` キャッシュ)、X API の GET 主体ユースケースでは過剰設計。
- (B) なら `*Response` (xapi 独自型) を返せて `RateLimitInfo` を構造化できる。Transport は OAuth1 署名専用に保つ (責務分離)。
- M5 の `NewHTTPClient` を**そのまま内包**するだけで済む。Transport を再構築せず、署名挙動を絶対に壊さない。

### D-2: エラー分類は `errors.Is` でディスパッチ

`APIError` (具象) と `ErrAuthentication` などの番兵 (sentinel error) を二段構えにする:

```go
type APIError struct {
    StatusCode int
    Body       []byte
    Header     http.Header
    // 内部的に番兵エラーを wrap する。errors.Is で番兵照合可能。
}

func (e *APIError) Error() string { ... }
func (e *APIError) Unwrap() error { ... }   // 番兵を返す

var (
    ErrAuthentication = errors.New("xapi: authentication failed (401)")
    ErrPermission     = errors.New("xapi: permission denied (403)")
    ErrNotFound       = errors.New("xapi: not found (404)")
    ErrRateLimit      = errors.New("xapi: rate limit exhausted after retries")
)
```

- 呼び出し側は `errors.Is(err, xapi.ErrAuthentication)` で分類できる。
- 同時に `var apiErr *xapi.APIError; if errors.As(err, &apiErr) { use apiErr.Body }` でレスポンス本体・ヘッダにアクセス可能。
- 却下案: 番兵だけ (Body 取れない) / APIError だけ (`errors.Is` で簡潔に書けない) のどちらか単独 → 表現力不足。

### D-3: `time.Sleep` / `time.Now` をフィールド注入で差し替え可能にする

テストで本物の `time.Sleep(2s, 4s, 8s)` を待つと 14 秒以上かかる。プロダクトコードに副作用なくテストを高速化するため、`Client` 構造体に以下を持たせる:

```go
type Client struct {
    httpClient *http.Client
    baseURL    string
    sleep      func(context.Context, time.Duration) error // context-aware
    now        func() time.Time

    maxRetries  int
    baseBackoff time.Duration
    maxBackoff  time.Duration
    minPageWait time.Duration // §10 ページ間最小 200ms (M8 で利用)
}
```

公開 API ではなく**未公開フィールド**にし、`NewClient` のオプション関数経由でのみ差し替え可:

```go
type Option func(*Client)

func WithBaseURL(u string) Option                                  { ... }
func WithMaxRetries(n int) Option                                  { ... }
func WithBackoff(base, max time.Duration) Option                   { ... }
// withSleep / withNow は同パッケージ内テスト用に小文字で公開。
func withSleep(fn func(context.Context, time.Duration) error) Option { ... }
func withNow(fn func() time.Time) Option                             { ... }
```

- `sleep` のシグネチャを `func(context.Context, time.Duration) error` にして、本物実装は `time.Sleep` ではなく `select { case <-time.After(d): case <-ctx.Done(): return ctx.Err() }` にする → **context cancel が即座に効く**。
- テストでは `sleep` を no-op (`return nil`) に差し替えて即時返却。検証用に `recordedSleeps []time.Duration` を捕捉する spy も同パッケージテストで実装。
- 却下案: `interface Clock { Sleep / Now }` 形式 → モック作成コストが嵩む。関数値のほうが Go idiomatic で軽量。

### D-4: `Response` 型 (`*http.Response` 拡張)

```go
type Response struct {
    *http.Response          // 標準型を embed して http.Response の全機能を継承
    RateLimit RateLimitInfo // 構造化済みヘッダ情報
}

type RateLimitInfo struct {
    Limit     int       // x-rate-limit-limit (なければ 0)
    Remaining int       // x-rate-limit-remaining (なければ -1)
    Reset     time.Time // x-rate-limit-reset (UNIX 秒, なければ zero value)
    Raw       bool      // ヘッダが 1 つでも見つかった場合 true
}
```

- 0 / -1 の使い分け: `Remaining=0` は X API が「今は枯渇」と返した値、`Remaining=-1` は「ヘッダ自体が無い」を区別する。M8 (`--all` ページネーション) で `remaining <= 2` 判定するときに「未取得 / ヘッダ無し」を誤誘発しないため。
- `Raw=true` を見れば M8 側で「rate-limit aware モードを有効にできる」と判断可能。

### D-5: `Do` の retry 戦略 (擬似コード)

```go
func (c *Client) Do(req *http.Request) (*Response, error) {
    ctx := req.Context()
    for attempt := 0; attempt <= c.maxRetries; attempt++ {
        if err := ctx.Err(); err != nil {
            return nil, err
        }
        resp, err := c.httpClient.Do(req) // ネットワークエラーはリトライしない (今回のスコープ)
        if err != nil {
            return nil, err
        }
        rateInfo := parseRateLimit(resp.Header)
        switch {
        case resp.StatusCode == 429 || resp.StatusCode >= 500:
            if attempt == c.maxRetries {
                // 枯渇: 本体を読んで APIError + 番兵に変換
                return nil, c.toAPIError(resp, exhaustedSentinel(resp.StatusCode))
            }
            // 429 で x-rate-limit-reset があれば reset まで sleep、無ければ exp backoff
            wait := c.computeBackoff(attempt, resp.StatusCode, rateInfo)
            _ = resp.Body.Close() // リトライ前に body を破棄
            if err := c.sleep(ctx, wait); err != nil {
                return nil, err
            }
            continue
        case resp.StatusCode >= 400:
            return nil, c.toAPIError(resp, mapClientErr(resp.StatusCode))
        default:
            return &Response{Response: resp, RateLimit: rateInfo}, nil
        }
    }
    // 到達不能 (for ループ内で必ず return)
    return nil, errors.New("xapi: unreachable retry loop")
}
```

- `computeBackoff`:
  - `status == 429` かつ `rateInfo.Reset` が未来 → `min(reset - now, maxBackoff, 15min)` (§10: 最大 15 分まで待つ)
  - それ以外 → `min(base << attempt, maxBackoff)` = exp backoff
- `exhaustedSentinel(429) → ErrRateLimit`、`exhaustedSentinel(5xx) → nil` (5xx 枯渇は番兵を持たず APIError のみ。シェルスクリプト判定では exit code 1 = generic に倒す)
- `mapClientErr`:
  - 401 → ErrAuthentication
  - 403 → ErrPermission
  - 404 → ErrNotFound
  - その他 4xx → nil (APIError のみ)

### D-6: `ExitCodeFor(err error) int`

```go
func ExitCodeFor(err error) int {
    if err == nil {
        return 0 // app.ExitSuccess
    }
    switch {
    case errors.Is(err, ErrAuthentication):
        return 3 // app.ExitAuthError
    case errors.Is(err, ErrPermission):
        return 4 // app.ExitPermissionError
    case errors.Is(err, ErrNotFound):
        return 5 // app.ExitNotFoundError
    default:
        return 1 // app.ExitGenericError
    }
}
```

- `internal/app` を import せず、数値を直接埋め込む (循環依存予防)。
- コメントに「`internal/app.ExitAuthError` 等と同期せよ」と明示。M1 で値は固定化済み。

### D-7: `NewClient` の baseURL デフォルト

`https://api.x.com` (スペック §10 / §11 で確定)。`WithBaseURL` で httptest server URL に差し替え可能。**M6 では `baseURL` は構築・保持のみ**、リクエスト URL はテストで指定する (Do は呼び出し側が生成した `*http.Request` をそのまま使う)。

将来 M7+ で `c.Get("/2/users/me")` のような糖衣メソッドを追加するときに baseURL を使う想定。

### D-9: `NewClient` シグネチャはタスク指示の `(ctx, creds, opts...)` を採用

タスク指示と一致させ、呼び出し側 (M7+) を 1 行で完結させる:

```go
func NewClient(ctx context.Context, creds *config.Credentials, opts ...Option) *Client {
    hc := NewHTTPClient(ctx, creds)
    c := &Client{ httpClient: hc, ... }
    for _, opt := range opts { opt(c) }
    return c
}
```

httptest 経由のテストで OAuth 署名を経由させずに済ませたい場合は **未公開オプション `withHTTPClient`** で内部 `httpClient` を差し替える:

```go
// 同パッケージ内テスト専用 (小文字)
func withHTTPClient(hc *http.Client) Option {
    return func(c *Client) { c.httpClient = hc }
}
```

テストでは `xapi.NewClient(ctx, nil, xapi.withHTTPClient(srv.Client()), xapi.WithBaseURL(srv.URL), xapi.withSleep(spy))` で組む。`creds=nil` でも M5 のフォールバックで panic しないことは既に保証済み。

却下案: `NewClient(httpClient *http.Client, ...)` → タスク指示と不一致 + M7+ で `hc := NewHTTPClient(...)` を毎回書くボイラープレートが増える。

### D-8: GetBody の保証

`http.NewRequestWithContext` は body=nil もしくは `*bytes.Buffer` / `*bytes.Reader` / `*strings.Reader` のとき `req.GetBody` を自動設定する。X API の GET 主体ユースケースでは body=nil が大半なので問題ないが、将来 POST を扱うときに retry で body が空になるリスクがある。

**M6 の対応**: `Do` の retry 前に `req.GetBody != nil` ならそれを呼び出して body を巻き戻す。`req.GetBody == nil` かつ `req.Body != nil` の場合 (= 本来 retry できない) は **2 回目以降の試行で空 body になる** ことを許容する (X API では POST liked がないので実害なし)。コードコメントで明示。

```go
if attempt > 0 && req.GetBody != nil {
    body, err := req.GetBody()
    if err != nil {
        return nil, fmt.Errorf("xapi: rewind request body: %w", err)
    }
    req.Body = body
}
```

## 実装ファイル設計

### `internal/xapi/client.go` (~200 LOC)

```go
package xapi

// Client は X API v2 に対する HTTP リクエストを送出するための高レベルクライアントである。
// OAuth 1.0a 署名は内包する http.Client (NewHTTPClient 由来) が担う。
// Client 自身は retry / backoff / rate-limit ヘッダパース / エラー分類を担当する。
type Client struct {
    httpClient  *http.Client
    baseURL     string
    sleep       func(context.Context, time.Duration) error
    now         func() time.Time
    maxRetries  int
    baseBackoff time.Duration
    maxBackoff  time.Duration
}

// Response は xapi.Client.Do が返す HTTP レスポンスである。
// *http.Response を embed しつつ、X API のレートリミット情報を構造化して保持する。
type Response struct {
    *http.Response
    RateLimit RateLimitInfo
}

// RateLimitInfo は X API レスポンスヘッダ x-rate-limit-* の構造化された値である。
type RateLimitInfo struct {
    Limit     int
    Remaining int
    Reset     time.Time
    Raw       bool
}

// Option は NewClient の挙動を変更するための関数オプションである。
type Option func(*Client)

// WithBaseURL は X API ベース URL を上書きする (テスト用 httptest server 等)。
func WithBaseURL(u string) Option { ... }

// WithMaxRetries は 429/5xx 時の再試行回数上限を設定する。
func WithMaxRetries(n int) Option { ... }

// WithBackoff は exponential backoff の base / max を設定する。
func WithBackoff(base, max time.Duration) Option { ... }

// NewClient は ctx と config.Credentials から retry/rate-limit aware な xapi.Client を生成する。
// 内部で xapi.NewHTTPClient(ctx, creds) を呼び OAuth 1.0a 署名済みの *http.Client を組み立てる。
// creds=nil の場合の挙動は NewHTTPClient と同方針 (panic せず、X API 側で 401 を発生させる)。
func NewClient(ctx context.Context, creds *config.Credentials, opts ...Option) *Client {
    c := &Client{
        httpClient:  NewHTTPClient(ctx, creds),
        baseURL:     "https://api.x.com",
        sleep:       defaultSleep,
        now:         time.Now,
        maxRetries:  3,
        baseBackoff: 2 * time.Second,
        maxBackoff:  30 * time.Second,
    }
    for _, opt := range opts {
        opt(c)
    }
    return c
}

// BaseURL は構成済みの X API ベース URL を返す。
func (c *Client) BaseURL() string { return c.baseURL }

// Do は req を送信し、リトライポリシーに従って成功レスポンスまたは分類済みエラーを返す。
func (c *Client) Do(req *http.Request) (*Response, error) { ... }

// 以下未公開ヘルパ
func defaultSleep(ctx context.Context, d time.Duration) error { ... }
func parseRateLimit(h http.Header) RateLimitInfo { ... }
func (c *Client) computeBackoff(attempt int, status int, info RateLimitInfo) time.Duration { ... }
func mapClientErr(status int) error { ... }
func exhaustedSentinel(status int) error { ... } // 429 → ErrRateLimit / others → nil
func (c *Client) toAPIError(resp *http.Response, sentinel error) error { ... }

// 同パッケージテスト専用 (小文字)
func withSleep(fn func(context.Context, time.Duration) error) Option { ... }
func withNow(fn func() time.Time) Option { ... }
func withHTTPClient(hc *http.Client) Option { ... } // OAuth 署名を経由しないテスト用
```

### `internal/xapi/errors.go` (~60 LOC)

```go
package xapi

import (
    "errors"
    "fmt"
    "net/http"
)

// 番兵エラー: errors.Is で照合する。具体的なレスポンス情報は APIError から取得する。
var (
    ErrAuthentication = errors.New("xapi: authentication failed (401)")
    ErrPermission     = errors.New("xapi: permission denied (403)")
    ErrNotFound       = errors.New("xapi: not found (404)")
    ErrRateLimit      = errors.New("xapi: rate limit exhausted after retries")
)

// APIError は X API から返却された HTTP エラーレスポンスを構造化したエラーである。
// errors.As で取り出して Body / Header / StatusCode を参照できる。
// errors.Is で番兵エラー (ErrAuthentication 等) と照合できる。
type APIError struct {
    StatusCode int
    Body       []byte
    Header     http.Header
    sentinel   error // 401/403/404/429 のいずれか、それ以外は nil
}

func (e *APIError) Error() string {
    if e == nil {
        return "<nil APIError>"
    }
    return fmt.Sprintf("xapi: HTTP %d: %s", e.StatusCode, truncate(e.Body, 200))
}

func (e *APIError) Unwrap() error { return e.sentinel }

// ExitCodeFor は err を CLI の exit code (internal/app の定数値) に写像する。
//
// マッピング (internal/app/exit.go と一致させること):
//   nil               → 0 (ExitSuccess)
//   ErrAuthentication → 3 (ExitAuthError)
//   ErrPermission     → 4 (ExitPermissionError)
//   ErrNotFound       → 5 (ExitNotFoundError)
//   その他            → 1 (ExitGenericError)
//
// 循環依存回避のため internal/app は import せず、数値リテラルで定義する。
func ExitCodeFor(err error) int { ... }

func truncate(b []byte, max int) string { ... }
```

## テスト設計 (TDD)

### `internal/xapi/client_test.go` (~280 LOC)

httptest.Server を毎テストで立て、`xapi.NewHTTPClient` をスキップして `xapi.NewClient(srv.Client(), xapi.WithBaseURL(srv.URL), xapi.withSleep(noOpSleep))` で組み立てる (OAuth 署名は M5 で検証済みなので M6 では Authorization ヘッダ検証は重複させない)。

| # | ケース | 期待 |
|---|---|---|
| 1 | 200 OK → Response 返却 + RateLimit 構造化 (`x-rate-limit-remaining=42`, `x-rate-limit-limit=75`, `x-rate-limit-reset=<future unix>`) | `RateLimit.Remaining=42 / Limit=75 / Reset≈future / Raw=true` |
| 2 | 200 OK + ヘッダ無し | `RateLimit.Remaining=-1, Raw=false` |
| 3 | 401 → `ErrAuthentication` + `APIError.Body` 取得可能 | `errors.Is(err, ErrAuthentication)` true / `errors.As(err, &APIError)` で Body 一致 |
| 4 | 403 → `ErrPermission` | 同上 |
| 5 | 404 → `ErrNotFound` | 同上 |
| 6 | 429 連続 3 回 → 4 回目 200 (max_retries=3) | 成功、`recordedSleeps` 長さ=3 |
| 7 | 429 連続 4 回 (max_retries=3 で枯渇) → `ErrRateLimit` | `errors.Is(err, ErrRateLimit)` |
| 8 | 500 → 200 (1 retry) | 成功、sleep 1 回 |
| 9 | 500 連続枯渇 → APIError (番兵 nil) | `errors.Is(err, ErrAuthentication)` 等は全て false / `errors.As(err, &APIError)` true / StatusCode=500 |
| 10 | 429 with `x-rate-limit-reset=<now+5s>` → backoff = reset 差分 (capped maxBackoff)、exp backoff より優先 | `recordedSleeps[0] ≈ 5s` (now を固定) |
| 11 | 429 with reset がない → exp backoff (base=2s, factor=2) で `recordedSleeps` 系列 | `[2s, 4s, 8s]` (max=30s で cap、3 回目=8s で範囲内) |
| 12 | context.Cancel during sleep → 即返却 + ctx.Err() | sleep が `context.Canceled` を返し、Do がそれを返却。**このテストのみ no-op spy ではなく `defaultSleep` を使い、`time.After / ctx.Done()` select の本物挙動を検証する** |
| 13 | exp backoff が maxBackoff (30s) を超えない | `WithBackoff(2s, 10s)` で base 連続超過時に 10s で頭打ち |
| 14 | 400 (Bad Request: 番兵にマッピングされない) → APIError (sentinel=nil) | `errors.As(err, &APIError) && err.StatusCode==400` / 番兵 Is は全 false |

**sleep spy 実装** (同パッケージ `client_internal_test.go` ではなく、`withSleep` を小文字オプションで露出して `_test.go` から呼ぶ。`_test.go` ファイルがパッケージ `xapi` の場合のみアクセス可なので、`internal/xapi/client_test.go` は `package xapi` (internal test) にする):

```go
package xapi

// 注意: ↑ external test (xapi_test) ではなく internal test
//       withSleep / withNow にアクセスするため

func recordedSleeper() (func(context.Context, time.Duration) error, *[]time.Duration) {
    var d []time.Duration
    fn := func(ctx context.Context, t time.Duration) error {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        d = append(d, t)
        return nil
    }
    return fn, &d
}
```

M5 の `oauth1_test.go` は external test (`package xapi_test`) なので干渉しない。

### `internal/xapi/errors_test.go` (~120 LOC, external test `package xapi_test`)

| # | ケース | 期待 |
|---|---|---|
| 1 | `ExitCodeFor(nil) == 0` | OK |
| 2 | `ExitCodeFor(ErrAuthentication) == 3` | OK |
| 3 | `ExitCodeFor(fmt.Errorf("wrap: %w", ErrPermission)) == 4` | wrap でも判定可能 |
| 4 | `ExitCodeFor(ErrNotFound) == 5` | OK |
| 5 | `ExitCodeFor(ErrRateLimit) == 1` | RateLimit は generic 扱い (§6 の exit code 表に 429 は無いため) |
| 6 | `ExitCodeFor(errors.New("other")) == 1` | generic |
| 7 | `APIError.Error()` フォーマット検証 + body truncate | "xapi: HTTP 401: ..." の prefix を含む |
| 8 | `errors.As` で APIError 取り出し→ Body/Header アクセス | OK |

**§6 確認**: スペックの exit code 表は 0/1/2/3/4/5 のみ、429 は明示なし。RateLimit を 1 (generic) にする判断はスペックと矛盾しない。CLI 層が必要なら別途扱える (将来 M9+ で再検討)。

## リスクと緩和策

| リスク | 緩和策 |
|---|---|
| テストが exp backoff で実時間 sleep し遅くなる | `withSleep` で no-op spy 注入 (D-3) |
| context cancel が backoff 待機中に効かない | `defaultSleep` を `select { case <-time.After / case <-ctx.Done() }` で実装 (D-3) |
| Transport ラップで OAuth 署名が壊れる | Transport は触らず Do 内 retry に閉じる (D-1) |
| Body が retry で空になる | `req.GetBody` があれば巻き戻す。なければコメントで明示 (D-8) |
| internal/app 循環依存 | 数値リテラルで exit code 定義、コメント同期 (D-6) |
| 5xx 枯渇時のエラー表現が曖昧 | APIError のみで番兵なし。`errors.As` で StatusCode を見る (D-5) |
| `revive: package-comments` 違反 | client.go / errors.go にパッケージ doc を書かない (`oauth1.go` のみに集約) |
| Body close 忘れによる leak | retry 時は必ず `resp.Body.Close()`、エラー終了パスは **`toAPIError` 内で `io.ReadAll(resp.Body)` → `resp.Body.Close()` を完結**、成功時は呼び出し側責務 (Go 慣習) |
| ヘッダケースの揺れ (`X-Rate-Limit-*` vs `x-rate-limit-*`) | `http.Header.Get` がケース非依存なので問題なし |

## TDD 順序

1. **Red 1**: `errors_test.go` で `ExitCodeFor(nil)==0` / `Err* sentinel` の Is 判定をテスト → コンパイル失敗
2. **Green 1**: `errors.go` を最小実装 (sentinel + APIError 雛形 + ExitCodeFor)
3. **Red 2**: `client_test.go` で 200 OK + RateLimit パース → コンパイル失敗
4. **Green 2**: `client.go` の Client / NewClient / parseRateLimit / 単純 Do を実装
5. **Red 3**: 401/403/404 ケース追加 → fail
6. **Green 3**: `mapClientErr` + `toAPIError` 実装
7. **Red 4**: 429/5xx retry + sleep spy → fail
8. **Green 4**: retry ループと computeBackoff を実装
9. **Red 5**: 429 with reset header → fail
10. **Green 5**: computeBackoff に reset 優先ロジック追加
11. **Red 6**: context cancel during sleep → fail
12. **Green 6**: defaultSleep を select + ctx.Done() で実装
13. **Refactor**: 共通化、コメント整備、`make lint/test` 緑維持

## 検証コマンド

```bash
# 高速 (sleep no-op で完走)
go test -race -count=1 ./internal/xapi/...

# 全体回帰
go test -race -count=1 ./...

# 静的解析
golangci-lint run ./...
go vet ./...
```

期待:
- 全テスト pass (M1-M5 既存 50+ テスト + M6 22 新規テスト)
- lint 0 issues
- vet 0 警告

## 完了条件チェックリスト

- [ ] `Client` / `Response` / `RateLimitInfo` / `Option` 公開、すべて日本語 doc コメント
- [ ] `NewClient` が baseURL=`https://api.x.com` デフォルト、Options で上書き可
- [ ] `Do` が 200 系で `*Response` を返す
- [ ] 401/403/404 が番兵エラー + APIError として返る
- [ ] 429/5xx で max 3 retry、exp backoff (base 2s, max 30s)
- [ ] 429 with `x-rate-limit-reset` を見て reset まで sleep
- [ ] context cancel が sleep 中に効く
- [ ] `parseRateLimit` が remaining=-1 (ヘッダ無し) / 0 (枯渇) を区別
- [ ] `ExitCodeFor` が internal/app と一致する 0/1/3/4/5 を返す
- [ ] パッケージ doc コメントは `oauth1.go` のみ (client.go / errors.go には書かない)
- [ ] `go test -race -count=1 ./...` 全 pass
- [ ] `golangci-lint run ./...` 0 issues
- [ ] git commit: `feat(xapi): リトライ・レート制限対応 HTTP クライアントを追加` + `Plan: plans/x-m06-xapi-client.md` フッター

## 次マイルストーンへの引き継ぎ (M7+ 向け)

- M7 (`users.go`): `client.Do(req)` の戻り値から `Response.Body` を JSON unmarshal して DTO に変換。`baseURL + "/2/users/me"` を組み立てる糖衣メソッドはこの段階では未導入なので `url := c.BaseURL() + "/2/users/me"` で組む。
- M8 (`likes.go`): `Response.RateLimit.Remaining <= 2` で `time.Until(RateLimit.Reset)` 待機 (最大 15 分) を実装。ページ間最小 200ms は別フィールドで導入予定 (本 M6 では未実装、計画のみ確認)。
- CLI 層 (M9+): `xapi.ExitCodeFor(err)` を `os.Exit` 直前に呼ぶ。エラーメッセージは別途 `_meta.error.code` 形式 (§6) で組み立てる。
