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
//   - 3 = auth error
//   - 4 = permission error
//   - 5 = not found
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/youyo/x/internal/app"
	"github.com/youyo/x/internal/cli"
)

// main は os.Exit に exit code を渡す唯一の責務を持つ。
// 実体は run() に委譲することでテスト時に exit code を検証できる。
func main() {
	os.Exit(run())
}

// run は CLI を実行し、終了コードを int で返す。
//
// Cobra の SilenceUsage / SilenceErrors を有効化しているため、エラー時には
// 本関数で stderr にメッセージを出力する。エラー種別は isArgumentError で判定し、
// 引数 / フラグエラーは ExitArgumentError、それ以外は ExitGenericError に分類する。
// M5 以降で X API エラーが入ってくる際は、エラー型 (xapi.AuthError 等) を
// errors.As で判定し ExitAuthError / ExitPermissionError / ExitNotFoundError へ
// マッピングを拡張する予定。
func run() int {
	root := cli.NewRootCmd()
	if err := root.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		if isArgumentError(err) {
			return app.ExitArgumentError
		}
		return app.ExitGenericError
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
