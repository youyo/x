# M20: authgate idproxy 基盤 + memory store 実装計画

## 概要

`X_MCP_AUTH=idproxy` モードを実装する。OIDC ベースの session 認証 middleware を
`github.com/youyo/idproxy` ライブラリで構築し、デフォルト store backend として
`github.com/youyo/idproxy/store` の `MemoryStore` を採用する。M21–M23 で
sqlite / redis / dynamodb の追加 store backend を順次差し込めるよう、
authgate パッケージ内に薄い factory レイヤーを設けるが、本マイルストーン (M20) では
memory のみを実装する。

- **対象**:
  - `internal/authgate/idproxy.go` (新規)
  - `internal/authgate/idproxy_test.go` (新規)
  - `internal/authgate/store_memory.go` (新規, 薄ラッパー)
  - `internal/authgate/store_memory_test.go` (新規, interface 適合性テスト)
  - `internal/authgate/gate.go` (修正: ModeIDProxy 分岐 + Option 関数群追加)
  - `internal/authgate/gate_test.go` (修正: ModeIDProxy ケース更新 + 新規ケース追加)
  - `internal/authgate/doc.go` (修正: idproxy 実装済み旨を反映)
  - `go.mod` (`github.com/youyo/idproxy` v0.4.2 以上を追加)
- **スペック根拠**:
  - §5 architecture: `internal/authgate/idproxy.go` に `idproxy.New + Wrap`、`store_memory.go` に memory store
  - §10 必須技術スタック: `github.com/youyo/idproxy` v0.4.2 以上
  - §11 環境変数: `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` / `COOKIE_SECRET` / `EXTERNAL_URL` / `STORE_BACKEND` (default=memory)
- **roadmap**: M20 (Phase F)

## 設計方針

### 1. OAuth 2.1 AS は M20 のスコープ外 (cookie flow のみ)

**結論: 本マイルストーンでは `idproxy.Config.OAuth = nil` とし、ブラウザ cookie
セッションフローのみをサポートする。Bearer JWT 検証 / OAuth 2.1 AS エンドポイントは
後続マイルストーン (M24 CLI 統合 or 別途) で必要になった時点で追加する。**

理由:
- M20 完了条件は「Google OIDC ログイン → セッション発行 → MCP tools 呼び出し成功」。
  cookie ベースの session 認証で十分に満たせる
- spec §11 の環境変数一覧に **`SIGNING_KEY` 系が存在しない** → spec 初版は OAuth AS を
  陽に有効化しない設計と読める
- OAuth AS を有効化すると ECDSA P-256 秘密鍵管理が必要になり、Option 設計と
  シークレットライフサイクルが肥大化する。M20 は memory store の確立に集中する
- 将来 OAuth AS が必要になった場合は `WithOAuthSigningKey(crypto.Signer)` 等を追加
  するだけで idproxy.Config.OAuth を組み立てられる構造を維持する

ドキュメント (doc.go) に「現状は browser cookie flow のみ。Bearer JWT / OAuth AS は
未対応」と明記する。

### 2. 環境変数読み込みの責務分離 (M19 と同じ)

**結論: authgate 層では環境変数を一切直読しない。CLI 層 (M24) が `OIDC_ISSUER` 等を
viper/cobra で読み、`WithOIDCIssuer(...)` 等の Option として渡す。**

`internal/authgate/gate.go` に追加する Option:

| Option | 用途 | 必須 |
|--------|------|------|
| `WithOIDCIssuer(string)` | カンマ区切りの OIDC Issuer 文字列 (spec §11 で csv 許容) | ✅ |
| `WithOIDCClientID(string)` | カンマ区切りの OIDC ClientID | ✅ |
| `WithOIDCClientSecret(string)` | カンマ区切りの OIDC ClientSecret (idproxy が provider ごとに必須化) | ✅ |
| `WithCookieSecret(string)` | hex エンコードされた Cookie Secret (32 バイト以上に decode できること) | ✅ |
| `WithExternalURL(string)` | idproxy の外部 URL (`https://` または `http://localhost`) | ✅ |
| `WithIDProxyStore(idproxy.Store)` | 任意の Store 実装 (M21–M23 用) | 任意 (default: memory) |
| `WithIDProxyPathPrefix(string)` | idproxy のパスプレフィックス (`/auth` 等) | 任意 (default: 空文字 = ルート直下、idproxy 既定) |

**スコープ外**: `AllowedDomains` / `AllowedEmails` は spec §11 の環境変数一覧に存在せず、M20
のタスク記述にも含まれない。本マイルストーンでは公開 Option として **追加しない**。idproxy.Config
側でも空スライスのままで「制限なし」となる挙動を維持する。将来 spec に env が追加された時点で
M24 もしくは別マイルストーンで `WithIDProxyAllowedDomains` / `...AllowedEmails` を追加する。

`options` 構造体は M19 ですでに `apiKey string` を持っているため、idproxy 用フィールドを
追加する形にする (パッケージ内部の単一構造体で集約)。

### 3. CSV pairing セマンティクス

**spec §11 は `OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` を CSV 許容と
記載するのみで、複数値の対応関係を定義していない。本実装で以下のルールを確定する:**

| 規則 | 内容 |
|------|------|
| Pairing | Issuer / ClientID / ClientSecret は **同じインデックス位置同士でペア** を成す |
| 長さ一致 | 3 CSV のエントリ数は **完全一致** が必須。不一致時は `ErrIDProxyProvidersMismatch` |
| Trim | 各エントリ前後のホワイトスペースは strip する |
| Empty entries | trim 後に空のエントリが含まれる場合は `ErrIDProxyConfigInvalid` |
| 件数 | 最低 1 エントリ必須 (idproxy 側で provider 0 件はエラー) |
| ClientSecret | idproxy.Config.Validate() が provider ごとに必須化しているため、
  「Issuer はあるが ClientSecret 空」を許容しない (空配列で渡しても idproxy 側で reject される) |

実装:
```go
func splitCSV(s string) []string {
    if s == "" { return nil }
    parts := strings.Split(s, ",")
    out := make([]string, 0, len(parts))
    for _, p := range parts {
        out = append(out, strings.TrimSpace(p))
    }
    return out
}
```

長さ一致チェックは authgate 側で行い、空エントリ検出も同階層で行う。理由は
idproxy 側のエラー文言が「providers[2]: issuer is required」のような index 表現で
authgate 文脈とユーザーに伝わりにくいため。

### 4. CookieSecret の hex デコード

**結論: authgate layer で hex.DecodeString を実行し、32 バイト未満なら
`ErrIDProxyConfigInvalid` を返す。decode 失敗時も `ErrIDProxyConfigInvalid` でラップ。**

- spec §11: `COOKIE_SECRET` は `hex 32B+`
- logvalet `mcp_auth.go:82-87` も同方式 (`hex.DecodeString` → `len < 32` で reject)
- idproxy 側 `Config.Validate` も `len(CookieSecret) < 32` で reject するが、エラー文言が
  authgate / spec 文脈と一致しないため authgate layer で先に弾く

### 5. memory store の薄ラッパー

**結論: `internal/authgate/store_memory.go` は `github.com/youyo/idproxy/store.NewMemoryStore()`
を返すだけの 1 関数 (`NewMemoryStore() idproxy.Store`) を提供する。**

- M21–M23 の sqlite / redis / dynamodb と並列に並べる構造を維持するため、ファイル自体は
  作成する
- 中身は `return store.NewMemoryStore()` 一行で十分。インラインラッパーで `MemoryStore`
  型を再定義したり、追加機能を持たせたりしない (over-engineering を避ける)
- テストは `var _ idproxy.Store = NewMemoryStore()` 相当のコンパイル時 + ランタイム適合性
  確認のみ

### 6. IDProxy 型と Wrap の責務

**結論: `IDProxy` 構造体は `*idproxy.Auth` を内包し、`Wrap(next) http.Handler` を
`a.auth.Wrap(next)` に委譲するだけ。**

- ロジックは idproxy ライブラリ側に集約。authgate パッケージは「設定組み立て + ライフサイクル管理」
  に純化
- `Close()` メソッドは **本マイルストーンでは公開しない**。idproxy.Store の Close は
  M24 (CLI 層) で `idpStore.Close()` を直接叩く方針 (memory store の Close は no-op だが、
  sqlite/redis/dynamodb 用に契約として残す)。authgate の interface に Close を足すと
  None/APIKey にも no-op Close が必要になり責務肥大化するため避ける

### 7. doc.go の更新

`internal/authgate/doc.go` を以下のように更新:
- 「M19 までで [ModeNone] / [ModeAPIKey] を実装済み。[ModeIDProxy] は後続マイルストーン
  (M20) で追加する」→ 「M20 までで 3 モード全実装済み (idproxy は memory store のみ。
  sqlite / redis / dynamodb は M21–M23 で追加)」
- 使用例セクションに idproxy + memory store の例を追記:
  ```go
  // idproxy モード (ローカル開発, memory store)
  mw, err := authgate.New(authgate.ModeIDProxy,
      authgate.WithOIDCIssuer(os.Getenv("OIDC_ISSUER")),
      authgate.WithOIDCClientID(os.Getenv("OIDC_CLIENT_ID")),
      authgate.WithOIDCClientSecret(os.Getenv("OIDC_CLIENT_SECRET")),
      authgate.WithCookieSecret(os.Getenv("COOKIE_SECRET")),
      authgate.WithExternalURL(os.Getenv("EXTERNAL_URL")),
  )
  ```

## 公開 API 案

### `internal/authgate/idproxy.go`

```go
package authgate

import (
    "context"
    "encoding/hex"
    "errors"
    "fmt"
    "net/http"
    "strings"

    "github.com/youyo/idproxy"
    "github.com/youyo/idproxy/store"
)

// ErrIDProxyConfigInvalid は ModeIDProxy で必須設定が欠落・不正値の際に [New] が返すエラー。
// errors.Is で判定すること。
var ErrIDProxyConfigInvalid = errors.New("authgate: idproxy config is invalid")

// ErrIDProxyProvidersMismatch は OIDC_ISSUER / OIDC_CLIENT_ID / OIDC_CLIENT_SECRET の
// CSV 件数が一致しない場合に返されるエラー。ErrIDProxyConfigInvalid でラップされる。
var ErrIDProxyProvidersMismatch = errors.New("authgate: oidc issuer/client_id/client_secret csv lengths do not match")

// IDProxy は github.com/youyo/idproxy の Auth をラップし、Middleware として
// 機能する型。`Wrap` はそのまま idproxy.Auth.Wrap に委譲する。
type IDProxy struct {
    auth *idproxy.Auth
}

// newIDProxy は authgate 内部の options から idproxy.Config を組み立て、idproxy.New を
// 呼んで IDProxy を生成する非エクスポート関数。設定不足・型不一致は
// ErrIDProxyConfigInvalid を返す。
// 公開 API は authgate.New(ModeIDProxy, opts...) 経由のみとする (idproxyOptions 型は
// 作らず、M19 と同じ internal options 構造体を再利用する)。
func newIDProxy(ctx context.Context, o options) (*IDProxy, error) { ... }

// Wrap は idproxy.Auth.Wrap を呼び、cookie session 認証 middleware を返す。
func (i *IDProxy) Wrap(next http.Handler) http.Handler {
    return i.auth.Wrap(next)
}
```

`idproxyOptions` は `options` 構造体内に集約するか、別構造体に分けるかは実装時判断。
M19 と一貫性を取るため、`gate.go` の `options` 構造体に `idproxy*` フィールドとして
集約する方針 (Option 関数群が同じ options を mutate する形)。

### `internal/authgate/store_memory.go`

```go
package authgate

import (
    "github.com/youyo/idproxy"
    "github.com/youyo/idproxy/store"
)

// NewMemoryStore は idproxy のデフォルト memory store を返す。
// シングルインスタンス起動 / テスト用途向け。プロセス再起動でセッションは消失する。
//
// M21–M23 で sqlite / redis / dynamodb を追加するまで、本関数が ModeIDProxy の
// デフォルト Store となる。
func NewMemoryStore() idproxy.Store {
    return store.NewMemoryStore()
}
```

### `internal/authgate/gate.go` の修正

```go
// options に idproxy 用フィールドを追加
type options struct {
    apiKey string

    oidcIssuer       string
    oidcClientID     string
    oidcClientSecret string
    cookieSecret     string
    externalURL      string
    idproxyStore     idproxy.Store
    idproxyPrefix    string
}

// Option 関数群を追加
func WithOIDCIssuer(s string) Option       { return func(o *options) { o.oidcIssuer = s } }
func WithOIDCClientID(s string) Option     { return func(o *options) { o.oidcClientID = s } }
func WithOIDCClientSecret(s string) Option { return func(o *options) { o.oidcClientSecret = s } }
func WithCookieSecret(s string) Option     { return func(o *options) { o.cookieSecret = s } }
func WithExternalURL(s string) Option      { return func(o *options) { o.externalURL = s } }
func WithIDProxyStore(s idproxy.Store) Option { return func(o *options) { o.idproxyStore = s } }
func WithIDProxyPathPrefix(s string) Option   { return func(o *options) { o.idproxyPrefix = s } }

// New を修正
func New(mode Mode, opts ...Option) (Middleware, error) {
    var o options
    for _, opt := range opts { opt(&o) }
    switch mode {
    case ModeNone:
        return &None{}, nil
    case ModeAPIKey:
        return NewAPIKey(o.apiKey)
    case ModeIDProxy:
        mw, err := newIDProxy(context.Background(), o)
        if err != nil { return nil, err }
        return mw, nil
    default:
        return nil, fmt.Errorf("%w: %q", ErrUnsupportedMode, string(mode))
    }
}
```

**注意**: `newIDProxy` の第 2 引数が internal の `options` 構造体のため、外部 (パッケージ外
テスト) からは直接呼べないが、`authgate.New(ModeIDProxy, opts...)` 経由ですべて完全に
テスト可能なため、別の公開ヘルパは追加しない。

### `internal/authgate/doc.go` 更新

- 「M19 までで [ModeNone] / [ModeAPIKey] を実装済み。[ModeIDProxy] は後続マイルストーン
  (M20) で追加する」を「M20 までで 3 モード全実装済み。[ModeIDProxy] は memory store
  のみを M20 で実装し、sqlite / redis / dynamodb は M21–M23 で追加する」に変更
- 使用例セクションに idproxy + memory store の例を追加 (上記 §7 参照)

## TDD: テストケース (Red 段階)

### `internal/authgate/idproxy_test.go` (新規)

| # | テスト名 | 入力 | 期待挙動 |
|---|---------|------|---------|
| 1 | `TestIDProxy_NewIDProxy_MissingIssuer_ReturnsErrConfigInvalid` | `WithOIDCIssuer("")` 他必須セット | `errors.Is(err, ErrIDProxyConfigInvalid)` |
| 2 | `TestIDProxy_NewIDProxy_MissingClientID_ReturnsErrConfigInvalid` | `WithOIDCClientID("")` | 同上 |
| 3 | `TestIDProxy_NewIDProxy_MissingClientSecret_ReturnsErrConfigInvalid` | `WithOIDCClientSecret("")` | 同上 |
| 4 | `TestIDProxy_NewIDProxy_MissingCookieSecret_ReturnsErrConfigInvalid` | `WithCookieSecret("")` | 同上 |
| 5 | `TestIDProxy_NewIDProxy_MissingExternalURL_ReturnsErrConfigInvalid` | `WithExternalURL("")` | 同上 |
| 6 | `TestIDProxy_NewIDProxy_InvalidHexCookieSecret_ReturnsErrConfigInvalid` | `WithCookieSecret("ZZZZ")` (非 hex) | 同上 |
| 7 | `TestIDProxy_NewIDProxy_ShortCookieSecret_ReturnsErrConfigInvalid` | `WithCookieSecret("00ff")` (2 バイトのみ) | 同上 |
| 8 | `TestIDProxy_NewIDProxy_CSVLengthMismatch_ReturnsErrProvidersMismatch` | Issuer 2 件 / ClientID 1 件 | `errors.Is(err, ErrIDProxyProvidersMismatch)` かつ `errors.Is(err, ErrIDProxyConfigInvalid)` |
| 9 | `TestIDProxy_NewIDProxy_EmptyEntryInCSV_ReturnsErrConfigInvalid` | `WithOIDCIssuer("https://a, ,https://c")` | `errors.Is(err, ErrIDProxyConfigInvalid)` |
| 10 | `TestIDProxy_NewIDProxy_ValidLocalhostHTTPS_Success` | MockIdP の Issuer, `EXTERNAL_URL=http://localhost:8080` 等を全 Option セット | `*IDProxy` を返す、err == nil |
| 11 | `TestIDProxy_NewIDProxy_ValidWithMemoryStoreDefault_Success` | 全必須セット, `WithIDProxyStore` 未指定 | 成功 (内部で memory store が使われる。Store 取り出し手段がないため、エラーなしで完走することのみ確認) |
| 12 | `TestIDProxy_NewIDProxy_ValidWithExplicitStore_Success` | `WithIDProxyStore(authgate.NewMemoryStore())` を渡す | 成功 |
| 13 | `TestIDProxy_NewIDProxy_MultipleProviders_Success` | MockIdP を 2 つ起動し、Issuer/ClientID/Secret 各 2 件 CSV | 成功 |
| 14 | `TestIDProxy_ImplementsMiddleware` | コンパイル時アサート | `var _ Middleware = (*IDProxy)(nil)` |

**設計**:
- 有効な CookieSecret は `hex.EncodeToString(make([]byte, 32))` 等で生成 (32 バイト以上)
- ExternalURL のテスト値は `http://localhost:8080` を使う (idproxy の Validate を通すため。
  `isLocalhostURL` で http スキームでもパスする)
- **OIDC Discovery のモック**: `github.com/youyo/idproxy/testutil` パッケージの
  `testutil.NewMockIdP(t)` を利用する。これは `.well-known/openid-configuration` /
  `jwks` / `authorize` / `token` 全エンドポイントを提供する httptest.Server を起動し、
  `Issuer()` メソッドで idproxy.Config.Providers[].Issuer に渡せる URL を返す。
  `t.Cleanup` で自動 close されるため明示的なクリーンアップ不要
- 失敗系テスト (#1–#9) は authgate 側で早期 reject されるため MockIdP 不要。
  成功系 (#10–#13) のみ MockIdP を使う
- Wrap 自体の挙動 (login redirect / cookie 復元 / 401) は idproxy ライブラリのテスト責務。
  本リポではテスト #14 のコンパイル時 interface 適合性のみ pin する (Wrap が non-nil handler
  を返すかは Go の method dispatch そのものなので冗長)

### `internal/authgate/store_memory_test.go` (新規)

| # | テスト名 | 確認内容 |
|---|---------|---------|
| 1 | `TestNewMemoryStore_ImplementsStore` | `var _ idproxy.Store = authgate.NewMemoryStore()` (コンパイル時 + runtime non-nil) |
| 2 | `TestNewMemoryStore_BasicSetGet` | session を Set → Get で同一データが取れること (idproxy ライブラリ側の動作確認の薄いコピー、リグレッション用) |

(2) は冗長気味だが、薄ラッパーが「正しい関数を呼んでいる」ことを最低限担保する目的で 1 ケースだけ追加。
過度な depth は idproxy ライブラリ側のテストに委ねる。

### `internal/authgate/gate_test.go` (修正)

- **削除**: `TestNew_IDProxy_ReturnsErrUnsupportedMode` (M16 で書いた契約 pin)
- **追加**:
  - `TestNew_IDProxy_MissingOpts_ReturnsErrConfigInvalid` (`New(ModeIDProxy)` のみ → `errors.Is(err, ErrIDProxyConfigInvalid)`)
  - `TestNew_IDProxy_AllOptsSet_Success` (Option 全部指定 → `*IDProxy` 取得)
  - `TestNew_IDProxy_WithCustomStore_Success` (`WithIDProxyStore` を経由)

既存テストは variadic Option 拡張で影響なし。

## 実装手順 (Red → Green → Refactor)

1. **Red 1**: `idproxy_test.go` の 15 ケースを書く (型未定義でコンパイルエラー)
2. **Red 2**: `store_memory_test.go` の 2 ケースを書く
3. **Red 3**: `gate_test.go` から旧 ModeIDProxy ケースを削除、新 3 ケースを追加
4. **Green 1**: `go.mod` に `github.com/youyo/idproxy` を `go get github.com/youyo/idproxy@v0.4.2` で追加
5. **Green 2**: `store_memory.go` を 1 関数で実装
6. **Green 3**: `idproxy.go` を実装 (CSV split + validate → idproxy.Config → idproxy.New)
7. **Green 4**: `gate.go` の `options` に idproxy 用フィールドを追加、Option 関数群、`New` の `ModeIDProxy` 分岐
8. **Green 5**: `doc.go` を更新 (M20 反映 + 使用例追記)
9. **検証**: `go test -race -count=1 ./...` 全 pass
10. **Refactor**: lint 違反、godoc 表現、未使用 import 整理
11. **最終検証**:
    - `golangci-lint run ./...` 0 issues
    - `go vet ./...` 通過
    - `go build -o /tmp/x ./cmd/x` 成功

## リスクと対策

| リスク | 対策 |
|--------|------|
| idproxy v0.4.2 の API が想定と違う | 事前確認済: `idproxy.New(ctx, Config) (*Auth, error)` / `Auth.Wrap(next) http.Handler` / `store.NewMemoryStore() *MemoryStore`。logvalet `internal/cli/mcp_auth.go` と整合 |
| idproxy.Config の `OAuth` を nil にしたとき Validate が通らない | idproxy.Config.Validate を確認: OAuth nil なら SigningKey 検査をスキップ (`if c.OAuth != nil && c.OAuth.SigningKey == nil`)。OK |
| OAuth 無効化が「Bearer JWT で MCP を叩けない」ことを意味する | M20 のスコープ内では受容。Routine からの呼び出しは M19 で実装済の apikey モードを使う前提 (spec ADR #5 と整合) |
| CSV pairing 規則の曖昧さ | 本計画 §3 で確定。テスト #8 / #9 で pin |
| Cookie Secret hex 形式の解釈差 | logvalet と同じ `hex.DecodeString` + `len >= 32` で統一 |
| idproxy が新しいプロバイダで Discovery エンドポイントを叩いて遅延 / 失敗 | `github.com/youyo/idproxy/testutil.NewMockIdP(t)` を成功系テスト (#10–#13) で使用する。これは Discovery / JWKS / authorize / token 全エンドポイントを実装した httptest.Server を内蔵し、idproxy.New がエラーなく完走する。確認済 (`mock_idp.go:42`)。失敗系テスト (#1–#9) は authgate 側で早期 reject するため Mock 不要 |

### Discovery 対策の確定方針

**採用**: `github.com/youyo/idproxy/testutil.NewMockIdP(t)` を直接利用する。

- 既存資産 (idproxy v0.4.2 配布物) に含まれる公開テストヘルパ
- `MockIdP.Issuer()` で `httptest.Server.URL` を取得 → そのまま
  `authgate.WithOIDCIssuer(mock.Issuer())` に渡す
- `t.Cleanup` でサーバの自動クローズ。Wait/Stop の明示記述不要
- 複数 provider テスト (#13) は MockIdP を 2 つ起動 (Issuer 異なる) し、CSV で結合する
- go.mod に testutil 用の追加依存は不要 (idproxy 本体に含まれるため)

**idproxy エラーラップ方針** (失敗系のみ):
`idproxy.New` が config validation 以外の理由で失敗した場合 (Discovery 失敗等) の挙動は
本マイルストーンでは「authgate 側で早期 reject されるため、idproxy.New までは到達しない」
ことで担保される。MockIdP を使う成功系では idproxy.New 自体は err == nil となる。
将来 Discovery 失敗をテストしたくなった場合は、別途 httptest で 500 を返すサーバを立てて
「idproxy 初期化エラー」として `fmt.Errorf("authgate: idproxy initialization failed: %w", err)`
で wrap する形を取る (M20 では実装しない)。

## 既存呼び出し確認 (variadic 互換性)

`grep -rn "authgate.New(" --include='*.go'` 結果 (M19 後):

- `internal/authgate/gate_test.go` (テスト内 5 箇所以上)
- `internal/authgate/doc.go` (使用例コメント)
- transport/http や cli からの呼び出しは **無し** (M24 で追加予定)

→ variadic Option 拡張は完全に source-compatible。

## 既存 doc コメントの更新 (重要)

実装時に **必ず** 以下の doc コメントを「実装済み」に更新する。

- `gate.go` の `ModeIDProxy` 定数 doc: 「M20 で実装する」→ 「M20 で memory store 実装済み。
  sqlite / redis / dynamodb は M21–M23 で追加」
- `gate.go` の `New` godoc: 「M19 までで [ModeNone] / [ModeAPIKey] を実装済み」→
  「M20 までで 3 モード全実装済み」
- `doc.go` のパッケージ doc: M20 反映 + 使用例追記

## 完了条件

- [ ] `internal/authgate/idproxy.go` 実装 (NewIDProxy + Wrap + 必須エラー)
- [ ] `internal/authgate/idproxy_test.go` 14 ケース pass
- [ ] `internal/authgate/store_memory.go` 実装 (1 関数)
- [ ] `internal/authgate/store_memory_test.go` 2 ケース pass
- [ ] `internal/authgate/gate.go` Option パターン拡張 + ModeIDProxy 分岐実装 + godoc 更新
- [ ] `internal/authgate/gate_test.go` ModeIDProxy 系テスト更新 (3 新規)
- [ ] `internal/authgate/doc.go` M20 反映 + idproxy 使用例追加
- [ ] `go.mod` に `github.com/youyo/idproxy` v0.4.2 以上を追加
- [ ] `go test -race -count=1 ./...` 全 pass
- [ ] `golangci-lint run ./...` 0 issues
- [ ] `go vet ./...` 通過
- [ ] `go build -o /tmp/x ./cmd/x` 成功
- [ ] 公開シンボル全てに日本語 doc コメント
- [ ] パッケージ doc は `doc.go` 1 ファイル集約 (`idproxy.go` / `store_memory.go` 冒頭に
      パッケージコメントを書かない)

## 次マイルストーンへの引き継ぎ (M21 sqlite store)

- **`WithIDProxyStore(idproxy.Store)` パターン**: M20 で確立済。M21 は
  `internal/authgate/store_sqlite.go` で `NewSQLiteStore(path string) (idproxy.Store, error)` を
  追加するだけ
- **CSV pairing 規則**: M20 で確定。M21 では再利用
- **`OAuth = nil` 方針**: M20 と同じ。OAuth AS 有効化は別マイルストーンで議論
- **エラー設計**: `ErrIDProxyConfigInvalid` でラップする方針を踏襲。sqlite-specific の
  errors (e.g. `ErrSQLitePathInvalid`) を追加する場合も同様のラップ
- **idproxy.Store の Close 責任**: 呼び出し側 (M24 CLI 層) が `idpStore.Close()` を defer する
  方針。authgate.Middleware に Close は追加しない
- **環境変数 `STORE_BACKEND`**: M24 で defaulting + dispatch (`memory` / `sqlite` / `redis` /
  `dynamodb`)。authgate 層では文字列を受けず、Store interface を直接受ける Option パターンを
  維持する
