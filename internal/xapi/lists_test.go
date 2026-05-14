package xapi

// lists_test.go は M33 で追加された Lists API ラッパーのテスト。
//
// 注意: 本ファイルは package xapi の internal test。client_test.go の
// newTestClient / withSleep / withNow と users_test.go の usersPageDef 等は
// 同パッケージ内ヘルパなのでそのまま再利用できる。

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

// =============================================================================
// M33 テストヘルパ
// =============================================================================

// listsPageDef は List 配列ページ (owned/memberships/followed) 用のページ定義。
type listsPageDef struct {
	listIDs   []string
	rateLimit *struct {
		remaining int
		resetUnix int64
	}
}

// makeListsPagedHandler は List 配列ページング endpoint 用のハンドラ。
// next_token はサーバが自動付与し、pagination_token をクエリ名として記録する。
func makeListsPagedHandler(t *testing.T, defs []listsPageDef, tokenKey string) (handler http.HandlerFunc, tokensSeen *[]string, requestCounter *int32) {
	t.Helper()
	var seenTokens []string
	var callCount int32
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := atomic.AddInt32(&callCount, 1) - 1
		seenTokens = append(seenTokens, r.URL.Query().Get(tokenKey))
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
		var items []string
		for _, id := range def.listIDs {
			items = append(items, fmt.Sprintf(`{"id":%q,"name":"L%s","owner_id":"42"}`, id, id))
		}
		var nextToken string
		if int(idx) < len(defs)-1 {
			nextToken = fmt.Sprintf("P%d", idx+1)
		}
		body := fmt.Sprintf(`{"data":[%s],"meta":{"result_count":%d`,
			strings.Join(items, ","), len(def.listIDs))
		if nextToken != "" {
			body += fmt.Sprintf(`,"next_token":%q`, nextToken)
		}
		body += `}}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
	return handler, &seenTokens, &callCount
}

// listTweetsPageDef は List Tweets ページ定義。
type listTweetsPageDef struct {
	tweetIDs []string
}

func makeListTweetsPagedHandler(t *testing.T, defs []listTweetsPageDef, tokenKey string) (handler http.HandlerFunc, tokensSeen *[]string, requestCounter *int32) {
	t.Helper()
	var seenTokens []string
	var callCount int32
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := atomic.AddInt32(&callCount, 1) - 1
		seenTokens = append(seenTokens, r.URL.Query().Get(tokenKey))
		if int(idx) >= len(defs) {
			t.Errorf("server called more times (%d) than pages (%d)", idx+1, len(defs))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		def := defs[idx]
		var items []string
		for _, id := range def.tweetIDs {
			items = append(items, fmt.Sprintf(`{"id":%q,"text":"t%s"}`, id, id))
		}
		var nextToken string
		if int(idx) < len(defs)-1 {
			nextToken = fmt.Sprintf("P%d", idx+1)
		}
		body := fmt.Sprintf(`{"data":[%s],"meta":{"result_count":%d`,
			strings.Join(items, ","), len(def.tweetIDs))
		if nextToken != "" {
			body += fmt.Sprintf(`,"next_token":%q`, nextToken)
		}
		body += `}}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
	return handler, &seenTokens, &callCount
}

// =============================================================================
// GetList (Lookup)
// =============================================================================

func TestGetList_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"12345","name":"Tech","owner_id":"42"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.GetList(context.Background(), "12345")
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if gotPath != "/2/lists/12345" {
		t.Errorf("path = %q, want /2/lists/12345", gotPath)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if resp == nil || resp.Data == nil || resp.Data.ID != "12345" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestGetList_AllOptionsReflected(t *testing.T) {
	t.Parallel()
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"1","name":"L"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetList(context.Background(), "1",
		WithListLookupListFields("name", "description", "private"),
		WithListLookupExpansions("owner_id"),
		WithListLookupUserFields("username", "name"),
	)
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	for _, want := range []string{
		"list.fields=name%2Cdescription%2Cprivate",
		"expansions=owner_id",
		"user.fields=username%2Cname",
	} {
		if !strings.Contains(gotURL, want) {
			t.Errorf("url = %q, missing %q", gotURL, want)
		}
	}
}

func TestGetList_404_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetList(context.Background(), "1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestGetList_EmptyListID_RejectsArgument(t *testing.T) {
	t.Parallel()
	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetList(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "must be non-empty") {
		t.Errorf("err = %v, want 'must be non-empty'", err)
	}
	if got := atomic.LoadInt32(&called); got != 0 {
		t.Errorf("server called %d times, want 0", got)
	}
}

func TestGetList_InvalidJSON_NoRetry(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetList(context.Background(), "1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v, want 'decode'", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server calls = %d, want 1 (no retry)", got)
	}
}

// =============================================================================
// GetListTweets / EachListTweetsPage
// =============================================================================

func TestGetListTweets_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"hi"}],"meta":{"result_count":1}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetListTweets(context.Background(), "L1")
	if err != nil {
		t.Fatalf("GetListTweets: %v", err)
	}
	if gotPath != "/2/lists/L1/tweets" {
		t.Errorf("path = %q, want /2/lists/L1/tweets", gotPath)
	}
}

func TestGetListTweets_AllOptionsReflected(t *testing.T) {
	t.Parallel()
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetListTweets(context.Background(), "L1",
		WithListPagedMaxResults(50),
		WithListPagedPaginationToken("TOK"),
		WithListPagedTweetFields("id", "text"),
		WithListPagedUserFields("username"),
		WithListPagedExpansions("author_id"),
		WithListPagedMediaFields("url"),
	)
	if err != nil {
		t.Fatalf("GetListTweets: %v", err)
	}
	for _, want := range []string{
		"max_results=50",
		"pagination_token=TOK",
		"tweet.fields=id%2Ctext",
		"user.fields=username",
		"expansions=author_id",
		"media.fields=url",
	} {
		if !strings.Contains(gotURL, want) {
			t.Errorf("url = %q, missing %q", gotURL, want)
		}
	}
}

func TestEachListTweetsPage_MultiPage_FullTraversal(t *testing.T) {
	t.Parallel()
	defs := []listTweetsPageDef{
		{tweetIDs: []string{"1", "2"}},
		{tweetIDs: []string{"3"}},
	}
	handler, tokensSeen, _ := makeListTweetsPagedHandler(t, defs, "pagination_token")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	var got []string
	err := c.EachListTweetsPage(context.Background(), "L1", func(p *ListTweetsResponse) error {
		for _, tw := range p.Data {
			got = append(got, tw.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("EachListTweetsPage: %v", err)
	}
	want := []string{"1", "2", "3"}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	wantTokens := []string{"", "P1"}
	if len(*tokensSeen) != len(wantTokens) {
		t.Fatalf("tokens = %v, want %v", *tokensSeen, wantTokens)
	}
}

func TestGetListTweets_PathEscape(t *testing.T) {
	t.Parallel()
	var gotEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEscapedPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetListTweets(context.Background(), "a/b"); err != nil {
		t.Fatalf("GetListTweets: %v", err)
	}
	if gotEscapedPath != "/2/lists/a%2Fb/tweets" {
		t.Errorf("escaped path = %q, want /2/lists/a%%2Fb/tweets", gotEscapedPath)
	}
}

func TestGetListTweets_InvalidJSON_NoRetry(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetListTweets(context.Background(), "L1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v, want 'decode'", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server calls = %d, want 1", got)
	}
}

// =============================================================================
// GetListMembers / EachListMembersPage
// =============================================================================

func TestGetListMembers_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","username":"a","name":"A"}],"meta":{"result_count":1}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.GetListMembers(context.Background(), "L1")
	if err != nil {
		t.Fatalf("GetListMembers: %v", err)
	}
	if gotPath != "/2/lists/L1/members" {
		t.Errorf("path = %q, want /2/lists/L1/members", gotPath)
	}
	if resp == nil || len(resp.Data) != 1 || resp.Data[0].ID != "1" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestGetListMembers_InvalidJSON_NoRetry(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetListMembers(context.Background(), "L1")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server calls = %d, want 1", got)
	}
}

// TestEachListMembersPage_PaginationParamName は X API ドキュメントで
// next_token と pagination_token の両表記が混在する members endpoint について、
// 本実装では他 paged endpoint と統一して **pagination_token** を送信することを pin する (advisor 反映)。
func TestEachListMembersPage_PaginationParamName(t *testing.T) {
	t.Parallel()
	defs := []usersPageDef{
		{userIDs: []string{"1"}},
		{userIDs: []string{"2"}},
	}
	handler, tokensSeen, _ := makeUsersPagedHandler(t, defs, "pagination_token")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	err := c.EachListMembersPage(context.Background(), "L1", func(_ *UsersResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachListMembersPage: %v", err)
	}
	// tokensSeen[0] は初回 (空)、tokensSeen[1] は 2 ページ目で `pagination_token=P1` の値を含む。
	if len(*tokensSeen) < 2 {
		t.Fatalf("tokensSeen = %v, want at least 2 entries", *tokensSeen)
	}
	if (*tokensSeen)[1] != "P1" {
		t.Errorf("pagination_token on 2nd request = %q, want P1", (*tokensSeen)[1])
	}
}

func TestEachListMembersPage_InterPageDelay(t *testing.T) {
	t.Parallel()
	defs := []usersPageDef{
		{userIDs: []string{"1"}, rateLimit: &struct {
			remaining int
			resetUnix int64
		}{remaining: 50, resetUnix: time.Now().Add(900 * time.Second).Unix()}},
		{userIDs: []string{"2"}},
	}
	handler, _, _ := makeUsersPagedHandler(t, defs, "pagination_token")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, sleeps := newTestClient(t, srv)
	err := c.EachListMembersPage(context.Background(), "L1", func(_ *UsersResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachListMembersPage: %v", err)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1", len(*sleeps))
	}
	if (*sleeps)[0] != 200*time.Millisecond {
		t.Errorf("sleep[0] = %v, want 200ms", (*sleeps)[0])
	}
}

// =============================================================================
// GetOwnedLists / EachOwnedListsPage
// =============================================================================

func TestGetOwnedLists_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"L1","name":"X"}],"meta":{"result_count":1}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetOwnedLists(context.Background(), "42"); err != nil {
		t.Fatalf("GetOwnedLists: %v", err)
	}
	if gotPath != "/2/users/42/owned_lists" {
		t.Errorf("path = %q, want /2/users/42/owned_lists", gotPath)
	}
}

func TestGetOwnedLists_AllOptionsReflected(t *testing.T) {
	t.Parallel()
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetOwnedLists(context.Background(), "42",
		WithListPagedMaxResults(100),
		WithListPagedPaginationToken("TOK"),
		WithListPagedListFields("name", "private"),
		WithListPagedExpansions("owner_id"),
		WithListPagedUserFields("username"),
	)
	if err != nil {
		t.Fatalf("GetOwnedLists: %v", err)
	}
	for _, want := range []string{
		"max_results=100",
		"pagination_token=TOK",
		"list.fields=name%2Cprivate",
		"expansions=owner_id",
		"user.fields=username",
	} {
		if !strings.Contains(gotURL, want) {
			t.Errorf("url = %q, missing %q", gotURL, want)
		}
	}
}

func TestGetOwnedLists_PathEscape(t *testing.T) {
	t.Parallel()
	var gotEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEscapedPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetOwnedLists(context.Background(), "u/x"); err != nil {
		t.Fatalf("GetOwnedLists: %v", err)
	}
	if gotEscapedPath != "/2/users/u%2Fx/owned_lists" {
		t.Errorf("escaped path = %q, want /2/users/u%%2Fx/owned_lists", gotEscapedPath)
	}
}

func TestGetOwnedLists_EmptyUserID_RejectsArgument(t *testing.T) {
	t.Parallel()
	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetOwnedLists(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "must be non-empty") {
		t.Errorf("err = %v, want 'must be non-empty'", err)
	}
	if got := atomic.LoadInt32(&called); got != 0 {
		t.Errorf("server called %d times, want 0", got)
	}
}

func TestEachOwnedListsPage_MultiPage_FullTraversal(t *testing.T) {
	t.Parallel()
	defs := []listsPageDef{
		{listIDs: []string{"1", "2"}},
		{listIDs: []string{"3"}},
	}
	handler, tokensSeen, _ := makeListsPagedHandler(t, defs, "pagination_token")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	var got []string
	err := c.EachOwnedListsPage(context.Background(), "42", func(p *ListsResponse) error {
		for _, l := range p.Data {
			got = append(got, l.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("EachOwnedListsPage: %v", err)
	}
	want := []string{"1", "2", "3"}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	wantTokens := []string{"", "P1"}
	if len(*tokensSeen) != len(wantTokens) {
		t.Fatalf("tokens = %v, want %v", *tokensSeen, wantTokens)
	}
}

func TestEachOwnedListsPage_MaxPagesTruncates(t *testing.T) {
	t.Parallel()
	defs := []listsPageDef{
		{listIDs: []string{"1"}},
		{listIDs: []string{"2"}},
		{listIDs: []string{"3"}},
	}
	handler, _, calls := makeListsPagedHandler(t, defs, "pagination_token")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	pageCount := 0
	err := c.EachOwnedListsPage(context.Background(), "42", func(_ *ListsResponse) error {
		pageCount++
		return nil
	}, WithListPagedMaxPages(2))
	if err != nil {
		t.Fatalf("EachOwnedListsPage: %v", err)
	}
	if pageCount != 2 {
		t.Errorf("callback count = %d, want 2", pageCount)
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Errorf("server calls = %d, want 2", got)
	}
}

func TestEachOwnedListsPage_RateLimitSleep(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	resetAt := fixedNow.Add(5 * time.Second)
	defs := []listsPageDef{
		{listIDs: []string{"1"}, rateLimit: &struct {
			remaining int
			resetUnix int64
		}{remaining: 1, resetUnix: resetAt.Unix()}},
		{listIDs: []string{"2"}},
	}
	handler, _, _ := makeListsPagedHandler(t, defs, "pagination_token")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, sleeps := newTestClient(t, srv, withNow(fixedTimeNow(fixedNow)))
	err := c.EachOwnedListsPage(context.Background(), "42", func(_ *ListsResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachOwnedListsPage: %v", err)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1", len(*sleeps))
	}
	diff := (*sleeps)[0] - 5*time.Second
	if diff < -time.Second || diff > time.Second {
		t.Errorf("sleep[0] = %v, want ≈ 5s", (*sleeps)[0])
	}
}

// TestEachOwnedListsPage_PaginationParamName は paged endpoint 全体で
// pagination_token を採用していることを明示的に pin する (advisor 反映)。
func TestEachOwnedListsPage_PaginationParamName(t *testing.T) {
	t.Parallel()
	defs := []listsPageDef{
		{listIDs: []string{"1"}},
		{listIDs: []string{"2"}},
	}
	handler, tokensSeen, _ := makeListsPagedHandler(t, defs, "pagination_token")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	err := c.EachOwnedListsPage(context.Background(), "42", func(_ *ListsResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachOwnedListsPage: %v", err)
	}
	if len(*tokensSeen) < 2 {
		t.Fatalf("tokensSeen = %v", *tokensSeen)
	}
	if (*tokensSeen)[1] != "P1" {
		t.Errorf("pagination_token on 2nd request = %q, want P1", (*tokensSeen)[1])
	}
}

// =============================================================================
// GetListMemberships / GetFollowedLists
// =============================================================================

func TestGetListMemberships_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetListMemberships(context.Background(), "42"); err != nil {
		t.Fatalf("GetListMemberships: %v", err)
	}
	if gotPath != "/2/users/42/list_memberships" {
		t.Errorf("path = %q, want /2/users/42/list_memberships", gotPath)
	}
}

func TestGetFollowedLists_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetFollowedLists(context.Background(), "42"); err != nil {
		t.Fatalf("GetFollowedLists: %v", err)
	}
	if gotPath != "/2/users/42/followed_lists" {
		t.Errorf("path = %q, want /2/users/42/followed_lists", gotPath)
	}
}

// =============================================================================
// GetPinnedLists
// =============================================================================

func TestGetPinnedLists_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"L1","name":"Pinned"}],"meta":{"result_count":1}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.GetPinnedLists(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetPinnedLists: %v", err)
	}
	if gotPath != "/2/users/42/pinned_lists" {
		t.Errorf("path = %q, want /2/users/42/pinned_lists", gotPath)
	}
	if resp == nil || len(resp.Data) != 1 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestGetPinnedLists_AllOptionsReflected(t *testing.T) {
	t.Parallel()
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetPinnedLists(context.Background(), "42",
		WithListLookupListFields("name", "private"),
		WithListLookupExpansions("owner_id"),
		WithListLookupUserFields("username"),
	)
	if err != nil {
		t.Fatalf("GetPinnedLists: %v", err)
	}
	for _, want := range []string{
		"list.fields=name%2Cprivate",
		"expansions=owner_id",
		"user.fields=username",
	} {
		if !strings.Contains(gotURL, want) {
			t.Errorf("url = %q, missing %q", gotURL, want)
		}
	}
}

func TestGetPinnedLists_EmptyUserID_RejectsArgument(t *testing.T) {
	t.Parallel()
	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetPinnedLists(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "must be non-empty") {
		t.Errorf("err = %v, want 'must be non-empty'", err)
	}
	if got := atomic.LoadInt32(&called); got != 0 {
		t.Errorf("server called %d times, want 0", got)
	}
}
