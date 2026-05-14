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

// stubListClientFactory は newListClient を httptest サーバ向けに差し替える。
func stubListClientFactory(t *testing.T, baseURL string) {
	t.Helper()
	prev := newListClient
	t.Cleanup(func() { newListClient = prev })
	newListClient = func(ctx context.Context, _ *config.Credentials) (listClient, error) {
		return xapi.NewClient(ctx, nil, xapi.WithBaseURL(baseURL)), nil
	}
}

// listHandlerState は httptest ハンドラのリクエスト記録器。
type listHandlerState struct {
	mu    sync.Mutex
	paths []string
	rawQs []string
}

func (s *listHandlerState) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths = append(s.paths, r.URL.Path)
	s.rawQs = append(s.rawQs, r.URL.RawQuery)
}

func (s *listHandlerState) snapshot() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...), append([]string(nil), s.rawQs...)
}

// newListTestServer は List 系コマンドのテスト用 httptest サーバを返す。
//   - /2/users/me                              : self
//   - /2/users/by/username/<u>                 : username → ID 解決 (id=7)
//   - /2/lists/<id>                            : single list
//   - /2/lists/<id>/tweets                     : list tweets
//   - /2/lists/<id>/members                    : list members
//   - /2/users/<id>/{owned_lists,list_memberships,followed_lists,pinned_lists}
func newListTestServer(t *testing.T) (*httptest.Server, *listHandlerState) {
	t.Helper()
	state := &listHandlerState{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.record(r)
		path := r.URL.Path
		switch {
		case path == "/2/users/me":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
		case strings.HasPrefix(path, "/2/users/by/username/"):
			uname := strings.TrimPrefix(path, "/2/users/by/username/")
			body := `{"data":{"id":"7","username":"` + uname + `","name":"Name"}}`
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		case strings.HasSuffix(path, "/tweets"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"100","text":"hello","author_id":"42","created_at":"2026-05-12T00:00:00.000Z"}],"meta":{"result_count":1}}`))
		case strings.HasSuffix(path, "/members"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"99","username":"member","name":"Member"}],"meta":{"result_count":1}}`))
		case strings.HasSuffix(path, "/owned_lists") ||
			strings.HasSuffix(path, "/list_memberships") ||
			strings.HasSuffix(path, "/followed_lists") ||
			strings.HasSuffix(path, "/pinned_lists"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"L1","name":"MyList","owner_id":"42"}],"meta":{"result_count":1}}`))
		case strings.HasPrefix(path, "/2/lists/"):
			id := strings.TrimPrefix(path, "/2/lists/")
			body := `{"data":{"id":"` + id + `","name":"List","owner_id":"42"}}`
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// =============================================================================
// list get
// =============================================================================

func TestListGet_ByID_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "get", "12345"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/lists/12345" {
		t.Fatalf("paths = %v, want /2/lists/12345", paths)
	}
	var got xapi.ListResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v, out=%q", err, buf.String())
	}
	if got.Data == nil || got.Data.ID != "12345" {
		t.Errorf("Data = %+v, want ID=12345", got.Data)
	}
}

func TestListGet_ByURL(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "get", "https://x.com/i/lists/9876"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/lists/9876" {
		t.Fatalf("paths = %v, want /2/lists/9876", paths)
	}
}

func TestListGet_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "get", "12345", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "id=12345") || !strings.Contains(out, "name=") {
		t.Errorf("out = %q, want id=12345 name=...", out)
	}
}

func TestListGet_InvalidID_Rejects(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "get", "not-a-list"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

// =============================================================================
// list tweets
// =============================================================================

func TestListTweets_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "tweets", "12345"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, qs := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/lists/12345/tweets" {
		t.Fatalf("paths = %v, want /2/lists/12345/tweets", paths)
	}
	if !strings.Contains(qs[0], "max_results=100") {
		t.Errorf("query = %q, missing max_results=100", qs[0])
	}
}

func TestListTweets_All_AggregatesPages(t *testing.T) {
	setAllXAPIEnv(t)
	var call int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		c := atomic.AddInt32(&call, 1)
		w.WriteHeader(http.StatusOK)
		if c == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"a"}],"meta":{"result_count":1,"next_token":"P1"}}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"id":"2","text":"b"}],"meta":{"result_count":1}}`))
		}
	}))
	t.Cleanup(srv.Close)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "tweets", "1234", "--all"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got xapi.ListTweetsResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v, out=%q", err, buf.String())
	}
	if len(got.Data) != 2 {
		t.Errorf("len(Data) = %d, want 2", len(got.Data))
	}
	if got.Meta.ResultCount != 2 {
		t.Errorf("Meta.ResultCount = %d, want 2", got.Meta.ResultCount)
	}
}

func TestListTweets_NDJSON_Streams(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "tweets", "12345", "--ndjson"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	lines := strings.Split(out, "\n")
	if len(lines) != 1 {
		t.Errorf("got %d NDJSON lines, want 1: %q", len(lines), out)
	}
	var tw xapi.Tweet
	if err := json.Unmarshal([]byte(lines[0]), &tw); err != nil {
		t.Errorf("line is not valid JSON: %v", err)
	}
}

func TestListTweets_MaxResultsOutOfRange(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	tests := []struct {
		name string
		val  string
	}{
		{"below", "0"},
		{"above", "101"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewRootCmd()
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs([]string{"list", "tweets", "12345", "--max-results", tc.val})

			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("err = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

// =============================================================================
// list members
// =============================================================================

func TestListMembers_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "members", "12345"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/lists/12345/members" {
		t.Fatalf("paths = %v, want /2/lists/12345/members", paths)
	}
}

func TestListMembers_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "members", "12345", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "id=99") || !strings.Contains(out, "username=member") {
		t.Errorf("out = %q, want id=99 username=member", out)
	}
}

// =============================================================================
// list owned / followed / memberships
// =============================================================================

func TestListOwned_UserIDDefaultsToMe(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "owned"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) < 2 {
		t.Fatalf("paths = %v, want at least 2 (me + owned_lists)", paths)
	}
	if paths[0] != "/2/users/me" {
		t.Errorf("paths[0] = %q, want /2/users/me", paths[0])
	}
	if paths[1] != "/2/users/42/owned_lists" {
		t.Errorf("paths[1] = %q, want /2/users/42/owned_lists", paths[1])
	}
}

func TestListOwned_UserIDExplicit(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "owned", "--user-id", "99"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users/99/owned_lists" {
		t.Fatalf("paths = %v, want /2/users/99/owned_lists (no GetUserMe call)", paths)
	}
	for _, p := range paths {
		if p == "/2/users/me" {
			t.Errorf("GetUserMe should not be called when --user-id is explicit, paths=%v", paths)
		}
	}
}

func TestListOwned_UsernamePositional_ResolvesViaGetUserByUsername(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "owned", "@bob"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) < 2 {
		t.Fatalf("paths = %v", paths)
	}
	if paths[0] != "/2/users/by/username/bob" {
		t.Errorf("paths[0] = %q, want /2/users/by/username/bob", paths[0])
	}
	if paths[1] != "/2/users/7/owned_lists" {
		t.Errorf("paths[1] = %q, want /2/users/7/owned_lists", paths[1])
	}
}

func TestListFollowed_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "followed", "--user-id", "42"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users/42/followed_lists" {
		t.Fatalf("paths = %v, want /2/users/42/followed_lists", paths)
	}
}

func TestListMemberships_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "memberships", "--user-id", "42"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users/42/list_memberships" {
		t.Fatalf("paths = %v, want /2/users/42/list_memberships", paths)
	}
}

// =============================================================================
// list pinned (self only)
// =============================================================================

func TestListPinned_AlwaysResolvesSelf(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "pinned"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) < 2 {
		t.Fatalf("paths = %v, want at least 2 (me + pinned_lists)", paths)
	}
	if paths[0] != "/2/users/me" {
		t.Errorf("paths[0] = %q, want /2/users/me", paths[0])
	}
	if paths[1] != "/2/users/42/pinned_lists" {
		t.Errorf("paths[1] = %q, want /2/users/42/pinned_lists", paths[1])
	}
}

func TestListPinned_NoUserIDFlag_Pinned(t *testing.T) {
	t.Parallel()
	// pinned コマンドに --user-id フラグが登録されていないことを pin する (M33 D-7)。
	cmd := newListPinnedCmd()
	if f := cmd.Flag("user-id"); f != nil {
		t.Errorf("pinned subcommand must NOT register --user-id flag (got: %+v)", f)
	}
}

func TestListPinned_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "pinned", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "id=L1") || !strings.Contains(out, "name=MyList") {
		t.Errorf("out = %q, want id=L1 name=MyList", out)
	}
}

// =============================================================================
// 共通
// =============================================================================

func TestList_NoJSON_NDJSON_MutuallyExclusive(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newListTestServer(t)
	stubListClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "tweets", "12345", "--no-json", "--ndjson"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

// =============================================================================
// extractListID
// =============================================================================

func TestExtractListID_Numeric(t *testing.T) {
	t.Parallel()
	v, err := extractListID("12345")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if v != "12345" {
		t.Errorf("got %q, want 12345", v)
	}
}

func TestExtractListID_URL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"https://x.com/i/lists/9876", "9876"},
		{"https://twitter.com/i/lists/9876", "9876"},
		{"http://x.com/i/lists/9876/", "9876"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			v, err := extractListID(tc.in)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if v != tc.want {
				t.Errorf("got %q, want %q", v, tc.want)
			}
		})
	}
}

func TestExtractListID_Invalid_Rejects(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"",
		"not-a-number",
		"https://x.com/alice",
		"https://x.com/i/web/status/123",
		"@alice",
	} {
		t.Run(s, func(t *testing.T) {
			_, err := extractListID(s)
			if err == nil {
				t.Errorf("%q: expected error, got nil", s)
			}
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("%q: err = %v, want ErrInvalidArgument", s, err)
			}
		})
	}
}
