# M19: authgate apikey モード 実装計画

## 概要

`X_MCP_AUTH=apikey` モードを実装する。Bearer token を共有シークレットと
`subtle.ConstantTimeCompare` で定数時間比較し、一致しない場合 401 を返す
HTTP middleware を追加する。

- **対象**: `internal/authgate/apikey.go` (新規) + `internal/authgate/gate.go` (修正)
- **スペック根拠**:
  - §11 `X_MCP_AUTH` / `X_MCP_API_KEY` (環境変数, apikey 時必須)
  - §10 セキュリティ: `subtle.ConstantTimeCompare` で shared secret 比較
- **roadmap**: M19 (Phase F)

## 設計方針

### 1. 環境変数読み込みの責務分離

**結論: authgate 層では環境変数を直読しない。CLI 層 (M24) から API キー文字列を Option 経由で渡す。**

理由:
- authgate パッケージは「認証ロジック」の責務に純化する (M16 の None と整合)
- env 直読は CLI 起動シーケンスで viper/cobra 統合の責務 (spec §11)
- テスタビリティ: テストで `os.Setenv` を使わずに済む

**具体策**: `func New(mode Mode, opts ...Option) (Middleware, error)` に拡張する。

```go
type Option func(*options)
type options struct {
    apiKey string
}
func WithAPIKey(key string) Option { return func(o *options) { o.apiKey = key } }
```

- `New(ModeAPIKey)` (Option なし) → `ErrAPIKeyMissing` を返す
- `New(ModeAPIKey, WithAPIKey(""))` → `ErrAPIKeyMissing` を返す
- `New(ModeAPIKey, WithAPIKey("secret"))` → `*APIKey` を返す

既存呼び出し (`New(ModeNone)` / `New(ModeIDProxy)` / `New(Mode(""))`) は variadic で
影響なし。後方互換性を維持する。

### 2. Bearer token 解釈

**結論: `Authorization: Bearer <token>` ヘッダのみを受理する。プレフィックスは大文字小文字を区別しない (case-insensitive)。**

理由:
- RFC 6750 §2.1 では auth-scheme は case-insensitive と規定 (`Bearer`/`bearer`/`BEARER` を許容)
- token 部分は完全一致 (case-sensitive)
- Authorization ヘッダ欠落、プレフィックス不一致、token 空、いずれも 401

実装:
```go
const bearerPrefix = "Bearer "
authz := r.Header.Get("Authorization")
if len(authz) < len(bearerPrefix) || !strings.EqualFold(authz[:len(bearerPrefix)], bearerPrefix) {
    // 401
}
token := authz[len(bearerPrefix):]
```

### 3. 定数時間比較

`subtle.ConstantTimeCompare([]byte(token), []byte(a.key))` を使う。

**重要**: `ConstantTimeCompare` は長さが異なるとそれだけで 0 を返す (長さ自体は漏洩する可能性あり)。
spec §10 は「shared secret は constant-time 比較」を要求しているのみで、長さ秘匿は要求していない。
そのまま使う。

戻り値が `1` のときのみ認証成功扱い。

### 4. 401 レスポンス

- ステータス: `http.StatusUnauthorized` (401)
- `WWW-Authenticate: Bearer realm="x-mcp"` ヘッダを付与 (RFC 6750 §3 準拠)
- body: シンプルな text/plain `"unauthorized\n"`
- ログ: 出力しない (token 自体は当然ログに出さない)

### 5. パッケージ doc 集約

`doc.go` を更新し、apikey が実装済みになった旨を反映する。
M19 以降は `Wrap` パッケージレベルドキュメントも apikey サンプルを追記。

## 公開 API 案

### `internal/authgate/apikey.go`

```go
package authgate

import (
    "crypto/subtle"
    "errors"
    "net/http"
    "strings"
)

// ErrAPIKeyMissing は ModeAPIKey で APIKey が指定されなかった場合に
// [New] が返すエラー。env 未設定や空文字を弾く。
var ErrAPIKeyMissing = errors.New("authgate: api key is required for apikey mode")

// APIKey は Bearer token を共有シークレットと subtle.ConstantTimeCompare で
// 定数時間比較する Middleware 実装。
type APIKey struct {
    key string
}

// NewAPIKey は与えられた key で APIKey middleware を生成する。
// key が空文字の場合 ErrAPIKeyMissing を返す。
func NewAPIKey(key string) (*APIKey, error) { ... }

// Wrap は Authorization: Bearer <token> を検証し、一致時のみ next に委譲する。
func (a *APIKey) Wrap(next http.Handler) http.Handler { ... }
```

### `internal/authgate/gate.go` の修正

```go
// Option は [New] の挙動を変更するオプション。
type Option func(*options)

// WithAPIKey は [ModeAPIKey] 用に共有シークレットを設定する。
func WithAPIKey(key string) Option { ... }

type options struct {
    apiKey string
}

// New は (Mode, Option...) で拡張。
func New(mode Mode, opts ...Option) (Middleware, error) {
    var o options
    for _, opt := range opts { opt(&o) }
    switch mode {
    case ModeNone:
        return &None{}, nil
    case ModeAPIKey:
        return NewAPIKey(o.apiKey)
    case ModeIDProxy:
        return nil, fmt.Errorf("%w: %q (not yet implemented)", ErrUnsupportedMode, string(mode))
    default:
        return nil, fmt.Errorf("%w: %q", ErrUnsupportedMode, string(mode))
    }
}
```

`ErrUnsupportedMode` は ModeAPIKey に対しては返さなくなる
(成功 or `ErrAPIKeyMissing`)。既存テスト
`TestNew_APIKey_ReturnsErrUnsupportedMode` は **更新する** (M19 で削除/書き換え)。

## TDD: テストケース (Red 段階)

### `internal/authgate/apikey_test.go` (新規)

| # | テスト名 | 入力 | 期待挙動 |
|---|---------|------|---------|
| 1 | `TestNewAPIKey_EmptyKey_ReturnsErrAPIKeyMissing` | `NewAPIKey("")` | エラー: `errors.Is(err, ErrAPIKeyMissing)`、middleware nil |
| 2 | `TestNewAPIKey_NonEmptyKey_Success` | `NewAPIKey("secret")` | エラーなし、`*APIKey` を返す |
| 3 | `TestAPIKey_Wrap_ValidBearer_PassesThrough` | Header `Authorization: Bearer secret` | inner handler が呼ばれ、200 が返る |
| 4 | `TestAPIKey_Wrap_InvalidBearer_Returns401` | Header `Authorization: Bearer wrong` | 401、inner 呼ばれない、`WWW-Authenticate` あり |
| 5 | `TestAPIKey_Wrap_MissingAuthHeader_Returns401` | Header なし | 401、inner 呼ばれない |
| 6 | `TestAPIKey_Wrap_MissingBearerPrefix_Returns401` | Header `Authorization: secret` (Bearer なし) | 401 |
| 7 | `TestAPIKey_Wrap_LowercaseBearer_PassesThrough` | Header `Authorization: bearer secret` | inner 呼ばれ、200 (case-insensitive prefix) |
| 8 | `TestAPIKey_Wrap_EmptyToken_Returns401` | Header `Authorization: Bearer ` | 401 |
| 9 | `TestAPIKey_Wrap_DifferentLength_Returns401` | secret="abc" / token="ab" | 401 (ConstantTimeCompare が長さ違いで 0) |
| 10 | `TestAPIKey_ImplementsMiddleware` | コンパイル時 | `var _ Middleware = (*APIKey)(nil)` |

### `internal/authgate/gate_test.go` (修正)

- 既存 `TestNew_APIKey_ReturnsErrUnsupportedMode` を **削除**
- 追加:
  - `TestNew_APIKey_NoOption_ReturnsErrAPIKeyMissing` (`New(ModeAPIKey)` → ErrAPIKeyMissing)
  - `TestNew_APIKey_WithAPIKey_Success` (`New(ModeAPIKey, WithAPIKey("k"))` → `*APIKey` 取得)
  - `TestNew_APIKey_WithEmptyAPIKey_ReturnsErrAPIKeyMissing` (`WithAPIKey("")`)

既存 `TestNew_None_ReturnsNoneMiddleware` 等は variadic 拡張で影響なし。

## 実装手順 (Red → Green → Refactor)

1. **Red**: 上記テストを書く (apikey_test.go 新規 + gate_test.go 修正)
   - `go test ./internal/authgate/...` がコンパイルエラーで失敗する状態
2. **Green 1**: `apikey.go` で `APIKey` / `NewAPIKey` / `ErrAPIKeyMissing` / `Wrap` を実装
3. **Green 2**: `gate.go` で `Option` / `WithAPIKey` を追加、`New` の switch case 更新
4. **Green 3**: `doc.go` を更新 (M19 反映)
5. **検証**: `go test -race -count=1 ./...` 全 pass
6. **Refactor**: lint 違反、godoc 表現、未使用 import 整理
7. **最終検証**:
   - `golangci-lint run ./...` 0 issues
   - `go vet ./...` 通過
   - `go build -o /tmp/x ./cmd/x` 成功

## リスクと対策

| リスク | 対策 |
|--------|------|
| `New` のシグネチャ変更で既存呼び出しが壊れる | variadic Option で既存 `New(Mode)` 呼び出しは無変更で通る |
| `subtle.ConstantTimeCompare` が長さ漏洩 | spec §10 は長さ秘匿を要求していない。そのまま使う |
| Bearer の大文字小文字 | RFC 6750 準拠で case-insensitive を採用 |
| token 内容を誤ってログ出力 | log 出力を一切しない方針 (middleware 内で fmt.Print 系を呼ばない) |
| `WWW-Authenticate` ヘッダ漏れ | RFC 6750 §3 準拠で `Bearer realm="x-mcp"` を 401 時に必ず付与 |

## 既存 doc コメントの更新 (重要)

実装時に **必ず** 以下の doc コメントを「実装済み」に更新する。
古い「M19 で実装する」表記が残ると将来の混乱の元となる。

- `gate.go:18-19` (ModeAPIKey 定数 doc): `"M19 で実装する"` → 削除 / 「実装済み」へ変更
- `gate.go:46-47` (New godoc): `"M16 では [ModeNone] のみ実装済み"` → `"M19 までで [ModeNone] / [ModeAPIKey] を実装済み"` 等に更新
- `doc.go:15-17` (パッケージ doc): `"本マイルストーン (M16) では [ModeNone] のみを実装し、apikey / idproxy は後続マイルストーン (M19 / M20) で追加する"` → apikey 完了を反映
- `doc.go:23` (使用例): apikey 用の使用例も追記 (`authgate.New(authgate.ModeAPIKey, authgate.WithAPIKey(apiKey))`)

## 既存呼び出し確認 (variadic 互換性)

`grep -rn "authgate.New(" --include='*.go'` で確認 (実装前にも再実行):

- `internal/authgate/gate_test.go` のみ呼び出し元 (テスト内で 5 箇所)
- `internal/authgate/doc.go` の使用例コメント
- transport/http など他パッケージからの呼び出しは **無し**

→ variadic 拡張は完全に source-compatible で、テスト変更のみで足りる。

## RFC 6750 ヘッダ方針の確定

- `WWW-Authenticate` レスポンスは **常に `Bearer realm="x-mcp"` 固定** とする
- `error="invalid_token"` 等の細分化は **行わない** (token 有無の漏洩防止 + 実装単純化)
- 401 レスポンスはすべてのケース (Authorization 欠落 / プレフィックス違い / token 不一致 / 空 token) で同じ
- body も同じ `"unauthorized\n"` (text/plain)

## 完了条件

- [x] `internal/authgate/apikey.go` 実装
- [x] `internal/authgate/apikey_test.go` 10 ケース pass
- [x] `internal/authgate/gate.go` Option パターン拡張、ModeAPIKey 分岐実装、godoc 更新
- [x] `internal/authgate/gate_test.go` ModeAPIKey 系テスト更新 (3 ケース)
- [x] `internal/authgate/doc.go` apikey 実装済み旨を追記、使用例追加
- [x] `go test -race -count=1 ./...` 全 pass
- [x] `golangci-lint run ./...` 0 issues
- [x] `go vet ./...` 通過
- [x] `go build -o /tmp/x ./cmd/x` 成功
- [x] 公開シンボル全てに日本語 doc コメント
- [x] パッケージ doc は `doc.go` 1 ファイル集約 (apikey.go の冒頭にパッケージコメントを書かない)

## 次マイルストーンへの引き継ぎ

- **M20 (idproxy memory store)**:
  - `New(mode, opts...)` の Option パターンが既に確立済み → idproxy 用 `WithIDProxyConfig(...)` を追加するだけ
  - `WWW-Authenticate` ヘッダ付与パターンを idproxy でも踏襲可能 (`Bearer error="invalid_token"` 等)
  - パッケージ doc 集約 (doc.go 1 ファイル) 維持
