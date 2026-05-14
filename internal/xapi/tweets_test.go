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

// -- GetTweet (single) -----------------------------------------------------

// TestGetTweet_HitsCorrectEndpoint は GET /2/tweets/:id の正しいパスを検証する。
func TestGetTweet_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"100","text":"hi"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetTweet(context.Background(), "100")
	if err != nil {
		t.Fatalf("GetTweet: %v", err)
	}
	if gotPath != "/2/tweets/100" {
		t.Errorf("path = %q, want /2/tweets/100", gotPath)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
}

// TestGetTweet_Success は data + includes が読めることを確認する。
func TestGetTweet_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data":{"id":"100","text":"hi","author_id":"42","note_tweet":{"text":"full body"}},
			"includes":{"users":[{"id":"42","username":"alice","name":"Alice"}]}
		}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.GetTweet(context.Background(), "100")
	if err != nil {
		t.Fatalf("GetTweet: %v", err)
	}
	if resp == nil || resp.Data == nil {
		t.Fatalf("resp/Data = nil")
	}
	if resp.Data.ID != "100" || resp.Data.Text != "hi" || resp.Data.AuthorID != "42" {
		t.Errorf("Data = %+v", resp.Data)
	}
	if resp.Data.NoteTweet == nil || resp.Data.NoteTweet.Text != "full body" {
		t.Errorf("NoteTweet = %+v", resp.Data.NoteTweet)
	}
	if len(resp.Includes.Users) != 1 || resp.Includes.Users[0].Username != "alice" {
		t.Errorf("Includes.Users = %+v", resp.Includes.Users)
	}
}

// TestGetTweet_PathEscape は tweetID の url.PathEscape を確認する。
func TestGetTweet_PathEscape(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"x"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.GetTweet(context.Background(), "100/admin"); err != nil {
		t.Fatalf("GetTweet: %v", err)
	}
	if gotPath != "/2/tweets/100%2Fadmin" {
		t.Errorf("path = %q, want /2/tweets/100%%2Fadmin", gotPath)
	}
}

// TestGetTweet_QueryParams は Option クエリ反映を検証する。
func TestGetTweet_QueryParams(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"100"}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetTweet(
		context.Background(), "100",
		WithGetTweetFields("id", "text", "note_tweet"),
		WithGetTweetExpansions("author_id"),
		WithGetTweetUserFields("username", "name"),
		WithGetTweetMediaFields("type", "url"),
	)
	if err != nil {
		t.Fatalf("GetTweet: %v", err)
	}
	if got, want := gotQuery.Get("tweet.fields"), "id,text,note_tweet"; got != want {
		t.Errorf("tweet.fields = %q, want %q", got, want)
	}
	if got, want := gotQuery.Get("expansions"), "author_id"; got != want {
		t.Errorf("expansions = %q, want %q", got, want)
	}
	if got, want := gotQuery.Get("user.fields"), "username,name"; got != want {
		t.Errorf("user.fields = %q, want %q", got, want)
	}
	if got, want := gotQuery.Get("media.fields"), "type,url"; got != want {
		t.Errorf("media.fields = %q, want %q", got, want)
	}
}

// TestGetTweet_401_AuthError は 401 → ErrAuthentication を検証する。
func TestGetTweet_401_AuthError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetTweet(context.Background(), "100")
	if !errors.Is(err, ErrAuthentication) {
		t.Errorf("err = %v, want ErrAuthentication", err)
	}
}

// TestGetTweet_404_NotFound は 404 → ErrNotFound を検証する。
func TestGetTweet_404_NotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetTweet(context.Background(), "100")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestGetTweet_InvalidJSON は decode エラーで decode 文字列が含まれることを確認する。
func TestGetTweet_InvalidJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":"not-an-object"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetTweet(context.Background(), "100")
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v, want substring 'decode'", err)
	}
}

// -- GetTweets (batch) -----------------------------------------------------

// TestGetTweets_HitsCorrectEndpoint は GET /2/tweets?ids=... の正しい URL を検証する。
func TestGetTweets_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath, gotIDs string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotIDs = r.URL.Query().Get("ids")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetTweets(context.Background(), []string{"1", "2", "3"})
	if err != nil {
		t.Fatalf("GetTweets: %v", err)
	}
	if gotPath != "/2/tweets" {
		t.Errorf("path = %q, want /2/tweets", gotPath)
	}
	if gotIDs != "1,2,3" {
		t.Errorf("ids = %q, want 1,2,3", gotIDs)
	}
}

// TestGetTweets_Success は data 配列と includes をデコードできることを確認する。
func TestGetTweets_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data":[
				{"id":"1","text":"a"},
				{"id":"2","text":"b"}
			],
			"includes":{"users":[{"id":"42","username":"alice","name":"Alice"}]}
		}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.GetTweets(context.Background(), []string{"1", "2"})
	if err != nil {
		t.Fatalf("GetTweets: %v", err)
	}
	if len(resp.Data) != 2 || resp.Data[0].ID != "1" || resp.Data[1].ID != "2" {
		t.Errorf("Data = %+v", resp.Data)
	}
	if len(resp.Includes.Users) != 1 {
		t.Errorf("Includes.Users = %+v", resp.Includes.Users)
	}
}

// TestGetTweets_EmptyIDs はバリデーションエラーを確認する。
func TestGetTweets_EmptyIDs(t *testing.T) {
	t.Parallel()

	c, _ := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	_, err := c.GetTweets(context.Background(), []string{})
	if err == nil {
		t.Fatal("expected error for empty ids, got nil")
	}
	if !strings.Contains(err.Error(), "ids") {
		t.Errorf("err = %v, want substring 'ids'", err)
	}
}

// TestGetTweets_TooManyIDs は 101 件で X API 上限エラーを返すことを確認する。
func TestGetTweets_TooManyIDs(t *testing.T) {
	t.Parallel()

	ids := make([]string, 101)
	for i := range ids {
		ids[i] = "1"
	}
	c, _ := newTestClient(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	_, err := c.GetTweets(context.Background(), ids)
	if err == nil {
		t.Fatal("expected error for 101 ids, got nil")
	}
	if !strings.Contains(err.Error(), "100") {
		t.Errorf("err = %v, want substring '100'", err)
	}
}

// TestGetTweets_TopLevelErrors は X API のバッチ partial error を
// TweetsResponse.Errors にデコードできることを確認する (D-9)。
func TestGetTweets_TopLevelErrors(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data":[{"id":"1","text":"a"}],
			"errors":[{
				"value":"2",
				"detail":"Could not find tweet with ids: [2].",
				"title":"Not Found Error",
				"resource_type":"tweet",
				"parameter":"ids",
				"resource_id":"2",
				"type":"https://api.twitter.com/2/problems/resource-not-found"
			}]
		}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.GetTweets(context.Background(), []string{"1", "2"})
	if err != nil {
		t.Fatalf("GetTweets: %v", err)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("Errors len = %d, want 1", len(resp.Errors))
	}
	e := resp.Errors[0]
	if e.Value != "2" || e.ResourceType != "tweet" || e.ResourceID != "2" || e.Parameter != "ids" {
		t.Errorf("Errors[0] = %+v", e)
	}
	if e.Title != "Not Found Error" || !strings.Contains(e.Detail, "Could not find") {
		t.Errorf("Errors[0] title/detail = %+v", e)
	}
}

// -- Social Signals --------------------------------------------------------

// TestGetLikingUsers_HitsCorrectEndpoint は GET /2/tweets/:id/liking_users の正しいパスを検証する。
func TestGetLikingUsers_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetLikingUsers(context.Background(), "100")
	if err != nil {
		t.Fatalf("GetLikingUsers: %v", err)
	}
	if gotPath != "/2/tweets/100/liking_users" {
		t.Errorf("path = %q, want /2/tweets/100/liking_users", gotPath)
	}
}

// TestGetLikingUsers_Success は data 配列・meta が読めることを確認する。
func TestGetLikingUsers_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data":[
				{"id":"7","username":"bob","name":"Bob"},
				{"id":"8","username":"carol","name":"Carol"}
			],
			"meta":{"result_count":2,"next_token":"NEXTT"}
		}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.GetLikingUsers(context.Background(), "100")
	if err != nil {
		t.Fatalf("GetLikingUsers: %v", err)
	}
	if len(resp.Data) != 2 || resp.Data[0].Username != "bob" || resp.Data[1].Username != "carol" {
		t.Errorf("Data = %+v", resp.Data)
	}
	if resp.Meta.ResultCount != 2 || resp.Meta.NextToken != "NEXTT" {
		t.Errorf("Meta = %+v", resp.Meta)
	}
}

// TestGetLikingUsers_QueryParams は max_results / pagination_token / user.fields 等が反映されることを検証する。
func TestGetLikingUsers_QueryParams(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetLikingUsers(
		context.Background(), "100",
		WithUsersByTweetMaxResults(50),
		WithUsersByTweetPaginationToken("PTOK"),
		WithUsersByTweetUserFields("username", "name"),
		WithUsersByTweetExpansions("pinned_tweet_id"),
		WithUsersByTweetTweetFields("id", "text"),
	)
	if err != nil {
		t.Fatalf("GetLikingUsers: %v", err)
	}
	if gotQuery.Get("max_results") != "50" {
		t.Errorf("max_results = %q", gotQuery.Get("max_results"))
	}
	if gotQuery.Get("pagination_token") != "PTOK" {
		t.Errorf("pagination_token = %q", gotQuery.Get("pagination_token"))
	}
	if gotQuery.Get("user.fields") != "username,name" {
		t.Errorf("user.fields = %q", gotQuery.Get("user.fields"))
	}
	if gotQuery.Get("expansions") != "pinned_tweet_id" {
		t.Errorf("expansions = %q", gotQuery.Get("expansions"))
	}
	if gotQuery.Get("tweet.fields") != "id,text" {
		t.Errorf("tweet.fields = %q", gotQuery.Get("tweet.fields"))
	}
}

// TestGetLikingUsers_401 は 401 → ErrAuthentication を確認する。
func TestGetLikingUsers_401(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetLikingUsers(context.Background(), "100")
	if !errors.Is(err, ErrAuthentication) {
		t.Errorf("err = %v, want ErrAuthentication", err)
	}
}

// TestGetRetweetedBy_HitsCorrectEndpoint は GET /2/tweets/:id/retweeted_by の正しいパスを検証する。
func TestGetRetweetedBy_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetRetweetedBy(context.Background(), "100")
	if err != nil {
		t.Fatalf("GetRetweetedBy: %v", err)
	}
	if gotPath != "/2/tweets/100/retweeted_by" {
		t.Errorf("path = %q, want /2/tweets/100/retweeted_by", gotPath)
	}
}

// TestGetRetweetedBy_404 は 404 → ErrNotFound を確認する。
func TestGetRetweetedBy_404(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetRetweetedBy(context.Background(), "100")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestGetQuoteTweets_HitsCorrectEndpoint は GET /2/tweets/:id/quote_tweets の正しいパスを検証する。
func TestGetQuoteTweets_HitsCorrectEndpoint(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetQuoteTweets(context.Background(), "100")
	if err != nil {
		t.Fatalf("GetQuoteTweets: %v", err)
	}
	if gotPath != "/2/tweets/100/quote_tweets" {
		t.Errorf("path = %q, want /2/tweets/100/quote_tweets", gotPath)
	}
}

// TestGetQuoteTweets_Success は data 配列・meta が読めることを確認する。
func TestGetQuoteTweets_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data":[{"id":"200","text":"quote!","author_id":"9"}],
			"includes":{"users":[{"id":"9","username":"dave","name":"Dave"}]},
			"meta":{"result_count":1}
		}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	resp, err := c.GetQuoteTweets(context.Background(), "100")
	if err != nil {
		t.Fatalf("GetQuoteTweets: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "200" {
		t.Errorf("Data = %+v", resp.Data)
	}
	if len(resp.Includes.Users) != 1 || resp.Includes.Users[0].Username != "dave" {
		t.Errorf("Includes.Users = %+v", resp.Includes.Users)
	}
}

// TestGetQuoteTweets_QueryParams は max_results / pagination_token / exclude / 各 fields が反映されることを確認する。
func TestGetQuoteTweets_QueryParams(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetQuoteTweets(
		context.Background(), "100",
		WithQuoteTweetsMaxResults(20),
		WithQuoteTweetsPaginationToken("QTOK"),
		WithQuoteTweetsExclude("retweets", "replies"),
		WithQuoteTweetsTweetFields("id", "text", "note_tweet"),
		WithQuoteTweetsExpansions("author_id"),
		WithQuoteTweetsUserFields("username", "name"),
		WithQuoteTweetsMediaFields("type"),
	)
	if err != nil {
		t.Fatalf("GetQuoteTweets: %v", err)
	}
	if gotQuery.Get("max_results") != "20" {
		t.Errorf("max_results = %q", gotQuery.Get("max_results"))
	}
	if gotQuery.Get("pagination_token") != "QTOK" {
		t.Errorf("pagination_token = %q", gotQuery.Get("pagination_token"))
	}
	if gotQuery.Get("exclude") != "retweets,replies" {
		t.Errorf("exclude = %q", gotQuery.Get("exclude"))
	}
	if gotQuery.Get("tweet.fields") != "id,text,note_tweet" {
		t.Errorf("tweet.fields = %q", gotQuery.Get("tweet.fields"))
	}
	if gotQuery.Get("expansions") != "author_id" {
		t.Errorf("expansions = %q", gotQuery.Get("expansions"))
	}
	if gotQuery.Get("user.fields") != "username,name" {
		t.Errorf("user.fields = %q", gotQuery.Get("user.fields"))
	}
	if gotQuery.Get("media.fields") != "type" {
		t.Errorf("media.fields = %q", gotQuery.Get("media.fields"))
	}
}

// TestGetTweets_QueryParams は Option クエリ反映を確認する。
func TestGetTweets_QueryParams(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.GetTweets(
		context.Background(), []string{"1"},
		WithGetTweetFields("id", "text"),
		WithGetTweetExpansions("author_id"),
	)
	if err != nil {
		t.Fatalf("GetTweets: %v", err)
	}
	if got := gotQuery.Get("tweet.fields"); got != "id,text" {
		t.Errorf("tweet.fields = %q", got)
	}
	if got := gotQuery.Get("expansions"); got != "author_id" {
		t.Errorf("expansions = %q", got)
	}
}

// -- SearchRecent ----------------------------------------------------------

// TestSearchRecent_HitsCorrectEndpoint は GET /2/tweets/search/recent の正しいパスを検証する。
func TestSearchRecent_HitsCorrectEndpoint(t *testing.T) {
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
	_, err := c.SearchRecent(context.Background(), "from:youyo")
	if err != nil {
		t.Fatalf("SearchRecent: %v", err)
	}
	if gotPath != "/2/tweets/search/recent" {
		t.Errorf("path = %q, want /2/tweets/search/recent", gotPath)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
}

// TestSearchRecent_QueryRequired は query="" を事前バリデーションで拒否することを検証する
// (httptest 呼ばれない)。
func TestSearchRecent_QueryRequired(t *testing.T) {
	t.Parallel()

	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.SearchRecent(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
	if !strings.Contains(err.Error(), "query must be non-empty") {
		t.Errorf("error = %q, want substring 'query must be non-empty'", err.Error())
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("server should not be called for empty query")
	}
}

// TestSearchRecent_QueryInURL は query パラメータがクエリに反映されることを検証する。
// 値は r.URL.Query().Get で auto-decode され "from:youyo" が読めることを確認する (D-3)。
func TestSearchRecent_QueryInURL(t *testing.T) {
	t.Parallel()

	var gotQueryParam, gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQueryParam = r.URL.Query().Get("query")
		gotRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.SearchRecent(context.Background(), "from:youyo")
	if err != nil {
		t.Fatalf("SearchRecent: %v", err)
	}
	if gotQueryParam != "from:youyo" {
		t.Errorf("query = %q, want from:youyo (auto-decoded)", gotQueryParam)
	}
	// D-3 wire-format pin: url.Values.Encode は `:` を `%3A` にエスケープする。
	// この期待値は CLI が送出する実際のフォーマットを documentation する。
	if !strings.Contains(gotRawQuery, "query=from%3Ayouyo") {
		t.Errorf("RawQuery = %q, want substring 'query=from%%3Ayouyo'", gotRawQuery)
	}
}

// TestSearchRecent_QueryOperators_ConversationID は conversation_id: 演算子も
// 同様にエンコードされることを検証する (thread サブコマンドの基礎)。
func TestSearchRecent_QueryOperators_ConversationID(t *testing.T) {
	t.Parallel()

	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.SearchRecent(context.Background(), "conversation_id:1234567"); err != nil {
		t.Fatalf("SearchRecent: %v", err)
	}
	if !strings.Contains(gotRawQuery, "query=conversation_id%3A1234567") {
		t.Errorf("RawQuery = %q, want 'query=conversation_id%%3A1234567'", gotRawQuery)
	}
}

// TestSearchRecent_MaxResultsInQuery は WithSearchMaxResults がクエリに反映されることを検証する。
func TestSearchRecent_MaxResultsInQuery(t *testing.T) {
	t.Parallel()

	var gotMax string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMax = r.URL.Query().Get("max_results")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	if _, err := c.SearchRecent(context.Background(), "test", WithSearchMaxResults(10)); err != nil {
		t.Fatalf("SearchRecent: %v", err)
	}
	if gotMax != "10" {
		t.Errorf("max_results = %q, want 10", gotMax)
	}
}

// TestSearchRecent_AllOptionsReflected は全 Option が URL クエリに反映されることを検証する。
func TestSearchRecent_AllOptionsReflected(t *testing.T) {
	t.Parallel()

	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	start := time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 12, 23, 59, 59, 0, time.UTC)
	_, err := c.SearchRecent(
		context.Background(), "test",
		WithSearchMaxResults(50),
		WithSearchStartTime(start),
		WithSearchEndTime(end),
		WithSearchPaginationToken("PTOK"),
		WithSearchTweetFields("id", "text", "conversation_id"),
		WithSearchExpansions("author_id"),
		WithSearchUserFields("username"),
		WithSearchMediaFields("url"),
	)
	if err != nil {
		t.Fatalf("SearchRecent: %v", err)
	}
	checks := map[string]string{
		"max_results":      "50",
		"start_time":       "2026-05-12T00:00:00Z",
		"end_time":         "2026-05-12T23:59:59Z",
		"pagination_token": "PTOK",
		"tweet.fields":     "id,text,conversation_id",
		"expansions":       "author_id",
		"user.fields":      "username",
		"media.fields":     "url",
	}
	for k, want := range checks {
		if got := gotQuery.Get(k); got != want {
			t.Errorf("query[%s] = %q, want %q", k, got, want)
		}
	}
}

// TestSearchRecent_StartTimeRFC3339Z は WithSearchStartTime のフォーマットを検証する (ナノ秒切り捨て)。
func TestSearchRecent_StartTimeRFC3339Z(t *testing.T) {
	t.Parallel()

	var gotStart string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotStart = r.URL.Query().Get("start_time")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	start := time.Date(2026, 5, 12, 0, 0, 0, 999999999, time.UTC)
	if _, err := c.SearchRecent(context.Background(), "test", WithSearchStartTime(start)); err != nil {
		t.Fatalf("SearchRecent: %v", err)
	}
	if gotStart != "2026-05-12T00:00:00Z" {
		t.Errorf("start_time = %q, want 2026-05-12T00:00:00Z (no nanoseconds)", gotStart)
	}
}

// TestSearchRecent_401_AuthError は 401 で ErrAuthentication が返ることを検証する。
func TestSearchRecent_401_AuthError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.SearchRecent(context.Background(), "test")
	if !errors.Is(err, ErrAuthentication) {
		t.Errorf("errors.Is(err, ErrAuthentication) = false (err=%v)", err)
	}
}

// TestSearchRecent_403_Permission は 403 で ErrPermission が返ることを検証する (Free tier シナリオ)。
func TestSearchRecent_403_Permission(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.SearchRecent(context.Background(), "test")
	if !errors.Is(err, ErrPermission) {
		t.Errorf("errors.Is(err, ErrPermission) = false (err=%v) — Free tier 403 シナリオ", err)
	}
}

// TestSearchRecent_InvalidJSON_NoRetry は 200 + 型不一致 JSON で decode エラーが
// 返り、リトライしないことを検証する。
func TestSearchRecent_InvalidJSON_NoRetry(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":"not-an-array"}`))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	_, err := c.SearchRecent(context.Background(), "test")
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error = %q, want substring 'decode'", err.Error())
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server calls = %d, want 1 (no retry on decode error)", got)
	}
}

// -- EachSearchPage --------------------------------------------------------

// makeSearchPagedHandler は next_token で連鎖する複数 search ページのハンドラを返す。
func makeSearchPagedHandler(t *testing.T, defs []pageDef) (handler http.HandlerFunc, tokensSeen *[]string, requestCounter *int32) {
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
			w.Header().Set("x-rate-limit-limit", "60")
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

// TestEachSearchPage_MultiPage_FullTraversal は 3 ページ走破を検証する。
func TestEachSearchPage_MultiPage_FullTraversal(t *testing.T) {
	t.Parallel()

	defs := []pageDef{
		{tweetIDs: []string{"1", "2"}},
		{tweetIDs: []string{"3", "4"}},
		{tweetIDs: []string{"5"}},
	}
	handler, tokensSeen, _ := makeSearchPagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	var got []string
	pageCount := 0
	err := c.EachSearchPage(context.Background(), "test", func(p *SearchResponse) error {
		pageCount++
		for _, tw := range p.Data {
			got = append(got, tw.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("EachSearchPage: %v", err)
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
	for i, w := range wantTokens {
		if (*tokensSeen)[i] != w {
			t.Errorf("tokens[%d] = %q, want %q", i, (*tokensSeen)[i], w)
		}
	}
}

// TestEachSearchPage_MaxPages_Truncates は max_pages=2 で 2 ページに打ち切られることを検証する。
func TestEachSearchPage_MaxPages_Truncates(t *testing.T) {
	t.Parallel()

	defs := []pageDef{
		{tweetIDs: []string{"1"}},
		{tweetIDs: []string{"2"}},
		{tweetIDs: []string{"3"}}, // 呼ばれないはず
	}
	handler, _, calls := makeSearchPagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, _ := newTestClient(t, srv)
	pageCount := 0
	err := c.EachSearchPage(context.Background(), "test", func(_ *SearchResponse) error {
		pageCount++
		return nil
	}, WithSearchMaxPages(2))
	if err != nil {
		t.Fatalf("EachSearchPage: %v", err)
	}
	if pageCount != 2 {
		t.Errorf("callback count = %d, want 2", pageCount)
	}
	if got := atomic.LoadInt32(calls); got != 2 {
		t.Errorf("server calls = %d, want 2", got)
	}
}

// TestEachSearchPage_RateLimitSleep は rate-limit remaining が閾値以下のとき
// reset まで sleep されることを検証する。
func TestEachSearchPage_RateLimitSleep(t *testing.T) {
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
	handler, _, _ := makeSearchPagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, sleeps := newTestClient(t, srv, withNow(fixedTimeNow(fixedNow)))
	err := c.EachSearchPage(context.Background(), "test", func(_ *SearchResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachSearchPage: %v", err)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1 (sleeps=%v)", len(*sleeps), *sleeps)
	}
	diff := (*sleeps)[0] - 5*time.Second
	if diff < -time.Second || diff > time.Second {
		t.Errorf("sleep[0] = %v, want ≈ 5s", (*sleeps)[0])
	}
}

// TestEachSearchPage_InterPageDelay は rate-limit に余裕がある場合 200ms 待機を検証する。
func TestEachSearchPage_InterPageDelay(t *testing.T) {
	t.Parallel()

	defs := []pageDef{
		{tweetIDs: []string{"1"}, rateLimit: &struct {
			remaining int
			resetUnix int64
		}{remaining: 50, resetUnix: time.Now().Add(900 * time.Second).Unix()}},
		{tweetIDs: []string{"2"}},
	}
	handler, _, _ := makeSearchPagedHandler(t, defs)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c, sleeps := newTestClient(t, srv)
	err := c.EachSearchPage(context.Background(), "test", func(_ *SearchResponse) error { return nil })
	if err != nil {
		t.Fatalf("EachSearchPage: %v", err)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1 (sleeps=%v)", len(*sleeps), *sleeps)
	}
	if (*sleeps)[0] != 200*time.Millisecond {
		t.Errorf("sleep[0] = %v, want 200ms", (*sleeps)[0])
	}
}
