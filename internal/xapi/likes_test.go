package xapi

// 注意: 本ファイルは package xapi の internal test (xapi_test ではない)。
// client_test.go と同じテストヘルパ (newTestClient / withHTTPClient / withSleep) を共有し、
// withNow など未公開オプションへアクセスするため。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fixedTimeNow は計画書 D-3 の rate-limit aware テストで `c.now()` を固定するためのヘルパ。
func fixedTimeNow(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

// TestListLikedTweets_HitsCorrectEndpoint は GET /2/users/42/liked_tweets に
// 正しいパス・メソッドで HTTP リクエストが送られることを確認する。
func TestListLikedTweets_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.ListLikedTweets(context.Background(), "42")
	if err != nil {
		t.Fatalf("ListLikedTweets: %v", err)
	}
	if gotPath != "/2/users/42/liked_tweets" {
		t.Errorf("path = %q, want /2/users/42/liked_tweets", gotPath)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
}

// TestListLikedTweets_Success は 200 OK + 正常 JSON で LikedTweetsResponse が
// デコードされることを確認する。
func TestListLikedTweets_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data":[
				{"id":"1","text":"t1","author_id":"42"},
				{"id":"2","text":"t2","author_id":"42"},
				{"id":"3","text":"t3","author_id":"7"}
			],
			"includes":{"users":[{"id":"42","username":"alice","name":"Alice"}]},
			"meta":{"result_count":3,"next_token":"NTOK1"}
		}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.ListLikedTweets(context.Background(), "42")
	if err != nil {
		t.Fatalf("ListLikedTweets: %v", err)
	}
	if resp == nil {
		t.Fatal("resp = nil")
	}
	if len(resp.Data) != 3 {
		t.Fatalf("Data len = %d, want 3", len(resp.Data))
	}
	if resp.Data[0].ID != "1" || resp.Data[2].AuthorID != "7" {
		t.Errorf("Data = %+v", resp.Data)
	}
	if len(resp.Includes.Users) != 1 || resp.Includes.Users[0].Username != "alice" {
		t.Errorf("Includes.Users = %+v", resp.Includes.Users)
	}
	if resp.Meta.ResultCount != 3 || resp.Meta.NextToken != "NTOK1" {
		t.Errorf("Meta = %+v", resp.Meta)
	}
}

// TestListLikedTweets_UserIDPathEscape は userID に URL 予約文字が含まれた場合に
// パスエスケープが効くことを確認する (パスインジェクション対策)。
func TestListLikedTweets_UserIDPathEscape(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	// `/` が含まれた場合、エスケープされて `%2F` になるべき (パスインジェクション防止)
	_, err := c.ListLikedTweets(context.Background(), "42/admin")
	if err != nil {
		t.Fatalf("ListLikedTweets: %v", err)
	}
	if gotPath != "/2/users/42%2Fadmin/liked_tweets" {
		t.Errorf("path = %q, want /2/users/42%%2Fadmin/liked_tweets", gotPath)
	}
}

// TestListLikedTweets_QueryParams は全 Option が URL クエリに反映されることを確認する。
func TestListLikedTweets_QueryParams(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.ListLikedTweets(
		context.Background(), "42",
		WithMaxResults(100),
		WithPaginationToken("PTOK"),
		WithTweetFields("id", "text", "public_metrics"),
		WithExpansions("author_id", "referenced_tweets.id"),
		WithLikedUserFields("username", "name"),
	)
	if err != nil {
		t.Fatalf("ListLikedTweets: %v", err)
	}
	if got, want := gotQuery.Get("max_results"), "100"; got != want {
		t.Errorf("max_results = %q, want %q", got, want)
	}
	if got, want := gotQuery.Get("pagination_token"), "PTOK"; got != want {
		t.Errorf("pagination_token = %q, want %q", got, want)
	}
	if got, want := gotQuery.Get("tweet.fields"), "id,text,public_metrics"; got != want {
		t.Errorf("tweet.fields = %q, want %q", got, want)
	}
	if got, want := gotQuery.Get("expansions"), "author_id,referenced_tweets.id"; got != want {
		t.Errorf("expansions = %q, want %q", got, want)
	}
	if got, want := gotQuery.Get("user.fields"), "username,name"; got != want {
		t.Errorf("user.fields = %q, want %q", got, want)
	}
}

// TestListLikedTweets_NoOptions_NoQuery は opts 無しで呼んだ場合 URL に ? が付かないことを確認する。
func TestListLikedTweets_NoOptions_NoQuery(t *testing.T) {
	t.Parallel()

	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.ListLikedTweets(context.Background(), "42"); err != nil {
		t.Fatalf("ListLikedTweets: %v", err)
	}
	if gotRawQuery != "" {
		t.Errorf("RawQuery = %q, want empty", gotRawQuery)
	}
}

// TestListLikedTweets_MaxResultsZero_NoOp は WithMaxResults(0) が
// no-op (クエリに付与されない) ことを確認する。CLI 層 (M10/M11) が default 100 を
// 必ずセットする責務を持つ前提 (spec §11)。
func TestListLikedTweets_MaxResultsZero_NoOp(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.ListLikedTweets(context.Background(), "42", WithMaxResults(0)); err != nil {
		t.Fatalf("ListLikedTweets: %v", err)
	}
	if got := gotQuery.Get("max_results"); got != "" {
		t.Errorf("max_results = %q, want empty (no-op for 0)", got)
	}
}

// TestListLikedTweets_StartTimeFormat_RFC3339 は WithStartTime が
// RFC3339 (ナノ秒なし) 形式でクエリに反映されることを明示的に検証する (advisor 指摘#4)。
func TestListLikedTweets_StartTimeFormat_RFC3339(t *testing.T) {
	t.Parallel()

	var gotStart string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotStart = r.URL.Query().Get("start_time")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	// JST 0:00 を UTC 化した想定
	start := time.Date(2026, 5, 12, 0, 0, 0, 123456789, time.UTC)
	if _, err := c.ListLikedTweets(context.Background(), "42", WithStartTime(start)); err != nil {
		t.Fatalf("ListLikedTweets: %v", err)
	}
	want := "2026-05-12T00:00:00Z" // ナノ秒は捨てる
	if gotStart != want {
		t.Errorf("start_time = %q, want %q (RFC3339 without nanoseconds)", gotStart, want)
	}
}

// TestListLikedTweets_EndTimeFormat_RFC3339 は WithEndTime も同様に RFC3339 形式であることを確認する。
func TestListLikedTweets_EndTimeFormat_RFC3339(t *testing.T) {
	t.Parallel()

	var gotEnd string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEnd = r.URL.Query().Get("end_time")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	end := time.Date(2026, 5, 12, 14, 59, 59, 0, time.UTC)
	if _, err := c.ListLikedTweets(context.Background(), "42", WithEndTime(end)); err != nil {
		t.Fatalf("ListLikedTweets: %v", err)
	}
	want := "2026-05-12T14:59:59Z"
	if gotEnd != want {
		t.Errorf("end_time = %q, want %q", gotEnd, want)
	}
}

// TestListLikedTweets_TimeUTCNormalization は non-UTC な time.Time を渡しても
// UTC に正規化されてクエリに反映されることを確認する。
func TestListLikedTweets_TimeUTCNormalization(t *testing.T) {
	t.Parallel()

	var gotStart string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotStart = r.URL.Query().Get("start_time")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	jst := time.FixedZone("JST", 9*60*60)
	start := time.Date(2026, 5, 12, 9, 0, 0, 0, jst) // = 2026-05-12T00:00:00Z
	if _, err := c.ListLikedTweets(context.Background(), "42", WithStartTime(start)); err != nil {
		t.Fatalf("ListLikedTweets: %v", err)
	}
	if gotStart != "2026-05-12T00:00:00Z" {
		t.Errorf("start_time = %q, want %q (UTC normalized)", gotStart, "2026-05-12T00:00:00Z")
	}
}

// TestListLikedTweets_401_AuthError は 401 で ErrAuthentication が返ることを確認する。
func TestListLikedTweets_401_AuthError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.ListLikedTweets(context.Background(), "42")
	if !errors.Is(err, ErrAuthentication) {
		t.Errorf("errors.Is(err, ErrAuthentication) = false, want true (err=%v)", err)
	}
}

// TestListLikedTweets_404_NotFound は 404 で ErrNotFound が返ることを確認する。
func TestListLikedTweets_404_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.ListLikedTweets(context.Background(), "42")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err=%v)", err)
	}
}

// TestListLikedTweets_ContextCanceled は事前 cancel context を渡すと
// context.Canceled が返ることを確認する。
func TestListLikedTweets_ContextCanceled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.ListLikedTweets(ctx, "42")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestListLikedTweets_InvalidJSON は 200 OK だが型不一致 JSON のとき
// decode エラーが返り、リトライしないこと (server call 1 回) を確認する。
func TestListLikedTweets_InvalidJSON(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		// data が文字列 (型不一致)
		_, _ = w.Write([]byte(`{"data":"not-an-array"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.ListLikedTweets(context.Background(), "42")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error message %q, want substring 'decode'", err.Error())
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server calls = %d, want 1 (no retry on decode error)", got)
	}
}

// -- EachLikedPage tests ---------------------------------------------------

// makePagedHandler は next_token で連鎖する複数ページのモックハンドラを返す。
//
// pages[i] はページ i (0-indexed) のレスポンス JSON テンプレート。
// `{{NEXT}}` プレースホルダは次ページがある場合のみ自動置換される (`"next_token":"P<i+1>"`)。
// 最終ページでは `,"next_token":""` 相当 (next_token 不在) になる。
type pageDef struct {
	tweetIDs []string
	// rateLimit が non-nil ならヘッダに付与する (Remaining / ResetUnix)。
	rateLimit *struct {
		remaining int
		resetUnix int64
	}
}

func makePagedHandler(t *testing.T, defs []pageDef) (handler http.HandlerFunc, tokensSeen *[]string, requestCounter *int32) {
	t.Helper()
	var seenTokens []string
	var callCount int32
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := atomic.AddInt32(&callCount, 1) - 1
		seenTokens = append(seenTokens, r.URL.Query().Get("pagination_token"))
		if int(idx) >= len(defs) {
			t.Errorf("server called more times (%d) than pages (%d)", idx+1, len(defs))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		def := defs[idx]
		if def.rateLimit != nil {
			w.Header().Set("x-rate-limit-limit", "75")
			w.Header().Set("x-rate-limit-remaining", strconv.Itoa(def.rateLimit.remaining))
			w.Header().Set("x-rate-limit-reset", strconv.FormatInt(def.rateLimit.resetUnix, 10))
		}
		// data 配列を組み立てる
		var dataItems []string
		for _, id := range def.tweetIDs {
			dataItems = append(dataItems, fmt.Sprintf(`{"id":%q,"text":"t%s","author_id":"42"}`, id, id))
		}
		var nextToken string
		if int(idx) < len(defs)-1 {
			nextToken = fmt.Sprintf("P%d", idx+1)
		}
		body := fmt.Sprintf(`{"data":[%s],"meta":{"result_count":%d`,
			strings.Join(dataItems, ","), len(def.tweetIDs))
		if nextToken != "" {
			body += fmt.Sprintf(`,"next_token":%q`, nextToken)
		}
		body += `}}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
	return handler, &seenTokens, &callCount
}

// TestEachLikedPage_MultiPage_FullTraversal は 3 ページ × 2 件のモックを
// callback で 3 回受け取り、計 6 件のツイート ID を辿れることを確認する。
func TestEachLikedPage_MultiPage_FullTraversal(t *testing.T) {
	t.Parallel()

	defs := []pageDef{
		{tweetIDs: []string{"1", "2"}},
		{tweetIDs: []string{"3", "4"}},
		{tweetIDs: []string{"5", "6"}},
	}
	handler, _, _ := makePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	var got []string
	pageCount := 0
	err := c.EachLikedPage(context.Background(), "42", func(p *LikedTweetsResponse) error {
		pageCount++
		for _, tw := range p.Data {
			got = append(got, tw.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("EachLikedPage: %v", err)
	}
	if pageCount != 3 {
		t.Errorf("page count = %d, want 3", pageCount)
	}
	want := []string{"1", "2", "3", "4", "5", "6"}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestEachLikedPage_PaginationTokenForwarded は 2 ページ目以降のリクエストに
// 前ページの next_token が `pagination_token` クエリパラメータとして付くことを確認する。
func TestEachLikedPage_PaginationTokenForwarded(t *testing.T) {
	t.Parallel()

	defs := []pageDef{
		{tweetIDs: []string{"1"}},
		{tweetIDs: []string{"2"}},
		{tweetIDs: []string{"3"}},
	}
	handler, tokensSeen, _ := makePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	err := c.EachLikedPage(context.Background(), "42", func(_ *LikedTweetsResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachLikedPage: %v", err)
	}
	want := []string{"", "P1", "P2"}
	if len(*tokensSeen) != len(want) {
		t.Fatalf("tokens = %v, want %v", *tokensSeen, want)
	}
	for i, w := range want {
		if (*tokensSeen)[i] != w {
			t.Errorf("tokens[%d] = %q, want %q", i, (*tokensSeen)[i], w)
		}
	}
}

// TestEachLikedPage_MaxPages_Truncates は max_pages=2 を指定すると
// 3 ページ用意してあっても 2 ページで打ち切られることを確認する。
func TestEachLikedPage_MaxPages_Truncates(t *testing.T) {
	t.Parallel()

	defs := []pageDef{
		{tweetIDs: []string{"1"}},
		{tweetIDs: []string{"2"}},
		{tweetIDs: []string{"3"}}, // 呼ばれないはず
	}
	handler, _, calls := makePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	pageCount := 0
	err := c.EachLikedPage(context.Background(), "42", func(_ *LikedTweetsResponse) error {
		pageCount++
		return nil
	}, WithMaxPages(2))
	if err != nil {
		t.Fatalf("EachLikedPage: %v", err)
	}
	if pageCount != 2 {
		t.Errorf("callback count = %d, want 2", pageCount)
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Errorf("server calls = %d, want 2", got)
	}
}

// TestEachLikedPage_RateLimitSleep は rate-limit remaining が閾値以下のとき
// reset 時刻まで sleep されることを確認する (advisor 指摘#1 反映済み)。
func TestEachLikedPage_RateLimitSleep(t *testing.T) {
	t.Parallel()

	// 1 ページ目で Remaining=1, Reset=5s 後 → sleep[0] ≈ 5s
	// 2 ページ目は最終 (next_token 無し)
	fixedNow := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	resetAt := fixedNow.Add(5 * time.Second)
	defs := []pageDef{
		{tweetIDs: []string{"1"}, rateLimit: &struct {
			remaining int
			resetUnix int64
		}{remaining: 1, resetUnix: resetAt.Unix()}},
		{tweetIDs: []string{"2"}},
	}
	handler, _, _ := makePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, sleeps := newTestClient(t, srv, withNow(fixedTimeNow(fixedNow)))
	err := c.EachLikedPage(context.Background(), "42", func(_ *LikedTweetsResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachLikedPage: %v", err)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1 (sleeps=%v)", len(*sleeps), *sleeps)
	}
	diff := (*sleeps)[0] - 5*time.Second
	if diff < -time.Second || diff > time.Second {
		t.Errorf("sleep[0] = %v, want ≈ 5s", (*sleeps)[0])
	}
}

// TestEachLikedPage_InterPageDelay は rate-limit に余裕がある場合
// ページ間で 200ms の最小待機が入ることを確認する。
func TestEachLikedPage_InterPageDelay(t *testing.T) {
	t.Parallel()

	defs := []pageDef{
		{tweetIDs: []string{"1"}, rateLimit: &struct {
			remaining int
			resetUnix int64
		}{remaining: 50, resetUnix: time.Now().Add(900 * time.Second).Unix()}},
		{tweetIDs: []string{"2"}},
	}
	handler, _, _ := makePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, sleeps := newTestClient(t, srv)
	err := c.EachLikedPage(context.Background(), "42", func(_ *LikedTweetsResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachLikedPage: %v", err)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1 (sleeps=%v)", len(*sleeps), *sleeps)
	}
	if (*sleeps)[0] != 200*time.Millisecond {
		t.Errorf("sleep[0] = %v, want 200ms", (*sleeps)[0])
	}
}

// TestEachLikedPage_RateLimitStaleReset_FallsBackTo200ms は (advisor 指摘#1)
// remaining が閾値以下でも reset が過去 (clock skew or stale) の場合に
// 200ms 最小待機にフォールバックすることを確認する。
func TestEachLikedPage_RateLimitStaleReset_FallsBackTo200ms(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	resetAt := fixedNow.Add(-10 * time.Second) // 過去
	defs := []pageDef{
		{tweetIDs: []string{"1"}, rateLimit: &struct {
			remaining int
			resetUnix int64
		}{remaining: 1, resetUnix: resetAt.Unix()}},
		{tweetIDs: []string{"2"}},
	}
	handler, _, _ := makePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, sleeps := newTestClient(t, srv, withNow(fixedTimeNow(fixedNow)))
	err := c.EachLikedPage(context.Background(), "42", func(_ *LikedTweetsResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachLikedPage: %v", err)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1", len(*sleeps))
	}
	if (*sleeps)[0] != 200*time.Millisecond {
		t.Errorf("sleep[0] = %v, want 200ms (stale reset fallback)", (*sleeps)[0])
	}
}

// TestEachLikedPage_RateLimitMaxWaitCap は reset が極端に遠い未来 (16 分後) でも
// 15 分で頭打ちになることを確認する。
func TestEachLikedPage_RateLimitMaxWaitCap(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	resetAt := fixedNow.Add(16 * time.Minute)
	defs := []pageDef{
		{tweetIDs: []string{"1"}, rateLimit: &struct {
			remaining int
			resetUnix int64
		}{remaining: 0, resetUnix: resetAt.Unix()}},
		{tweetIDs: []string{"2"}},
	}
	handler, _, _ := makePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, sleeps := newTestClient(t, srv, withNow(fixedTimeNow(fixedNow)))
	err := c.EachLikedPage(context.Background(), "42", func(_ *LikedTweetsResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachLikedPage: %v", err)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1", len(*sleeps))
	}
	if (*sleeps)[0] != 15*time.Minute {
		t.Errorf("sleep[0] = %v, want 15m (cap)", (*sleeps)[0])
	}
}

// TestEachLikedPage_CallbackError_Aborts は callback が error を返した時点で
// 即中断され、次ページ取得が走らないことを確認する。
func TestEachLikedPage_CallbackError_Aborts(t *testing.T) {
	t.Parallel()

	defs := []pageDef{
		{tweetIDs: []string{"1"}},
		{tweetIDs: []string{"2"}}, // 呼ばれないはず
	}
	handler, _, calls := makePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	sentinel := errors.New("callback abort")
	err := c.EachLikedPage(context.Background(), "42", func(_ *LikedTweetsResponse) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want %v", err, sentinel)
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("server calls = %d, want 1 (abort after first callback)", got)
	}
}

// TestEachLikedPage_ContextCanceled は事前 cancel context を渡すと
// 1 ページも取得せず context.Canceled が返ることを確認する。
func TestEachLikedPage_ContextCanceled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.EachLikedPage(ctx, "42", func(_ *LikedTweetsResponse) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestEachLikedPage_429_RetryThenSuccess は単一ページ内で 429 を 1 度受けた後に
// 200 が返るケースで EachLikedPage が正常完了することを確認する (Client.Do の retry を利用)。
func TestEachLikedPage_429_RetryThenSuccess(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"t"}]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	pageCount := 0
	err := c.EachLikedPage(context.Background(), "42", func(p *LikedTweetsResponse) error {
		pageCount++
		if len(p.Data) != 1 || p.Data[0].ID != "1" {
			t.Errorf("Data = %+v", p.Data)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("EachLikedPage: %v", err)
	}
	if pageCount != 1 {
		t.Errorf("page count = %d, want 1", pageCount)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("server calls = %d, want 2 (1 retry + success)", got)
	}
}

// TestEachLikedPage_ReferencedTweetsExpansion_IntegratesIncludesTweets は (advisor 指摘#3)
// WithExpansions("referenced_tweets.id") を指定したリクエストに対して
// mock が `includes.tweets` を返した場合に Includes.Tweets が読めることを確認する。
func TestEachLikedPage_ReferencedTweetsExpansion_IntegratesIncludesTweets(t *testing.T) {
	t.Parallel()

	var gotExpansions string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotExpansions = r.URL.Query().Get("expansions")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data":[{"id":"1","text":"rt","referenced_tweets":[{"type":"retweeted","id":"999"}]}],
			"includes":{
				"tweets":[{"id":"999","text":"original","author_id":"7"}],
				"users":[{"id":"7","username":"orig","name":"Original"}]
			},
			"meta":{"result_count":1}
		}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	var captured *LikedTweetsResponse
	err := c.EachLikedPage(
		context.Background(), "42",
		func(p *LikedTweetsResponse) error {
			captured = p
			return nil
		},
		WithExpansions("referenced_tweets.id"),
		WithLikedUserFields("username", "name"),
	)
	if err != nil {
		t.Fatalf("EachLikedPage: %v", err)
	}
	if gotExpansions != "referenced_tweets.id" {
		t.Errorf("expansions query = %q, want %q", gotExpansions, "referenced_tweets.id")
	}
	if captured == nil {
		t.Fatal("captured = nil")
	}
	if len(captured.Includes.Tweets) != 1 || captured.Includes.Tweets[0].ID != "999" {
		t.Errorf("Includes.Tweets = %+v", captured.Includes.Tweets)
	}
	if len(captured.Data) != 1 || len(captured.Data[0].ReferencedTweets) != 1 {
		t.Errorf("Data = %+v", captured.Data)
	}
	if captured.Data[0].ReferencedTweets[0].ID != "999" {
		t.Errorf("ReferencedTweets[0].ID = %q, want %q",
			captured.Data[0].ReferencedTweets[0].ID, "999")
	}
}

// TestEachLikedPage_DefaultMaxPages_DoesNotExceed50 は WithMaxPages を指定しない場合に
// default 50 が適用されることを確認する。51 ページ用意して 50 ページで打ち切られる。
func TestEachLikedPage_DefaultMaxPages_DoesNotExceed50(t *testing.T) {
	t.Parallel()

	defs := make([]pageDef, 60) // 60 ページ用意
	for i := range defs {
		defs[i] = pageDef{tweetIDs: []string{strconv.Itoa(i + 1)}}
	}
	handler, _, calls := makePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	pageCount := 0
	err := c.EachLikedPage(context.Background(), "42", func(_ *LikedTweetsResponse) error {
		pageCount++
		return nil
	})
	if err != nil {
		t.Fatalf("EachLikedPage: %v", err)
	}
	if pageCount != 50 {
		t.Errorf("callback count = %d, want 50 (default max_pages)", pageCount)
	}
	if got := atomic.LoadInt32(calls); got != 50 {
		t.Errorf("server calls = %d, want 50", got)
	}
}
