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

// stubUserClientFactory は newUserClient を httptest サーバ向けに差し替える。
func stubUserClientFactory(t *testing.T, baseURL string) {
	t.Helper()
	prev := newUserClient
	t.Cleanup(func() { newUserClient = prev })
	newUserClient = func(ctx context.Context, _ *config.Credentials) (userClient, error) {
		return xapi.NewClient(ctx, nil, xapi.WithBaseURL(baseURL)), nil
	}
}

// userHandlerState は httptest ハンドラが受信したリクエストを記録する。
type userHandlerState struct {
	mu    sync.Mutex
	paths []string
	rawQs []string
}

func (s *userHandlerState) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths = append(s.paths, r.URL.Path)
	s.rawQs = append(s.rawQs, r.URL.RawQuery)
}

func (s *userHandlerState) snapshot() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...), append([]string(nil), s.rawQs...)
}

// newUserTestServer はテスト用 httptest サーバを返す。
// path にしたがって異なるレスポンスを返す。
//   - /2/users/me                          : self
//   - /2/users/<id>                        : single user
//   - /2/users (with ?ids=)                : batch by ids
//   - /2/users/by/username/<u>             : single by username
//   - /2/users/by (with ?usernames=)       : batch by usernames
//   - /2/users/search                      : search
//   - /2/users/<id>/{following,followers,blocking,muting} : graph
func newUserTestServer(t *testing.T) (*httptest.Server, *userHandlerState) {
	t.Helper()
	state := &userHandlerState{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.record(r)
		path := r.URL.Path
		switch {
		case path == "/2/users/me":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
		case path == "/2/users":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"1","username":"u1","name":"U1"},{"id":"2","username":"u2","name":"U2"}]}`))
		case path == "/2/users/by":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"100","username":"alice","name":"Alice"}]}`))
		case strings.HasPrefix(path, "/2/users/by/username/"):
			uname := strings.TrimPrefix(path, "/2/users/by/username/")
			body := `{"data":{"id":"7","username":"` + uname + `","name":"Name"}}`
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		case path == "/2/users/search":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"1","username":"go_user","name":"Go"}],"meta":{"result_count":1}}`))
		case strings.HasSuffix(path, "/following") ||
			strings.HasSuffix(path, "/followers") ||
			strings.HasSuffix(path, "/blocking") ||
			strings.HasSuffix(path, "/muting"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"99","username":"target","name":"Target"}],"meta":{"result_count":1}}`))
		case strings.HasPrefix(path, "/2/users/"):
			// /2/users/<id> 単一 lookup
			id := strings.TrimPrefix(path, "/2/users/")
			body := `{"data":{"id":"` + id + `","username":"u","name":"N"}}`
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
// user get
// =============================================================================

func TestUserGet_ByID_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "get", "42"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users/42" {
		t.Fatalf("paths = %v, want first /2/users/42", paths)
	}
	var got xapi.UserResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v, out=%q", err, buf.String())
	}
	if got.Data == nil || got.Data.ID != "42" {
		t.Errorf("Data.ID = %+v, want 42", got.Data)
	}
}

func TestUserGet_ByUsername_StripsAt(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "get", "@alice"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users/by/username/alice" {
		t.Fatalf("paths = %v, want first /2/users/by/username/alice", paths)
	}
}

func TestUserGet_ByURL(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "get", "https://x.com/alice"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users/by/username/alice" {
		t.Fatalf("paths = %v, want /2/users/by/username/alice", paths)
	}
}

func TestUserGet_ReservedURLPath_Rejects(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "get", "https://x.com/home"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestUserGet_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "get", "42", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "id=42") || !strings.Contains(out, "username=") || !strings.Contains(out, "name=") {
		t.Errorf("out = %q, want id=42 / username= / name=", out)
	}
}

func TestUserGet_ByIDs_BatchLookup(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "get", "--ids", "1,2,3"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, qs := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users" {
		t.Fatalf("paths = %v, want /2/users", paths)
	}
	if !strings.Contains(qs[0], "ids=1%2C2%2C3") && !strings.Contains(qs[0], "ids=1,2,3") {
		t.Errorf("raw query = %q, missing ids=1,2,3", qs[0])
	}
}

func TestUserGet_ByUsernames_BatchLookup(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "get", "--usernames", "alice,bob"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, qs := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users/by" {
		t.Fatalf("paths = %v, want /2/users/by", paths)
	}
	if !strings.Contains(qs[0], "usernames=alice%2Cbob") && !strings.Contains(qs[0], "usernames=alice,bob") {
		t.Errorf("raw query = %q, missing usernames=alice,bob", qs[0])
	}
}

func TestUserGet_NoArguments_Rejects(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "get"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestUserGet_PositionalAndIDs_ConflictRejected(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "get", "42", "--ids", "1,2"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

// =============================================================================
// user search
// =============================================================================

func TestUserSearch_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "search", "golang"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, qs := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users/search" {
		t.Fatalf("paths = %v, want /2/users/search", paths)
	}
	if !strings.Contains(qs[0], "query=golang") {
		t.Errorf("query = %q, missing query=golang", qs[0])
	}
}

func TestUserSearch_All_AggregatesPages(t *testing.T) {
	setAllXAPIEnv(t)
	// 2 ページのレスポンス: 1 ページ目に next_token, 2 ページ目で終わり
	var call int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		c := atomic.AddInt32(&call, 1)
		w.WriteHeader(http.StatusOK)
		if c == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"1","username":"a","name":"A"}],"meta":{"result_count":1,"next_token":"P1"}}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"id":"2","username":"b","name":"B"}],"meta":{"result_count":1}}`))
		}
	}))
	t.Cleanup(srv.Close)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "search", "golang", "--all"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got xapi.UsersResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v, out=%q", err, buf.String())
	}
	if len(got.Data) != 2 {
		t.Errorf("len(Data) = %d, want 2", len(got.Data))
	}
	if got.Meta.NextToken != "" {
		t.Errorf("Meta.NextToken = %q, want empty after aggregation", got.Meta.NextToken)
	}
	if got.Meta.ResultCount != 2 {
		t.Errorf("Meta.ResultCount = %d, want 2", got.Meta.ResultCount)
	}
}

func TestUserSearch_NDJSON_StreamsUsers(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "search", "golang", "--ndjson"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	lines := strings.Split(out, "\n")
	if len(lines) != 1 {
		t.Errorf("got %d NDJSON lines, want 1: %q", len(lines), out)
	}
	var u xapi.User
	if err := json.Unmarshal([]byte(lines[0]), &u); err != nil {
		t.Errorf("line is not valid JSON: %v", err)
	}
}

func TestUserSearch_MaxResultsOutOfRange(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	tests := []struct {
		name string
		val  string
	}{
		{"below", "0"},
		{"above", "1001"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewRootCmd()
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs([]string{"user", "search", "x", "--max-results", tc.val})

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
// user following / followers
// =============================================================================

func TestUserFollowing_UserIDDefaultsToMe(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "following"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	// expect: /2/users/me → /2/users/42/following
	if len(paths) < 2 {
		t.Fatalf("paths = %v, want at least 2 (me + following)", paths)
	}
	if paths[0] != "/2/users/me" {
		t.Errorf("paths[0] = %q, want /2/users/me", paths[0])
	}
	if paths[1] != "/2/users/42/following" {
		t.Errorf("paths[1] = %q, want /2/users/42/following", paths[1])
	}
}

func TestUserFollowers_UserIDExplicit(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "followers", "--user-id", "99"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users/99/followers" {
		t.Fatalf("paths = %v, want /2/users/99/followers (GetUserMe NOT called)", paths)
	}
	for _, p := range paths {
		if p == "/2/users/me" {
			t.Errorf("GetUserMe should not be called when --user-id is explicit, paths=%v", paths)
		}
	}
}

func TestUserFollowing_UsernamePositional_ResolvesViaGetUserByUsername(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "following", "@bob"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	// Should call GetUserByUsername first (id=7 from mock), then GetFollowing on /2/users/7/following
	if len(paths) < 2 {
		t.Fatalf("paths = %v, want at least 2", paths)
	}
	if paths[0] != "/2/users/by/username/bob" {
		t.Errorf("paths[0] = %q, want /2/users/by/username/bob", paths[0])
	}
	if paths[1] != "/2/users/7/following" {
		t.Errorf("paths[1] = %q, want /2/users/7/following", paths[1])
	}
}

func TestUserFollowing_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "following", "--user-id", "42", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "id=99") || !strings.Contains(out, "username=target") {
		t.Errorf("out = %q, want id=99 / username=target", out)
	}
}

func TestUserFollowing_MaxResults1000_OK(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "following", "--user-id", "42", "--max-results", "1000"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute (--max-results 1000 should be OK for graph): %v", err)
	}
	_, qs := state.snapshot()
	if len(qs) == 0 || !strings.Contains(qs[0], "max_results=1000") {
		t.Errorf("query = %q, missing max_results=1000", qs)
	}
}

func TestUserFollowing_MaxResultsOutOfRange(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "following", "--user-id", "42", "--max-results", "1001"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

// =============================================================================
// user blocking / muting (self only)
// =============================================================================

func TestUserBlocking_AlwaysResolvesSelf(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "blocking"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) < 2 {
		t.Fatalf("paths = %v, want at least 2 (me + blocking)", paths)
	}
	if paths[0] != "/2/users/me" {
		t.Errorf("paths[0] = %q, want /2/users/me (self always resolved)", paths[0])
	}
	if paths[1] != "/2/users/42/blocking" {
		t.Errorf("paths[1] = %q, want /2/users/42/blocking", paths[1])
	}
}

func TestUserBlocking_NoUserIDFlag_Pinned(t *testing.T) {
	t.Parallel()
	// blocking コマンドに --user-id フラグが登録されていないことを pin する (M32 D-5)。
	cmd := newUserBlockingCmd()
	if f := cmd.Flag("user-id"); f != nil {
		t.Errorf("blocking subcommand must NOT register --user-id flag (got: %+v)", f)
	}
}

func TestUserMuting_AlwaysResolvesSelf(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "muting"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) < 2 {
		t.Fatalf("paths = %v, want at least 2 (me + muting)", paths)
	}
	if paths[0] != "/2/users/me" {
		t.Errorf("paths[0] = %q, want /2/users/me", paths[0])
	}
	if paths[1] != "/2/users/42/muting" {
		t.Errorf("paths[1] = %q, want /2/users/42/muting", paths[1])
	}
}

func TestUserMuting_NoUserIDFlag_Pinned(t *testing.T) {
	t.Parallel()
	cmd := newUserMutingCmd()
	if f := cmd.Flag("user-id"); f != nil {
		t.Errorf("muting subcommand must NOT register --user-id flag (got: %+v)", f)
	}
}

// =============================================================================
// extractUserRef
// =============================================================================

func TestExtractUserRef_NumericID(t *testing.T) {
	t.Parallel()
	v, isUsername, err := extractUserRef("12345")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if v != "12345" || isUsername {
		t.Errorf("got (%q, %v), want (12345, false)", v, isUsername)
	}
}

func TestExtractUserRef_AtUsername(t *testing.T) {
	t.Parallel()
	v, isUsername, err := extractUserRef("@alice")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if v != "alice" || !isUsername {
		t.Errorf("got (%q, %v), want (alice, true)", v, isUsername)
	}
}

func TestExtractUserRef_URL_AllowsAlice(t *testing.T) {
	t.Parallel()
	v, isUsername, err := extractUserRef("https://x.com/alice")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if v != "alice" || !isUsername {
		t.Errorf("got (%q, %v), want (alice, true)", v, isUsername)
	}
}

func TestExtractUserRef_ReservedPaths_Reject(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"https://x.com/home",
		"https://twitter.com/i/web/status/123",
		"https://x.com/explore",
		"https://x.com/messages",
		"https://x.com/notifications",
	} {
		t.Run(s, func(t *testing.T) {
			_, _, err := extractUserRef(s)
			if err == nil {
				t.Errorf("%q: expected error, got nil", s)
			}
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("%q: err = %v, want ErrInvalidArgument", s, err)
			}
		})
	}
}

func TestExtractUserRef_InvalidUsername_Rejects(t *testing.T) {
	t.Parallel()
	for _, s := range []string{
		"@too-long-username-with-dashes",
		"@user.name",
		"@",
	} {
		t.Run(s, func(t *testing.T) {
			_, _, err := extractUserRef(s)
			if err == nil {
				t.Errorf("%q: expected error, got nil", s)
			}
		})
	}
}

// =============================================================================
// 共通
// =============================================================================

func TestUser_NoJSON_NDJSON_MutuallyExclusive(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newUserTestServer(t)
	stubUserClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"user", "search", "golang", "--no-json", "--ndjson"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}
