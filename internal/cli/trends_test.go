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

func stubTrendsClientFactory(t *testing.T, baseURL string) {
	t.Helper()
	prev := newTrendsClient
	t.Cleanup(func() { newTrendsClient = prev })
	newTrendsClient = func(ctx context.Context, _ *config.Credentials) (trendsClient, error) {
		return xapi.NewClient(ctx, nil, xapi.WithBaseURL(baseURL)), nil
	}
}

type trendsHandlerState struct {
	mu    sync.Mutex
	paths []string
	rawQs []string
}

func (s *trendsHandlerState) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths = append(s.paths, r.URL.Path)
	s.rawQs = append(s.rawQs, r.URL.RawQuery)
}

func (s *trendsHandlerState) snapshot() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...), append([]string(nil), s.rawQs...)
}

func newTrendsTestServer(t *testing.T) (*httptest.Server, *trendsHandlerState) {
	t.Helper()
	state := &trendsHandlerState{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.record(r)
		path := r.URL.Path
		switch {
		case strings.HasPrefix(path, "/2/trends/by/woeid/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"trend_name":"#golang","tweet_count":1234}]}`))
		case path == "/2/users/personalized_trends":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"trend_name":"#golang","category":"Technology","post_count":42,"trending_since":"2026-05-15T00:00:00.000Z"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// =============================================================================
// trends get
// =============================================================================

func TestTrendsGet_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newTrendsTestServer(t)
	stubTrendsClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"trends", "get", "1118370"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, _ := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/trends/by/woeid/1118370" {
		t.Fatalf("paths = %v", paths)
	}
	var got xapi.TrendsResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v out=%q", err, buf.String())
	}
	if len(got.Data) != 1 || got.Data[0].TrendName != "#golang" {
		t.Errorf("Data = %+v", got.Data)
	}
}

func TestTrendsGet_MaxTrendsFlag(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newTrendsTestServer(t)
	stubTrendsClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"trends", "get", "1", "--max-trends", "10"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, qs := state.snapshot()
	if !strings.Contains(qs[0], "max_trends=10") {
		t.Errorf("query %q missing max_trends=10 (param name pin)", qs[0])
	}
	if strings.Contains(qs[0], "max_results=") {
		t.Errorf("query %q must not contain max_results", qs[0])
	}
}

func TestTrendsGet_MaxTrendsOutOfRange(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newTrendsTestServer(t)
	stubTrendsClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"trends", "get", "1", "--max-trends", "51"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v", err)
	}
}

func TestTrendsGet_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newTrendsTestServer(t)
	stubTrendsClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"trends", "get", "1", "--no-json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"trend_name=#golang", "tweet_count=1234"} {
		if !strings.Contains(out, want) {
			t.Errorf("out=%q missing %q", out, want)
		}
	}
}

func TestTrendsGet_InvalidWoeid_Rejects(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newTrendsTestServer(t)
	stubTrendsClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"trends", "get", "abc"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v", err)
	}
}

func TestTrendsGet_NegativeWoeid_Rejects(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newTrendsTestServer(t)
	stubTrendsClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"trends", "get", "--", "-1"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v", err)
	}
}

// =============================================================================
// trends personal
// =============================================================================

func TestTrendsPersonal_DefaultJSON(t *testing.T) {
	setAllXAPIEnv(t)
	srv, state := newTrendsTestServer(t)
	stubTrendsClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"trends", "personal"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	paths, qs := state.snapshot()
	if len(paths) == 0 || paths[0] != "/2/users/personalized_trends" {
		t.Fatalf("paths = %v", paths)
	}
	// クエリ名が personalized_trend.fields であることを pin
	if !strings.Contains(qs[0], "personalized_trend.fields=") {
		t.Errorf("query %q missing personalized_trend.fields", qs[0])
	}
	var got xapi.TrendsResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v out=%q", err, buf.String())
	}
	if len(got.Data) != 1 || got.Data[0].PostCount != 42 {
		t.Errorf("Data = %+v", got.Data)
	}
}

func TestTrendsPersonal_NoJSON_HumanFormat(t *testing.T) {
	setAllXAPIEnv(t)
	srv, _ := newTrendsTestServer(t)
	stubTrendsClientFactory(t, srv.URL)
	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"trends", "personal", "--no-json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"trend_name=#golang", "category=Technology", "post_count=42"} {
		if !strings.Contains(out, want) {
			t.Errorf("out=%q missing %q", out, want)
		}
	}
}

func TestTrendsPersonal_NoUserIDFlag(t *testing.T) {
	// personal サブコマンドに --user-id フラグが存在しないことを pin (M34 D-7)。
	cmd := newTrendsPersonalCmd()
	if cmd.Flag("user-id") != nil {
		t.Errorf("--user-id flag should NOT exist on `trends personal` (X API auto-resolves)")
	}
}
