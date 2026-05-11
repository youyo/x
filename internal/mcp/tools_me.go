package mcp

import (
	"context"
	"fmt"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/youyo/x/internal/xapi"
)

// GetUserMeResult は MCP tool `get_user_me` の出力スキーマを表す DTO である。
//
// xapi.User の json タグ "id" を MCP 仕様 (docs/specs/x-spec.md §6) に合わせて
// "user_id" にリネームするための中間型。username / name はそのまま転送する。
// 本リネームは MCP 層内で完結し、CLI / xapi 層には影響しない。
type GetUserMeResult struct {
	// UserID は認証ユーザーの数値 ID (文字列表現)。xapi.User.ID と同値。
	UserID string `json:"user_id"`
	// Username は @ を除いたスクリーンネーム (例: "alice")。
	Username string `json:"username"`
	// Name は表示名 (X 上のプロフィール名)。
	Name string `json:"name"`
}

// NewGetUserMeHandler は MCP tool `get_user_me` のハンドラ関数を生成する。
//
// 登録 (registerToolMe) からハンドラ生成を分離している理由はテスト容易性である:
// httptest で X API をモックして本関数で得たハンドラを直接呼び出し、
// CallToolResult.IsError / StructuredContent を検証できる。
//
// 引数:
//   - client: X API クライアント。nil の場合、ハンドラ呼び出し時に IsError=true を返す
//     (パニックは起こさない)。
//
// ハンドラ挙動:
//   - 成功時: xapi.GetUserMe の結果を GetUserMeResult に変換し、
//     mcp.NewToolResultJSON で StructuredContent + TextContent (JSON 文字列) の
//     両形式を埋めた CallToolResult を返す。protocol-level error は返さない。
//   - 失敗時: mcp.NewToolResultError で IsError=true の CallToolResult を返す。
//     protocol-level error は返さない (go-mcp の慣習: 業務エラーは IsError で表現)。
func NewGetUserMeHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		if client == nil {
			return gomcp.NewToolResultError("xapi client is not configured"), nil
		}
		user, err := client.GetUserMe(ctx)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		result := GetUserMeResult{
			UserID:   user.ID,
			Username: user.Username,
			Name:     user.Name,
		}
		res, mErr := gomcp.NewToolResultJSON(result)
		if mErr != nil {
			// GetUserMeResult は string 3 フィールドのため実用上到達不能だが、
			// 防御的に保護する (linter / 将来のフィールド追加に備える)。
			return gomcp.NewToolResultError(
				fmt.Sprintf("marshal get_user_me result: %v", mErr),
			), nil
		}
		return res, nil
	}
}

// registerToolMe は `get_user_me` ツールを MCP サーバーに登録する。
//
// NewServer から呼ばれ、tool 定義 (gomcp.NewTool) とハンドラ
// (NewGetUserMeHandler) をセットで s.AddTool に渡す。
// 入力スキーマは空 (引数なし) のため Option 引数は description のみで十分。
//
// client は nil でも登録自体は成功する (ハンドラ実行時にしか参照されないため)。
// nil client での実運用は想定しないが、テストでの NewServer(nil, ...) 互換性のため許容する。
func registerToolMe(s *mcpserver.MCPServer, client *xapi.Client) {
	tool := gomcp.NewTool(
		"get_user_me",
		gomcp.WithDescription("認証済みユーザー (自分) の user_id / username / name を取得する。"),
	)
	s.AddTool(tool, NewGetUserMeHandler(client))
}
