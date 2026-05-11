// Command x は X (Twitter) API CLI 兼 Remote MCP サーバーのエントリーポイント。
//
// 本ファイルは Cobra ルートコマンドを生成し、エラー → exit code マッピングを
// 一元的に行う。テスト容易性のため run() を main() から分離している
// (os.Exit は main() のみで呼び、run() は int を返すだけ)。
//
// exit code 規約 (internal/app/exit.go 参照):
//   - 0 = success
//   - 1 = generic error
//   - 2 = argument / validation error
//   - 3 = auth error (X API 401 / 認証情報欠落)
//   - 4 = permission error (X API 403)
//   - 5 = not found (X API 404)
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/youyo/x/internal/app"
	"github.com/youyo/x/internal/cli"
	"github.com/youyo/x/internal/xapi"
)

// main は os.Exit に exit code を渡す唯一の責務を持つ。
// 実体は run() に委譲することでテスト時に exit code を検証できる。
func main() {
	os.Exit(run())
}

// run は CLI を実行し、終了コードを int で返す。
//
// Cobra の SilenceUsage / SilenceErrors を有効化しているため、エラー時には
// 本関数で stderr にメッセージを出力する。
//
// エラー → exit code 写像 (上から優先):
//  1. isArgumentError(err)                       → ExitArgumentError (2): Cobra 未知サブコマンド / フラグ
//  2. errors.Is(err, cli.ErrInvalidArgument)     → ExitArgumentError (2): RunE 内の引数バリデーション失敗
//  3. errors.Is(err, xapi.ErrAuthentication)     → ExitAuthError (3): 401 / 認証情報欠落
//  4. errors.Is(err, xapi.ErrPermission)         → ExitPermissionError (4): 403
//  5. errors.Is(err, xapi.ErrNotFound)           → ExitNotFoundError (5): 404
//  6. fallback                                   → ExitGenericError (1)
//
// 番兵エラーは xapi 層 (M6) と cli 層 (M9 ErrCredentialsMissing / M10 ErrInvalidArgument)
// で定義されており、CLI 層の ErrCredentialsMissing は xapi.ErrAuthentication を Unwrap で
// 内包しているため上記 3. の経路で exit 3 に写像される (plans/x-m09-cli-me.md D-1)。
// M10 で追加した ErrInvalidArgument は CLI 固有番兵で 2. の経路で exit 2 に写像される
// (plans/x-m10-cli-liked-basic.md D-1)。
func run() int {
	root := cli.NewRootCmd()
	if err := root.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		switch {
		case isArgumentError(err):
			return app.ExitArgumentError
		case errors.Is(err, cli.ErrInvalidArgument):
			return app.ExitArgumentError
		case errors.Is(err, xapi.ErrAuthentication):
			return app.ExitAuthError
		case errors.Is(err, xapi.ErrPermission):
			return app.ExitPermissionError
		case errors.Is(err, xapi.ErrNotFound):
			return app.ExitNotFoundError
		default:
			return app.ExitGenericError
		}
	}
	return app.ExitSuccess
}

// isArgumentError は Cobra のエラーメッセージプレフィックスから引数 / フラグエラーを判定する。
//
// Cobra v1 は型付きエラーを公開していないため、エラーメッセージの文字列マッチに依存する。
// 英語ロケール (LC_ALL=C 等) を前提とした実装である点に注意。
// 将来 Cobra が型付きエラーを導入した場合は errors.As ベースに切り替える。
func isArgumentError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.HasPrefix(msg, "unknown command") ||
		strings.HasPrefix(msg, "unknown flag") ||
		strings.HasPrefix(msg, "unknown shorthand")
}
