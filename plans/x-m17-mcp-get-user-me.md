# M17: MCP tool `get_user_me` 実装計画

> Layer 2: M17 詳細計画。M15 (MCP サーバー雛形) / M16 (authgate 基盤) を前提とし、最初の MCP tool として `get_user_me` を追加する。

## Context

- スペック: `docs/specs/x-spec.md` §6 MCP tools `get_user_me`
- 入力: なし
- 出力: `{ "user_id": "...", "username": "...", "name": "..." }`
- 既存資産:
  - `internal/xapi/users.go` の `*Client.GetUserMe(ctx, ...)` (M7 完了)
  - `internal/mcp/server.go` の `NewServer(client *xapi.Client, version string)` (M15 完了)
  - `mark3labs/mcp-go` v0.52.0
- **重要**: `xapi.User.ID` の json タグは `"id"` だが MCP 出力では `"user_id"` にリネームする (DTO 中間型で吸収)

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M17 |
| ステータス | 進行中 |
| 作成日 | 2026-05-12 |
| 依存 | M15 (NewServer), M7 (GetUserMe) |
| 後続 | M18 (`get_liked_tweets`) |

## Design Decisions

### D-1: ハンドラ生成を独立関数に分離
`registerToolMe` のクロージャに埋めず、`newGetUserMeHandler(client *xapi.Client) server.ToolHandlerFunc` として切り出す。

- 理由: テストでハンドラを直接呼べる (httptest で X API モック → handler 呼び出し → IsError/StructuredContent 検証)
- 整合性: `cli/me_test.go` の `stubMeClientFactory` パターンと同じ "testability first" 方針

### D-2: 出力 DTO `GetUserMeResult`
`xapi.User.ID` の json タグは `"id"` だが MCP 仕様では `"user_id"`。中間 DTO を `internal/mcp/tools_me.go` 内に置いて変換する。

```go
// GetUserMeResult は MCP tool `get_user_me` の出力スキーマを表す。
// xapi.User の "id" フィールドを MCP 仕様 (§6) に合わせて "user_id" にリネームする。
type GetUserMeResult struct {
    UserID   string `json:"user_id"`
    Username string `json:"username"`
    Name     string `json:"name"`
}
```

理由: User.ID の json タグを変えると CLI / xapi 層に影響。MCP 層内で吸収するのが最小影響。

### D-3: `mcp.NewToolResultJSON` を使う
v0.52.0 の `NewToolResultJSON[T any](data T) (*CallToolResult, error)` は `StructuredContent` と `TextContent` の両方を埋める。MCP クライアント側で構造化アクセスが可能。

- 失敗時 (`json.Marshal` エラー、ただし string-only struct では実用上不発): `mcp.NewToolResultErrorf("marshal failed: %v", err)` でフォールバック
- ハンドラ戻り値: `(*mcp.CallToolResult, error)` — go-mcp の慣習では **error を返さず IsError=true の result を返す** のが標準 (server 側がプロトコルレベル error と区別するため)

### D-4: エラーは IsError=true CallToolResult で返却
xapi 呼び出しが失敗した場合 (`xapi.GetUserMe` がエラー):
- `mcp.NewToolResultError(err.Error())` で IsError=true として返却
- ハンドラ関数自体は `(result, nil)` を返す (protocol-level error と業務 error を区別する go-mcp ベストプラクティス)
- 番兵エラー (`xapi.ErrAuthentication` 等) の細分は M17 単独ではせず、後続マイルストーン (M19+ apikey/idproxy) で MCP `_meta.error` 規約に統合する余地を残す

### D-5: tool description は実態に即した完全形にする
スペックに従い「認証済みユーザー (自分) の user_id / username / name を取得」とする。Routine プロンプト側がこの description を読むので情報量が効く。

### D-6: 入力スキーマは空
スペック §6 で「入力: なし」のため `mcp.NewTool("get_user_me", mcp.WithDescription(...))` のみで Option 不要。

### D-7: パッケージ doc は doc.go 1 ファイル集約
スペック上の方針 (パッケージ doc は doc.go) を踏襲。`tools_me.go` 冒頭にはパッケージドキュメントを書かない。

### D-8: client nil 時の挙動
`registerToolMe(s, client)` は client が nil でも panic しない (登録時は参照しない、ハンドラ実行時のみ参照)。
- 既存テスト `TestNewServer_NotNil` (client=nil で NewServer を呼ぶ) を壊さない
- ハンドラ実行時に nil client が呼ばれるケースは本番では発生しない (`x mcp` サブコマンド (M24) で必ず非 nil 注入)

## Implementation Plan

### Step 1: Red — テスト作成
`internal/mcp/tools_me_test.go` を作成。`package mcp_test` (external test) で書く。

#### テストケース

| # | 名前 | 検証内容 |
|---|---|---|
| 1 | `TestGetUserMeHandler_Success_StructuredContent` | 200 + 正常 JSON で IsError=false / StructuredContent が `GetUserMeResult{user_id, username, name}` を含む |
| 2 | `TestGetUserMeHandler_Success_TextContent` | 同上、`Content[0].(TextContent).Text` が `"user_id"` を含む JSON 文字列 |
| 3 | `TestGetUserMeHandler_Error_AuthFailed_401` | 401 → IsError=true / Content にエラーメッセージ |
| 4 | `TestGetUserMeHandler_Error_NetworkFailure` | httptest を Close() してから呼ぶ → IsError=true |
| 5 | `TestRegisterToolMe_ToolListed` | NewServer + registerToolMe で `ListTools` 経由で "get_user_me" が登録される (テスト容易性: NewServer → registerToolMe を内部 export で確認できる方針なら直接、難しければ ServerTool 経由) |
| 6 | `TestGetUserMeResult_JSONShape` | DTO 単体: `json.Marshal(GetUserMeResult{...})` の出力に `"user_id"` キーが含まれる |

#### テスト戦略

```go
// テストヘルパ (tools_me_test.go 内に定義)
func newTestServer(t *testing.T, handler http.HandlerFunc) (*xapi.Client, func()) {
    t.Helper()
    srv := httptest.NewServer(handler)
    cl := xapi.NewClient(context.Background(), nil, xapi.WithBaseURL(srv.URL))
    return cl, srv.Close
}

// ハンドラ直接呼び出し
h := mcpinternal.NewGetUserMeHandler(client)  // ← 公開ヘルパ (テスト用にも export)
res, err := h(ctx, mcp.CallToolRequest{})
```

`NewGetUserMeHandler` を export するか、それとも `package mcp` の internal test (tools_me_internal_test.go) で書くか。
- **採用**: external test (`package mcp_test`) + `NewGetUserMeHandler` を export。理由は (i) tests も Routine 利用者と同じ視点で見える、(ii) M18 (likes) でも同じパターンを取れる、(iii) 関数を export することの抽象漏洩は handler factory なので軽微

ツール登録確認 (#5) は `s.ListTools(ctx)` 相当が外部に無いため、`registerToolMe` を呼んだ後 `s.HandleMessage(ctx, raw)` で `tools/list` リクエストを投げて検証するのが厳密。が、これは煩雑なので **registerToolMe の存在自体は #1-#4 で間接検証** とし、#5 は省略する。

### Step 2: Green — 実装
`internal/mcp/tools_me.go` を新規作成:

```go
package mcp

import (
    "context"
    "fmt"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"

    "github.com/youyo/x/internal/xapi"
)

// GetUserMeResult は MCP tool `get_user_me` の出力スキーマを表す DTO である。
//
// xapi.User の json タグ "id" を MCP 仕様 (docs/specs/x-spec.md §6) に合わせて
// "user_id" にリネームするための中間型。username / name はそのまま転送する。
type GetUserMeResult struct {
    UserID   string `json:"user_id"`
    Username string `json:"username"`
    Name     string `json:"name"`
}

// NewGetUserMeHandler は `get_user_me` ツールのハンドラを生成する。
//
// テスト容易性のため、登録 (registerToolMe) からハンドラ生成を分離している。
// テストでは本関数を直接呼んでハンドラを単体検証する。
//
// 引数:
//   - client: X API クライアント。nil の場合ハンドラ呼び出し時に IsError=true を返す。
//
// ハンドラ挙動:
//   - 成功時: xapi.GetUserMe の結果を GetUserMeResult に変換し
//     mcp.NewToolResultJSON で StructuredContent + TextContent 両形式で返却
//   - 失敗時: mcp.NewToolResultError で IsError=true を返却 (ハンドラ戻り値 error は nil)
func NewGetUserMeHandler(client *xapi.Client) server.ToolHandlerFunc {
    return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        if client == nil {
            return mcp.NewToolResultError("xapi client is not configured"), nil
        }
        user, err := client.GetUserMe(ctx)
        if err != nil {
            return mcp.NewToolResultError(err.Error()), nil
        }
        result := GetUserMeResult{
            UserID:   user.ID,
            Username: user.Username,
            Name:     user.Name,
        }
        res, mErr := mcp.NewToolResultJSON(result)
        if mErr != nil {
            return mcp.NewToolResultError(fmt.Sprintf("marshal get_user_me result: %v", mErr)), nil
        }
        return res, nil
    }
}

// registerToolMe は `get_user_me` ツールを MCP サーバーに登録する。
//
// NewServer から呼ばれ、tool 定義 (mcp.NewTool) + ハンドラ (NewGetUserMeHandler) を
// セットで s.AddTool に渡す。入力スキーマは空 (引数なし) なので
// WithString 等の Option は不要。
func registerToolMe(s *server.MCPServer, client *xapi.Client) {
    tool := mcp.NewTool(
        "get_user_me",
        mcp.WithDescription("認証済みユーザー (自分) の user_id / username / name を取得する。"),
    )
    s.AddTool(tool, NewGetUserMeHandler(client))
}
```

`internal/mcp/server.go` の修正:
```go
func NewServer(client *xapi.Client, version string) *mcpserver.MCPServer {
    s := mcpserver.NewMCPServer(
        ServerName,
        version,
        mcpserver.WithToolCapabilities(true),
    )
    registerToolMe(s, client)
    return s
}
```

`_ = client` は削除。

### Step 3: Refactor
- 公開シンボル (GetUserMeResult / NewGetUserMeHandler) の doc コメントを日本語で精緻化
- パッケージ doc (doc.go) の M17 反映: `tools_me.go` で `get_user_me` を実装したことを追記

### Step 4: 動作確認
```bash
go test -race -count=1 ./...    # 全 pass
go vet ./...                     # clean
golangci-lint run ./...          # 0 issues
go build -o /tmp/x ./cmd/x       # 成功
```

### Step 5: Commit
```
feat(mcp): get_user_me ツールを追加 (user_id リネーム対応)

- internal/mcp/tools_me.go: GetUserMeResult DTO + NewGetUserMeHandler + registerToolMe
- internal/mcp/server.go: NewServer 内で registerToolMe(s, client) を呼ぶ
- internal/mcp/doc.go: M17 反映
- TDD: 成功 / 401 / network error / nil client / JSON shape の 5 ケース

xapi.User.ID の json タグ "id" を MCP 仕様 §6 に合わせて "user_id" に
リネームする中間 DTO を導入し、xapi / cli 層への影響を回避した。

Plan: plans/x-m17-mcp-get-user-me.md
```

## Risks

| リスク | 影響 | 対策 |
|---|---|---|
| `mcp.NewToolResultJSON` のジェネリック API が他の使い方を要求 | 中 | 既に v0.52.0 のソースで `(data T) (*CallToolResult, error)` 確認済み |
| `server.ToolHandlerFunc` シグネチャ違い | 中 | v0.52.0 で `func(ctx, req mcp.CallToolRequest) (*mcp.CallToolResult, error)` 確認済み |
| client が nil で panic | 低 | ハンドラ内で nil ガード追加 (D-8) |
| User.ID リネーム漏れで CLI 既存テスト破壊 | 高 | xapi.User の構造は変更しない (中間 DTO で吸収)。既存 cli/me_test.go の `"id"` テストはそのまま pass する |

## Test Plan

### 検証コマンド

```bash
cd /Users/youyo/src/github.com/youyo/x
go test -race -count=1 ./internal/mcp/...
go test -race -count=1 ./...
go vet ./...
golangci-lint run ./...
go build -o /tmp/x ./cmd/x
```

### 期待結果

- 既存 30+ test 全 pass
- 新規 4-5 test 全 pass
- lint 0 issues
- build success

## TDD Loop

1. **Red**: tools_me_test.go を書く → `go test ./internal/mcp/...` → コンパイルエラー (実装無し)
2. **Green**: tools_me.go を書く → server.go 修正 → `go test ./internal/mcp/...` 全 pass
3. **Refactor**: doc コメント精緻化、doc.go 反映 → 再テスト pass 確認
4. **Lint/Build**: golangci-lint + go vet + go build → 全 clean
5. **Commit**: feat(mcp): get_user_me ツールを追加 (user_id リネーム対応)

## Future Work (M17 範囲外)

- M18: `get_liked_tweets` で同じ registerToolXxx + NewXxxHandler パターンを踏襲
- M19+: 番兵エラー (ErrAuthentication / ErrPermission / ErrNotFound / ErrRateLimit) を MCP `_meta.error` の `code` に展開する規約導入
