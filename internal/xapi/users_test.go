package xapi

// 注意: 本ファイルは package xapi の internal test (xapi_test ではない)。
// client_test.go と同じテストヘルパ (newTestClient / withHTTPClient / withSleep) を共有するため。

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

// TestGetUserMe_HitsCorrectEndpoint は GET /2/users/me に対して正しいパスで
// HTTP リクエストが送られることを確認する。
func TestGetUserMe_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserMe(context.Background())
	if err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	if gotPath != "/2/users/me" {
		t.Errorf("path = %q, want /2/users/me", gotPath)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
}

// TestGetUserMe_Success は 200 OK + 正常 JSON で User が返ることを確認する。
func TestGetUserMe_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	u, err := c.GetUserMe(context.Background())
	if err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	if u == nil {
		t.Fatal("user = nil, want non-nil")
	}
	if u.ID != "42" {
		t.Errorf("ID = %q, want %q", u.ID, "42")
	}
	if u.Username != "alice" {
		t.Errorf("Username = %q, want %q", u.Username, "alice")
	}
	if u.Name != "Alice" {
		t.Errorf("Name = %q, want %q", u.Name, "Alice")
	}
}

// TestGetUserMe_Success_WithFields は user.fields 指定時のレスポンスが
// User.Verified / User.PublicMetrics に展開されることを確認する。
func TestGetUserMe_Success_WithFields(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{
			"id":"42","username":"alice","name":"Alice",
			"verified":true,
			"public_metrics":{"followers_count":100,"following_count":50,"tweet_count":300,"listed_count":4}
		}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	u, err := c.GetUserMe(context.Background(), WithUserFields("verified", "public_metrics"))
	if err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	if !u.Verified {
		t.Errorf("Verified = false, want true")
	}
	if u.PublicMetrics == nil {
		t.Fatal("PublicMetrics = nil")
	}
	if u.PublicMetrics.FollowersCount != 100 {
		t.Errorf("FollowersCount = %d, want 100", u.PublicMetrics.FollowersCount)
	}
}

// TestGetUserMe_WithUserFields_AppendsQueryParam は
// WithUserFields(...) が ?user.fields=... に変換されることを確認する。
func TestGetUserMe_WithUserFields_AppendsQueryParam(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"u","name":"N"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserMe(
		context.Background(),
		WithUserFields("username", "name", "verified", "public_metrics"),
	)
	if err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	got := gotQuery.Get("user.fields")
	want := "username,name,verified,public_metrics"
	if got != want {
		t.Errorf("user.fields = %q, want %q", got, want)
	}
}

// TestGetUserMe_NoOptions_NoQuery は opts 無しで呼んだ場合 URL に ? が付かないことを確認する。
func TestGetUserMe_NoOptions_NoQuery(t *testing.T) {
	t.Parallel()

	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"u","name":"N"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetUserMe(context.Background()); err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	if gotRawQuery != "" {
		t.Errorf("RawQuery = %q, want empty", gotRawQuery)
	}
}

// TestGetUserMe_WithUserFields_Empty は WithUserFields() を空引数で呼んでも
// クエリパラメータが付与されないことを確認する。
func TestGetUserMe_WithUserFields_Empty(t *testing.T) {
	t.Parallel()

	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"u","name":"N"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetUserMe(context.Background(), WithUserFields()); err != nil {
		t.Fatalf("GetUserMe: %v", err)
	}
	if gotRawQuery != "" {
		t.Errorf("RawQuery = %q, want empty (WithUserFields() should be no-op)", gotRawQuery)
	}
}

// TestGetUserMe_401_AuthError は 401 で ErrAuthentication が返ることを確認する。
func TestGetUserMe_401_AuthError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserMe(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAuthentication) {
		t.Errorf("errors.Is(err, ErrAuthentication) = false, want true (err=%v)", err)
	}
}

// TestGetUserMe_404_NotFound は 404 で ErrNotFound が返ることを確認する。
func TestGetUserMe_404_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserMe(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("errors.Is(err, ErrNotFound) = false, want true (err=%v)", err)
	}
}

// TestGetUserMe_ContextCanceled は事前に cancel された context を渡すと
// context.Canceled が返ることを確認する。
func TestGetUserMe_ContextCanceled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// このハンドラは呼ばれない想定 (context cancel 済みなので)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 即 cancel

	_, err := c.GetUserMe(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// =============================================================================
// M32: Users Extended テストヘルパ
// =============================================================================

// makeUsersPagedHandler は graph / search endpoint 用の複数ページ ハンドラを返す。
// data はユーザー配列、meta.next_token は次ページがある場合のみ自動付与。
//
// tokenKey はクエリパラメータ名 ("pagination_token" or "next_token")、各 endpoint で異なる。
func makeUsersPagedHandler(t *testing.T, defs []usersPageDef, tokenKey string) (handler http.HandlerFunc, tokensSeen *[]string, requestCounter *int32) {
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
		var dataItems []string
		for _, id := range def.userIDs {
			dataItems = append(dataItems, fmt.Sprintf(`{"id":%q,"username":"u%s","name":"U%s"}`, id, id, id))
		}
		var nextToken string
		if int(idx) < len(defs)-1 {
			nextToken = fmt.Sprintf("P%d", idx+1)
		}
		body := fmt.Sprintf(`{"data":[%s],"meta":{"result_count":%d`,
			strings.Join(dataItems, ","), len(def.userIDs))
		if nextToken != "" {
			body += fmt.Sprintf(`,"next_token":%q`, nextToken)
		}
		body += `}}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
	return handler, &seenTokens, &callCount
}

// usersPageDef は makeUsersPagedHandler の各ページ定義。
type usersPageDef struct {
	userIDs   []string
	rateLimit *struct {
		remaining int
		resetUnix int64
	}
}

// =============================================================================
// M32 T1: Lookup endpoints (4 endpoint)
// =============================================================================

// -- GetUser -----------------------------------------------------------------

func TestGetUser_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.GetUser(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if gotPath != "/2/users/42" {
		t.Errorf("path = %q, want /2/users/42", gotPath)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if resp == nil || resp.Data == nil || resp.Data.ID != "42" {
		t.Errorf("resp = %+v", resp)
	}
}

func TestGetUser_AllOptionsReflected(t *testing.T) {
	t.Parallel()
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"a","name":"A"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUser(context.Background(), "42",
		WithUserLookupUserFields("username", "name", "verified"),
		WithUserLookupExpansions("pinned_tweet_id"),
		WithUserLookupTweetFields("id", "text"),
	)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	for _, want := range []string{
		"user.fields=username%2Cname%2Cverified",
		"expansions=pinned_tweet_id",
		"tweet.fields=id%2Ctext",
	} {
		if !strings.Contains(gotURL, want) {
			t.Errorf("url = %q, missing %q", gotURL, want)
		}
	}
}

func TestGetUser_404_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUser(context.Background(), "42")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestGetUser_EmptyUserID_RejectsArgument(t *testing.T) {
	t.Parallel()
	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUser(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "must be non-empty") {
		t.Errorf("err = %v, want 'must be non-empty'", err)
	}
	if got := atomic.LoadInt32(&called); got != 0 {
		t.Errorf("server called %d times, want 0", got)
	}
}

func TestGetUser_InvalidJSON_NoRetry(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUser(context.Background(), "42")
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

// -- GetUsers ----------------------------------------------------------------

func TestGetUsers_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath, gotIDs string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotIDs = r.URL.Query().Get("ids")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","username":"a","name":"A"},{"id":"2","username":"b","name":"B"}]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.GetUsers(context.Background(), []string{"1", "2"})
	if err != nil {
		t.Fatalf("GetUsers: %v", err)
	}
	if gotPath != "/2/users" {
		t.Errorf("path = %q, want /2/users", gotPath)
	}
	if gotIDs != "1,2" {
		t.Errorf("ids = %q, want 1,2", gotIDs)
	}
	if len(resp.Data) != 2 {
		t.Errorf("len(Data) = %d, want 2", len(resp.Data))
	}
}

func TestGetUsers_TooManyIDs_Rejects(t *testing.T) {
	t.Parallel()
	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = strconv.Itoa(i + 1)
	}
	_, err := c.GetUsers(context.Background(), ids)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "100") {
		t.Errorf("err = %v, want mention of 100", err)
	}
	if got := atomic.LoadInt32(&called); got != 0 {
		t.Errorf("server called %d times, want 0", got)
	}
}

func TestGetUsers_EmptyIDs_Rejects(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	_, err := c.GetUsers(context.Background(), []string{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "non-empty") {
		t.Errorf("err = %v, want 'non-empty'", err)
	}
}

// -- GetUserByUsername -------------------------------------------------------

func TestGetUserByUsername_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if gotPath != "/2/users/by/username/alice" {
		t.Errorf("path = %q, want /2/users/by/username/alice", gotPath)
	}
}

func TestGetUserByUsername_PathEscape(t *testing.T) {
	t.Parallel()
	var gotEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEscapedPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"42","username":"a","name":"A"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetUserByUsername(context.Background(), "a/b"); err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if gotEscapedPath != "/2/users/by/username/a%2Fb" {
		t.Errorf("escaped path = %q, want a%%2Fb", gotEscapedPath)
	}
}

// -- GetUsersByUsernames -----------------------------------------------------

func TestGetUsersByUsernames_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath, gotUsernames string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUsernames = r.URL.Query().Get("usernames")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","username":"alice","name":"Alice"}]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUsersByUsernames(context.Background(), []string{"alice", "bob"})
	if err != nil {
		t.Fatalf("GetUsersByUsernames: %v", err)
	}
	if gotPath != "/2/users/by" {
		t.Errorf("path = %q, want /2/users/by", gotPath)
	}
	if gotUsernames != "alice,bob" {
		t.Errorf("usernames = %q, want alice,bob", gotUsernames)
	}
}

func TestGetUsersByUsernames_TooManyUsernames_Rejects(t *testing.T) {
	t.Parallel()
	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	usernames := make([]string, 101)
	for i := range usernames {
		usernames[i] = fmt.Sprintf("u%d", i)
	}
	_, err := c.GetUsersByUsernames(context.Background(), usernames)
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&called); got != 0 {
		t.Errorf("server called %d times, want 0", got)
	}
}

// =============================================================================
// M32 T1: SearchUsers / EachSearchUsersPage
// =============================================================================

func TestSearchUsers_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("query")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","username":"go_user","name":"Go User"}],"meta":{"result_count":1}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.SearchUsers(context.Background(), "golang")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if gotPath != "/2/users/search" {
		t.Errorf("path = %q, want /2/users/search", gotPath)
	}
	if gotQuery != "golang" {
		t.Errorf("query = %q, want golang", gotQuery)
	}
}

func TestSearchUsers_AllOptionsReflected(t *testing.T) {
	t.Parallel()
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.SearchUsers(context.Background(), "golang",
		WithUserSearchMaxResults(50),
		WithUserSearchNextToken("NTK"),
		WithUserSearchUserFields("username", "name"),
		WithUserSearchExpansions("pinned_tweet_id"),
		WithUserSearchTweetFields("id"),
	)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	for _, want := range []string{
		"query=golang",
		"max_results=50",
		"next_token=NTK",
		"user.fields=username%2Cname",
		"expansions=pinned_tweet_id",
		"tweet.fields=id",
	} {
		if !strings.Contains(gotURL, want) {
			t.Errorf("url = %q, missing %q", gotURL, want)
		}
	}
}

func TestSearchUsers_EmptyQuery_Rejects(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	_, err := c.SearchUsers(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "non-empty") {
		t.Errorf("err = %v, want 'non-empty'", err)
	}
}

func TestEachSearchUsersPage_MultiPage_FullTraversal(t *testing.T) {
	t.Parallel()
	defs := []usersPageDef{
		{userIDs: []string{"1", "2"}},
		{userIDs: []string{"3"}},
	}
	handler, tokensSeen, _ := makeUsersPagedHandler(t, defs, "next_token")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	var got []string
	err := c.EachSearchUsersPage(context.Background(), "golang", func(p *UsersResponse) error {
		for _, u := range p.Data {
			got = append(got, u.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("EachSearchUsersPage: %v", err)
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

func TestSearchUsers_InvalidJSON_NoRetry(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.SearchUsers(context.Background(), "golang")
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
// M32 T1: Graph endpoints (following / followers / blocking / muting)
// =============================================================================

func TestGetFollowing_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetFollowing(context.Background(), "42"); err != nil {
		t.Fatalf("GetFollowing: %v", err)
	}
	if gotPath != "/2/users/42/following" {
		t.Errorf("path = %q, want /2/users/42/following", gotPath)
	}
}

func TestGetFollowers_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetFollowers(context.Background(), "42"); err != nil {
		t.Fatalf("GetFollowers: %v", err)
	}
	if gotPath != "/2/users/42/followers" {
		t.Errorf("path = %q, want /2/users/42/followers", gotPath)
	}
}

func TestGetBlocking_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetBlocking(context.Background(), "42"); err != nil {
		t.Fatalf("GetBlocking: %v", err)
	}
	if gotPath != "/2/users/42/blocking" {
		t.Errorf("path = %q, want /2/users/42/blocking", gotPath)
	}
}

func TestGetMuting_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetMuting(context.Background(), "42"); err != nil {
		t.Fatalf("GetMuting: %v", err)
	}
	if gotPath != "/2/users/42/muting" {
		t.Errorf("path = %q, want /2/users/42/muting", gotPath)
	}
}

func TestGetFollowing_AllOptionsReflected(t *testing.T) {
	t.Parallel()
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetFollowing(context.Background(), "42",
		WithUserGraphMaxResults(500),
		WithUserGraphPaginationToken("TOK"),
		WithUserGraphUserFields("username"),
		WithUserGraphExpansions("pinned_tweet_id"),
		WithUserGraphTweetFields("id"),
	)
	if err != nil {
		t.Fatalf("GetFollowing: %v", err)
	}
	for _, want := range []string{
		"max_results=500",
		"pagination_token=TOK",
		"user.fields=username",
		"expansions=pinned_tweet_id",
		"tweet.fields=id",
	} {
		if !strings.Contains(gotURL, want) {
			t.Errorf("url = %q, missing %q", gotURL, want)
		}
	}
}

func TestGetFollowing_PathEscape(t *testing.T) {
	t.Parallel()
	var gotEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEscapedPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetFollowing(context.Background(), "user/x"); err != nil {
		t.Fatalf("GetFollowing: %v", err)
	}
	if gotEscapedPath != "/2/users/user%2Fx/following" {
		t.Errorf("escaped path = %q, want /2/users/user%%2Fx/following", gotEscapedPath)
	}
}

func TestGetFollowing_EmptyUserID_RejectsArgument(t *testing.T) {
	t.Parallel()
	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetFollowing(context.Background(), "")
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

func TestGetFollowing_401_AuthError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetFollowing(context.Background(), "42")
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("err = %v, want ErrAuthentication", err)
	}
}

func TestEachFollowingPage_MultiPage_FullTraversal(t *testing.T) {
	t.Parallel()
	defs := []usersPageDef{
		{userIDs: []string{"1", "2"}},
		{userIDs: []string{"3", "4"}},
		{userIDs: []string{"5"}},
	}
	handler, tokensSeen, _ := makeUsersPagedHandler(t, defs, "pagination_token")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	var got []string
	pageCount := 0
	err := c.EachFollowingPage(context.Background(), "42", func(p *UsersResponse) error {
		pageCount++
		for _, u := range p.Data {
			got = append(got, u.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("EachFollowingPage: %v", err)
	}
	if pageCount != 3 {
		t.Errorf("page count = %d, want 3", pageCount)
	}
	want := []string{"1", "2", "3", "4", "5"}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	wantTokens := []string{"", "P1", "P2"}
	if len(*tokensSeen) != len(wantTokens) {
		t.Fatalf("tokens = %v, want %v", *tokensSeen, wantTokens)
	}
}

func TestEachFollowersPage_MaxPages_Truncates(t *testing.T) {
	t.Parallel()
	defs := []usersPageDef{
		{userIDs: []string{"1"}},
		{userIDs: []string{"2"}},
		{userIDs: []string{"3"}},
	}
	handler, _, calls := makeUsersPagedHandler(t, defs, "pagination_token")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	pageCount := 0
	err := c.EachFollowersPage(context.Background(), "42", func(_ *UsersResponse) error {
		pageCount++
		return nil
	}, WithUserGraphMaxPages(2))
	if err != nil {
		t.Fatalf("EachFollowersPage: %v", err)
	}
	if pageCount != 2 {
		t.Errorf("callback count = %d, want 2", pageCount)
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Errorf("server calls = %d, want 2", got)
	}
}

func TestEachBlockingPage_RateLimitSleep(t *testing.T) {
	t.Parallel()
	fixedNow := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	resetAt := fixedNow.Add(5 * time.Second)
	defs := []usersPageDef{
		{userIDs: []string{"1"}, rateLimit: &struct {
			remaining int
			resetUnix int64
		}{remaining: 1, resetUnix: resetAt.Unix()}},
		{userIDs: []string{"2"}},
	}
	handler, _, _ := makeUsersPagedHandler(t, defs, "pagination_token")
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, sleeps := newTestClient(t, srv, withNow(fixedTimeNow(fixedNow)))
	err := c.EachBlockingPage(context.Background(), "42", func(_ *UsersResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachBlockingPage: %v", err)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1", len(*sleeps))
	}
	diff := (*sleeps)[0] - 5*time.Second
	if diff < -time.Second || diff > time.Second {
		t.Errorf("sleep[0] = %v, want ≈ 5s", (*sleeps)[0])
	}
}

func TestEachMutingPage_InterPageDelay(t *testing.T) {
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
	err := c.EachMutingPage(context.Background(), "42", func(_ *UsersResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachMutingPage: %v", err)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1", len(*sleeps))
	}
	if (*sleeps)[0] != 200*time.Millisecond {
		t.Errorf("sleep[0] = %v, want 200ms", (*sleeps)[0])
	}
}

// TestGetUserMe_InvalidJSON は 200 だが本体が型不一致 JSON のとき
// 明示的なエラーが返り、リトライしないこと (server call 1 回) を確認する。
func TestGetUserMe_InvalidJSON(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		// data が文字列 (型不一致) で確実に Unmarshal を失敗させる
		_, _ = w.Write([]byte(`{"data":"not-an-object"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetUserMe(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 番兵エラーには該当しないことを確認
	for _, sentinel := range []error{ErrAuthentication, ErrPermission, ErrNotFound, ErrRateLimit} {
		if errors.Is(err, sentinel) {
			t.Errorf("err unexpectedly matched sentinel %v", sentinel)
		}
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error message %q, want substring 'decode'", err.Error())
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server calls = %d, want 1 (no retry on decode error)", got)
	}
}
