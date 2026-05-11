package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// likedHumanTextMaxRunes は --no-json 出力時の text フィールドの最大ルーン数である。
// この値を超えた場合は (likedHumanTextMaxRunes - 1) ルーン + "…" (U+2026) で truncate する。
//
// 80 は端末幅 80 桁前提 + 他フィールド (id / author / created) を考慮した実用的な上限。
// 仕様 (spec §6) に明示は無いため、CLI の独自判断として plans/x-m10-cli-liked-basic.md D-5 で確定。
const likedHumanTextMaxRunes = 80

// likedClient は newLikedListCmd が必要とする X API クライアントの最小インターフェイスである。
//
// 本番では *xapi.Client が GetUserMe / ListLikedTweets の両メソッドを実装するためそのまま満たす。
// テストでは httptest.Server に紐付いた実装を newLikedClient 経由で差し替えることで
// ネットワークアクセスなしに 各種シナリオを検証できる (M9 meClient と同じ流儀)。
//
// GetUserMe を含めている理由: --user-id 未指定時に self の数値 ID を解決する必要があるため
// (spec §6 "default: me" は self 解決を意味する。"me" 文字列を :id にそのまま渡しても X API は
// 受け付けない、plans/x-m10-cli-liked-basic.md D-2)。
type likedClient interface {
	GetUserMe(ctx context.Context, opts ...xapi.UserFieldsOption) (*xapi.User, error)
	ListLikedTweets(ctx context.Context, userID string, opts ...xapi.LikedTweetsOption) (*xapi.LikedTweetsResponse, error)
}

// newLikedClient は newLikedListCmd 内部で利用する likedClient の生成関数である。
//
// パッケージ変数 (var) として公開しているのはテストから t.Cleanup 経由で差し替えるためである。
// 本番では xapi.NewClient(ctx, creds) をそのまま返却する。
//
// 関数シグネチャに error を含めているのは将来 client 構築時にバリデーションが入る可能性を
// 想定した拡張余地である。M10 時点では常に nil error。
var newLikedClient = func(ctx context.Context, creds *config.Credentials) (likedClient, error) {
	return xapi.NewClient(ctx, creds), nil
}

// newLikedCmd は `x liked` 親コマンドを生成する factory である。
//
// 親コマンド自体は実処理を行わず help を表示するのみ。実体は `list` サブコマンド
// (newLikedListCmd) に委譲する。将来 M11 以降で `x liked count` などのサブコマンドを
// 増やす余地を残すために親コマンドを分離している。
func newLikedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "liked",
		Short: "manage X liked tweets",
		Long:  "Subcommands to retrieve liked tweets from the X API v2.",
	}
	cmd.AddCommand(newLikedListCmd())
	return cmd
}

// newLikedListCmd は `x liked list` サブコマンドを生成する factory である。
//
// 動作 (順序固定):
//  1. フラグ値を解釈し --max-results / --start-time / --end-time を検証する
//     - 不正値は cli.ErrInvalidArgument で wrap して返却 (exit 2)
//  2. LoadCredentialsFromEnvOrFile で env > credentials.toml の優先順位で認証情報を解決
//  3. newLikedClient で xapi クライアントを生成
//  4. --user-id 未指定なら GetUserMe で self の ID を取得して置換 (plans D-2)
//  5. xapi.LikedTweetsOption を組み立て ListLikedTweets を呼び出す
//  6. 出力:
//     - --no-json=false (default): *xapi.LikedTweetsResponse 全体を JSON エンコード (plans D-4)
//     - --no-json=true: resp.Data を 1 ツイート 1 行で human フォーマット (plans D-5)
//
// バリデーションを認証情報ロードより先に行う理由は plans D-7 を参照
// (引数エラーが先にユーザに見える、ファイル I/O を避ける)。
//
// エラーは wrap せず呼び出し側 (cmd/x/main.go run()) に伝搬する。番兵エラーは:
//   - cli.ErrInvalidArgument → exit 2
//   - xapi.ErrAuthentication → exit 3 (ErrCredentialsMissing 含む)
//   - xapi.ErrPermission     → exit 4
//   - xapi.ErrNotFound       → exit 5
func newLikedListCmd() *cobra.Command {
	var (
		userID          string
		startTime       string
		endTime         string
		maxResults      int
		paginationToken string
		noJSON          bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list liked tweets",
		Long: "List liked tweets for the authenticated user (or a specified user).\n" +
			"Credentials are loaded in the order: env (X_API_KEY/X_API_SECRET/X_ACCESS_TOKEN/X_ACCESS_TOKEN_SECRET) > credentials.toml.\n" +
			"By default outputs the full response ({data, includes, meta}) as JSON.\n" +
			"With --no-json prints one tweet per line as id=...\\tauthor=...\\tcreated=...\\ttext=...\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// 1. 引数バリデーション (plans D-7: 認証ロードより先)。
			if maxResults < 1 || maxResults > 100 {
				return fmt.Errorf("%w: --max-results must be in 1..100, got %d", ErrInvalidArgument, maxResults)
			}
			var startT, endT time.Time
			if startTime != "" {
				t, err := time.Parse(time.RFC3339, startTime)
				if err != nil {
					return fmt.Errorf("%w: --start-time: %v", ErrInvalidArgument, err)
				}
				startT = t
			}
			if endTime != "" {
				t, err := time.Parse(time.RFC3339, endTime)
				if err != nil {
					return fmt.Errorf("%w: --end-time: %v", ErrInvalidArgument, err)
				}
				endT = t
			}

			// 2. 認証情報ロード (env > file)。
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}

			// 3. クライアント生成。
			ctx := cmd.Context()
			client, err := newLikedClient(ctx, creds)
			if err != nil {
				return err
			}

			// 4. --user-id 未指定なら self を解決 (plans D-2)。
			targetUserID := userID
			if targetUserID == "" {
				user, err := client.GetUserMe(ctx)
				if err != nil {
					return err
				}
				targetUserID = user.ID
			}

			// 5. オプション組み立て。
			//    WithMaxResults は常に呼ぶ (0 を流さない、plans D-3)。
			opts := []xapi.LikedTweetsOption{
				xapi.WithMaxResults(maxResults),
			}
			if !startT.IsZero() {
				opts = append(opts, xapi.WithStartTime(startT))
			}
			if !endT.IsZero() {
				opts = append(opts, xapi.WithEndTime(endT))
			}
			if paginationToken != "" {
				opts = append(opts, xapi.WithPaginationToken(paginationToken))
			}

			resp, err := client.ListLikedTweets(ctx, targetUserID, opts...)
			if err != nil {
				return err
			}

			// 6. 出力。
			if noJSON {
				return writeLikedHuman(cmd, resp)
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
		},
	}
	cmd.Flags().StringVar(&userID, "user-id", "", "target user ID (default: authenticated user)")
	cmd.Flags().StringVar(&startTime, "start-time", "", "earliest tweet time in RFC3339 (e.g. 2026-05-11T15:00:00Z)")
	cmd.Flags().StringVar(&endTime, "end-time", "", "latest tweet time in RFC3339 (e.g. 2026-05-12T14:59:59Z)")
	cmd.Flags().IntVar(&maxResults, "max-results", 100, "max tweets per page (1..100)")
	cmd.Flags().StringVar(&paginationToken, "pagination-token", "", "resume from a previous page using next_token")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	return cmd
}

// writeLikedHuman は --no-json 出力時の human フォーマット出力を担う。
//
// フォーマット (plans D-5):
//   - 1 ツイート 1 行: `id=<id>\tauthor=<author_id>\tcreated=<created_at>\ttext=<text>`
//   - text は改行 (\n / \r) / タブ (\t) を半角スペースに置換し、80 ルーン超なら 79 ルーン + "…" で truncate
//   - 0 件のときは何も出さない (改行も出さない)
func writeLikedHuman(cmd *cobra.Command, resp *xapi.LikedTweetsResponse) error {
	if resp == nil || len(resp.Data) == 0 {
		return nil
	}
	for _, tw := range resp.Data {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatLikedHumanLine(tw)); err != nil {
			return err
		}
	}
	return nil
}

// formatLikedHumanLine は 1 ツイートを human 1 行に整形する。
// 改行 / タブを半角スペースに置換し、本文を truncateRunes で抑制する。
func formatLikedHumanLine(tw xapi.Tweet) string {
	text := sanitizeLikedText(tw.Text)
	text = truncateRunes(text, likedHumanTextMaxRunes)
	return fmt.Sprintf("id=%s\tauthor=%s\tcreated=%s\ttext=%s",
		tw.ID, tw.AuthorID, tw.CreatedAt, text)
}

// sanitizeLikedText は text 中の制御文字 (CR / LF / TAB) を半角スペースに置換する。
// 順序は \r\n → ' ' を先に処理してから個別の \r / \n / \t を処理することで、
// CRLF が 2 連続スペースになることを防ぐ。
func sanitizeLikedText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return s
}

// truncateRunes は s の UTF-8 ルーン数が maxRunes を超える場合に
// (maxRunes-1) ルーン + "…" で truncate する。
// maxRunes <= 0 のとき、または s が maxRunes ルーン以下のときはそのまま返す。
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	// (maxRunes-1) ルーンを取り出して "…" を付ける。
	runes := []rune(s)
	return string(runes[:maxRunes-1]) + "…"
}
