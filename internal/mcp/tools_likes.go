package mcp

import (
	"context"
	"fmt"
	"time"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/youyo/x/internal/xapi"
)

// MCP tool `get_liked_tweets` の入力既定値 (spec §11 [liked] と整合)。
//
// MCP モードでは config.toml を読まない (spec §11) ため、CLI 層の loadLikedDefaults
// 相当の責務をここでハードコードとして担う。CLI と値が乖離しないよう注意する
// (将来 internal/config から参照する場合は M11 ハンドオフの「共通ヘルパ昇格」と合わせて検討)。
const (
	likedDefaultMaxResults = 100
	likedDefaultMaxPages   = 50
)

// likedDefaultTweetFields / likedDefaultExpansions / likedDefaultUserFields は
// spec §11 のデフォルトで、ハンドラ初期化時に未指定 / 空配列の場合に適用する。
//
//nolint:gochecknoglobals // 不変のスペック由来デフォルト値であり package level const slice の代替
var (
	likedDefaultTweetFields = []string{"id", "text", "author_id", "created_at", "entities", "public_metrics"}
	likedDefaultExpansions  = []string{"author_id"}
	likedDefaultUserFields  = []string{"username", "name"}
)

// likedTweetsCallConfig はハンドラが受け取った 11 個の入力パラメータをパースした結果である。
//
// 引数バリデーション通過後のみ生成され、後段 (resolveLikedUserID / runLikedSingle / runLikedAll /
// buildLikedTweetsCallOptions) に渡される。
type likedTweetsCallConfig struct {
	userID      string
	startTime   time.Time // ゼロ値なら未設定
	endTime     time.Time
	maxResults  int
	all         bool
	maxPages    int
	tweetFields []string
	expansions  []string
	userFields  []string
}

// NewGetLikedTweetsHandler は MCP tool `get_liked_tweets` のハンドラ関数を生成する。
//
// 登録 (registerToolLikes) からハンドラ生成を分離している理由は M17 と同じテスト容易性である:
// httptest で X API をモックして本関数で得たハンドラを直接呼び出し、
// CallToolResult.IsError / StructuredContent を検証できる。
//
// 引数:
//   - client: X API クライアント。nil の場合、ハンドラ呼び出し時に IsError=true を返す
//     (panic は起こさない)。
//
// ハンドラ挙動:
//   - 引数パース失敗 (型違反 / 範囲外 / フォーマット不正) → IsError=true (X API 呼び出し前に弾く)
//   - 時間窓: yesterday_jst > since_jst > start_time/end_time (spec §6, CLI と同優先順位)
//   - user_id 未指定 → GetUserMe で self 解決
//   - all=false (default): ListLikedTweets シングルページ
//   - all=true: EachLikedPage で next_token を辿りつつ全件取得後、likedAggregator 同等ロジックで
//     Data / Includes.Users / Includes.Tweets を append し、Meta は再構築 (ResultCount = 集約件数,
//     NextToken = "")
//   - 成功時: NewToolResultJSON(*xapi.LikedTweetsResponse) で StructuredContent (pointer 型) +
//     TextContent (JSON 文字列) を埋めた CallToolResult を返す
//   - 失敗時 (API エラー含む): NewToolResultError で IsError=true を返す。protocol-level error は
//     返さない (go-mcp 慣習)
func NewGetLikedTweetsHandler(client *xapi.Client) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		if client == nil {
			return gomcp.NewToolResultError("xapi client is not configured"), nil
		}
		args := req.GetArguments()
		cfg, err := buildLikedTweetsCallConfig(args)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		userID, err := resolveLikedUserID(ctx, client, cfg.userID)
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		opts := buildLikedTweetsCallOptions(cfg)
		var resp *xapi.LikedTweetsResponse
		if cfg.all {
			resp, err = runLikedAll(ctx, client, userID, opts)
		} else {
			resp, err = runLikedSingle(ctx, client, userID, opts)
		}
		if err != nil {
			return gomcp.NewToolResultError(err.Error()), nil
		}
		res, mErr := gomcp.NewToolResultJSON(resp)
		if mErr != nil {
			return gomcp.NewToolResultError(
				fmt.Sprintf("marshal get_liked_tweets result: %v", mErr),
			), nil
		}
		return res, nil
	}
}

// registerToolLikes は `get_liked_tweets` ツールを MCP サーバーに登録する。
//
// NewServer から呼ばれ、tool 定義 (spec §6 の 11 入力パラメータ + description) と
// ハンドラ (NewGetLikedTweetsHandler) をセットで s.AddTool に渡す。
//
// 各引数の Description には「default applied when omitted or empty」を 1 行入れ、
// 明示的な空配列でデフォルト値が適用される挙動 (D-12) を利用者に伝える。
func registerToolLikes(s *mcpserver.MCPServer, client *xapi.Client) {
	tool := gomcp.NewTool(
		"get_liked_tweets",
		gomcp.WithDescription(
			"指定ユーザー (未指定なら自分) の Like したツイートを取得する。"+
				"start_time/end_time (RFC3339 UTC) または since_jst (YYYY-MM-DD) "+
				"または yesterday_jst で時間窓を指定可能。"+
				"all=true で next_token を辿りつつ全件取得し、Data/Includes を集約して返す。",
		),
		gomcp.WithString("user_id",
			gomcp.Description("対象ユーザーの数値 ID。未指定なら認証ユーザー (self) を解決する。"),
		),
		gomcp.WithString("start_time",
			gomcp.Description("最も古いツイート時刻 (RFC3339 UTC)。例: 2026-05-11T15:00:00Z"),
		),
		gomcp.WithString("end_time",
			gomcp.Description("最も新しいツイート時刻 (RFC3339 UTC)。例: 2026-05-12T14:59:59Z"),
		),
		gomcp.WithString("since_jst",
			gomcp.Description("JST 日付 (YYYY-MM-DD)。指定日 0:00〜23:59 JST を UTC に変換して start_time/end_time を上書きする。"),
		),
		gomcp.WithBoolean("yesterday_jst",
			gomcp.DefaultBool(false),
			gomcp.Description("true の場合 JST 前日の 0:00〜23:59 を時間窓とする。since_jst / start_time / end_time を上書きする。"),
		),
		gomcp.WithNumber("max_results",
			gomcp.Min(1), gomcp.Max(100),
			gomcp.DefaultNumber(float64(likedDefaultMaxResults)),
			gomcp.Description("1 ページあたりの最大ツイート数 (1-100)。default applied when omitted."),
		),
		gomcp.WithBoolean("all",
			gomcp.DefaultBool(false),
			gomcp.Description("true なら next_token を辿って全件取得し、結果を集約して返す。"),
		),
		gomcp.WithNumber("max_pages",
			gomcp.Min(1),
			gomcp.DefaultNumber(float64(likedDefaultMaxPages)),
			gomcp.Description("all=true 時の最大ページ数。暴走防止用の上限。default applied when omitted."),
		),
		gomcp.WithArray("tweet_fields",
			gomcp.Description("tweet.fields クエリの値 (例: [\"id\",\"text\"])。default applied when omitted or empty."),
			gomcp.WithStringItems(),
		),
		gomcp.WithArray("expansions",
			gomcp.Description("expansions クエリの値 (例: [\"author_id\"])。default applied when omitted or empty."),
			gomcp.WithStringItems(),
		),
		gomcp.WithArray("user_fields",
			gomcp.Description("user.fields クエリの値 (例: [\"username\",\"name\"])。default applied when omitted or empty."),
			gomcp.WithStringItems(),
		),
	)
	s.AddTool(tool, NewGetLikedTweetsHandler(client))
}

// buildLikedTweetsCallConfig は引数 map を解釈し、likedTweetsCallConfig を組み立てる。
//
// パース順:
//  1. 各引数を arg* ヘルパで取得 (型違反は ここでエラー)
//  2. 時間窓決定: yesterday_jst > since_jst > start_time/end_time
//  3. デフォルト値適用 (max_results / max_pages / tweet_fields / expansions / user_fields)
//
// エラーは fmt.Errorf でラップし、ハンドラ最上位で NewToolResultError に変換される。
//
//nolint:gocyclo // 11 個の引数を直線的に処理しており、責務分離は他の build/run ヘルパで行っている
func buildLikedTweetsCallConfig(args map[string]any) (*likedTweetsCallConfig, error) {
	cfg := &likedTweetsCallConfig{}

	// user_id
	if v, ok, err := argString(args, "user_id"); err != nil {
		return nil, err
	} else if ok {
		cfg.userID = v
	}

	// start_time / end_time (RFC3339)
	startStr, startOK, err := argString(args, "start_time")
	if err != nil {
		return nil, err
	}
	endStr, endOK, err := argString(args, "end_time")
	if err != nil {
		return nil, err
	}
	if startOK && startStr != "" {
		t, perr := time.Parse(time.RFC3339, startStr)
		if perr != nil {
			return nil, fmt.Errorf("start_time: %w", perr)
		}
		cfg.startTime = t
	}
	if endOK && endStr != "" {
		t, perr := time.Parse(time.RFC3339, endStr)
		if perr != nil {
			return nil, fmt.Errorf("end_time: %w", perr)
		}
		cfg.endTime = t
	}

	// since_jst (start/end を上書き)
	sinceStr, sinceOK, err := argString(args, "since_jst")
	if err != nil {
		return nil, err
	}
	if sinceOK && sinceStr != "" {
		s, e, perr := parseJSTDate(sinceStr)
		if perr != nil {
			return nil, perr
		}
		cfg.startTime, cfg.endTime = s, e
	}

	// yesterday_jst (since_jst / start/end を上書き)
	yesterday, _, err := argBool(args, "yesterday_jst")
	if err != nil {
		return nil, err
	}
	if yesterday {
		s, e, perr := yesterdayJSTRange(time.Now())
		if perr != nil {
			return nil, perr
		}
		cfg.startTime, cfg.endTime = s, e
	}

	// max_results (1-100, default 100)
	mr, mrOK, err := argInt(args, "max_results")
	if err != nil {
		return nil, err
	}
	switch {
	case !mrOK:
		cfg.maxResults = likedDefaultMaxResults
	case mr < 1 || mr > 100:
		return nil, fmt.Errorf("max_results must be in 1..100, got %d", mr)
	default:
		cfg.maxResults = mr
	}

	// all (default false)
	all, _, err := argBool(args, "all")
	if err != nil {
		return nil, err
	}
	cfg.all = all

	// max_pages (default 50, > 0)
	mp, mpOK, err := argInt(args, "max_pages")
	if err != nil {
		return nil, err
	}
	switch {
	case !mpOK:
		cfg.maxPages = likedDefaultMaxPages
	case mp <= 0:
		return nil, fmt.Errorf("max_pages must be > 0, got %d", mp)
	default:
		cfg.maxPages = mp
	}

	// tweet_fields / expansions / user_fields (default applied when omitted or empty)
	cfg.tweetFields, err = argStringSliceOrDefault(args, "tweet_fields", likedDefaultTweetFields)
	if err != nil {
		return nil, err
	}
	cfg.expansions, err = argStringSliceOrDefault(args, "expansions", likedDefaultExpansions)
	if err != nil {
		return nil, err
	}
	cfg.userFields, err = argStringSliceOrDefault(args, "user_fields", likedDefaultUserFields)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

// buildLikedTweetsCallOptions は likedTweetsCallConfig を xapi.LikedTweetsOption の slice に変換する。
//
// ゼロ値 (startTime / endTime) は WithStartTime / WithEndTime を呼ばないことで X API クエリから除外する。
// 配列系 (tweetFields / expansions / userFields) は buildLikedTweetsCallConfig で必ず非空に補完済。
func buildLikedTweetsCallOptions(cfg *likedTweetsCallConfig) []xapi.LikedTweetsOption {
	opts := []xapi.LikedTweetsOption{
		xapi.WithMaxResults(cfg.maxResults),
	}
	if !cfg.startTime.IsZero() {
		opts = append(opts, xapi.WithStartTime(cfg.startTime))
	}
	if !cfg.endTime.IsZero() {
		opts = append(opts, xapi.WithEndTime(cfg.endTime))
	}
	if len(cfg.tweetFields) > 0 {
		opts = append(opts, xapi.WithTweetFields(cfg.tweetFields...))
	}
	if len(cfg.expansions) > 0 {
		opts = append(opts, xapi.WithExpansions(cfg.expansions...))
	}
	if len(cfg.userFields) > 0 {
		opts = append(opts, xapi.WithLikedUserFields(cfg.userFields...))
	}
	if cfg.all {
		opts = append(opts, xapi.WithMaxPages(cfg.maxPages))
	}
	return opts
}

// resolveLikedUserID は cfg の user_id が空なら GetUserMe で self の ID を解決する。
// 非空ならそのまま返す。GetUserMe のエラーはそのまま返却する (呼び出し側で IsError に変換される)。
func resolveLikedUserID(ctx context.Context, client *xapi.Client, configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	user, err := client.GetUserMe(ctx)
	if err != nil {
		return "", err
	}
	return user.ID, nil
}

// runLikedSingle は all=false 時の単一ページ取得を行う。
func runLikedSingle(
	ctx context.Context,
	client *xapi.Client,
	userID string,
	opts []xapi.LikedTweetsOption,
) (*xapi.LikedTweetsResponse, error) {
	return client.ListLikedTweets(ctx, userID, opts...)
}

// runLikedAll は all=true 時の全件取得 + 集約を行う。
//
// 集約規則 (CLI 層 likedAggregator と同型, D-8):
//   - Data: 全ページの Tweet を append (重複排除なし)
//   - Includes.Users / Includes.Tweets: 全ページの要素を append (重複排除なし)
//   - Meta: 再構築 (ResultCount = len(Data), NextToken = "")
//     最終ページの meta を流すと「全体件数か最終ページ件数か」が曖昧になるため明示的に再構築する。
func runLikedAll(
	ctx context.Context,
	client *xapi.Client,
	userID string,
	opts []xapi.LikedTweetsOption,
) (*xapi.LikedTweetsResponse, error) {
	agg := &likedTweetsAggregator{}
	if err := client.EachLikedPage(ctx, userID, agg.add, opts...); err != nil {
		return nil, err
	}
	return agg.build(), nil
}

// likedTweetsAggregator は EachLikedPage の callback として複数ページを集約する内部構造体である
// (CLI 層 likedAggregator と同等ロジックの MCP 内部再実装、D-5 の方針通り独立実装)。
type likedTweetsAggregator struct {
	data   []xapi.Tweet
	users  []xapi.User
	tweets []xapi.Tweet
}

// add は EachLikedPage callback として 1 ページ分の応答を集約する。
func (a *likedTweetsAggregator) add(p *xapi.LikedTweetsResponse) error {
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
func (a *likedTweetsAggregator) build() *xapi.LikedTweetsResponse {
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

// jstLocation は JST (Asia/Tokyo) の *time.Location を返す。
//
// `time.LoadLocation("Asia/Tokyo")` をまず試み、失敗時は固定 +9:00 オフセットへフォールバック
// (zoneinfo データ未配置の minimal Linux / distroless / Lambda 等の保険)。
//
// CLI 層 internal/cli/liked.go.jstLocation と同等の独立実装である (D-5: 将来
// internal/timeutil への昇格対象)。
func jstLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Tokyo"); err == nil {
		return loc
	}
	return time.FixedZone("JST", 9*3600)
}

// parseJSTDate は YYYY-MM-DD 形式の JST 日付を 0:00:00〜23:59:59 (JST) の time.Time 2 つで返す。
//
// CLI 層 internal/cli/liked.go.parseJSTDate と同等の独立実装である (D-5)。
// 戻り値の time.Time は内部表現は JST のままだが、xapi.WithStartTime/WithEndTime 内で
// `.UTC().Format(time.RFC3339)` が呼ばれるため最終的なクエリは UTC 表現になる。
func parseJSTDate(s string) (start, end time.Time, err error) {
	jst := jstLocation()
	day, err := time.ParseInLocation("2006-01-02", s, jst)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("since_jst: %w", err)
	}
	start = day
	end = day.Add(24*time.Hour - time.Second)
	return start, end, nil
}

// yesterdayJSTRange は与えられた基準時刻を JST に変換した上で「前日」の 0:00-23:59 範囲を返す。
//
// 基準時刻 (now) を JST に変換し、その JST 日付の前日に対して parseJSTDate を呼び出す。
// テスト時に固定時刻を渡せるよう引数化している。CLI 層 yesterdayJSTRange と同等の独立実装 (D-5)。
func yesterdayJSTRange(now time.Time) (start, end time.Time, err error) {
	jst := jstLocation()
	y := now.In(jst).AddDate(0, 0, -1).Format("2006-01-02")
	return parseJSTDate(y)
}

// YesterdayJSTRangeForTest は yesterdayJSTRange の test-only export である。
//
// テストから決定論的に範囲計算 (B-1a) を検証するための薄いラッパー。本番コードからは使わない。
func YesterdayJSTRangeForTest(now time.Time) (start, end time.Time, err error) {
	return yesterdayJSTRange(now)
}

// argString は map[string]any から string 値を取得する。
//
// セマンティクス:
//   - key 不在: ("", false, nil)
//   - 値が nil (JSON null): ("", false, nil) — key 不在と同じ扱い (advisor 指摘#2)
//   - 値が string: (値, true, nil)
//   - 値が string 以外: ("", false, 型違反 error)
func argString(args map[string]any, key string) (value string, found bool, err error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", false, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", false, fmt.Errorf("%s: expected string, got %T", key, v)
	}
	return s, true, nil
}

// argBool は map[string]any から bool 値を取得する。argString と同じセマンティクス。
func argBool(args map[string]any, key string) (value, found bool, err error) {
	v, ok := args[key]
	if !ok || v == nil {
		return false, false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, false, fmt.Errorf("%s: expected boolean, got %T", key, v)
	}
	return b, true, nil
}

// argInt は map[string]any から int 値を取得する。
//
// JSON 数値は encoding/json により float64 として届く。小数部があるか NaN / Inf の場合は
// 型違反として error を返す (D-9 advisor 指摘: `v != float64(int(v))` で判定)。
func argInt(args map[string]any, key string) (value int, found bool, err error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false, nil
	}
	switch n := v.(type) {
	case float64:
		// NaN / Inf を弾く: NaN は自身と等しくない、Inf は int 変換でオーバーフロー
		if n != n || n > float64(int64(^uint64(0)>>1)) || n < -float64(int64(^uint64(0)>>1))-1 { //nolint:gocritic // float NaN check
			return 0, false, fmt.Errorf("%s: invalid numeric value %v", key, n)
		}
		if n != float64(int(n)) {
			return 0, false, fmt.Errorf("%s: expected integer, got %v", key, n)
		}
		return int(n), true, nil
	case int:
		return n, true, nil
	case int64:
		return int(n), true, nil
	default:
		return 0, false, fmt.Errorf("%s: expected number, got %T", key, v)
	}
}

// argStringSliceOrDefault は map[string]any から []string を取得し、未指定 / 空配列なら fallback を返す。
//
// セマンティクス (D-12):
//   - key 不在 / 値が nil / 空配列 → fallback (spec §11 のデフォルト) を返す
//   - 値が []any でその要素が string → 変換して返す
//   - 値が []any でその要素が string 以外 → 型違反 error
//   - 値が []any 以外の型 → 型違反 error
func argStringSliceOrDefault(args map[string]any, key string, fallback []string) ([]string, error) {
	// 防御的コピー: package-level の default slice が下流ミューテーションで破壊されないよう保護。
	cloneFallback := func() []string { return append([]string(nil), fallback...) }
	v, ok := args[key]
	if !ok || v == nil {
		return cloneFallback(), nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected array, got %T", key, v)
	}
	if len(raw) == 0 {
		return cloneFallback(), nil
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
