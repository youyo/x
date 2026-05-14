package xapi

// 注意: 本ファイルは package xapi の internal test (xapi_test ではない)。
// client_test.go / likes_test.go の newTestClient / pageDef / fixedTimeNow / withNow を共有する。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// -- GetUserTweets / GetUserMentions / GetHomeTimeline ----------------------

// TestGetUserTweets_HitsCorrectEndpoint は GET /2/users/:id/tweets を検証する。
func TestGetUserTweets_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"hi","author_id":"42"}],"meta":{"result_count":1}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.GetUserTweets(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetUserTweets: %v", err)
	}
	if gotPath != "/2/users/42/tweets" {
		t.Errorf("path = %q, want /2/users/42/tweets", gotPath)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if resp == nil || len(resp.Data) != 1 || resp.Data[0].ID != "1" {
		t.Errorf("resp = %+v", resp)
	}
}

// TestGetUserMentions_HitsCorrectEndpoint は GET /2/users/:id/mentions を検証する。
func TestGetUserMentions_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetUserMentions(context.Background(), "42"); err != nil {
		t.Fatalf("GetUserMentions: %v", err)
	}
	if gotPath != "/2/users/42/mentions" {
		t.Errorf("path = %q, want /2/users/42/mentions", gotPath)
	}
}

// TestGetHomeTimeline_HitsCorrectEndpoint は GET /2/users/:id/timelines/reverse_chronological を検証する。
func TestGetHomeTimeline_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetHomeTimeline(context.Background(), "42"); err != nil {
		t.Fatalf("GetHomeTimeline: %v", err)
	}
	if gotPath != "/2/users/42/timelines/reverse_chronological" {
		t.Errorf("path = %q, want /2/users/42/timelines/reverse_chronological", gotPath)
	}
}

// TestGetUserTweets_PathEscape は userID の url.PathEscape を検証する。
func TestGetUserTweets_PathEscape(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetUserTweets(context.Background(), "user/x"); err != nil {
		t.Fatalf("GetUserTweets: %v", err)
	}
	if gotPath != "/2/users/user%2Fx/tweets" {
		t.Errorf("path = %q, want /2/users/user%%2Fx/tweets", gotPath)
	}
}

// TestGetUserTweets_EmptyUserID_RejectsArgument は userID="" を事前バリデーションで拒否する。
func TestGetUserTweets_EmptyUserID_RejectsArgument(t *testing.T) {
	t.Parallel()

	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserTweets(context.Background(), "")
	if err == nil {
		t.Fatalf("GetUserTweets(\"\") expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must be non-empty") {
		t.Errorf("err = %v, want contains 'must be non-empty'", err)
	}
	if got := atomic.LoadInt32(&called); got != 0 {
		t.Errorf("server called %d times, want 0", got)
	}
}

// TestGetUserTweets_AllOptionsReflected は全 Option がクエリに反映されることを検証する。
func TestGetUserTweets_AllOptionsReflected(t *testing.T) {
	t.Parallel()

	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	start := time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 12, 23, 59, 59, 0, time.UTC)
	_, err := c.GetUserTweets(context.Background(), "42",
		WithTimelineMaxResults(50),
		WithTimelineStartTime(start),
		WithTimelineEndTime(end),
		WithTimelineSinceID("100"),
		WithTimelineUntilID("200"),
		WithTimelinePaginationToken("TOK"),
		WithTimelineExclude("retweets", "replies"),
		WithTimelineTweetFields("id", "text", "note_tweet"),
		WithTimelineExpansions("author_id"),
		WithTimelineUserFields("username"),
		WithTimelineMediaFields("url"),
	)
	if err != nil {
		t.Fatalf("GetUserTweets: %v", err)
	}
	wants := []string{
		"max_results=50",
		"start_time=2026-05-12T00%3A00%3A00Z",
		"end_time=2026-05-12T23%3A59%3A59Z",
		"since_id=100",
		"until_id=200",
		"pagination_token=TOK",
		"exclude=retweets%2Creplies",
		"tweet.fields=id%2Ctext%2Cnote_tweet",
		"expansions=author_id",
		"user.fields=username",
		"media.fields=url",
	}
	for _, want := range wants {
		if !strings.Contains(gotURL, want) {
			t.Errorf("url = %q, missing %q", gotURL, want)
		}
	}
}

// TestGetUserTweets_ExcludeFlag は WithTimelineExclude のみが正しくクエリに乗ることを検証する。
func TestGetUserTweets_ExcludeFlag(t *testing.T) {
	t.Parallel()

	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetUserTweets(context.Background(), "42",
		WithTimelineExclude("retweets", "replies")); err != nil {
		t.Fatalf("GetUserTweets: %v", err)
	}
	if !strings.Contains(gotQuery, "exclude=retweets%2Creplies") {
		t.Errorf("query = %q, want exclude=retweets,replies", gotQuery)
	}
}

// TestGetUserTweets_StartTimeRFC3339Z は WithTimelineStartTime のフォーマット (ナノ秒切り捨て) を検証する。
func TestGetUserTweets_StartTimeRFC3339Z(t *testing.T) {
	t.Parallel()

	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	start := time.Date(2026, 5, 12, 12, 34, 56, 789, time.UTC)
	if _, err := c.GetUserTweets(context.Background(), "42", WithTimelineStartTime(start)); err != nil {
		t.Fatalf("GetUserTweets: %v", err)
	}
	if !strings.Contains(gotQuery, "start_time=2026-05-12T12%3A34%3A56Z") {
		t.Errorf("query = %q, want start_time without nanos", gotQuery)
	}
}

// TestGetUserTweets_401_AuthError は 401 が ErrAuthentication になることを検証する。
func TestGetUserTweets_401_AuthError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserTweets(context.Background(), "42")
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("err = %v, want ErrAuthentication", err)
	}
}

// TestGetUserMentions_404_NotFound は 404 → ErrNotFound を検証する。
func TestGetUserMentions_404_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserMentions(context.Background(), "42")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestGetHomeTimeline_403_Permission は 403 → ErrPermission を検証する。
func TestGetHomeTimeline_403_Permission(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"title":"Forbidden"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetHomeTimeline(context.Background(), "42")
	if !errors.Is(err, ErrPermission) {
		t.Fatalf("err = %v, want ErrPermission", err)
	}
}

// TestGetUserTweets_InvalidJSON_NoRetry は decode error が retry なしで返ることを検証する。
func TestGetUserTweets_InvalidJSON_NoRetry(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserTweets(context.Background(), "42")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v, want decode error", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server calls = %d, want 1 (no retry)", got)
	}
}

// -- Each*TimelinePage -----------------------------------------------------

// makeTimelinePagedHandler は next_token で連鎖する複数 timeline ページのハンドラを返す。
// path 指定なしで全 endpoint に同じ応答を返す (テスト側で endpoint を切り替えて使う)。
func makeTimelinePagedHandler(t *testing.T, defs []pageDef) (handler http.HandlerFunc, tokensSeen *[]string, requestCounter *int32) {
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
			w.Header().Set("x-rate-limit-limit", "1500")
			w.Header().Set("x-rate-limit-remaining", strconv.Itoa(def.rateLimit.remaining))
			w.Header().Set("x-rate-limit-reset", strconv.FormatInt(def.rateLimit.resetUnix, 10))
		}
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

// TestEachUserTweetsPage_MultiPage_FullTraversal は 3 ページ走破を検証する。
func TestEachUserTweetsPage_MultiPage_FullTraversal(t *testing.T) {
	t.Parallel()

	defs := []pageDef{
		{tweetIDs: []string{"1", "2"}},
		{tweetIDs: []string{"3", "4"}},
		{tweetIDs: []string{"5"}},
	}
	handler, tokensSeen, _ := makeTimelinePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	var got []string
	pageCount := 0
	err := c.EachUserTweetsPage(context.Background(), "42", func(p *TimelineResponse) error {
		pageCount++
		for _, tw := range p.Data {
			got = append(got, tw.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("EachUserTweetsPage: %v", err)
	}
	if pageCount != 3 {
		t.Errorf("page count = %d, want 3", pageCount)
	}
	want := []string{"1", "2", "3", "4", "5"}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("ids[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	wantTokens := []string{"", "P1", "P2"}
	if len(*tokensSeen) != len(wantTokens) {
		t.Fatalf("tokens = %v, want %v", *tokensSeen, wantTokens)
	}
}

// TestEachUserMentionsPage_MaxPages_Truncates は max_pages=2 で打ち切りを検証する。
func TestEachUserMentionsPage_MaxPages_Truncates(t *testing.T) {
	t.Parallel()

	defs := []pageDef{
		{tweetIDs: []string{"1"}},
		{tweetIDs: []string{"2"}},
		{tweetIDs: []string{"3"}}, // 呼ばれないはず
	}
	handler, _, calls := makeTimelinePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	pageCount := 0
	err := c.EachUserMentionsPage(context.Background(), "42", func(_ *TimelineResponse) error {
		pageCount++
		return nil
	}, WithTimelineMaxPages(2))
	if err != nil {
		t.Fatalf("EachUserMentionsPage: %v", err)
	}
	if pageCount != 2 {
		t.Errorf("callback count = %d, want 2", pageCount)
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Errorf("server calls = %d, want 2", got)
	}
}

// TestEachHomeTimelinePage_RateLimitSleep は remaining 閾値以下時の sleep を検証する。
func TestEachHomeTimelinePage_RateLimitSleep(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	resetAt := fixedNow.Add(5 * time.Second)
	defs := []pageDef{
		{tweetIDs: []string{"1"}, rateLimit: &struct {
			remaining int
			resetUnix int64
		}{remaining: 1, resetUnix: resetAt.Unix()}},
		{tweetIDs: []string{"2"}},
	}
	handler, _, _ := makeTimelinePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, sleeps := newTestClient(t, srv, withNow(fixedTimeNow(fixedNow)))
	err := c.EachHomeTimelinePage(context.Background(), "42", func(_ *TimelineResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachHomeTimelinePage: %v", err)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1 (sleeps=%v)", len(*sleeps), *sleeps)
	}
	diff := (*sleeps)[0] - 5*time.Second
	if diff < -time.Second || diff > time.Second {
		t.Errorf("sleep[0] = %v, want ≈ 5s", (*sleeps)[0])
	}
}

// TestEachUserTweetsPage_InterPageDelay は rate-limit に余裕があるとき 200ms 待機を検証する。
func TestEachUserTweetsPage_InterPageDelay(t *testing.T) {
	t.Parallel()

	defs := []pageDef{
		{tweetIDs: []string{"1"}, rateLimit: &struct {
			remaining int
			resetUnix int64
		}{remaining: 50, resetUnix: time.Now().Add(900 * time.Second).Unix()}},
		{tweetIDs: []string{"2"}},
	}
	handler, _, _ := makeTimelinePagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, sleeps := newTestClient(t, srv)
	err := c.EachUserTweetsPage(context.Background(), "42", func(_ *TimelineResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachUserTweetsPage: %v", err)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1", len(*sleeps))
	}
	if (*sleeps)[0] != 200*time.Millisecond {
		t.Errorf("sleep[0] = %v, want 200ms", (*sleeps)[0])
	}
}
