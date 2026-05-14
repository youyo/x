package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// stubTweetClientFactory は newTweetClient を httptest サーバ向けに差し替える。
func stubTweetClientFactory(t *testing.T, baseURL string) {
	t.Helper()
	prev := newTweetClient
	t.Cleanup(func() { newTweetClient = prev })
	newTweetClient = func(ctx context.Context, _ *config.Credentials) (tweetClient, error) {
		return xapi.NewClient(ctx, nil, xapi.WithBaseURL(baseURL)), nil
	}
}

// tweetHandlerState は httptest ハンドラが受信したリクエストを記録する。
type tweetHandlerState struct {
	mu    sync.Mutex
	paths []string
	rawQs []string
}

func (s *tweetHandlerState) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths = append(s.paths, r.URL.Path)
	s.rawQs = append(s.rawQs, r.URL.RawQuery)
}

func (s *tweetHandlerState) snapshot() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...), append([]string(nil), s.rawQs...)
}

// -- extractTweetID -------------------------------------------------------

func TestExtractTweetID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"numeric ID", "1234567890", "1234567890", false},
		{"numeric ID with whitespace", "  1234567890  ", "1234567890", false},
		{"x.com status URL", "https://x.com/youyo/status/1798765432109876543", "1798765432109876543", false},
		{"twitter.com status URL", "https://twitter.com/youyo/status/1798765432109876543", "1798765432109876543", false},
		{"mobile.twitter.com URL", "https://mobile.twitter.com/youyo/status/123", "123", false},
		{"statuses (legacy)", "https://x.com/youyo/statuses/100", "100", false},
		{"i/web/status URL", "https://x.com/i/web/status/200", "200", false},
		{"URL with query string", "https://x.com/u/status/300?s=20&t=abc", "300", false},
		{"URL with fragment", "https://x.com/u/status/400#bottom", "400", false},
		{"empty string", "", "", true},
		{"non-numeric non-URL", "not-a-tweet", "", true},
		{"URL without status", "https://x.com/youyo", "", true},
		{"URL with non-numeric id", "https://x.com/u/status/abc", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractTweetID(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				if !errors.Is(err, ErrInvalidArgument) {
					t.Errorf("errors.Is(err, ErrInvalidArgument) = false (err=%v)", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got = %q, want %q", got, tc.want)
			}
		})
	}
}

// -- tweet get (single) ---------------------------------------------------

// newTweetTestServer はテスト用 httptest サーバを返す。
//
// テスト本体は handler を渡して挙動を切り替える。
func newTweetTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *tweetHandlerState) {
	t.Helper()
	state := &tweetHandlerState{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.record(r)
		if handler != nil {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// TestTweetGet_Single_ByID は数値 ID 直接指定で GetTweet を呼ぶことを確認する。
func TestTweetGet_Single_ByID(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"100","text":"hi","author_id":"42"}}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "get", "100"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) != 1 || paths[0] != "/2/tweets/100" {
		t.Errorf("paths = %v, want [/2/tweets/100]", paths)
	}
	var got xapi.TweetResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v out=%q", err, buf.String())
	}
	if got.Data == nil || got.Data.ID != "100" {
		t.Errorf("data = %+v", got.Data)
	}
}

// TestTweetGet_Single_ByURL は URL 引数からの ID 抽出を確認する。
func TestTweetGet_Single_ByURL(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"100"}}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "get", "https://x.com/youyo/status/100?s=20"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) != 1 || paths[0] != "/2/tweets/100" {
		t.Errorf("paths = %v, want [/2/tweets/100]", paths)
	}
}

// TestTweetGet_Single_NoteTweet_HumanOutput は --no-json で note_tweet.text が
// 優先表示されることを確認する。
func TestTweetGet_Single_NoteTweet_HumanOutput(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"100","text":"truncated…","author_id":"42","note_tweet":{"text":"FULL BODY HERE"}}}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "get", "100", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "text=FULL BODY HERE") {
		t.Errorf("expected text=FULL BODY HERE, got: %q", out)
	}
	if strings.Contains(out, "truncated") {
		t.Errorf("did not expect truncated text in output, got: %q", out)
	}
}

// TestTweetGet_Batch_IDs は --ids でバッチ取得を行うことを確認する。
func TestTweetGet_Batch_IDs(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1"},{"id":"2"},{"id":"3"}]}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "get", "--ids", "1,2,3"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, qs := state.snapshot()
	if len(paths) != 1 || paths[0] != "/2/tweets" {
		t.Errorf("paths = %v, want [/2/tweets]", paths)
	}
	if !strings.Contains(qs[0], "ids=1%2C2%2C3") {
		t.Errorf("query = %q, want ids=1,2,3", qs[0])
	}
}

// TestTweetGet_ArgAndIDsConflict は引数と --ids 両方指定でエラーを確認する。
func TestTweetGet_ArgAndIDsConflict(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newTweetTestServer(t, nil)
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "get", "100", "--ids", "1,2"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("errors.Is(err, ErrInvalidArgument) = false (err=%v)", err)
	}
}

// TestTweetGet_NoArg_NoIDs は引数も --ids も未指定でエラーを確認する。
func TestTweetGet_NoArg_NoIDs(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newTweetTestServer(t, nil)
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "get"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("errors.Is(err, ErrInvalidArgument) = false (err=%v)", err)
	}
}

// TestTweetGet_PartialErrors_Warning は --no-json + partial error で stderr に warning を出すことを確認する。
func TestTweetGet_PartialErrors_Warning(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data":[{"id":"1","text":"ok"}],
			"errors":[{"resource_id":"2","detail":"Could not find tweet with ids: [2].","resource_type":"tweet"}]
		}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	cmd.SetOut(stdoutBuf)
	cmd.SetErr(stderrBuf)
	cmd.SetArgs([]string{"tweet", "get", "--ids", "1,2", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(stderrBuf.String(), "id=2") {
		t.Errorf("stderr missing partial error warning: %q", stderrBuf.String())
	}
	if !strings.Contains(stdoutBuf.String(), "id=1") {
		t.Errorf("stdout missing id=1: %q", stdoutBuf.String())
	}
}

// -- tweet liking-users ---------------------------------------------------

func TestTweetLikingUsers_Success(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"7","username":"bob","name":"Bob"}],"meta":{"result_count":1}}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "liking-users", "100", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) != 1 || paths[0] != "/2/tweets/100/liking_users" {
		t.Errorf("paths = %v", paths)
	}
	out := buf.String()
	for _, want := range []string{"id=7", "username=bob", "name=Bob"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %q", want, out)
		}
	}
}

func TestTweetLikingUsers_InvalidMaxResults(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newTweetTestServer(t, nil)
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "liking-users", "100", "--max-results", "0"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

// -- tweet retweeted-by ---------------------------------------------------

func TestTweetRetweetedBy_Success(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"8","username":"carol","name":"Carol"}]}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "retweeted-by", "https://x.com/u/status/100"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) != 1 || paths[0] != "/2/tweets/100/retweeted_by" {
		t.Errorf("paths = %v", paths)
	}
	var got xapi.UsersByTweetResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got.Data) != 1 || got.Data[0].Username != "carol" {
		t.Errorf("Data = %+v", got.Data)
	}
}

// -- tweet quote-tweets ---------------------------------------------------

func TestTweetQuoteTweets_ExcludeReflected(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "quote-tweets", "100", "--exclude", "retweets,replies"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, qs := state.snapshot()
	if len(paths) != 1 || paths[0] != "/2/tweets/100/quote_tweets" {
		t.Errorf("paths = %v", paths)
	}
	if !strings.Contains(qs[0], "exclude=retweets%2Creplies") {
		t.Errorf("query missing exclude=retweets,replies: %q", qs[0])
	}
}

// -- tweet root help ------------------------------------------------------

func TestTweetHelp(t *testing.T) {
	t.Parallel()
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, sub := range []string{"get", "liking-users", "retweeted-by", "quote-tweets", "search", "thread"} {
		if !strings.Contains(out, sub) {
			t.Errorf("tweet help missing subcommand %q, got: %q", sub, out)
		}
	}
}

// =========================================================================
// M30: tweet search / tweet thread tests
// =========================================================================

// -- tweet search ---------------------------------------------------------

// newSearchPagedServer は 1 ページ / 複数ページの search/recent モックを返す。
//
// pages の各要素は [tweetIDs..., nextToken?]。nextToken が空のページが最終。
type searchPage struct {
	tweetIDs  []string
	nextToken string
}

func newSearchPagedServer(t *testing.T, pages []searchPage) (*httptest.Server, *tweetHandlerState) {
	t.Helper()
	state := &tweetHandlerState{}
	var idx int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.record(r)
		i := int(atomic.AddInt32(&idx, 1) - 1)
		if i >= len(pages) {
			t.Errorf("server called more times (%d) than pages (%d)", i+1, len(pages))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		p := pages[i]
		var items []string
		for _, id := range p.tweetIDs {
			items = append(items, `{"id":"`+id+`","text":"t`+id+`","author_id":"42","created_at":"2026-05-`+id+`T00:00:00Z"}`)
		}
		body := `{"data":[` + strings.Join(items, ",") + `],"meta":{"result_count":` + strconv.Itoa(len(p.tweetIDs))
		if p.nextToken != "" {
			body += `,"next_token":"` + p.nextToken + `"`
		}
		body += `}}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// TestTweetSearch_BasicQuery_DefaultJSON は 1 ページ取得で JSON 出力に data が含まれることを確認する。
func TestTweetSearch_BasicQuery_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"hi","author_id":"42"}],"meta":{"result_count":1}}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "search", "from:youyo"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, qs := state.snapshot()
	if len(paths) != 1 || paths[0] != "/2/tweets/search/recent" {
		t.Errorf("paths = %v", paths)
	}
	if !strings.Contains(qs[0], "query=from%3Ayouyo") {
		t.Errorf("query missing 'query=from%%3Ayouyo': %q", qs[0])
	}
	var got xapi.SearchResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v out=%q", err, buf.String())
	}
	if len(got.Data) != 1 || got.Data[0].ID != "1" {
		t.Errorf("Data = %+v", got.Data)
	}
}

// TestTweetSearch_QueryTrimmed は前後 whitespace の trim を検証する。
func TestTweetSearch_QueryTrimmed(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "search", "  from:youyo  "})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	if strings.Contains(qs[0], "+from") || strings.Contains(qs[0], "%20from") {
		t.Errorf("query has leading whitespace, expected trim: %q", qs[0])
	}
	if !strings.Contains(qs[0], "query=from%3Ayouyo") {
		t.Errorf("query missing trimmed value: %q", qs[0])
	}
}

// TestTweetSearch_EmptyQuery_RejectsArgument は空 query で exit 2 + httptest 呼ばれない。
func TestTweetSearch_EmptyQuery_RejectsArgument(t *testing.T) {
	setAllXAPIEnv(t)

	var called int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "search", "   "})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for empty query")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("server should not be called (called=%d)", called)
	}
}

// TestTweetSearch_NoJSON_HumanFormat は --no-json で 1 行/tweet 形式を確認する。
func TestTweetSearch_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"hello","author_id":"42","created_at":"2026-05-12T00:00:00Z"}]}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "search", "test", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "id=1") || !strings.Contains(out, "text=hello") {
		t.Errorf("human output missing id/text: %q", out)
	}
}

// TestTweetSearch_NoJSON_NoteTweetPreferred は note_tweet.text 優先表示を確認する。
func TestTweetSearch_NoJSON_NoteTweetPreferred(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"truncated","author_id":"42","note_tweet":{"text":"FULL CONTENT"}}]}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "search", "test", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "text=FULL CONTENT") {
		t.Errorf("expected text=FULL CONTENT, got: %q", out)
	}
	if strings.Contains(out, "truncated") {
		t.Errorf("did not expect 'truncated' in output: %q", out)
	}
}

// TestTweetSearch_MaxResultsBelow10_SinglePage_TruncatesAndSendsTen は
// --max-results 3 → API 10 を送り、応答を [:3] で truncate することを確認する。
func TestTweetSearch_MaxResultsBelow10_SinglePage_TruncatesAndSendsTen(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		// 10 件返す
		var items []string
		for i := 1; i <= 10; i++ {
			items = append(items, `{"id":"`+strconv.Itoa(i)+`","text":"t","author_id":"42"}`)
		}
		body := `{"data":[` + strings.Join(items, ",") + `],"meta":{"result_count":10}}`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "search", "test", "--max-results", "3"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	if !strings.Contains(qs[0], "max_results=10") {
		t.Errorf("query missing max_results=10: %q", qs[0])
	}
	var got xapi.SearchResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got.Data) != 3 {
		t.Errorf("Data len = %d, want 3 (truncated)", len(got.Data))
	}
	if got.Meta.ResultCount != 3 {
		t.Errorf("Meta.ResultCount = %d, want 3", got.Meta.ResultCount)
	}
}

// TestTweetSearch_MaxResultsBelow10_All_RejectsArgument は
// --all --max-results 3 で exit 2 を確認する (D-2)。
func TestTweetSearch_MaxResultsBelow10_All_RejectsArgument(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newTweetTestServer(t, nil)
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "search", "test", "--all", "--max-results", "3"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

// TestTweetSearch_MaxResultsOutOfRange は 0 / 101 で exit 2 を確認する。
func TestTweetSearch_MaxResultsOutOfRange(t *testing.T) {
	setAllXAPIEnv(t)
	for _, n := range []string{"0", "101"} {
		t.Run("n="+n, func(t *testing.T) {
			srv, _ := newTweetTestServer(t, nil)
			stubTweetClientFactory(t, srv.URL)
			cmd := NewRootCmd()
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs([]string{"tweet", "search", "test", "--max-results", n})
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error for max-results=%s", n)
			}
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("err = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

// TestTweetSearch_NDJSON_All_StreamingPagination は
// --all --ndjson で 2 ページ × N 件をストリーミング出力することを確認する。
func TestTweetSearch_NDJSON_All_StreamingPagination(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newSearchPagedServer(t, []searchPage{
		{tweetIDs: []string{"1", "2"}, nextToken: "P1"},
		{tweetIDs: []string{"3", "4"}, nextToken: ""},
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "search", "test", "--all", "--ndjson"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("ndjson lines = %d, want 4 — out: %q", len(lines), buf.String())
	}
}

// TestTweetSearch_All_Aggregated_JSON は --all で JSON 集約出力を確認する。
func TestTweetSearch_All_Aggregated_JSON(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newSearchPagedServer(t, []searchPage{
		{tweetIDs: []string{"1"}, nextToken: "P1"},
		{tweetIDs: []string{"2"}, nextToken: ""},
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "search", "test", "--all"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got xapi.SearchResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got.Data) != 2 {
		t.Errorf("Data = %+v, want 2 elements", got.Data)
	}
	if got.Meta.ResultCount != 2 {
		t.Errorf("Meta.ResultCount = %d, want 2", got.Meta.ResultCount)
	}
	if got.Meta.NextToken != "" {
		t.Errorf("Meta.NextToken = %q, want empty (aggregated)", got.Meta.NextToken)
	}
}

// TestTweetSearch_YesterdayJST_OverridesStartEnd は
// --yesterday-jst が --start-time/--end-time を上書きすることを確認する。
func TestTweetSearch_YesterdayJST_OverridesStartEnd(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"tweet", "search", "test",
		"--yesterday-jst",
		"--start-time", "2020-01-01T00:00:00Z",
		"--end-time", "2020-01-01T23:59:59Z",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	// --yesterday-jst が override → 2020-01 ではない start_time
	if strings.Contains(qs[0], "start_time=2020-01-01") {
		t.Errorf("--yesterday-jst should override --start-time, got: %q", qs[0])
	}
	if !strings.Contains(qs[0], "start_time=") {
		t.Errorf("query missing start_time: %q", qs[0])
	}
}

// TestTweetSearch_SinceJST は --since-jst の JST→UTC 変換を確認する。
func TestTweetSearch_SinceJST(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "search", "test", "--since-jst", "2026-05-12"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	// JST 2026-05-12 0:00 → UTC 2026-05-11 15:00
	if !strings.Contains(qs[0], "start_time=2026-05-11T15%3A00%3A00Z") {
		t.Errorf("start_time = %q, want 2026-05-11T15:00:00Z (UTC of JST 2026-05-12)", qs[0])
	}
}

// TestTweetSearch_403_Forbidden_ExitsFour は Free tier 403 シナリオ。
func TestTweetSearch_403_Forbidden_ExitsFour(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newTweetTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "search", "test"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !errors.Is(err, xapi.ErrPermission) {
		t.Errorf("err = %v, want xapi.ErrPermission (Free tier 403)", err)
	}
}

// TestTweetSearch_NoJSON_NDJSON_MutuallyExclusive は両指定で exit 2 を確認する。
func TestTweetSearch_NoJSON_NDJSON_MutuallyExclusive(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newTweetTestServer(t, nil)
	stubTweetClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"tweet", "search", "test", "--no-json", "--ndjson"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}
