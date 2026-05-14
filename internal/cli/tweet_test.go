package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	for _, sub := range []string{"get", "liking-users", "retweeted-by", "quote-tweets"} {
		if !strings.Contains(out, sub) {
			t.Errorf("tweet help missing subcommand %q, got: %q", sub, out)
		}
	}
}
