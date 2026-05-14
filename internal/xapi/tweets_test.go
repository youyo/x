package xapi

// 注意: 本ファイルは package xapi の internal test (xapi_test ではない)。
// client_test.go と同じテストヘルパ (newTestClient / withHTTPClient / withSleep) を共有するため。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
