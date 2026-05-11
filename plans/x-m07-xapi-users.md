# M07 詳細計画: `internal/xapi/users.go` — GetUserMe + DTO

> Layer 2: M7 マイルストーンの詳細実装計画。
> 親ロードマップ: [plans/x-roadmap.md](./x-roadmap.md)
> スペック: [docs/specs/x-spec.md](../docs/specs/x-spec.md) §6 MCP `get_user_me` 出力スキーマ

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M7 |
| 範囲 | `internal/xapi/types.go` (共通 DTO) + `internal/xapi/users.go` (`GetUserMe` + Option) |
| 前提 | M6 (Client) commit `93b8558` |
| 完了条件 | `httptest` mock で `/2/users/me` 200/401/404 + `user.fields` クエリ + context cancel が pass、`go test -race ./...` 全件 pass、`golangci-lint run` 0 issues |
| TDD | Red → Green → Refactor 厳守 |

## Scope

### In Scope

1. **`internal/xapi/types.go`** (新規)
   - X API v2 の共通 DTO を**最小**で先行配置 (M8 likes が同ファイルを拡張する)
   - `User`, `Tweet`, `Meta`, `Includes`, `ErrorResponse`, `APIErrorPayload`, `UserPublicMetrics`
   - `TweetPublicMetrics` は M8 で追加する (User と Tweet で field が異なるため命名を分離する)
   - 全フィールドに `json:"..."` タグ
   - **パッケージ doc コメントは書かない** (oauth1.go に既に存在)

2. **`internal/xapi/users.go`** (新規)
   - `GetUserMe(ctx context.Context, opts ...UserFieldsOption) (*User, error)`
   - `type UserFieldsOption func(*url.Values)`
   - `func WithUserFields(fields ...string) UserFieldsOption`
   - **パッケージ doc コメントは書かない**

3. **`internal/xapi/users_test.go`** (新規)
   - 200 success → `User` decode
   - 401 → `ErrAuthentication`
   - 404 → `ErrNotFound`
   - context cancel → `context.Canceled`
   - `WithUserFields("username","name","verified","public_metrics")` → URL クエリに `user.fields=username,name,verified,public_metrics`
   - 不正 JSON → 明示エラー (リトライしない)
   - `WithUserFields` を複数回呼んだ場合の挙動 (フィールド merge or 最後勝ち)

4. **`internal/xapi/types_test.go`** (新規)
   - `User` の JSON unmarshal 検証 (X API v2 の典型レスポンス)
   - `ErrorResponse` の unmarshal 検証

### Out of Scope (M8 以降)

- `Tweet.Entities` の詳細構造 → M8
- `Tweet.PublicMetrics` の `retweet_count` 等の詳細 → M8 で型確定 (M7 は最小)
- ページネーション (`Meta.NextToken` を活用したループ) → M8
- ErrorResponse のレスポンス Body 自動パース (`APIError.Body` の `[]byte` を呼び出し側で `json.Unmarshal` する想定で M7 はパース実装しない)

## Design Decisions

### D-1: DTO は **`types.go` 単一ファイル**に集約

スペック §5 architecture に `types.go — Tweet / User / Meta DTO` と明示されている。M7 では `User` + 最小 `Tweet` + `Meta` + `Includes` + `ErrorResponse` を一括導入。M8 で `Tweet` 内部を拡張する際も同ファイルに追記する。

**根拠**: パッケージ内 DTO の発見可能性、`go doc internal/xapi` の見やすさ、M8 マイルストーンが同ファイルを編集する前提。

### D-2: `GetUserMe` の戻り値は **`*User`** (`*UserResponse` でラップしない)

X API v2 のレスポンスは `{ "data": { ... } }` 形式だが、`/2/users/me` は **単一 User オブジェクト**しか返さない (配列ではない)。`get_user_me` MCP tool 出力は `{ "user_id", "username", "name" }` で `data` ラッパーは無い。

→ 内部レスポンス型 `userMeEnvelope { Data User }` を未公開で定義し、`GetUserMe` は `*User` のみ返す。`Includes`/`Meta` は `/2/users/me` には付かないため返さない。

**却下**: `(User, *Meta, error)` (Meta は never-set のため API ノイズ) / `*UserResponse{Data, Errors}` (`Errors` は `APIError.Body` 経由で取れる)

### D-3: `UserFieldsOption` は **`func(*url.Values)` の関数型**

理由:
- M8 likes でも `WithTweetFields`/`WithExpansions`/`WithUserFields`/`WithMaxResults` 等のオプションが必要 → **同じ Option パターンを共有**できる
- `url.Values.Set` / `Add` の薄いラッパーで実装が簡潔
- テスト時に option を単独で `url.Values` に適用して検査可能

```go
type UserFieldsOption func(*url.Values)
func WithUserFields(fields ...string) UserFieldsOption {
    return func(v *url.Values) {
        if len(fields) > 0 {
            v.Set("user.fields", strings.Join(fields, ","))
        }
    }
}
```

複数回呼ばれた場合: `Set` で**最後勝ち**。同じキーを別オプションが書く可能性は M7 範囲では無いが、API として明示する。

### D-4: M8 likes との option 共用は **M8 で再分離**

M7 で `UserFieldsOption` という名前にしておくが、M8 で `WithUserFields` が `/2/users/:id/liked_tweets` でも使われるため**型名を `RequestOption`** にリネームする予定。M7 で先取りリネームしない理由は YAGNI (M7 の API 表面を最小化)。

→ M8 で `type RequestOption = UserFieldsOption` の type alias を入れて段階移行する想定。本 M7 ではこのリスクをハンドオフに明記する。

### D-5: エンドポイント URL は `client.BaseURL() + "/2/users/me"` で組み立て

`net/url.URL{...}` でビルドするより `http.NewRequestWithContext(ctx, GET, baseURL + path + "?" + values.Encode(), nil)` の方が簡潔。

ただし `baseURL` が `?` を含む可能性は無いので問題なし。`values.Encode()` が空文字列の場合は `?` を付けない条件分岐を入れる (空クエリ `/path?` は X API が許容するか不明)。

### D-6: パッケージ doc コメントは `oauth1.go` のみ

M6 ハンドオフ準拠。`users.go` / `types.go` の冒頭は `package xapi` のみで doc コメントなし。違反すると golangci-lint v2 の `revive` が `package-comments` で警告するため、`oauth1.go` 以外には**書かない**。

### D-7: `safeCredentials` 依存無し / DTO に `time.Time` を使う

- `User.CreatedAt` (オプションフィールド) は `time.Time` で json タグ + `,omitempty`
- X API v2 は ISO 8601 (`"2026-05-12T12:00:00.000Z"`) を返す → `time.Time` の標準 JSON unmarshaler で対応可

ただし**ナノ秒精度を含む RFC3339 ライク**な形式の場合 `time.Time` 標準 unmarshaler だと失敗する可能性がある。M7 では `CreatedAt` を入れず、M8 で必要なら `Tweet.CreatedAt` 共々検証する。

→ **M7 の `User` は `CreatedAt` フィールドを含めない** (フィールド最小化)。

### D-8: `User` の最小フィールドセット

スペック §6 `get_user_me` 出力スキーマ:
```json
{ "user_id": "...", "username": "...", "name": "..." }
```

CLI/MCP 出力で必要なのは `id` / `username` / `name`。それ以外は X API v2 の `user.fields` で有効化されるオプショナルフィールド:
- `verified` (bool)
- `public_metrics.followers_count` 等
- `description`, `profile_image_url`, `protected`, ...

→ M7 は次の構成を採用 (最小実装 + よく使われる field):

```go
type User struct {
    ID            string             `json:"id"`
    Username      string             `json:"username"`
    Name          string             `json:"name"`
    Verified      bool               `json:"verified,omitempty"`
    Description   string             `json:"description,omitempty"`
    PublicMetrics *UserPublicMetrics `json:"public_metrics,omitempty"`
}

type UserPublicMetrics struct {
    FollowersCount int `json:"followers_count"`
    FollowingCount int `json:"following_count"`
    TweetCount     int `json:"tweet_count"`
    ListedCount    int `json:"listed_count"`
}
```

`Verified` / `Description` / `PublicMetrics` は `user.fields` 指定時のみ X API から返却される → `omitempty` で MCP/CLI 出力時に省略。
**型名は `UserPublicMetrics`** とし、M8 で `Tweet.PublicMetrics` を別型 `TweetPublicMetrics` として追加する (advisor 指摘: User と Tweet で field が異なるため命名を分離する)。

### D-9: `Tweet` は M7 で最小定義

M8 で詳細化する前提だが、`types.go` の責務分離のため最低限の枠は M7 で定義する:

```go
type Tweet struct {
    ID        string              `json:"id"`
    Text      string              `json:"text"`
    AuthorID  string              `json:"author_id,omitempty"`
    CreatedAt string              `json:"created_at,omitempty"` // 文字列のまま (M8 で time.Time 化検討)
}

type Meta struct {
    ResultCount int    `json:"result_count,omitempty"`
    NextToken   string `json:"next_token,omitempty"`
}

type Includes struct {
    Users []User `json:"users,omitempty"`
}
```

`CreatedAt` を `string` のままにする理由は D-7 と同じ (M8 で要検討の判断を遅延)。

### D-10: `ErrorResponse` の構造

X API v2 のエラーレスポンス標準形式:

```json
{
  "title": "Unauthorized",
  "detail": "Unauthorized",
  "type": "about:blank",
  "status": 401,
  "errors": [{ "message": "...", "parameters": {} }]
}
```

→

```go
type ErrorResponse struct {
    Title  string            `json:"title,omitempty"`
    Detail string            `json:"detail,omitempty"`
    Type   string            `json:"type,omitempty"`
    Status int               `json:"status,omitempty"`
    Errors []APIErrorPayload `json:"errors,omitempty"`
}

type APIErrorPayload struct {
    Message    string                 `json:"message,omitempty"`
    Parameters map[string]any         `json:"parameters,omitempty"`
}
```

これは `APIError.Body` を呼び出し側で `json.Unmarshal` する際の型として M7 で提供する。本 M7 では `GetUserMe` 内部では使わない (M6 の番兵分類で十分)。

## Implementation Steps (TDD)

### Step 1: Red — `types_test.go` で DTO の JSON 形式を確定 ✅ Test First

新規ファイル `internal/xapi/types_test.go`:

```go
package xapi

import (
    "encoding/json"
    "testing"
)

// TestUser_Unmarshal は X API v2 /2/users/me の典型レスポンス JSON が
// User 構造体に正しくデコードされることを確認する。
func TestUser_Unmarshal(t *testing.T) { ... }

// TestUser_Unmarshal_WithFields は user.fields=verified,public_metrics 指定時の
// オプショナルフィールドが正しくデコードされることを確認する。
func TestUser_Unmarshal_WithFields(t *testing.T) { ... }

// TestErrorResponse_Unmarshal は X API v2 のエラーレスポンス JSON が
// ErrorResponse 構造体にデコードされることを確認する。
func TestErrorResponse_Unmarshal(t *testing.T) { ... }
```

→ コンパイルエラー (User / ErrorResponse 未定義) で **Red 確認**。

### Step 2: Green — `types.go` を実装

`internal/xapi/types.go` を新規作成:

```go
package xapi

// User は X API v2 のユーザーオブジェクトを表す DTO である。... (日本語 doc)
type User struct { ... }

type PublicMetrics struct { ... }
type Tweet struct { ... }
type Meta struct { ... }
type Includes struct { ... }
type ErrorResponse struct { ... }
type APIErrorPayload struct { ... }
```

→ `go test ./internal/xapi/ -run TestUser_ -race` が pass で **Green 確認**。

### Step 3: Red — `users_test.go` で `GetUserMe` の振る舞いを確定

新規ファイル `internal/xapi/users_test.go`:

```go
package xapi

// TestGetUserMe_Success は 200 OK + 正常 JSON で User が返ることを確認する。
func TestGetUserMe_Success(t *testing.T) { ... }

// TestGetUserMe_WithUserFields_AppendsQueryParam は
// WithUserFields(...) が ?user.fields=... に変換されることを確認する。
func TestGetUserMe_WithUserFields_AppendsQueryParam(t *testing.T) { ... }

// TestGetUserMe_401_AuthError は 401 で ErrAuthentication が返ることを確認する。
func TestGetUserMe_401_AuthError(t *testing.T) { ... }

// TestGetUserMe_404_NotFound は 404 で ErrNotFound が返ることを確認する。
func TestGetUserMe_404_NotFound(t *testing.T) { ... }

// TestGetUserMe_ContextCanceled は context.Cancel 後に呼ぶと
// context.Canceled が返ることを確認する。
func TestGetUserMe_ContextCanceled(t *testing.T) { ... }

// TestGetUserMe_InvalidJSON は 200 だが本体が壊れた JSON のとき
// 明示的なエラーが返り、リトライしないことを確認する。
func TestGetUserMe_InvalidJSON(t *testing.T) { ... }

// TestGetUserMe_HitsCorrectEndpoint は HTTP リクエストの Path が "/2/users/me" であることを確認する。
func TestGetUserMe_HitsCorrectEndpoint(t *testing.T) { ... }
```

→ コンパイルエラー (`GetUserMe` 未定義) で **Red 確認**。

### Step 4: Green — `users.go` を実装

```go
package xapi

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
    "strings"
)

// UserFieldsOption は GetUserMe のクエリパラメータを設定する関数オプションである。
type UserFieldsOption func(*url.Values)

// WithUserFields は X API v2 の `user.fields` クエリパラメータを設定する。
// 例: WithUserFields("username","name","verified","public_metrics")
// 同じキーで複数回呼ばれた場合は最後の呼び出しが勝つ。
// 空引数の場合は何もしない。
func WithUserFields(fields ...string) UserFieldsOption {
    return func(v *url.Values) {
        if len(fields) == 0 {
            return
        }
        v.Set("user.fields", strings.Join(fields, ","))
    }
}

// GetUserMe は X API v2 GET /2/users/me を呼び出して認証ユーザー情報を取得する。
//
// opts でクエリパラメータをカスタマイズできる (例: WithUserFields)。
// エラー時は番兵エラー (ErrAuthentication / ErrNotFound 等) を errors.Is で判別可能。
// 詳細は APIError を errors.As で取得する。
func (c *Client) GetUserMe(ctx context.Context, opts ...UserFieldsOption) (*User, error) {
    values := url.Values{}
    for _, opt := range opts {
        opt(&values)
    }
    endpoint := c.BaseURL() + "/2/users/me"
    if q := values.Encode(); q != "" {
        endpoint += "?" + q
    }
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
    if err != nil {
        return nil, fmt.Errorf("xapi: build GetUserMe request: %w", err)
    }
    resp, err := c.Do(req)
    if err != nil {
        return nil, err
    }
    defer func() { _ = resp.Body.Close() }()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("xapi: read GetUserMe response: %w", err)
    }
    var env struct {
        Data User `json:"data"`
    }
    if err := json.Unmarshal(body, &env); err != nil {
        return nil, fmt.Errorf("xapi: decode GetUserMe response: %w", err)
    }
    return &env.Data, nil
}
```

→ `go test -race ./...` 全 pass で **Green 確認**。

### Step 5: Refactor

- doc コメントの磨き上げ
- 同パッケージ内既存コードとのスタイル統一 (errorf メッセージ prefix `xapi:`)
- `internal/xapi/users.go` の package コメントが無いことを再確認
- `golangci-lint run` 0 issues 確認

## Test Plan (詳細)

| # | テスト | mock 応答 | 期待 |
|---|---|---|---|
| T-1 | `TestUser_Unmarshal` | `{"data":{"id":"1","username":"u","name":"N"}}` の `data` 部分のみ | User.ID/Username/Name 検証 |
| T-2 | `TestUser_Unmarshal_WithFields` | `{"id":"1","username":"u","name":"N","verified":true,"public_metrics":{"followers_count":10,...}}` | Verified/PublicMetrics 検証 |
| T-3 | `TestErrorResponse_Unmarshal` | `{"title":"Unauthorized","detail":"...","status":401,"errors":[...]}` | Title/Status/Errors 検証 |
| T-4 | `TestGetUserMe_Success` | 200 + `{"data":{"id":"42","username":"alice","name":"Alice"}}` | `User{ID:"42",...}` |
| T-5 | `TestGetUserMe_WithUserFields_AppendsQueryParam` | 200 + 任意 + クエリ検証 | `r.URL.Query().Get("user.fields") == "username,name,verified,public_metrics"` |
| T-6 | `TestGetUserMe_401_AuthError` | 401 | `errors.Is(err, ErrAuthentication) == true` |
| T-7 | `TestGetUserMe_404_NotFound` | 404 | `errors.Is(err, ErrNotFound) == true` |
| T-8 | `TestGetUserMe_ContextCanceled` | ハンドラ呼ばれない (cancel 先行) | `errors.Is(err, context.Canceled) == true` |
| T-9 | `TestGetUserMe_InvalidJSON` | 200 + `{"data": "not-an-object"}` (型不一致で確実に Decode 失敗) | エラー、errors.Is に番兵マッチしない、リトライしない (server call 1 回)。Body 全読 → `json.Unmarshal` で意図を明確化 |
| T-10 | `TestGetUserMe_HitsCorrectEndpoint` | 200 + path 検証 | `r.URL.Path == "/2/users/me"` |

すべて `httptest.NewServer` + `newTestClient` ヘルパ (client_test.go の `withHTTPClient/withSleep` を活用) を再利用。**users_test.go は `package xapi` (internal test)** にして `newTestClient` を共有する。

## Risks

| リスク | 影響 | 緩和 |
|---|---|---|
| X API レスポンス形式の変更 | 中 | 最小フィールドのみ採用、`omitempty` で柔軟化、M8 で追補可 |
| `user.fields` デフォルト値の取り扱い | 低 | M7 ではデフォルト無し (呼び出し側が明示) |
| `Tweet.CreatedAt` の time.Time 化判断遅延 | 低 | 文字列のまま M7 確定、M8 で型化検証 |
| M8 で `UserFieldsOption` → `RequestOption` リネームが必要 | 中 | ハンドオフに明記、M8 で type alias 経由で段階移行 |
| `Decoder.Decode` だと不完全 JSON でも成功する可能性 | 低 | `io.ReadAll` + `json.Unmarshal` で全体パース、テスト T-9 は型不一致レスポンスで確実に失敗させる (advisor 指摘) |
| `package xapi` doc コメントを `users.go` / `types.go` に書く違反 | 低 | レビューチェックリスト + golangci-lint `revive package-comments` |

## Acceptance Criteria

- [x] `plans/x-m07-xapi-users.md` 作成
- [ ] `internal/xapi/types.go` 新規 (DTO 一式、json タグ、日本語 doc コメント)
- [ ] `internal/xapi/users.go` 新規 (GetUserMe + UserFieldsOption + WithUserFields)
- [ ] `internal/xapi/types_test.go` 新規 (T-1 〜 T-3)
- [ ] `internal/xapi/users_test.go` 新規 (T-4 〜 T-10)
- [ ] `go test -race -count=1 ./...` 全 pass (既存 70+ テスト + 新規 10 テスト)
- [ ] `golangci-lint run ./...` 0 issues
- [ ] `go vet ./...` clean
- [ ] commit: `feat(xapi): GetUserMe と User/Tweet/Meta DTO を追加` (フッターに `Plan: plans/x-m07-xapi-users.md`)

## Handoff to M8

- **types.go の Tweet 拡張**: `Entities` (mentions/urls/hashtags)、`TweetPublicMetrics` (retweet_count 等)、`CreatedAt` の `time.Time` 化判断
- **option 名のリネーム**: `UserFieldsOption` → `RequestOption` (type alias で段階移行)、`WithTweetFields` / `WithExpansions` / `WithMaxResults` / `WithPaginationToken` / `WithStartTime` / `WithEndTime` を追加
- **Meta.NextToken のページネーション利用**: `ListLikedTweets` で next_token を辿るループ
- **エンドポイント URL 組み立てパターン**: `BaseURL + "/2/users/:id/liked_tweets"` の `:id` 置換
