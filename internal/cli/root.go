// Package cli は x コマンドのルート (Cobra) を構築する。
//
// 本ファイルは NewRootCmd() factory のみを提供する。グローバル状態を持たないため
// テストでは複数の RootCmd を並行生成でき、本番では cmd/x/main.go から一度だけ
// 呼び出される。サブコマンド (`version`, `me`, `liked`, `mcp`, `configure` 等) は
// AddCommand で順次足していく方針。M3 以降では PersistentFlags
// (--format / --no-color / --config 等) を root.PersistentFlags() に追加する。
package cli

import (
	"github.com/spf13/cobra"

	"github.com/youyo/x/internal/version"
)

// NewRootCmd は x コマンドのルート *cobra.Command を生成する factory。
//
// Cobra が以下を自動提供することに依存する:
//   - `completion {bash,zsh,fish,powershell}` サブコマンド (HasSubCommands 真の時)
//   - `--version` フラグ (Version フィールドが非空の時)
//   - `__complete` 隠しサブコマンド (動的補完エンジン)
//   - `help` サブコマンド + `--help` フラグ
//
// SilenceUsage:true / SilenceErrors:true により RunE エラー時の Usage / err
// 自動出力を抑制し、cmd/x/main.go の run() で一元的にエラー → exit code を
// マッピングする責務分離を行う。
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "x",
		Short: "X (Twitter) API CLI & Remote MCP",
		Long: "x is a CLI / MCP wrapper for the X API v2.\n" +
			"See https://github.com/youyo/x for details.",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// --version の出力テンプレートを差し替える。
	// デフォルトでは "x version dev (commit: none, built: unknown)" のように
	// 冗長な接頭辞が付くため、Version 文字列単体 + 改行のみにする。
	root.SetVersionTemplate("{{.Version}}\n")

	root.AddCommand(newVersionCmd())
	root.AddCommand(newMeCmd())
	root.AddCommand(newLikedCmd())
	root.AddCommand(newConfigureCmd())
	// NOTE: completion サブコマンドは Cobra が自動追加する (HasAvailableSubCommands が真のため)。
	// NOTE: M3 で root.PersistentFlags().StringP("format","f","json",...) を追加予定。
	return root
}
