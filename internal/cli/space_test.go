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

// stubSpaceClientFactory は newSpaceClient を httptest 向けに差し替える。
func stubSpaceClientFactory(t *testing.T, baseURL string) {
	t.Helper()
	prev := newSpaceClient
	t.Cleanup(func() { newSpaceClient = prev })
	newSpaceClient = func(ctx context.Context, _ *config.Credentials) (spaceClient, error) {
		return xapi.NewClient(ctx, nil, xapi.WithBaseURL(baseURL)), nil
	}
}

type spaceHandlerState struct {
	mu    sync.Mutex
	paths []string
	rawQs []string
}

func (s *spaceHandlerState) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths = append(s.paths, r.URL.Path)
	s.rawQs = append(s.rawQs, r.URL.RawQuery)
}

func (s *spaceHandlerState) snapshot() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...), append([]string(nil), s.rawQs...)
}

// newSpaceTestServer は Space 系コマンドのテスト用 httptest サーバを返す。
func newSpaceTestServer(t *testing.T) (*httptest.Server, *spaceHandlerState) {
	t.Helper()
	state := &spaceHandlerState{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.record(r)
		path := r.URL.Path
		switch {
		case path == "/2/spaces":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"S1","state":"live","title":"a"},{"id":"S2","state":"scheduled","title":"b"}]}`))
		case path == "/2/spaces/search":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"S1","state":"live","title":"hit"}]}`))
		case path == "/2/spaces/by/creator_ids":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"S9","state":"live","creator_id":"U1"}]}`))
		case strings.HasSuffix(path, "/tweets") && strings.HasPrefix(path, "/2/spaces/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"T1","text":"hello","author_id":"42","created_at":"2026-05-12T00:00:00.000Z"}],"meta":{"result_count":1}}`))
		case strings.HasPrefix(path, "/2/spaces/"):
			id := strings.TrimPrefix(path, "/2/spaces/")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"` + id + `","state":"live","title":"T","creator_id":"42","participant_count":5,"host_ids":["H1"]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// =============================================================================
// extractSpaceID
// =============================================================================

func TestExtractSpaceID_Alnum_URL_Invalid(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"1OdJrXWaPVPGX", "1OdJrXWaPVPGX", false},
		{"abc123", "abc123", false},
		{"https://x.com/i/spaces/abc123", "abc123", false},
		{"https://twitter.com/i/spaces/XYZ/", "XYZ", false},
		{"not a id!", "", true},
		{"", "", true},
		{"https://x.com/i/lists/123", "", true},
	}
	for _, c := range cases {
		got, err := extractSpaceID(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: want error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}

// =============================================================================
// space get
// =============================================================================

func TestSpaceGet_ByID_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "get", "abc123"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/spaces/abc123" {
		t.Fatalf("paths = %v", paths)
	}
	var got xapi.SpaceResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v out=%q", err, buf.String())
	}
	if got.Data == nil || got.Data.ID != "abc123" {
		t.Errorf("Data = %+v", got.Data)
	}
}

func TestSpaceGet_ByURL(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "get", "https://x.com/i/spaces/abc999"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/spaces/abc999" {
		t.Fatalf("paths = %v", paths)
	}
}

func TestSpaceGet_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "get", "abc123", "--no-json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"id=abc123", "state=live", "title=T", "creator_id=42", "participants=5"} {
		if !strings.Contains(out, want) {
			t.Errorf("out=%q missing %q", out, want)
		}
	}
}

func TestSpaceGet_InvalidID_Rejects(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "get", "not a id!"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v", err)
	}
}

// =============================================================================
// space by-ids
// =============================================================================

func TestSpaceByIDs_DefaultJSON_FlagIDs(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "by-ids", "--ids", "S1,S2"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, qs := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/spaces" {
		t.Fatalf("paths = %v", paths)
	}
	if !strings.Contains(qs[0], "ids=S1%2CS2") {
		t.Errorf("query %q missing ids=S1,S2", qs[0])
	}
}

func TestSpaceByIDs_EmptyIDs_Rejects(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "by-ids"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want error")
	}
	// exit 2 (ErrInvalidArgument) であることを pin (M34 D-5、cobra MarkFlagRequired を回避)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument (exit 2)", err)
	}
}

// =============================================================================
// space search
// =============================================================================

func TestSpaceSearch_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "search", "AI"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, qs := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/spaces/search" {
		t.Fatalf("paths = %v", paths)
	}
	if !strings.Contains(qs[0], "query=AI") {
		t.Errorf("query %q missing query=AI", qs[0])
	}
}

func TestSpaceSearch_StateReflected(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "search", "AI", "--state", "live"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	if !strings.Contains(qs[0], "state=live") {
		t.Errorf("query %q missing state=live", qs[0])
	}
}

func TestSpaceSearch_MaxResultsReflected(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "search", "AI", "--max-results", "50"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	if !strings.Contains(qs[0], "max_results=50") {
		t.Errorf("query %q missing max_results=50", qs[0])
	}
}

func TestSpaceSearch_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "search", "AI", "--no-json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "id=S1") {
		t.Errorf("out=%q missing id=S1", buf.String())
	}
}

func TestSpaceSearch_EmptyQuery_Rejects(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "search", "  "})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v", err)
	}
}

func TestSpaceSearch_All_NotSupportedFlag(t *testing.T) {
	// search サブコマンドに --all フラグが存在しないことを pin (M34 D-2)。
	cmd := newSpaceSearchCmd()
	if cmd.Flags().Lookup("all") != nil {
		t.Errorf("--all flag should NOT exist on `space search` (X API does not paginate)")
	}
}

// =============================================================================
// space by-creator
// =============================================================================

func TestSpaceByCreator_DefaultJSON_FlagIDs(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "by-creator", "--ids", "U1,U2"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, qs := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/spaces/by/creator_ids" {
		t.Fatalf("paths = %v", paths)
	}
	if !strings.Contains(qs[0], "user_ids=U1%2CU2") {
		t.Errorf("query %q missing user_ids=U1,U2", qs[0])
	}
}

func TestSpaceByCreator_EmptyIDs_Rejects(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "by-creator"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want error")
	}
	// exit 2 (ErrInvalidArgument) であることを pin (M34 D-5)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument (exit 2)", err)
	}
}

// =============================================================================
// space tweets
// =============================================================================

func TestSpaceTweets_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "tweets", "abc123"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, qs := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/spaces/abc123/tweets" {
		t.Fatalf("paths = %v", paths)
	}
	if !strings.Contains(qs[0], "max_results=100") {
		t.Errorf("query %q missing max_results=100", qs[0])
	}
}

// pagination 用の追加サーバ
func newPagingSpaceTweetsServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := atomic.AddInt32(&calls, 1) - 1
		w.WriteHeader(http.StatusOK)
		switch idx {
		case 0:
			_, _ = w.Write([]byte(`{"data":[{"id":"T1","text":"a"}],"meta":{"result_count":1,"next_token":"P1"}}`))
		case 1:
			_, _ = w.Write([]byte(`{"data":[{"id":"T2","text":"b"}],"meta":{"result_count":1}}`))
		default:
			t.Errorf("unexpected call %d", idx)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestSpaceTweets_All_AggregatesPages(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newPagingSpaceTweetsServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "tweets", "abc123", "--all"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got xapi.SpaceTweetsResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v out=%q", err, buf.String())
	}
	if len(got.Data) != 2 {
		t.Errorf("Data len = %d, want 2 (aggregate)", len(got.Data))
	}
}

func TestSpaceTweets_MaxResultsOutOfRange(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	for _, v := range []string{"0", "101"} {
		cmd := NewRootCmd()
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"space", "tweets", "abc123", "--max-results", v})
		err := cmd.Execute()
		if err == nil {
			t.Errorf("max-results=%s: want error", v)
			continue
		}
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("max-results=%s: err = %v", v, err)
		}
	}
}

func TestSpaceTweets_NDJSON_Streams(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "tweets", "abc123", "--ndjson"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("lines = %d, want 1 (single page test data)", len(lines))
	}
	// 1 行目が 1 ツイートの JSON
	var tw xapi.Tweet
	if err := json.Unmarshal([]byte(lines[0]), &tw); err != nil {
		t.Fatalf("invalid ndjson line: %v", err)
	}
	if tw.ID != "T1" {
		t.Errorf("Tweet.ID = %q, want T1", tw.ID)
	}
}

func TestSpaceTweets_NoJSON_NDJSON_MutuallyExclusive(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newSpaceTestServer(t)
	stubSpaceClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"space", "tweets", "abc123", "--no-json", "--ndjson"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v", err)
	}
}
