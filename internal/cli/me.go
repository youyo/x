package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// meClient は newMeCmd が必要とする X API クライアントの最小インターフェイスである。
//
// 本番では *xapi.Client がこれを満たす。テストでは httptest.Server に紐付いた
// 別の実装を newMeClient 経由で差し替えることで、ネットワークアクセスなしに
// 401 / 404 / 200 OK の各シナリオを検証できる。
//
// 将来 GetUserMe 以外のメソッドが必要になった場合は本 interface を拡張する。
type meClient interface {
	GetUserMe(ctx context.Context, opts ...xapi.UserFieldsOption) (*xapi.User, error)
}

// newMeClient は newMeCmd 内部で利用する meClient の生成関数である。
//
// パッケージ変数 (var) として公開しているのは、テストから t.Cleanup 経由で
// 差し替えるためである (plans/x-m09-cli-me.md D-2 を参照)。本番では
// xapi.NewClient(ctx, creds) をそのまま返却する。
//
// 関数シグネチャに error を含めているのは、将来 client 構築時にバリデーションが
// 入る可能性を想定した拡張余地である。M9 時点では常に nil error。
var newMeClient = func(ctx context.Context, creds *config.Credentials) (meClient, error) {
	return xapi.NewClient(ctx, creds), nil
}

// newMeCmd は `x me` サブコマンドを生成する factory である。
//
// 動作:
//  1. LoadCredentialsFromEnvOrFile で env > credentials.toml の優先順位で認証情報を解決
//  2. newMeClient で xapi クライアントを生成
//  3. GetUserMe を呼び出して認証ユーザーの User 情報を取得
//  4. --no-json なら `id=... username=... name=...` の 1 行を出力
//     それ以外なら {"id":"...","username":"...","name":"..."} を JSON で出力
//
// エラーは wrap せず呼び出し側 (cmd/x/main.go の run()) に伝搬する。run() 内で
// xapi.ErrAuthentication / ErrPermission / ErrNotFound を errors.Is で判別し
// exit code 3 / 4 / 5 に写像する。
//
// 出力先は cmd.OutOrStdout() を経由するため、テストでは cmd.SetOut() で
// 自由に差し替えられる (version サブコマンドと同じ流儀)。
func newMeCmd() *cobra.Command {
	var noJSON bool
	cmd := &cobra.Command{
		Use:   "me",
		Short: "print authenticated X user information",
		Long: "Print the authenticated user's id, username, and name as JSON.\n" +
			"Credentials are loaded in the order: env (X_API_KEY/X_API_SECRET/X_ACCESS_TOKEN/X_ACCESS_TOKEN_SECRET) > credentials.toml.\n" +
			"Exit codes: 0 success, 1 generic error, 3 authentication error, 4 permission error, 5 not found.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			client, err := newMeClient(ctx, creds)
			if err != nil {
				return err
			}
			user, err := client.GetUserMe(ctx)
			if err != nil {
				return err
			}
			if noJSON {
				_, err := fmt.Fprintf(
					cmd.OutOrStdout(),
					"id=%s username=%s name=%s\n",
					user.ID, user.Username, user.Name,
				)
				return err
			}
			// 出力フィールドを {id, username, name} に絞る (spec §6 の例と整合)。
			// User 構造体をそのまま Encode すると user.fields 未指定でも空フィールドが
			// 増減する余地があるため、固定スキーマで包む。
			payload := struct {
				ID       string `json:"id"`
				Username string `json:"username"`
				Name     string `json:"name"`
			}{
				ID:       user.ID,
				Username: user.Username,
				Name:     user.Name,
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(payload)
		},
	}
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}
