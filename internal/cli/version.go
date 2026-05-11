// Package cli は x コマンドのサブコマンド群 (Cobra ベース) を提供する。
//
// 本ファイルは version サブコマンドを定義する。デフォルトでは JSON 出力 (M3 以降の
// 統一出力フォーマットへ向けた中間形態) で、`--no-json` フラグで human-readable な
// 単一行表示に切り替わる。M3 で `--format json|table|yaml` を PersistentFlags に
// 導入する予定で、その際 `--no-json` は alias として残す方針。
package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/youyo/x/internal/version"
)

// newVersionCmd は `x version` サブコマンドを生成する factory。
//
// グローバル状態を持たないようクロージャ内で noJSON フラグを保持する。
// 出力先は cmd.OutOrStdout() を経由するため、テストから cmd.SetOut() で
// 自由に差し替えられる。
func newVersionCmd() *cobra.Command {
	var noJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "display version information",
		Long: "Print the version, commit SHA, and build date of this binary.\n" +
			"By default outputs JSON; use --no-json for human-readable text.",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := version.NewInfo()
			if noJSON {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), version.String()); err != nil {
					return err
				}
				return nil
			}
			// json.Encoder.Encode は末尾に改行を付与するので Fprintln 不要。
			return json.NewEncoder(cmd.OutOrStdout()).Encode(info)
		},
	}
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}
