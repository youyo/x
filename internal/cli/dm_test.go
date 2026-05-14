package cli

// dm_test.go は M35 で追加された `x dm {list,get,conversation,with}` コマンド群のテスト。

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

// stubDMClientFactory は newDMClient を httptest サーバ向けに差し替える。
func stubDMClientFactory(t *testing.T, baseURL string) {
	t.Helper()
	prev := newDMClient
	t.Cleanup(func() { newDMClient = prev })
	newDMClient = func(ctx context.Context, _ *config.Credentials) (dmClient, error) {
		return xapi.NewClient(ctx, nil, xapi.WithBaseURL(baseURL)), nil
	}
}

// dmHandlerState は DM テスト用の httptest リクエスト記録器。
type dmHandlerState struct {
	mu    sync.Mutex
	paths []string
	rawQs []string
}

func (s *dmHandlerState) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths = append(s.paths, r.URL.Path)
	s.rawQs = append(s.rawQs, r.URL.RawQuery)
}

func (s *dmHandlerState) snapshot() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...), append([]string(nil), s.rawQs...)
}

// newDMTestServer は DM 系コマンドのテスト用 httptest サーバを返す。
//
//   - /2/users/me                              : self
//   - /2/users/by/username/<u>                 : username → ID 解決 (id=7)
//   - /2/dm_events                             : list (1 ページ)
//   - /2/dm_events/<id>                        : single event
//   - /2/dm_conversations/<convID>/dm_events   : conversation
//   - /2/dm_conversations/with/<pid>/dm_events : with user
func newDMTestServer(t *testing.T) (*httptest.Server, *dmHandlerState) {
	t.Helper()
	state := &dmHandlerState{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.record(r)
		path := r.URL.Path
		switch {
		case path == "/2/users/me":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
		case strings.HasPrefix(path, "/2/users/by/username/"):
			uname := strings.TrimPrefix(path, "/2/users/by/username/")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"7","username":"` + uname + `","name":"Name"}}`))
		case path == "/2/dm_events":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"E1","event_type":"MessageCreate","text":"hello","sender_id":"42","dm_conversation_id":"42-99","created_at":"2026-05-10T00:00:00.000Z"}],"meta":{"result_count":1}}`))
		case strings.HasPrefix(path, "/2/dm_events/"):
			id := strings.TrimPrefix(path, "/2/dm_events/")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"` + id + `","event_type":"MessageCreate","text":"single"}}`))
		case strings.HasSuffix(path, "/dm_events") && strings.HasPrefix(path, "/2/dm_conversations/with/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"W1","event_type":"MessageCreate","text":"with"}],"meta":{"result_count":1}}`))
		case strings.HasSuffix(path, "/dm_events") && strings.HasPrefix(path, "/2/dm_conversations/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"C1","event_type":"MessageCreate","text":"conv"}],"meta":{"result_count":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// =============================================================================
// dm list
// =============================================================================

func TestDMList_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/dm_events" {
		t.Fatalf("paths = %v, want /2/dm_events", paths)
	}
	var got xapi.DMEventsResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v, out=%q", err, buf.String())
	}
	if len(got.Data) != 1 || got.Data[0].ID != "E1" {
		t.Errorf("Data = %+v", got.Data)
	}
}

func TestDMList_EventTypesFlagReflected(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "list", "--event-types", "MessageCreate,ParticipantsJoin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	if len(qs) == 0 || !strings.Contains(qs[0], "event_types=MessageCreate%2CParticipantsJoin") {
		t.Errorf("rawQ = %q, want event_types=MessageCreate%%2CParticipantsJoin", qs)
	}
}

func TestDMList_EventTypesInvalidValue_Rejects(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "list", "--event-types", "Foo"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestDMList_EventTypesCaseSensitive_Rejects(t *testing.T) {
	// advisor #4: case-sensitive ホワイトリスト pin
	setAllXAPIEnv(t)
	srv, _ := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "list", "--event-types", "messagecreate"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for lowercase event_type")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestDMList_MaxResultsOutOfRange(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	for _, mr := range []string{"0", "101"} {
		cmd := NewRootCmd()
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs([]string{"dm", "list", "--max-results", mr})
		if err := cmd.Execute(); err == nil || !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("max-results=%s: err = %v, want ErrInvalidArgument", mr, err)
		}
	}
}

func TestDMList_All_AggregatesPages(t *testing.T) {
	setAllXAPIEnv(t)
	pageCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pageCount++
		w.WriteHeader(http.StatusOK)
		if pageCount == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"A"}],"meta":{"next_token":"n2","result_count":1}}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"id":"B"}],"meta":{"result_count":1}}`))
		}
	}))
	t.Cleanup(srv.Close)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "list", "--all", "--max-pages", "5"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got xapi.DMEventsResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v, out=%q", err, buf.String())
	}
	if len(got.Data) != 2 {
		t.Errorf("aggregated len = %d, want 2", len(got.Data))
	}
}

func TestDMList_NDJSON_Streams(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "list", "--ndjson"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// 1 event のレスポンスなので 1 行
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("lines = %d, want 1", len(lines))
	}
	var e xapi.DMEvent
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Errorf("invalid NDJSON line: %v: %q", err, lines[0])
	}
}

func TestDMList_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "list", "--no-json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "id=E1") || !strings.Contains(out, "type=MessageCreate") {
		t.Errorf("human output missing fields: %q", out)
	}
}

func TestDMList_NoJSON_NDJSON_MutuallyExclusive(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "list", "--no-json", "--ndjson"})
	err := cmd.Execute()
	if err == nil || !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

// =============================================================================
// dm get
// =============================================================================

func TestDMGet_HitsCorrectEndpoint(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "get", "12345"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/dm_events/12345" {
		t.Fatalf("paths = %v, want /2/dm_events/12345", paths)
	}
}

func TestDMGet_InvalidEventID_Rejects(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "get", "abc"})
	err := cmd.Execute()
	if err == nil || !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestDMGet_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "get", "12345", "--no-json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "id=12345") {
		t.Errorf("human output missing id=12345: %q", out)
	}
}

// =============================================================================
// dm conversation
// =============================================================================

func TestDMConversation_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "conversation", "A-B"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/dm_conversations/A-B/dm_events" {
		t.Fatalf("paths = %v, want /2/dm_conversations/A-B/dm_events", paths)
	}
}

func TestDMConversation_EmptyConvID_Rejects(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "conversation", "  "})
	err := cmd.Execute()
	if err == nil || !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestDMConversation_All_AggregatesPages(t *testing.T) {
	setAllXAPIEnv(t)
	pageCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pageCount++
		w.WriteHeader(http.StatusOK)
		if pageCount == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"X"}],"meta":{"next_token":"n2"}}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"id":"Y"}],"meta":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "conversation", "A-B", "--all"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got xapi.DMEventsResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v, out=%q", err, buf.String())
	}
	if len(got.Data) != 2 {
		t.Errorf("len = %d, want 2", len(got.Data))
	}
}

// =============================================================================
// dm with
// =============================================================================

func TestDMWith_ByNumericID(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "with", "789"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/dm_conversations/with/789/dm_events" {
		t.Fatalf("paths = %v, want /2/dm_conversations/with/789/dm_events", paths)
	}
}

func TestDMWith_ByUsername_ResolvesViaGetUserByUsername(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newDMTestServer(t)
	stubDMClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"dm", "with", "@alice"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	// 期待: /2/users/by/username/alice → id=7 → /2/dm_conversations/with/7/dm_events
	if len(paths) < 2 {
		t.Fatalf("paths = %v, want at least 2 calls", paths)
	}
	if paths[0] != "/2/users/by/username/alice" {
		t.Errorf("paths[0] = %q, want /2/users/by/username/alice", paths[0])
	}
	if paths[1] != "/2/dm_conversations/with/7/dm_events" {
		t.Errorf("paths[1] = %q, want /2/dm_conversations/with/7/dm_events", paths[1])
	}
}

// =============================================================================
// extractDMConversationID / extractDMEventID
// =============================================================================

func TestExtractDMConversationID_Table(t *testing.T) {
	cases := []struct {
		in        string
		want      string
		wantError bool
	}{
		{"123", "123", false},
		{"123-456", "123-456", false},
		{"group:abc", "group:abc", false},
		{"abc_def", "abc_def", false},
		{"  trimmed  ", "trimmed", false},
		{"", "", true},
		{"   ", "", true},
		{"has space", "", true},
		{"has/slash", "", true},
		{"has?", "", true},
	}
	for _, tc := range cases {
		got, err := extractDMConversationID(tc.in)
		if tc.wantError {
			if err == nil {
				t.Errorf("extractDMConversationID(%q) want error, got nil (got=%q)", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("extractDMConversationID(%q) unexpected err: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("extractDMConversationID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
