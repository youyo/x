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
	"sync/atomic"
	"testing"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// stubTimelineClientFactory は newTimelineClient を httptest サーバ向けに差し替える。
func stubTimelineClientFactory(t *testing.T, baseURL string) {
	t.Helper()
	prev := newTimelineClient
	t.Cleanup(func() { newTimelineClient = prev })
	newTimelineClient = func(ctx context.Context, _ *config.Credentials) (timelineClient, error) {
		return xapi.NewClient(ctx, nil, xapi.WithBaseURL(baseURL)), nil
	}
}

// timelineHandlerState は httptest ハンドラが受信したリクエストを記録する。
type timelineHandlerState struct {
	mu    sync.Mutex
	paths []string
	rawQs []string
}

func (s *timelineHandlerState) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths = append(s.paths, r.URL.Path)
	s.rawQs = append(s.rawQs, r.URL.RawQuery)
}

func (s *timelineHandlerState) snapshot() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...), append([]string(nil), s.rawQs...)
}

// newTimelineTestServer はテスト用の httptest サーバを返す。
// `/2/users/me` で self を返し、`/2/users/<id>/tweets|mentions|timelines/reverse_chronological`
// で `dataJSON` を返す。dataJSON が空なら 1 件のデフォルト ツイート JSON を返す。
func newTimelineTestServer(t *testing.T, dataJSON string) (*httptest.Server, *timelineHandlerState) {
	t.Helper()
	state := &timelineHandlerState{}
	if dataJSON == "" {
		dataJSON = `{"data":[{"id":"100","text":"hello","author_id":"42","created_at":"2026-05-12T01:23:45.000Z"}],"meta":{"result_count":1}}`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.record(r)
		switch {
		case r.URL.Path == "/2/users/me":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
		case strings.HasSuffix(r.URL.Path, "/tweets") ||
			strings.HasSuffix(r.URL.Path, "/mentions") ||
			strings.HasSuffix(r.URL.Path, "/timelines/reverse_chronological"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(dataJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// -- T2 #1 timeline tweets default JSON -----------------------------------

func TestTimelineTweets_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newTimelineTestServer(t, "")
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "tweets", "--user-id", "42"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users/42/tweets" {
		t.Fatalf("paths = %v, want first /2/users/42/tweets", paths)
	}
	var got xapi.TimelineResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v, out=%q", err, buf.String())
	}
	if len(got.Data) != 1 || got.Data[0].ID != "100" {
		t.Errorf("got = %+v", got)
	}
}

// -- T2 #2 --no-json human format -----------------------------------------

func TestTimelineTweets_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newTimelineTestServer(t, "")
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "tweets", "--user-id", "42", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "id=100") || !strings.Contains(out, "author=42") || !strings.Contains(out, "text=hello") {
		t.Errorf("human output = %q", out)
	}
}

// -- T2 #3 note_tweet preferred in human output ----------------------------

func TestTimelineTweets_NoJSON_NoteTweetPreferred(t *testing.T) {
	setAllXAPIEnv(t)
	body := `{"data":[{"id":"100","text":"short","author_id":"42","note_tweet":{"text":"FULL NOTE BODY"}}],"meta":{"result_count":1}}`
	srv, _ := newTimelineTestServer(t, body)
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "tweets", "--user-id", "42", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "text=FULL NOTE BODY") {
		t.Errorf("note_tweet not preferred: %q", buf.String())
	}
}

// -- T2 #4 max-results<5 single-page truncates, sends 5 to X API ----------

func TestTimelineTweets_MaxResultsBelow5_SinglePage_TruncatesAndSendsFive(t *testing.T) {
	setAllXAPIEnv(t)
	body := `{"data":[
		{"id":"1","text":"a","author_id":"42"},
		{"id":"2","text":"b","author_id":"42"},
		{"id":"3","text":"c","author_id":"42"},
		{"id":"4","text":"d","author_id":"42"},
		{"id":"5","text":"e","author_id":"42"}
	],"meta":{"result_count":5}}`
	srv, state := newTimelineTestServer(t, body)
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "tweets", "--user-id", "42", "--max-results", "3"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	var lastQ string
	for _, q := range qs {
		if strings.Contains(q, "max_results=") {
			lastQ = q
		}
	}
	if !strings.Contains(lastQ, "max_results=5") {
		t.Errorf("query = %q, want max_results=5 (auto-corrected)", lastQ)
	}
	var got xapi.TimelineResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v, out=%q", err, buf.String())
	}
	if len(got.Data) != 3 {
		t.Errorf("len(Data) = %d, want 3 (truncated)", len(got.Data))
	}
	if got.Meta.ResultCount != 3 {
		t.Errorf("Meta.ResultCount = %d, want 3", got.Meta.ResultCount)
	}
}

// -- T2 #5 max-results<5 + --all rejected ----------------------------------

func TestTimelineTweets_MaxResultsBelow5_All_RejectsArgument(t *testing.T) {
	setAllXAPIEnv(t)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "tweets", "--user-id", "42", "--max-results", "3", "--all"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

// -- T2 #6 max-results out of range ----------------------------------------

func TestTimelineTweets_MaxResultsOutOfRange(t *testing.T) {
	setAllXAPIEnv(t)
	for _, n := range []string{"0", "101"} {
		cmd := NewRootCmd()
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"timeline", "tweets", "--user-id", "42", "--max-results", n})
		err := cmd.Execute()
		if err == nil {
			t.Errorf("max-results=%s: expected error", n)
		}
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("max-results=%s: err = %v, want ErrInvalidArgument", n, err)
		}
	}
}

// -- T2 #7 user-id defaults to me ------------------------------------------

func TestTimelineTweets_UserIDDefaultsToMe(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newTimelineTestServer(t, "")
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "tweets"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) < 2 {
		t.Fatalf("expected 2 requests (me + tweets), got %d: %v", len(paths), paths)
	}
	if paths[0] != "/2/users/me" {
		t.Errorf("first path = %q, want /2/users/me", paths[0])
	}
	if paths[1] != "/2/users/42/tweets" {
		t.Errorf("second path = %q, want /2/users/42/tweets", paths[1])
	}
}

// -- T2 #8 user-id explicit ------------------------------------------------

func TestTimelineTweets_UserIDExplicit(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newTimelineTestServer(t, "")
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "tweets", "--user-id", "99"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	for _, p := range paths {
		if p == "/2/users/me" {
			t.Errorf("/2/users/me should NOT be called when --user-id given, paths=%v", paths)
		}
	}
	if len(paths) == 0 || paths[0] != "/2/users/99/tweets" {
		t.Errorf("first path = %v, want /2/users/99/tweets", paths)
	}
}

// -- T2 #9 --exclude flag --------------------------------------------------

func TestTimelineTweets_ExcludeFlag(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newTimelineTestServer(t, "")
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "tweets", "--user-id", "42", "--exclude", "retweets,replies"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	if len(qs) == 0 || !strings.Contains(qs[0], "exclude=retweets%2Creplies") {
		t.Errorf("query = %v, want exclude=retweets,replies", qs)
	}
}

// -- T2 #10 --since-id / --until-id ----------------------------------------

func TestTimelineTweets_SinceUntilID(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newTimelineTestServer(t, "")
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "tweets", "--user-id", "42", "--since-id", "100", "--until-id", "200"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	if len(qs) == 0 || !strings.Contains(qs[0], "since_id=100") || !strings.Contains(qs[0], "until_id=200") {
		t.Errorf("query = %v, want since_id=100 and until_id=200", qs)
	}
}

// -- T2 #11 --yesterday-jst overrides --start-time/--end-time ---------------

func TestTimelineTweets_YesterdayJST_Overrides(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newTimelineTestServer(t, "")
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"timeline", "tweets", "--user-id", "42",
		"--start-time", "2020-01-01T00:00:00Z",
		"--end-time", "2020-01-02T00:00:00Z",
		"--yesterday-jst",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests")
	}
	if strings.Contains(qs[0], "start_time=2020-01-01") {
		t.Errorf("query = %q, --yesterday-jst should override --start-time", qs[0])
	}
	if !strings.Contains(qs[0], "start_time=") || !strings.Contains(qs[0], "end_time=") {
		t.Errorf("query = %q, missing start_time/end_time", qs[0])
	}
}

// -- T2 #12 --ndjson + --all streaming --------------------------------------

func TestTimelineTweets_NDJSON_All_Streaming(t *testing.T) {
	setAllXAPIEnv(t)

	// 2 ページのレスポンスを返すサーバを構築する。
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := atomic.AddInt32(&calls, 1) - 1
		if r.URL.Path == "/2/users/me" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		switch idx {
		case 1:
			_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"a","author_id":"42"}],"meta":{"result_count":1,"next_token":"P1"}}`))
		default:
			_, _ = w.Write([]byte(`{"data":[{"id":"2","text":"b","author_id":"42"}],"meta":{"result_count":1}}`))
		}
	}))
	t.Cleanup(srv.Close)
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "tweets", "--all", "--ndjson"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("ndjson lines = %d, want 2 (lines=%v)", len(lines), lines)
	}
}

// -- T2 #13 --all aggregated JSON ------------------------------------------

func TestTimelineTweets_All_Aggregated_JSON(t *testing.T) {
	setAllXAPIEnv(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := atomic.AddInt32(&calls, 1) - 1
		if r.URL.Path == "/2/users/me" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		switch idx {
		case 1:
			_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"a","author_id":"42"}],"meta":{"result_count":1,"next_token":"P1"}}`))
		default:
			_, _ = w.Write([]byte(`{"data":[{"id":"2","text":"b","author_id":"42"}],"meta":{"result_count":1}}`))
		}
	}))
	t.Cleanup(srv.Close)
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "tweets", "--all"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got xapi.TimelineResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v, out=%q", err, buf.String())
	}
	if len(got.Data) != 2 {
		t.Errorf("len(Data) = %d, want 2", len(got.Data))
	}
	if got.Meta.ResultCount != 2 {
		t.Errorf("Meta.ResultCount = %d, want 2", got.Meta.ResultCount)
	}
	if got.Meta.NextToken != "" {
		t.Errorf("Meta.NextToken = %q, want empty", got.Meta.NextToken)
	}
}

// -- T2 #14 timeline mentions default flow ---------------------------------

func TestTimelineMentions_DefaultFlow(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newTimelineTestServer(t, "")
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "mentions", "--user-id", "42"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users/42/mentions" {
		t.Errorf("paths = %v, want first /2/users/42/mentions", paths)
	}
}

// -- T2 #15 mentions does NOT have --exclude flag (D-9) --------------------

func TestTimelineMentions_NoExcludeFlag(t *testing.T) {
	cmd := newTimelineMentionsCmd()
	if cmd.Flag("exclude") != nil {
		t.Errorf("--exclude flag should NOT be registered on `timeline mentions` (D-9)")
	}
}

// -- T2 #16 home always resolves self (D-4) --------------------------------

func TestTimelineHome_AlwaysResolvesSelf(t *testing.T) {
	setAllXAPIEnv(t)

	// 1) `home` コマンドに --user-id フラグが登録されていないことを pin (D-4)。
	homeCmd := newTimelineHomeCmd()
	if homeCmd.Flag("user-id") != nil {
		t.Errorf("--user-id flag should NOT be registered on `timeline home` (D-4)")
	}

	// 2) GetUserMe が必ず呼ばれ、その self ID が home endpoint に渡る。
	srv, state := newTimelineTestServer(t, "")
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "home"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) < 2 {
		t.Fatalf("expected 2 requests, got %d: %v", len(paths), paths)
	}
	if paths[0] != "/2/users/me" {
		t.Errorf("first path = %q, want /2/users/me", paths[0])
	}
	if paths[1] != "/2/users/42/timelines/reverse_chronological" {
		t.Errorf("second path = %q, want /2/users/42/timelines/reverse_chronological", paths[1])
	}
}

// -- T2 #17 home --max-results 1 (no truncation, D-1) ----------------------

func TestTimelineHome_MaxResults_1_NoTruncation(t *testing.T) {
	setAllXAPIEnv(t)
	body := `{"data":[{"id":"1","text":"a","author_id":"42"}],"meta":{"result_count":1}}`
	srv, state := newTimelineTestServer(t, body)
	stubTimelineClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "home", "--max-results", "1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	var homeQ string
	for _, q := range qs {
		if strings.Contains(q, "max_results=") {
			homeQ = q
		}
	}
	if !strings.Contains(homeQ, "max_results=1") {
		t.Errorf("query = %q, want max_results=1 (NO auto-correction for home, D-1)", homeQ)
	}
	var got xapi.TimelineResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v, out=%q", err, buf.String())
	}
	if len(got.Data) != 1 {
		t.Errorf("len(Data) = %d, want 1", len(got.Data))
	}
}

// -- T2 #18 --no-json + --ndjson mutually exclusive ------------------------

func TestTimelineHome_NoJSON_NDJSON_MutuallyExclusive(t *testing.T) {
	setAllXAPIEnv(t)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"timeline", "home", "--no-json", "--ndjson"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}
