package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// likedAPIMinMaxResults は X API が `/2/users/:id/liked_tweets` の max_results に課す下限値である。
// CLI 側で n<5 を指定された場合は 5 を投げ、レスポンスを `[:n]` で絞る (M29 D-2)。
const likedAPIMinMaxResults = 5

// loadLikedDefaults は `x liked list` の各フラグデフォルト値を解決して返す (M12)。
//
// 解決順 (spec §11 優先順位、env > config.toml > 組み込みデフォルト の文脈で
// 本マイルストーンは config.toml > 組み込みデフォルト 部分のみを担う):
//  1. `config.DefaultCLIConfigPath()` でパス解決
//  2. `config.LoadCLI(path)` で `[liked]` セクションを取得
//     - パス解決 / 読み込み失敗時は `log.Printf` で warning し、`config.DefaultCLIConfig()`
//     (spec §11 のテンプレ値) にフォールバック (D-8: best-effort 方針)
//     - LoadCLI 自身がファイル不在を ErrNotExist で握り潰し DefaultCLIConfig 相当を返すため、
//     通常運用 (まだ configure していないユーザ) では warning は出ない
//  3. LoadCLI 内部の applyDefaults により空フィールドは spec デフォルト値で補完済
//
// 環境変数 (`X_LIKED_*` 等) は spec §11 環境変数一覧に存在しないため対象外。
//
// 返却値は順に: tweet.fields / expansions / user.fields / max-pages。文字列は CSV、
// 数値は --max-pages のデフォルト値 (--all 指定時のページ上限) として使う。
func loadLikedDefaults() (tweetFields, expansions, userFields string, maxPages int) {
	fallback := config.DefaultCLIConfig().Liked
	path, err := config.DefaultCLIConfigPath()
	if err != nil {
		log.Printf("warning: cannot resolve config.toml path (using built-in defaults): %v", err)
		return fallback.DefaultTweetFields, fallback.DefaultExpansions, fallback.DefaultUserFields, fallback.DefaultMaxPages
	}
	cfg, err := config.LoadCLI(path)
	if err != nil {
		log.Printf("warning: cannot load config.toml at %s (using built-in defaults): %v", path, err)
		return fallback.DefaultTweetFields, fallback.DefaultExpansions, fallback.DefaultUserFields, fallback.DefaultMaxPages
	}
	return cfg.Liked.DefaultTweetFields, cfg.Liked.DefaultExpansions, cfg.Liked.DefaultUserFields, cfg.Liked.DefaultMaxPages
}

// likedOutputMode は --no-json / --ndjson フラグから決定される出力モードである。
//
// 排他関係 (D-1): --no-json と --ndjson は同時指定できない。
// 既定 (両 false) は likedOutputModeJSON で *xapi.LikedTweetsResponse 全体を 1 JSON で出力する。
type likedOutputMode int

const (
	likedOutputModeJSON   likedOutputMode = iota // 既定 (両 false): {data, includes, meta} を単一 JSON
	likedOutputModeHuman                         // --no-json: 1 行/ツイートの human フォーマット
	likedOutputModeNDJSON                        // --ndjson: 1 ツイート 1 行 JSON
)

// likedClient は newLikedListCmd が必要とする X API クライアントの最小インターフェイスである。
//
// 本番では *xapi.Client が GetUserMe / ListLikedTweets / EachLikedPage を実装するためそのまま満たす。
// テストでは httptest.Server に紐付いた実装を newLikedClient 経由で差し替えることで
// ネットワークアクセスなしに 各種シナリオを検証できる (M9 meClient と同じ流儀)。
//
// GetUserMe を含めている理由: --user-id 未指定時に self の数値 ID を解決する必要があるため
// (spec §6 "default: me" は self 解決を意味する。"me" 文字列を :id にそのまま渡しても X API は
// 受け付けない、plans/x-m10-cli-liked-basic.md D-2)。
//
// EachLikedPage は --all 時の next_token 自動辿り (M11) で利用する。
type likedClient interface {
	GetUserMe(ctx context.Context, opts ...xapi.UserFieldsOption) (*xapi.User, error)
	ListLikedTweets(ctx context.Context, userID string, opts ...xapi.LikedTweetsOption) (*xapi.LikedTweetsResponse, error)
	EachLikedPage(ctx context.Context, userID string, fn func(*xapi.LikedTweetsResponse) error, opts ...xapi.LikedTweetsOption) error
}

// newLikedClient は newLikedListCmd 内部で利用する likedClient の生成関数である。
//
// パッケージ変数 (var) として公開しているのはテストから t.Cleanup 経由で差し替えるためである。
// 本番では xapi.NewClient(ctx, creds) をそのまま返却する。
//
// 関数シグネチャに error を含めているのは将来 client 構築時にバリデーションが入る可能性を
// 想定した拡張余地である。M10/M11 時点では常に nil error。
var newLikedClient = func(ctx context.Context, creds *config.Credentials) (likedClient, error) {
	return xapi.NewClient(ctx, creds), nil
}

// newLikedCmd は `x liked` 親コマンドを生成する factory である。
//
// 親コマンド自体は実処理を行わず help を表示するのみ。実体は `list` サブコマンド
// (newLikedListCmd) に委譲する。将来 M12 以降で `x liked count` などのサブコマンドを
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
//  1. フラグ値を解釈・検証する (--max-results / 時刻文字列 / 排他フラグ等)
//     - 不正値は cli.ErrInvalidArgument で wrap して返却 (exit 2)
//  2. 時間窓を決定する: --yesterday-jst > --since-jst > --start-time/--end-time (D-2)
//  3. LoadCredentialsFromEnvOrFile で env > credentials.toml の優先順位で認証情報を解決
//  4. newLikedClient で xapi クライアントを生成
//  5. --user-id 未指定なら GetUserMe で self の ID を取得して置換
//  6. xapi.LikedTweetsOption を組み立て:
//     - --all=true 時のみ WithMaxPages を追加
//     - tweet/expansions/user fields は CLI 既定値または明示指定の csv を反映
//  7. 取得:
//     - --all=false (default): ListLikedTweets (単一ページ)
//     - --all=true: EachLikedPage で next_token 自動辿り
//  8. 出力 (likedOutputMode で分岐):
//     - JSON (既定): 集約レスポンスを単一 JSON、--all 時は meta を再構築 (D-8)
//     - Human (--no-json): 1 行/ツイート、改行/タブ正規化 + 80 ルーン truncate
//     - NDJSON (--ndjson): 1 ツイート 1 行 JSON、HTML エスケープ無効 (D-12)
//
// バリデーションを認証情報ロードより先に行う理由は plans D-7 を参照
// (引数エラーが先にユーザに見える、ファイル I/O を避ける)。
//
// エラーは wrap せず呼び出し側 (cmd/x/main.go run()) に伝搬する。番兵エラーは:
//   - cli.ErrInvalidArgument → exit 2
//   - xapi.ErrAuthentication → exit 3 (ErrCredentialsMissing 含む)
//   - xapi.ErrPermission     → exit 4
//   - xapi.ErrNotFound       → exit 5
//
//nolint:gocyclo // CLI コマンドのフラグ処理は分岐が多いが手続き的に追える流れに揃えている
func newLikedListCmd() *cobra.Command {
	// M12: config.toml [liked] からデフォルト値を解決する (失敗時は spec デフォルトにフォールバック)。
	dfTweetFields, dfExpansions, dfUserFields, dfMaxPages := loadLikedDefaults()

	var (
		userID          string
		startTime       string
		endTime         string
		sinceJST        string
		yesterdayJST    bool
		maxResults      int
		paginationToken string
		all             bool
		maxPages        int
		noJSON          bool
		ndjson          bool
		tweetFields     string
		expansions      string
		userFields      string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list liked tweets",
		Long: "List liked tweets for the authenticated user (or a specified user).\n" +
			"Credentials are loaded in the order: env (X_API_KEY/X_API_SECRET/X_ACCESS_TOKEN/X_ACCESS_TOKEN_SECRET) > credentials.toml.\n" +
			"By default outputs the full response ({data, includes, meta}) as JSON.\n" +
			"--no-json prints one tweet per line as id=...\\tauthor=...\\tcreated=...\\ttext=...\n" +
			"--ndjson prints one JSON object per tweet (line-delimited, HTML escape disabled).\n" +
			"--since-jst YYYY-MM-DD or --yesterday-jst override --start-time/--end-time.\n" +
			"--all auto-follows next_token up to --max-pages (default 50).\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth, 4 permission, 5 not found.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// 1. 引数バリデーション (plans D-7: 認証ロードより先)。
			if maxResults < 1 || maxResults > 100 {
				return fmt.Errorf("%w: --max-results must be in 1..100, got %d", ErrInvalidArgument, maxResults)
			}
			// M29 D-11: --all と --max-results 1..4 の組み合わせは UX 混乱を避けるため拒否。
			// X API は per-page 5 件以上を要求するため、補正すると「1 件しか要らない」意図と
			// 実挙動 (5×N) が乖離する。--all 無しの単一ページ補正のみサポート (D-2)。
			if all && maxResults < likedAPIMinMaxResults {
				return fmt.Errorf("%w: --max-results 1..4 cannot be combined with --all (X API per-page minimum is 5)", ErrInvalidArgument)
			}
			outMode, err := decideOutputMode(noJSON, ndjson)
			if err != nil {
				return err
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
			// 2. 時間窓決定 (--yesterday-jst > --since-jst > --start-time/--end-time, D-2)。
			switch {
			case yesterdayJST:
				s, e, err := yesterdayJSTRange(time.Now())
				if err != nil {
					return err
				}
				startT, endT = s, e
			case sinceJST != "":
				s, e, err := parseJSTDate(sinceJST)
				if err != nil {
					return err
				}
				startT, endT = s, e
			}

			// 3. 認証情報ロード (env > file)。
			creds, err := LoadCredentialsFromEnvOrFile()
			if err != nil {
				return err
			}

			// 4. クライアント生成。
			ctx := cmd.Context()
			client, err := newLikedClient(ctx, creds)
			if err != nil {
				return err
			}

			// 5. --user-id 未指定なら self を解決 (plans D-2)。
			targetUserID := userID
			if targetUserID == "" {
				user, err := client.GetUserMe(ctx)
				if err != nil {
					return err
				}
				targetUserID = user.ID
			}

			// 6. オプション組み立て。
			// M29 D-2: --max-results 1..4 は X API 下限 (5) を投げて応答を slice する。
			// --all 時はそもそも上で拒否済 (D-11) なので、ここに来るのは --all=false のみ。
			effectiveMaxResults := maxResults
			truncateTo := 0
			if maxResults < likedAPIMinMaxResults {
				effectiveMaxResults = likedAPIMinMaxResults
				truncateTo = maxResults
			}
			opts := []xapi.LikedTweetsOption{
				xapi.WithMaxResults(effectiveMaxResults),
			}
			if !startT.IsZero() {
				opts = append(opts, xapi.WithStartTime(startT))
			}
			if !endT.IsZero() {
				opts = append(opts, xapi.WithEndTime(endT))
			}
			if paginationToken != "" {
				if all {
					// D-5: --all 時の --pagination-token は警告して無視 (stderr に 1 行)。
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
						"warning: --pagination-token is ignored when --all is set")
				} else {
					opts = append(opts, xapi.WithPaginationToken(paginationToken))
				}
			}
			if fs := splitCSV(tweetFields); len(fs) > 0 {
				opts = append(opts, xapi.WithTweetFields(fs...))
			}
			if fs := splitCSV(expansions); len(fs) > 0 {
				opts = append(opts, xapi.WithExpansions(fs...))
			}
			if fs := splitCSV(userFields); len(fs) > 0 {
				opts = append(opts, xapi.WithLikedUserFields(fs...))
			}
			if all {
				opts = append(opts, xapi.WithMaxPages(maxPages))
			}

			// 7. 取得 & 8. 出力 (--all と outMode の組み合わせで分岐)。
			if !all {
				resp, err := client.ListLikedTweets(ctx, targetUserID, opts...)
				if err != nil {
					return err
				}
				// M29 D-2: 下限補正があれば応答を slice する。
				if truncateTo > 0 && resp != nil && len(resp.Data) > truncateTo {
					resp.Data = resp.Data[:truncateTo]
					resp.Meta.ResultCount = truncateTo
				}
				return writeLikedSinglePage(cmd, resp, outMode)
			}
			return runLikedAll(cmd, client, ctx, targetUserID, opts, outMode)
		},
	}
	cmd.Flags().StringVar(&userID, "user-id", "", "target user ID (default: authenticated user)")
	cmd.Flags().StringVar(&startTime, "start-time", "", "earliest tweet time in RFC3339 (e.g. 2026-05-11T15:00:00Z)")
	cmd.Flags().StringVar(&endTime, "end-time", "", "latest tweet time in RFC3339 (e.g. 2026-05-12T14:59:59Z)")
	cmd.Flags().StringVar(&sinceJST, "since-jst", "", "JST date YYYY-MM-DD (overrides --start-time/--end-time)")
	cmd.Flags().BoolVar(&yesterdayJST, "yesterday-jst", false, "fetch the previous JST day (overrides --since-jst)")
	cmd.Flags().IntVar(&maxResults, "max-results", 100, "max tweets per page (1..100)")
	cmd.Flags().StringVar(&paginationToken, "pagination-token", "", "resume from a previous page using next_token")
	cmd.Flags().BoolVar(&all, "all", false, "auto-follow next_token until end or --max-pages")
	cmd.Flags().IntVar(&maxPages, "max-pages", dfMaxPages, "max pages to fetch when --all is set")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "output human-readable text instead of JSON")
	cmd.Flags().BoolVar(&ndjson, "ndjson", false, "output line-delimited JSON (one tweet per line)")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", dfTweetFields, "comma-separated tweet.fields")
	cmd.Flags().StringVar(&expansions, "expansions", dfExpansions, "comma-separated expansions")
	cmd.Flags().StringVar(&userFields, "user-fields", dfUserFields, "comma-separated user.fields")
	return cmd
}

// decideOutputMode は --no-json / --ndjson から出力モードを決定する (D-1)。
//
// 両 true は ErrInvalidArgument で拒否する (排他制約)。
// 既定 (両 false) は likedOutputModeJSON。
func decideOutputMode(noJSON, ndjson bool) (likedOutputMode, error) {
	if noJSON && ndjson {
		return likedOutputModeJSON, fmt.Errorf("%w: --no-json and --ndjson are mutually exclusive", ErrInvalidArgument)
	}
	switch {
	case ndjson:
		return likedOutputModeNDJSON, nil
	case noJSON:
		return likedOutputModeHuman, nil
	default:
		return likedOutputModeJSON, nil
	}
}

// jstLocation は JST (Asia/Tokyo) の *time.Location を返す (D-3)。
//
// `time.LoadLocation("Asia/Tokyo")` をまず試み、失敗時は固定 +9:00 オフセットへフォールバック
// (zoneinfo データ未配置の minimal Linux / distroless / Lambda 等の保険)。
func jstLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Tokyo"); err == nil {
		return loc
	}
	return time.FixedZone("JST", 9*3600)
}

// parseJSTDate は YYYY-MM-DD 形式の JST 日付を 0:00:00〜23:59:59 (JST) の time.Time 2 つで返す (D-2/D-4)。
//
// 戻り値の time.Time は内部表現は JST のままだが、xapi.WithStartTime/WithEndTime 内で
// `.UTC().Format(time.RFC3339)` が呼ばれるため最終的なクエリは UTC 表現になる。
// 例: --since-jst 2026-05-12 → start=2026-05-12T00:00:00+09:00 / end=2026-05-12T23:59:59+09:00
// → UTC: 2026-05-11T15:00:00Z / 2026-05-12T14:59:59Z
//
// パース失敗時は ErrInvalidArgument を wrap して返却する (exit 2)。
func parseJSTDate(s string) (start, end time.Time, err error) {
	jst := jstLocation()
	day, err := time.ParseInLocation("2006-01-02", s, jst)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: --since-jst: %v", ErrInvalidArgument, err)
	}
	start = day
	end = day.Add(24*time.Hour - time.Second)
	return start, end, nil
}

// yesterdayJSTRange は与えられた基準時刻を JST に変換した上で「前日」の 0:00-23:59 範囲を返す。
//
// 基準時刻 (now) を JST に変換し、その JST 日付の前日に対して parseJSTDate を呼び出す。
// テスト時に固定時刻を渡せるよう引数化している (CLI からは time.Now() を渡す)。
func yesterdayJSTRange(now time.Time) (start, end time.Time, err error) {
	jst := jstLocation()
	y := now.In(jst).AddDate(0, 0, -1).Format("2006-01-02")
	return parseJSTDate(y)
}

// splitCSV は csv 文字列を要素配列に分解する (D-9)。
//
// 要素ごとに strings.TrimSpace を適用し、結果が空文字列となる要素は除外する。
// 入力が空文字列のときは長さ 0 の slice を返す。
//
// 注意 (D-10): pflag の `--tweet-fields ""` (明示空文字) と「フラグ未指定」(デフォルト値)
// を区別する手段はない。実用上どちらでも CLI 側のデフォルト挙動として問題ないため
// 区別しない方針 (M12 で config.toml 連携追加時に再評価する余地あり)。
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// writeLikedSinglePage は --all=false 時の出力を担う共通ヘルパ。
//
// outMode に応じて:
//   - JSON (既定): json.Encoder.Encode(resp) で {data, includes, meta} 全体を出力
//   - Human: writeLikedHuman で 1 行/ツイート
//   - NDJSON: resp.Data の各要素を 1 行 JSON で順次出力 (HTML エスケープ無効)
func writeLikedSinglePage(cmd *cobra.Command, resp *xapi.LikedTweetsResponse, outMode likedOutputMode) error {
	switch outMode {
	case likedOutputModeHuman:
		return writeLikedHuman(cmd, resp)
	case likedOutputModeNDJSON:
		if resp == nil {
			return nil
		}
		return writeNDJSONTweets(cmd.OutOrStdout(), resp.Data)
	default: // likedOutputModeJSON or unknown → JSON 既定
		return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
	}
}

// runLikedAll は --all=true 時の取得 + 出力を担う共通ヘルパ。
//
// 出力モードで挙動を切替:
//   - NDJSON: ストリーミング (callback 内で逐次 encode、集約しない、D-6)
//   - JSON / Human: 全ページ集約して最後にまとめて出力 (D-8: meta は再構築)
//
//nolint:revive // ctx を引数に取るが、引数順は cobra の RunE 流儀に合わせる
func runLikedAll(
	cmd *cobra.Command,
	client likedClient,
	ctx context.Context,
	userID string,
	opts []xapi.LikedTweetsOption,
	outMode likedOutputMode,
) error {
	if outMode == likedOutputModeNDJSON {
		// ストリーミング (D-6): 集約しない、callback 内で即時 encode。
		w := cmd.OutOrStdout()
		return client.EachLikedPage(ctx, userID, func(p *xapi.LikedTweetsResponse) error {
			if p == nil {
				return nil
			}
			return writeNDJSONTweets(w, p.Data)
		}, opts...)
	}
	// JSON / Human: 集約して最後に出力。
	agg := &likedAggregator{}
	if err := client.EachLikedPage(ctx, userID, agg.add, opts...); err != nil {
		return err
	}
	resp := agg.build()
	if outMode == likedOutputModeHuman {
		return writeLikedHuman(cmd, resp)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(resp)
}

// likedAggregator は EachLikedPage の callback として複数ページを集約する内部構造体 (D-8)。
//
// 集約規則:
//   - Data: 全ページの Tweet を append (重複排除しない)
//   - Includes.Users / Includes.Tweets: 全ページの要素を append (重複排除しない)
//   - Meta: build() 時に再構築 (ResultCount = len(Data), NextToken = "")
//     最終ページの meta を流すと「全体件数か最終ページ件数か」が曖昧になるため明示的に再構築
type likedAggregator struct {
	data   []xapi.Tweet
	users  []xapi.User
	tweets []xapi.Tweet
}

// add は EachLikedPage callback として 1 ページ分の応答を集約する。
func (a *likedAggregator) add(p *xapi.LikedTweetsResponse) error {
	if p == nil {
		return nil
	}
	a.data = append(a.data, p.Data...)
	a.users = append(a.users, p.Includes.Users...)
	a.tweets = append(a.tweets, p.Includes.Tweets...)
	return nil
}

// build は集約結果を *xapi.LikedTweetsResponse として返す。
// Meta は再構築 (result_count = 集約後の総件数, next_token = "")。
func (a *likedAggregator) build() *xapi.LikedTweetsResponse {
	return &xapi.LikedTweetsResponse{
		Data: a.data,
		Includes: xapi.Includes{
			Users:  a.users,
			Tweets: a.tweets,
		},
		Meta: xapi.Meta{
			ResultCount: len(a.data),
			NextToken:   "",
		},
	}
}

// writeNDJSONTweets は ツイート配列を NDJSON (1 ツイート 1 行 JSON) で書き出す (D-12)。
//
// SetEscapeHTML(false) を設定し `<`, `>`, `&` を生のまま出力する (X tweet text に頻出するため、
// NDJSON consumer に余計な再変換コストを強いないため)。
// データが空または nil なら何も書き出さない (改行のみも出さない)。
func writeNDJSONTweets(w io.Writer, tweets []xapi.Tweet) error {
	if len(tweets) == 0 {
		return nil
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for i := range tweets {
		if err := enc.Encode(tweets[i]); err != nil {
			return err
		}
	}
	return nil
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
//
// M29 D-3: note_tweet.text が非空の場合は truncated text より優先する
// (ロングツイートの真の本文を表示するため)。
func formatLikedHumanLine(tw xapi.Tweet) string {
	text := tw.Text
	if tw.NoteTweet != nil && tw.NoteTweet.Text != "" {
		text = tw.NoteTweet.Text
	}
	text = sanitizeLikedText(text)
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
