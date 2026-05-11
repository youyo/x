# M16: authgate 基盤 + none モード 詳細実装計画

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M16 (Phase E: MCP コア) |
| スペック | `docs/specs/x-spec.md` §5 (internal/authgate), §6 / §8, §11 (`X_MCP_AUTH=none|apikey|idproxy`) |
| 前提 | M1-M15 完了 (latest: 6fd7000) / `internal/mcp` + `internal/transport/http` 雛形済 |
| ステータス | 計画中 → 実装へ |
| 作成日 | 2026-05-12 |

## Goal

`internal/authgate` パッケージを新規追加し、MCP 着信認証のための middleware framework を確立する。本マイルストーンでは **none モード (passthrough) のみ実装**し、apikey / idproxy は後続マイルストーン (M19 / M20) に委ねる。

同時に `internal/transport/http` を拡張して以下を提供する:

1. `WithHandlerMiddleware(mw func(http.Handler) http.Handler) Option` — MCP handler に任意 middleware を差し込めるフック。
2. `/healthz` エンドポイント — LWA / Lambda 健全性確認用。**authgate middleware の外側に常時露出**する (Lambda が認証必須の middleware に阻まれて死活確認できなくなる事故を防ぐ)。

## Scope

### In scope

- `internal/authgate/doc.go` (パッケージドキュメント、3 モード設計の概観)
- `internal/authgate/gate.go` (`Middleware` interface, `Mode` 型と定数, `New` ファクトリ, `ErrUnsupportedMode`)
- `internal/authgate/none.go` (`None` 型 + `Wrap` passthrough)
- `internal/authgate/gate_test.go` / `internal/authgate/none_test.go`
- `internal/transport/http/server.go` 拡張: `WithHandlerMiddleware` Option + `/healthz` route
- `internal/transport/http/server_test.go` 拡張: middleware 適用 / nil passthrough / /healthz 200 / **/healthz が middleware の外**であることの確認
- `golangci-lint v2` 違反 0 維持
- `go vet ./...` clean
- 既存テスト全 pass (`go test -race -count=1 ./...`)

### Out of scope (本マイルストーン外)

- apikey モードの実装 — M19
- idproxy モードの実装 (memory store) — M20
- sqlite/redis/dynamodb store backend — M21–M23
- cobra サブコマンド `x mcp` への接続 — M24
- LWA bootstrap / lambroll サンプル — M26

## Design

### Package `internal/authgate`

#### doc.go

```go
// Package authgate は MCP サーバーの着信認証 middleware を提供する。
//
// スペック §11 に定義される 3 モード (none / apikey / idproxy) を切り替え可能な
// Middleware インターフェースを公開する:
//
//   - none:    認証なし。passthrough。ローカル開発のみで使用する。
//   - apikey:  Bearer token を共有シークレットと定数時間比較。CI/Routine 想定。
//   - idproxy: OIDC ベースの session 認証。本番想定。memory/sqlite/redis/dynamodb
//              の 4 store backend をサポートする。
//
// 本マイルストーン (M16) では none のみを実装し、apikey/idproxy は M19/M20 で
// 追加する。New(mode) はサポート外モードに対して ErrUnsupportedMode を返す。
package authgate
```

#### gate.go

```go
package authgate

import (
	"errors"
	"fmt"
	"net/http"
)

// Mode は authgate の認証モードを表す。スペック §11 の X_MCP_AUTH 値と
// 1:1 対応する。
type Mode string

// 利用可能な認証モードの定数。
const (
	// ModeNone は認証を行わない passthrough モード。ローカル開発専用。
	ModeNone Mode = "none"
	// ModeAPIKey は Bearer token と共有シークレットの定数時間比較モード。
	// CI/Routine からの呼び出しを想定する。M19 で実装する。
	ModeAPIKey Mode = "apikey"
	// ModeIDProxy は OIDC ベースの session 認証モード。本番想定。M20 で実装する。
	ModeIDProxy Mode = "idproxy"
)

// ErrUnsupportedMode は New() がサポート外モードを受け取った際に返すエラー。
// 空文字 "" もこのエラーで弾く (defaulting は呼び出し側 CLI 層の責務)。
var ErrUnsupportedMode = errors.New("authgate: unsupported mode")

// Middleware は MCP handler を任意のロジックでラップする責務を表す。
//
// Wrap は next を受け取り、認証チェック等を行ったうえで next を呼び出す
// http.Handler を返す。実装は ServeHTTP 内で 401/403 を返すか next.ServeHTTP
// に処理を委譲するか選択する。
//
// シグネチャは idproxy.Wrap (`func(http.Handler) http.Handler` 相当) と整合させ、
// 後続 M19/M20 で apikey/idproxy 実装をそのまま差し込めるようにする。
type Middleware interface {
	Wrap(next http.Handler) http.Handler
}

// New は指定された Mode に対応する Middleware を返す。
//
// M16 では ModeNone のみ実装済み。ModeAPIKey / ModeIDProxy および
// その他の値 (空文字を含む) は ErrUnsupportedMode を返す。
func New(mode Mode) (Middleware, error) {
	switch mode {
	case ModeNone:
		return &None{}, nil
	case ModeAPIKey, ModeIDProxy:
		return nil, fmt.Errorf("%w: %q (not yet implemented)", ErrUnsupportedMode, mode)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedMode, mode)
	}
}
```

#### none.go

```go
package authgate

import "net/http"

// None は認証を行わない passthrough Middleware である。
//
// ローカル開発・テスト・LWA 経由でセキュリティを別レイヤー (function URL / IAM)
// で担保するケースで利用する。本番環境では使用しないこと。
type None struct{}

// Wrap は next をそのまま返し、認証チェックを行わない。
//
// 引数 next が nil の場合の挙動は呼び出し側 (transport/http) で next != nil を
// 保証する責務とし、本関数では nil チェックを行わない (Go 標準の middleware
// 慣習に合わせる)。
func (n *None) Wrap(next http.Handler) http.Handler {
	return next
}
```

### Package `internal/transport/http` 拡張

#### server.go 追加要素

```go
// Server struct に handlerMW フィールドを追加
type Server struct {
	// 既存フィールド ...
	handlerMW func(http.Handler) http.Handler
}

// WithHandlerMiddleware は MCP handler を任意の middleware でラップする Option。
//
// 主な用途は authgate (none/apikey/idproxy) の挿し込み。/healthz エンドポイントは
// この middleware の影響を受けず、常に認証なしで応答する (Lambda 健全性確認用)。
//
// mw が nil の場合は passthrough (Option を渡さなかった場合と等価)。
//
// 引数のシグネチャは authgate.Middleware インターフェースの Wrap メソッドと
// 整合する。呼び出し側で `WithHandlerMiddleware(mw.Wrap)` のように渡すことを
// 想定する。transport/http パッケージは authgate に依存しない (循環依存防止)。
func WithHandlerMiddleware(mw func(http.Handler) http.Handler) Option {
	return func(s *Server) {
		s.handlerMW = mw
	}
}
```

#### Run 内部の変更

```go
func (s *Server) Run(ctx context.Context) error {
	h := mcpserver.NewStreamableHTTPServer(s.mcp, mcpserver.WithEndpointPath(s.path))

	var handler http.Handler = h
	if s.handlerMW != nil {
		handler = s.handlerMW(handler)
	}

	mux := stdhttp.NewServeMux()
	mux.Handle(s.path, handler)
	// /healthz は middleware の外側に常時公開する (LWA 健全性確認用)
	mux.HandleFunc("/healthz", healthzHandler)

	// 以降は既存と同じ ...
}

// healthzHandler は LWA / Lambda の死活確認用エンドポイント。
// 認証 middleware の外側に露出させ、常に 200 "ok\n" を返す。
func healthzHandler(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
	w.WriteHeader(stdhttp.StatusOK)
	_, _ = fmt.Fprintln(w, "ok")
}
```

### 設計判断

1. **Middleware interface vs raw func**: transport/http 側は `func(http.Handler) http.Handler` を直接受け取る。これは:
   - transport/http が authgate パッケージに依存しない (循環依存・関心分離)
   - 呼び出し側 (M24 の cobra) で `WithHandlerMiddleware(mw.Wrap)` するだけ
   - interface は authgate 内部で M19/M20 の apikey/idproxy 実装を統一型に揃えるため

2. **/healthz を middleware の外側に置く設計不変条件**: LWA / function URL は /healthz を健全性チェックに使う可能性が高い。仮に authgate が全リクエスト 401 を返すモードでも /healthz は 200 でなければならない。これを **テストでガード**する (TestHealthz_BypassesMiddleware)。

3. **空文字 Mode のハンドリング**: spec §11 では env-var レベルで `X_MCP_AUTH` の default は idproxy だが、これは CLI 層 (M24) の defaulting 責務。`authgate.New("")` は ErrUnsupportedMode を返す (defaults はもっと上のレイヤーで)。テストで pin する。

4. **`New(ModeAPIKey)` / `New(ModeIDProxy)` は ErrUnsupportedMode**: M19/M20 でこの分岐を実装に書き換える。M16 ではエラーが正しい契約。

5. **`Options ...Option` への将来拡張は今は入れない**: M19 で apikey の API key 受け渡しが必要になった時点で考える。M16 ではミニマル契約。

6. **doc.go は 1 ファイル集約**: パッケージドキュメントは doc.go のみ。gate.go の冒頭にはコメントを書かない (M15 の方針を踏襲)。

## TDD 計画

### internal/authgate/gate_test.go

| # | テスト名 | Red 状態の挙動 | Green 後の挙動 |
|---|---|---|---|
| 1 | `TestNew_None_ReturnsNoneMiddleware` | `authgate` パッケージ未作成 / `New` 未実装 | `New(ModeNone)` が `*None` 型の Middleware を返す (`assert as *None`) |
| 2 | `TestNew_APIKey_ReturnsErrUnsupportedMode` | 同上 | `New(ModeAPIKey)` が `nil, err` で `errors.Is(err, ErrUnsupportedMode)` |
| 3 | `TestNew_IDProxy_ReturnsErrUnsupportedMode` | 同上 | `New(ModeIDProxy)` が `nil, err` で `errors.Is(err, ErrUnsupportedMode)` |
| 4 | `TestNew_EmptyMode_ReturnsErrUnsupportedMode` | 同上 | `New("")` が `nil, err` で `errors.Is(err, ErrUnsupportedMode)` (defaults は CLI 層責務) |
| 5 | `TestNew_UnknownMode_ReturnsErrUnsupportedMode` | 同上 | `New("invalid")` が `nil, err` で `errors.Is(err, ErrUnsupportedMode)` |
| 6 | `TestMode_StringValues` | 同上 | 定数 `ModeNone="none"`, `ModeAPIKey="apikey"`, `ModeIDProxy="idproxy"` (spec §11 と一致) |

### internal/authgate/none_test.go

| # | テスト名 | 検証 |
|---|---|---|
| 1 | `TestNone_Wrap_ReturnsSameHandler` | `(&None{}).Wrap(next)` が `next` と同じ pointer を返す (Go では equal interface か `==` で `http.Handler` を比較) |
| 2 | `TestNone_Wrap_PassesThroughRequest` | `httptest.NewRecorder` + `httptest.NewRequest` で wrapped handler を呼び、inner が呼ばれて 200 が記録される |

### internal/transport/http/server_test.go 拡張

既存テストは regression guard として温存。新規追加:

| # | テスト名 | 検証 |
|---|---|---|
| 1 | `TestRun_NilMiddleware_DefaultPassthrough` | `WithHandlerMiddleware` 未指定で M15 の挙動を維持 (これは既存 `TestRun_HandlesInitializeRequest` が暗黙的にカバー。新規 explicit テストとして `WithHandlerMiddleware(nil)` も passthrough であることを 1 ケース追加) |
| 2 | `TestRun_WithHandlerMiddleware_AppliesMiddleware` | 任意 middleware (`X-Authgate-Test: applied` ヘッダ付与) を渡し、`/mcp` への POST レスポンスでヘッダが付くことを確認。MCP 応答も正しく返る (middleware 適用順 = mw 先、handler 後) |
| 3 | `TestRun_Healthz_Returns200OK` | `GET /healthz` が `200` + body `"ok\n"` を返す |
| 4 | `TestRun_Healthz_BypassesMiddleware` | **設計不変条件のテスト**: 全リクエストを 401 にする middleware を `WithHandlerMiddleware` で挿しても `/healthz` は 200 を返す。`/mcp` は 401 になる |
| 5 | `TestWithHandlerMiddleware_Option_StoresMiddleware` | Server 構築時 (Run しない) で Option が反映されていることを確認 (getter があれば。なければスキップ。優先度低) |

> #1 と #5 は実装ノイズの可能性があるので軽め。#2 / #3 / #4 が本質。

### 既存テスト regression guard

- `TestRun_HandlesInitializeRequest` (M15) はそのまま pass し続けること。`WithHandlerMiddleware` を渡さない場合の挙動を pin する。
- `TestNewServer_Defaults` / `TestNewServer_WithOptions` も M16 で新 Option を追加しても pass し続けること。

## リスク

| リスク | 影響 | 対策 |
|---|---|---|
| middleware 適用順の取り違い (handler → mw になる) | 中 (認証バイパス) | TestRun_WithHandlerMiddleware_AppliesMiddleware で marker ヘッダ確認 + Healthz_BypassesMiddleware で挙動 pin |
| /healthz が誤って authgate middleware の内側に入る | 高 (Lambda 死活確認不能) | TestRun_Healthz_BypassesMiddleware (全 401 mw でも 200 を返すこと) で防御 |
| `authgate.Middleware` interface と transport の関数型のずれ | 低 | doc.go と gate.go の doc コメントに記載。CLI 層 (M24) で `WithHandlerMiddleware(mw.Wrap)` パターンで接続 |
| 空文字 `Mode("")` の挙動が CLI 層 default と矛盾 | 低 | 仕様 pin: authgate.New は defaulting しない、CLI 層 (M24) が default = idproxy をセットする責務 |
| 既存 TestRun_HandlesInitializeRequest が /healthz route 追加で壊れる | 低 | /healthz は別 path なので衝突しないが、テストで pass を確認 |

## 検証 (Definition of Done)

1. `cd /Users/youyo/src/github.com/youyo/x && go test -race -count=1 ./...` 全 pass
2. `cd /Users/youyo/src/github.com/youyo/x && golangci-lint run ./...` 0 issues
3. `cd /Users/youyo/src/github.com/youyo/x && go vet ./...` clean
4. `cd /Users/youyo/src/github.com/youyo/x && go build -o /tmp/x ./cmd/x` 成功
5. 新規ファイル: `internal/authgate/{doc.go,gate.go,none.go,gate_test.go,none_test.go}` (5 ファイル)
6. 拡張ファイル: `internal/transport/http/{server.go,server_test.go}` (2 ファイル)
7. 既存テストが回帰していない (M15 までのテストが全 pass)
8. 全公開シンボル (Mode / ModeNone / ModeAPIKey / ModeIDProxy / ErrUnsupportedMode / Middleware / New / None / None.Wrap / WithHandlerMiddleware) に日本語 doc コメントが付与されている

## コミット計画

実装完了後、以下 1 コミットで提出:

```
feat(authgate): authgate 基盤と none モード、transport の middleware フック・/healthz を追加

- internal/authgate パッケージ新規追加 (gate.go / none.go / doc.go)
- Mode 定数 (none/apikey/idproxy) と Middleware interface + New ファクトリ
- M16 では none のみ実装、apikey/idproxy は ErrUnsupportedMode を返す
- transport/http に WithHandlerMiddleware Option を追加 (authgate 切り離し)
- /healthz エンドポイントを middleware の外側に常時公開 (LWA 死活確認用)
- 全変更を TDD (Red→Green→Refactor) で実装

Plan: plans/x-m16-authgate-none.md
```
