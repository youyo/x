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

// stubLikedClientFactory は newLikedClient を httptest サーバ向けに差し替えるテストヘルパ。
// M9 の stubMeClientFactory と同じ流儀で t.Cleanup で元の関数に戻す。
func stubLikedClientFactory(t *testing.T, baseURL string) {
	t.Helper()
	prev := newLikedClient
	t.Cleanup(func() { newLikedClient = prev })
	newLikedClient = func(ctx context.Context, _ *config.Credentials) (likedClient, error) {
		return xapi.NewClient(ctx, nil, xapi.WithBaseURL(baseURL)), nil
	}
}

// likedHandlerState は httptest ハンドラが受信したリクエストを記録する共有状態。
// テスト本体から goroutine 競合無しで読み出せるよう Mutex で保護する。
type likedHandlerState struct {
	mu    sync.Mutex
	paths []string
	rawQs []string
}

func (s *likedHandlerState) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths = append(s.paths, r.URL.Path)
	s.rawQs = append(s.rawQs, r.URL.RawQuery)
}

func (s *likedHandlerState) snapshot() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := append([]string(nil), s.paths...)
	q := append([]string(nil), s.rawQs...)
	return p, q
}

// newLikedTestServer はテスト用の httptest サーバを返す。
//
//   - GET /2/users/me → {"data":{"id":"42","username":"alice","name":"Alice"}}
//   - GET /2/users/:id/liked_tweets → likedHandler が組み立てる本文
//
// likedHandler が nil の場合はデフォルトで {data: [1件], meta: {result_count:1}} を返す。
func newLikedTestServer(t *testing.T, likedHandler http.HandlerFunc) (*httptest.Server, *likedHandlerState) {
	t.Helper()
	state := &likedHandlerState{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.record(r)
		switch {
		case r.URL.Path == "/2/users/me":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
		case strings.HasSuffix(r.URL.Path, "/liked_tweets"):
			if likedHandler != nil {
				likedHandler(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[{"id":"100","text":"hello","author_id":"42","created_at":"2026-05-12T01:23:45.000Z"}],"meta":{"result_count":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// TestLikedList_Success_DefaultUser は --user-id 未指定で
// GetUserMe → ListLikedTweets の 2 段呼び出しを経由し、
// {data, includes, meta} 形式の JSON が stdout に出力されることを検証する。
func TestLikedList_Success_DefaultUser(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	paths, _ := state.snapshot()
	if len(paths) < 2 {
		t.Fatalf("expected at least 2 requests (me + liked_tweets), got %d: %v", len(paths), paths)
	}
	if paths[0] != "/2/users/me" {
		t.Errorf("first request path = %q, want /2/users/me", paths[0])
	}
	if paths[1] != "/2/users/42/liked_tweets" {
		t.Errorf("second request path = %q, want /2/users/42/liked_tweets", paths[1])
	}

	// JSON 全体 ({data, includes, meta}) が出ることを検証 (D-4)。
	var got xapi.LikedTweetsResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON output: %v, output: %q", err, buf.String())
	}
	if got.Meta.ResultCount != 1 || len(got.Data) != 1 || got.Data[0].ID != "100" {
		t.Errorf("got = %+v, want ResultCount=1 Data[0].ID=100", got)
	}
}

// TestLikedList_Success_UserIDSpecified は --user-id 指定時に
// GetUserMe を呼ばずに直接 liked_tweets を取得することを検証する。
func TestLikedList_Success_UserIDSpecified(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	paths, _ := state.snapshot()
	for _, p := range paths {
		if p == "/2/users/me" {
			t.Errorf("/2/users/me should not be requested when --user-id is given, got paths: %v", paths)
		}
	}
	if len(paths) == 0 || paths[0] != "/2/users/12345/liked_tweets" {
		t.Errorf("first request path = %v, want /2/users/12345/liked_tweets", paths)
	}
}

// TestLikedList_MaxResultsInQuery は --max-results 50 がクエリに反映されることを検証する。
func TestLikedList_MaxResultsInQuery(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--max-results", "50"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	if !strings.Contains(qs[0], "max_results=50") {
		t.Errorf("query = %q, want max_results=50", qs[0])
	}
}

// TestLikedList_TimeWindowInQuery は --start-time / --end-time がクエリに反映されることを検証する。
func TestLikedList_TimeWindowInQuery(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"liked", "list",
		"--user-id", "12345",
		"--start-time", "2026-05-11T15:00:00Z",
		"--end-time", "2026-05-12T14:59:59Z",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	if !strings.Contains(qs[0], "start_time=2026-05-11T15%3A00%3A00Z") {
		t.Errorf("query = %q, want start_time=2026-05-11T15:00:00Z (url-encoded)", qs[0])
	}
	if !strings.Contains(qs[0], "end_time=2026-05-12T14%3A59%3A59Z") {
		t.Errorf("query = %q, want end_time=2026-05-12T14:59:59Z (url-encoded)", qs[0])
	}
}

// TestLikedList_FractionalSecondAccepted は fractional second 入りの RFC3339 入力が
// パース成功し、xapi 層で UTC 秒精度に丸まることを検証する (D-6, advisor 補足 #3)。
func TestLikedList_FractionalSecondAccepted(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"liked", "list",
		"--user-id", "12345",
		"--start-time", "2026-05-11T15:00:00.123Z",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	// xapi.WithStartTime の Format(time.RFC3339) は秒精度まで。fractional は落ちる。
	if !strings.Contains(qs[0], "start_time=2026-05-11T15%3A00%3A00Z") {
		t.Errorf("query = %q, want start_time=2026-05-11T15:00:00Z (fractional dropped)", qs[0])
	}
}

// TestLikedList_PaginationTokenInQuery は --pagination-token がクエリに反映されることを検証する。
func TestLikedList_PaginationTokenInQuery(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--pagination-token", "abc"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	if !strings.Contains(qs[0], "pagination_token=abc") {
		t.Errorf("query = %q, want pagination_token=abc", qs[0])
	}
}

// TestLikedList_NoJSON_Human は --no-json で human 1 行/ツイートが出力されることを検証する。
//
// 検証ポイント:
//   - 各行に id= author= created= text= が含まれる
//   - text 中の改行が半角スペースに置換される
//   - UTF-8 ルーン数 80 超過時に … で truncate される
func TestLikedList_NoJSON_Human(t *testing.T) {
	setAllXAPIEnv(t)

	// 1 件目: 通常 / 2 件目: 改行入り / 3 件目: 80 ルーン超 (a×100)
	body := `{"data":[
		{"id":"100","text":"line1","author_id":"42","created_at":"2026-05-12T01:23:45.000Z"},
		{"id":"200","text":"foo\nbar\tbaz","author_id":"42","created_at":"2026-05-12T01:23:46.000Z"},
		{"id":"300","text":"` + strings.Repeat("a", 100) + `","author_id":"42","created_at":"2026-05-12T01:23:47.000Z"}
	],"meta":{"result_count":3}}`

	srv, _ := newLikedTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	// JSON ではないことの確認
	var any map[string]any
	if err := json.Unmarshal(buf.Bytes(), &any); err == nil {
		t.Errorf("expected non-JSON output, got JSON: %q", out)
	}
	for _, want := range []string{
		"id=100",
		"id=200",
		"id=300",
		"author=42",
		"created=2026-05-12T01:23:45.000Z",
		"text=line1",
		"text=foo bar baz", // 改行とタブが半角スペースに置換
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %q", want, out)
		}
	}
	// truncate 確認: 80 ルーン制限 (79 a + …)
	expectedTruncated := strings.Repeat("a", 79) + "…"
	if !strings.Contains(out, "text="+expectedTruncated) {
		t.Errorf("expected truncated text=%s in output, got: %q", expectedTruncated, out)
	}
	// TAB セパレータが消えていないことの assert (フィールド連結時の取りこぼし検出)。
	if !strings.Contains(out, "id=100\tauthor=42\tcreated=2026-05-12T01:23:45.000Z\ttext=line1") {
		t.Errorf("expected tab-separated fields for id=100, got: %q", out)
	}
	// 3 行であることを検証 (末尾の trailing newline を除去してから数える)
	trimmed := strings.TrimRight(out, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %q", len(lines), out)
	}
}

// TestLikedList_NoJSON_EmptyResponse は 0 件レスポンス時に stdout が空であることを検証する。
func TestLikedList_NoJSON_EmptyResponse(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newLikedTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	})
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty stdout for 0 tweets, got: %q", buf.String())
	}
}

// TestLikedList_InvalidArgument は引数バリデーション失敗時に
// cli.ErrInvalidArgument が返ることを table-driven で検証する (advisor 補足 #4)。
func TestLikedList_InvalidArgument(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"max_results=0", []string{"liked", "list", "--max-results", "0"}},
		{"max_results=101", []string{"liked", "list", "--max-results", "101"}},
		{"max_results=-1", []string{"liked", "list", "--max-results", "-1"}},
		{"invalid_start_time", []string{"liked", "list", "--start-time", "invalid-date"}},
		{"date_only_end_time", []string{"liked", "list", "--end-time", "2026-05-11"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAllXAPIEnv(t)
			// httptest を立てる必要はない (バリデーションで失敗する想定) が、
			// 万一バリデーションを通り抜けても通信を発生させないため stub 化しておく。
			srv, _ := newLikedTestServer(t, nil)
			stubLikedClientFactory(t, srv.URL)

			cmd := NewRootCmd()
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error, got nil (out=%q)", buf.String())
			}
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("errors.Is(err, ErrInvalidArgument) = false (err=%v)", err)
			}
			if errors.Is(err, xapi.ErrAuthentication) {
				t.Errorf("err must not be xapi.ErrAuthentication (would mis-map to exit 3): %v", err)
			}
		})
	}
}

// TestLikedList_CredentialsMissing は env / file ともに認証情報が無いとき
// ErrCredentialsMissing が返ることを検証する (caller で exit 3 写像)。
func TestLikedList_CredentialsMissing(t *testing.T) {
	clearXAPIEnv(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrCredentialsMissing) {
		t.Errorf("errors.Is(err, ErrCredentialsMissing) = false (err=%v)", err)
	}
	if !errors.Is(err, xapi.ErrAuthentication) {
		t.Errorf("errors.Is(err, xapi.ErrAuthentication) = false (err=%v)", err)
	}
}

// TestLikedList_401_AuthError は X API 401 で xapi.ErrAuthentication が返ることを検証する。
func TestLikedList_401_AuthError(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newLikedTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401}`))
	})
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, xapi.ErrAuthentication) {
		t.Errorf("errors.Is(err, xapi.ErrAuthentication) = false (err=%v)", err)
	}
}

// TestLikedList_403_Permission は X API 403 で xapi.ErrPermission が返ることを検証する。
func TestLikedList_403_Permission(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newLikedTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, xapi.ErrPermission) {
		t.Errorf("errors.Is(err, xapi.ErrPermission) = false (err=%v)", err)
	}
}

// TestLikedList_404_NotFound は X API 404 で xapi.ErrNotFound が返ることを検証する。
func TestLikedList_404_NotFound(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newLikedTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, xapi.ErrNotFound) {
		t.Errorf("errors.Is(err, xapi.ErrNotFound) = false (err=%v)", err)
	}
}

// TestLikedHelp_ShowsSubcommand は `x liked --help` に list サブコマンドが表示されることを検証する。
func TestLikedHelp_ShowsSubcommand(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "list") {
		t.Errorf("help output missing list, got: %s", out)
	}
}

// TestLikedListHelp_ShowsAllFlags は `x liked list --help` に 6 フラグすべてが表示されることを検証する。
func TestLikedListHelp_ShowsAllFlags(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"--user-id",
		"--start-time",
		"--end-time",
		"--max-results",
		"--pagination-token",
		"--no-json",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q, got: %s", want, out)
		}
	}
}

// TestRootHelp_ShowsLiked は `x --help` に liked サブコマンドが表示されることを検証する。
func TestRootHelp_ShowsLiked(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "liked") {
		t.Errorf("help output missing liked, got: %s", out)
	}
}

// TestLikedNoSubcommand は `x liked` (サブコマンド省略) で help が出力され
// エラーにならないことを検証する。
func TestLikedNoSubcommand(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "list") {
		t.Errorf("expected 'list' subcommand in help, got: %s", out)
	}
}
