package mcp

import (
	"fmt"

	gomcp "github.com/mark3labs/mcp-go/mcp"
)

// toolResultJSON は任意の値を JSON シリアライズした CallToolResult を返す共通ヘルパ (M36)。
//
// mcp.NewToolResultJSON は StructuredContent (元の値) と TextContent (JSON 文字列) を
// 両方埋める。シリアライズ失敗時は IsError=true で fmt.Sprintf 形式のエラーメッセージを
// 含む CallToolResult を返す (protocol-level error は返さない、go-mcp 慣習)。
//
// toolName はエラーメッセージのコンテキストに使用する (例: "get_tweet")。
func toolResultJSON(v any, toolName string) (*gomcp.CallToolResult, error) {
	res, err := gomcp.NewToolResultJSON(v)
	if err != nil {
		return gomcp.NewToolResultError(
			fmt.Sprintf("marshal %s result: %v", toolName, err),
		), nil
	}
	return res, nil
}

// argStringSliceOptional は map[string]any から []string を取得する (M36)。
//
// セマンティクス:
//   - key 不在 / 値 nil / 空配列 → (nil, nil) — 呼び出し側で xapi.WithXxx を呼ばないことで「未指定」を表現
//   - 値が []any でその要素が string → 変換して返す
//   - 値が []any でその要素が string 以外 → 型違反 error
//   - 値が []any 以外の型 → 型違反 error
//
// argStringSliceOrDefault と異なり default fallback を持たない (各ツールの呼び出し側で
// "未指定なら省略" を決められるようにする)。
func argStringSliceOptional(args map[string]any, key string) ([]string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected array, got %T", key, v)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(raw))
	for i, e := range raw {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d]: expected string, got %T", key, i, e)
		}
		out = append(out, s)
	}
	return out, nil
}

// argStringSliceRequired は map[string]any から []string を取得する (必須版)。
//
// 必須なので空配列 / 不在ともに err を返さず空 slice を返す。呼び出し側で len チェックする
// (必須 vs 空配列 vs 不在を区別する責務は呼び出し側に委ねる)。
func argStringSliceRequired(args map[string]any, key string) ([]string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return []string{}, nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected array, got %T", key, v)
	}
	out := make([]string, 0, len(raw))
	for i, e := range raw {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d]: expected string, got %T", key, i, e)
		}
		out = append(out, s)
	}
	return out, nil
}
