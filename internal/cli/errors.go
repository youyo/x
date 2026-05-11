package cli

import "errors"

// ErrInvalidArgument は CLI 層で発生する引数バリデーション失敗を表す番兵エラーである。
//
// `cmd/x/main.go` の run() で `errors.Is(err, cli.ErrInvalidArgument)` を判定し、
// `app.ExitArgumentError` (2) に写像する。RunE 内で `fmt.Errorf("%w: ...", cli.ErrInvalidArgument, ...)`
// と wrap して返すことで、Cobra の "unknown command/flag" 文字列マッチでは捉えられない
// アプリ層のバリデーション失敗 (時刻フォーマット不正 / `--max-results` 範囲外など) を
// 一貫した exit code 2 に集約する責務を持つ (spec §6 エラーハンドリングポリシー)。
//
// 配置先を internal/app ではなく internal/cli にしている理由は plans/x-m10-cli-liked-basic.md
// D-1 を参照: 引数バリデーションは CLI 層固有の関心事であり、`internal/app` は exit code
// 定数の責務に絞っているため。
var ErrInvalidArgument = errors.New("invalid argument")
