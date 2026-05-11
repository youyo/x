// Package app は x コマンド共通の exit code 定数群を提供する。
//
// 仕様 (docs/specs/x-spec.md §6 エラーハンドリングポリシー) で定義された
// 6 個の終了コードを定数として公開する。これにより呼び出し側 (cmd/x/main.go)
// は分類済みエラーを os.Exit に渡すだけでよく、Shell スクリプトからの判定も
// 安定する。M5 以降でエラー分類が増えた場合は本パッケージに追加する。
package app

// Exit code 定数群。
// X API のステータスコード (401 / 403 / 404) と CLI 利用上の代表的な失敗
// 種別を 1 対 1 で割り当て、呼び出し側はエラーから本定数のいずれかに
// 写像する責務を持つ。値は変更しないこと（ユーザーのスクリプト互換性のため）。
const (
	// ExitSuccess は正常終了を示す (0)。
	ExitSuccess = 0
	// ExitGenericError は分類不能な汎用エラーを示す (1)。
	ExitGenericError = 1
	// ExitArgumentError は引数 / フラグの検証エラーを示す (2)。
	ExitArgumentError = 2
	// ExitAuthError は認証エラー (X API 401 / idproxy 401) を示す (3)。
	ExitAuthError = 3
	// ExitPermissionError は権限エラー (X API 403) を示す (4)。
	ExitPermissionError = 4
	// ExitNotFoundError は対象未発見エラー (X API 404) を示す (5)。
	ExitNotFoundError = 5
)
