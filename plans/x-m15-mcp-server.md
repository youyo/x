# M15: MCP サーバー雛形 + transport/http 詳細実装計画

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M15 (Phase E: MCP コア) |
| スペック | `docs/specs/x-spec.md` §5 Architecture / §6 MCP Tools / §8 Runtime Flows (フロー2) |
| 前提 | M1–M14 完了 (latest: fbb2df8) / xapi.Client・config・version 整備済み |
| 参考実装 | `youyo/logvalet:internal/mcp/server.go` v0.52.0 (mcp-go) |
| mcp-go バージョン | `v0.52.0` (M14 ハンドオフは v0.49+ 指定だが、`WithEndpointPath` は v0.50+ で利用可、logvalet と揃えて v0.52.0 をピン留め) |
| ステータス | 計画中 → 実装へ |
| 作成日 | 2026-05-12 |

## Goal

`internal/mcp/` と `internal/transport/http/` の 2 パッケージを新規追加し、以下を満たすことを目標とする:

1. **`internal/mcp/server.go`**: `NewServer(client *xapi.Client, version string) *server.MCPServer` のファクトリを公開する。tools は M17/M18 で登録するため、本マイルストーンでは **空サーバー**を返す。
2. **`internal/transport/http/server.go`**: mark3labs/mcp-go の Streamable HTTP server を内包し、`Run(ctx context.Context) error` で graceful shutdown する `Server` 型を公開する。LWA / Lambda Web Adapter 互換 (HTTP server)。
3. **TDD**: server (mcp) / Run (transport/http) 双方を契約テストで検証する。`initialize` リクエストへの 200 + JSON-RPC 2.0 応答を httptest で確認する。
4. **cobra へは未接続**: `x mcp` サブコマンドの追加は M24 で行う。M15 は純粋に internal/ 配下のみ。

## Scope

### In scope

- `internal/mcp/server.go` (NewServer ファクトリ)
- `internal/mcp/server_test.go` (NewServer の戻り値検証)
- `internal/mcp/doc.go` (パッケージ doc)
- `internal/transport/http/server.go` (Server + Option + Run)
- `internal/transport/http/server_test.go` (Run ライフサイクル + initialize 応答)
- `internal/transport/http/doc.go` (パッケージ doc)
- `go.mod` / `go.sum` の更新 (`github.com/mark3labs/mcp-go v0.52.0`)
- 既存テストを壊さないこと (`go test -race -count=1 ./...` 全 pass)
- `golangci-lint v2` 違反 0 維持
- `go vet ./...` clean

### Out of scope (本マイルストーン外)

- MCP tools の登録 (`get_user_me`, `get_liked_tweets`) — M17 / M18
- authgate middleware 接続 — M16 以降
- cobra サブコマンド `x mcp` — M24
- LWA bootstrap script / lambroll サンプル — M28
- OAuth (idproxy) 統合 — M19–M23

## Design

### Package `internal/mcp`

```go
// internal/mcp/doc.go
//
// Package mcp は X (旧 Twitter) API CLI 向けの MCP サーバー雛形を提供する。
// mark3labs/mcp-go を基盤に「x」サーバーをファクトリで構築する。tools は別途
// 登録関数を経由して差し込む設計とし、本パッケージでは tool capability の宣言
// と最小限のメタ情報 (name / version) を担う。
package mcp
```

```go
// internal/mcp/server.go
package mcp

import (
    mcpserver "github.com/mark3labs/mcp-go/server"
    "github.com/youyo/x/internal/xapi"
)

// ServerName は MCP サーバーが initialize レスポンスで返す server.name である。
const ServerName = "x"

// NewServer は X API CLI 用の MCP サーバーを構築する。
// 本マイルストーン (M15) では tools 登録を行わず、空サーバーを返す。
// tools (get_user_me / get_liked_tweets) は後続マイルストーン (M17 / M18) で
// 別ファイルの登録関数経由で差し込む設計とする。
//
// client は X API 呼び出しに利用する xapi.Client であり、現時点では保持しない
// (空サーバーで unused)。ただし API シグネチャを M17 と整合させるために
// 引数に含める。version は server.version として initialize レスポンスに反映される。
func NewServer(client *xapi.Client, version string) *mcpserver.MCPServer {
    // M15 では client は未使用 (M17 の get_user_me 実装で必要)。Go の未使用引数は
    // 言語仕様上問題なく、linter (unused / staticcheck / unparam) も flag しないため
    // discard は記述しない。
    return mcpserver.NewMCPServer(
        ServerName,
        version,
        mcpserver.WithToolCapabilities(true),
    )
}
```

#### 設計判断

- **client を引数に含める**: M17 (`get_user_me`) で xapi.Client を tool handler に注入する想定。M15 で空サーバーでも引数化しておくことで API 変更を回避し、M17 で内部の `registerTools(s, client)` 呼び出し追加だけで済むようにする。
- **未使用引数の discard は書かない**: advisor 指摘。未使用関数引数は idiomatic で linter も flag しない。`_ = client` ノイズを避ける。
- **server.name = `"x"`**: スペック §5 / バイナリ名 `x` に一致。
- **WithToolCapabilities(true)**: MCP capabilities 宣言。M17 で tools 登録が成立する前提。

### Package `internal/transport/http`

```go
// internal/transport/http/doc.go
//
// Package http は MCP サーバーを HTTP transport で提供するための薄いラッパーを
// 提供する。mark3labs/mcp-go の Streamable HTTP server を内包し、LWA (Lambda
// Web Adapter) 互換のシンプルな net/http.Server として起動できる。
// 認証 middleware の接続は internal/authgate に委ねる。
package http
```

```go
// internal/transport/http/server.go
package http

import (
    "context"
    "errors"
    "fmt"
    nethttp "net/http"
    "time"

    mcpserver "github.com/mark3labs/mcp-go/server"
)

// デフォルト値定数群。
const (
    DefaultAddr            = "127.0.0.1:8080"
    DefaultPath            = "/mcp"
    DefaultShutdownTimeout = 30 * time.Second
)

// Server は MCP サーバーを HTTP 経由で提供する小さなラッパーである。
// ライフサイクル: NewServer → Run(ctx) → ctx.Done() で graceful shutdown。
type Server struct {
    mcp             *mcpserver.MCPServer
    addr            string
    path            string
    shutdownTimeout time.Duration
}

// Option は Server の任意設定を表す関数オプションである。
type Option func(*Server)

// WithAddr は HTTP listen アドレスを上書きする。
func WithAddr(addr string) Option { ... }

// WithPath は MCP エンドポイントパスを上書きする (既定 /mcp)。
func WithPath(path string) Option { ... }

// WithShutdownTimeout は graceful shutdown の最大待機時間を上書きする (既定 30s)。
func WithShutdownTimeout(d time.Duration) Option { ... }

// NewServer は mcp サーバーを HTTP transport でラップする Server を返す。
// オプション未指定時のデフォルトは DefaultAddr / DefaultPath / DefaultShutdownTimeout。
func NewServer(mcp *mcpserver.MCPServer, opts ...Option) *Server { ... }

// Addr / Path / ShutdownTimeout は内部状態を観測する getter (テスト + 観測用)。
func (s *Server) Addr() string                       { return s.addr }
func (s *Server) Path() string                       { return s.path }
func (s *Server) ShutdownTimeout() time.Duration     { return s.shutdownTimeout }

// Run は HTTP サーバーを起動し、ctx.Done() で graceful shutdown する。
// 戻り値:
//   - ctx 終了による正常停止: nil
//   - listen / shutdown のエラー: 非 nil error (http.ErrServerClosed は隠蔽)
//
// シグナル監視 (SIGTERM / SIGINT) は呼び出し側 (cobra/main) が
// signal.NotifyContext で行い、その ctx を渡す責務とする。
// transport 層はあくまで ctx ベースのライフサイクルのみを扱う。
func (s *Server) Run(ctx context.Context) error {
    h := mcpserver.NewStreamableHTTPServer(s.mcp, mcpserver.WithEndpointPath(s.path))

    mux := nethttp.NewServeMux()
    mux.Handle(s.path, h)

    srv := &nethttp.Server{
        Addr:              s.addr,
        Handler:           mux,
        ReadHeaderTimeout: 10 * time.Second, // gosec G114 対策
    }

    errCh := make(chan error, 1)
    go func() {
        errCh <- srv.ListenAndServe()
    }()

    select {
    case err := <-errCh:
        if err != nil && !errors.Is(err, nethttp.ErrServerClosed) {
            return fmt.Errorf("http server: %w", err)
        }
        return nil
    case <-ctx.Done():
        shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
        defer cancel()
        if err := srv.Shutdown(shutdownCtx); err != nil {
            return fmt.Errorf("http shutdown: %w", err)
        }
        return nil
    }
}
```

#### 設計判断

- **Option 関数パターン**: addr / path / shutdownTimeout のいずれも optional。logvalet の `McpCmd` は flag を直接持つが、`x` では transport を再利用しやすいよう Option パターンに変えた (CLI 層 M24 で env → opt 写像する)。
- **`mcpserver.WithEndpointPath`**: mark3labs/mcp-go v0.52 の Streamable HTTP server で path を切り替える正式 API。
- **`signal.NotifyContext` は呼び出し側責務**: transport は ctx のみを扱う設計に純化。テスト容易性向上 + Lambda Web Adapter 環境 (SIGTERM は AWS Lambda Runtime が処理する) との分離。
- **listen エラーと ctx 完了の race を select で処理**: `ListenAndServe` が即時失敗 (port 競合等) しても errCh から非 nil を返せるようにする。
- **ReadHeaderTimeout**: gosec G114 (Slowloris 攻撃対策) を満たすため 10s を設定。golangci-lint v2 の `gosec` が有効な場合に違反を防ぐ。
- **graceful shutdown 30s**: スペック非明示だが M14 ハンドオフで指定。`SIGTERM` 受信後 30s 以内に in-flight リクエストを完了させる。

### TDD: テスト設計

#### `internal/mcp/server_test.go`

1. `TestNewServer_NotNil`: 戻り値が nil でないことを確認。
2. `TestNewServer_VersionFormats`: dev / 1.0.0 で panic しないこと (空文字列は M17 で別途扱う)。

advisor 指摘により `TestNewServer_AcceptsNilClient` は **書かない**: M17 で nil 拒否に変えたい際に「テストが pinning しているせいで削除/更新が必要」というアンチパターンになる。M15 の契約は「コンストラクタが成功して非 nil サーバーを返す」のみに絞る。

ListTools / ListResources は M17 まで空なので、本マイルストーンでは「コンストラクタが成功する」契約のみテストする。

#### `internal/transport/http/server_test.go`

1. `TestNewServer_Defaults`: Option 無しで addr=`127.0.0.1:8080`、path=`/mcp`、shutdownTimeout=`30s` になること。
2. `TestNewServer_WithOptions`: WithAddr / WithPath / WithShutdownTimeout がそれぞれ反映されること。
3. `TestRun_ContextCancel_GracefulShutdown`: ランダムポート (`127.0.0.1:0` 相当) で起動 → ctx cancel → Run が nil で返ること。listen 開始は `net.Listen` を直接行うのではなく、`Server.Run` を goroutine で呼び、`/mcp` への HTTP HEAD/POST が成功した時点で起動完了とみなしてから cancel する。
4. `TestRun_HandlesInitializeRequest`: ランダムポートで起動 → `POST /mcp` に JSON-RPC 2.0 の `initialize` リクエスト (protocolVersion / clientInfo 付き) を投げ、HTTP 200 + 応答 body に server.name `"x"` が含まれることを確認する。
   - **Content-Type 寛容化** (advisor 指摘 #1): mcp-go v0.52 の Streamable HTTP は `Accept` ヘッダに応じて `application/json` または `text/event-stream` (SSE) を返す。テストは:
     - `Accept: application/json, text/event-stream` をリクエストに付与
     - レスポンスの `Content-Type` を見て分岐:
       - `application/json*` → そのまま JSON-RPC envelope を decode
       - `text/event-stream*` → `data: {json}\n\n` 形式の最初の `data:` 行を切り出して decode
     - 解析後の JSON-RPC envelope に `result.serverInfo.name == "x"` を確認 (バージョン文字列は本テストでは中身検証しない)
   - body 全文をログ出力しないが、`t.Logf` で `Content-Type` のみ出して将来の retrospect に備える。
5. `TestRun_ReturnsListenError`: 既に使用中のポートに bind しようとした際に **非 nil error が 1 秒以内に返る** ことのみ確認 (advisor 指摘 #5: `syscall.EADDRINUSE` の文字列比較は darwin/linux で差があるため避ける)。

#### ランダムポート確保戦略

`net.Listen("tcp", "127.0.0.1:0")` で空きポートを取得 → `Close()` → 取得したポート番号で Server を起動。短時間の race はあるが、CI / ローカルともに実用上問題ない。logvalet も同パターンを採用。

advisor 指摘 #4 で `WithListener(net.Listener)` Option を将来追加する選択肢が提案されているが、本マイルストーンでは race の実害が観測されていないため見送り。CI で flake が観測されたタイミングで追加する。

#### 起動完了の同期戦略

`http.Get(fmt.Sprintf("http://%s/mcp", addr))` を 200ms 間隔 × 最大 20 回 (=4s) で polling し、`net.OpError` (connection refused) が消えた時点で起動完了とみなす。

## Test Plan

```bash
# Red (server がまだ存在しない → コンパイルエラー)
go test ./internal/mcp/... ./internal/transport/http/...

# Green (実装後)
go test -race -count=1 ./internal/mcp/... ./internal/transport/http/...

# 既存テスト regression
go test -race -count=1 ./...

# Lint
golangci-lint run ./...
go vet ./...

# ビルド
go build -o /tmp/x ./cmd/x
/tmp/x version
```

## Risks

| リスク | 対策 |
|---|---|
| mark3labs/mcp-go v0.52 の API 変更 (NewStreamableHTTPServer / WithEndpointPath) | logvalet (v0.52.0) と完全一致のため低リスク。万一の差分は実装時に godoc 参照で吸収 |
| Streamable HTTP server の停止方法 (内部 goroutine リーク) | `srv.Shutdown(shutdownCtx)` で http.Server を止めれば handler 内 goroutine も終了。残留が見つかれば mcp-go の `WithSessionRegistry` 経由でクリーンアップ追加 (v0.52 で公開済み) |
| Lambda Web Adapter での挙動差異 | LWA は SIGTERM を投げて来るため、呼び出し側 (M24 cobra) が `signal.NotifyContext` で受け取り Run の ctx に伝える設計。transport 自身は ctx のみ扱うので影響なし |
| `/mcp` 以外の path (例 `/healthz`) を本マイルストーンで追加すべきか | スコープ外。M16 で authgate を入れる際に healthz と一緒に整える方が綺麗。M15 は MVP に絞る |
| port 8080 が CI で衝突 | テストはランダムポート (`127.0.0.1:0`) を使う。本番デフォルトの 8080 は M24 cobra 層でフラグ上書き可能とする |
| ReadHeaderTimeout が無い場合の gosec G114 警告 | 10s を固定で設定。将来 Option 化したくなったら `WithReadHeaderTimeout` を追加する余地を残す |
| `*xapi.Client` を nil で渡されたケース | M15 では問題なし (空サーバー)。M17 で必須化する際にバリデーション追加 |

## Acceptance Criteria

- [ ] `internal/mcp/server.go`, `internal/mcp/doc.go`, `internal/mcp/server_test.go` が追加されている
- [ ] `internal/transport/http/server.go`, `internal/transport/http/doc.go`, `internal/transport/http/server_test.go` が追加されている
- [ ] `go.mod` に `github.com/mark3labs/mcp-go v0.52.0` がある (`require` 直接依存)
- [ ] `go test -race -count=1 ./...` がすべて pass する
- [ ] `golangci-lint run ./...` の違反 0
- [ ] `go vet ./...` clean
- [ ] `go build -o /tmp/x ./cmd/x` 成功
- [ ] `initialize` リクエストへの応答テストが pass する
- [ ] context cancel での graceful shutdown テストが pass する
- [ ] 全公開シンボルに日本語 doc コメント
- [ ] commit: `feat(mcp): MCP サーバー雛形と Streamable HTTP transport を追加` + Plan フッター

## Implementation Order (TDD: Red → Green → Refactor)

1. **(Red 1)** `internal/mcp/server_test.go` を書く → コンパイルエラー
2. **(Green 1)** `internal/mcp/doc.go` + `internal/mcp/server.go` を書く → test pass
3. **(Red 2)** `internal/transport/http/server_test.go` を書く → コンパイルエラー
4. **(Green 2)** `internal/transport/http/doc.go` + `internal/transport/http/server.go` を書く → test pass
5. **(Refactor)** ServerName 定数化、Option 関数の整理、エラーメッセージ統一
6. **検証**: `go test -race -count=1 ./...` / `golangci-lint run ./...` / `go vet ./...` / `go build`
7. **commit**: Conventional Commits 日本語 + Plan フッター

## Open Questions

(本計画作成時点では全て解決済み)

- [x] `client` 引数を M15 で含めるか → 含める (M17 で API 変更を避けるため)
- [x] graceful shutdown の長さ → 30s (M14 ハンドオフ指定)
- [x] path / addr のデフォルト → `/mcp` / `127.0.0.1:8080` (スペック §5 暗黙、logvalet と揃える)
- [x] signal 監視を transport に含めるか → 含めない (ctx 一元化、cli 層で `signal.NotifyContext`)

## Handoff to Next Milestone

### M16 (authgate 基盤 + none モード) が知るべき情報

- `internal/transport/http/server.go` は現状 `http.ServeMux` を内部で組み立てている。authgate middleware を差し込むためには `Server.Run` 内で `mcpserver.NewStreamableHTTPServer` 出力を **任意の `http.Handler` で wrap できるフック** が必要。M16 では Option `WithHandlerMiddleware(func(http.Handler) http.Handler)` を追加する想定。
- `/healthz` 追加も M16 で行う (現状未実装)。
- `mcpserver.NewStreamableHTTPServer` の出力 handler は `s.path` (=`/mcp`) のみを受け持つため、`/healthz` は別 mux entry として `mux.HandleFunc("/healthz", ...)` を追加する。
