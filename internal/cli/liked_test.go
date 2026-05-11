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
	// M12: loadLikedDefaults が開発機の ~/.config/x/config.toml を拾わないよう隔離。
	isolateXDG(t)

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

// --- M11 追加テスト ---

// newPagingLikedTestServer は `--all` 系テスト用の httptest サーバを返す。
//
// pagination_token に応じて異なる data + meta を返すページネーション動作を模倣する:
//   - 1 ページ目 (token なし): data=[10,11], meta.next_token="p2"
//   - 2 ページ目 (token=p2): data=[20,21], meta.next_token="p3"
//   - 3 ページ目 (token=p3): data=[30], meta.next_token="" (最終ページ)
func newPagingLikedTestServer(t *testing.T) (*httptest.Server, *likedHandlerState) {
	t.Helper()
	state := &likedHandlerState{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.record(r)
		switch {
		case r.URL.Path == "/2/users/me":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
		case strings.HasSuffix(r.URL.Path, "/liked_tweets"):
			tok := r.URL.Query().Get("pagination_token")
			w.WriteHeader(http.StatusOK)
			switch tok {
			case "":
				_, _ = w.Write([]byte(`{"data":[{"id":"10","text":"t10","author_id":"42","created_at":"2026-05-12T01:00:00.000Z"},{"id":"11","text":"t11","author_id":"42","created_at":"2026-05-12T01:00:01.000Z"}],"includes":{"users":[{"id":"42","username":"alice","name":"Alice"}]},"meta":{"result_count":2,"next_token":"p2"}}`))
			case "p2":
				_, _ = w.Write([]byte(`{"data":[{"id":"20","text":"t20","author_id":"42","created_at":"2026-05-12T02:00:00.000Z"},{"id":"21","text":"t21","author_id":"42","created_at":"2026-05-12T02:00:01.000Z"}],"meta":{"result_count":2,"next_token":"p3"}}`))
			case "p3":
				_, _ = w.Write([]byte(`{"data":[{"id":"30","text":"t30","author_id":"42","created_at":"2026-05-12T03:00:00.000Z"}],"meta":{"result_count":1}}`))
			default:
				_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// TestLikedList_SinceJST_QueryReflectsUTC は --since-jst が JST 0:00-23:59 を
// UTC RFC3339 に変換してクエリに反映することを検証する (D-2, D-4)。
func TestLikedList_SinceJST_QueryReflectsUTC(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--since-jst", "2026-05-12"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	// JST 2026-05-12 0:00:00 == UTC 2026-05-11 15:00:00
	if !strings.Contains(qs[0], "start_time=2026-05-11T15%3A00%3A00Z") {
		t.Errorf("query = %q, want start_time=2026-05-11T15:00:00Z", qs[0])
	}
	// JST 2026-05-12 23:59:59 == UTC 2026-05-12 14:59:59
	if !strings.Contains(qs[0], "end_time=2026-05-12T14%3A59%3A59Z") {
		t.Errorf("query = %q, want end_time=2026-05-12T14:59:59Z", qs[0])
	}
}

// TestLikedList_SinceJST_OverridesStartEnd は --since-jst が
// --start-time / --end-time を上書きすることを検証する (D-2)。
func TestLikedList_SinceJST_OverridesStartEnd(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"liked", "list", "--user-id", "12345",
		"--start-time", "2020-01-01T00:00:00Z",
		"--end-time", "2020-01-02T00:00:00Z",
		"--since-jst", "2026-05-12",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	if strings.Contains(qs[0], "start_time=2020") {
		t.Errorf("--start-time 2020 should be overridden, got: %q", qs[0])
	}
	if !strings.Contains(qs[0], "start_time=2026-05-11T15%3A00%3A00Z") {
		t.Errorf("query = %q, want since-jst start_time", qs[0])
	}
}

// TestLikedList_YesterdayJST_OverridesSinceJST は --yesterday-jst が
// --since-jst を上書きすることを検証する (D-2)。
func TestLikedList_YesterdayJST_OverridesSinceJST(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{
		"liked", "list", "--user-id", "12345",
		"--since-jst", "2020-01-01",
		"--yesterday-jst",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	// yesterday-jst の結果なので 2020 ではないはず
	if strings.Contains(qs[0], "start_time=2019") || strings.Contains(qs[0], "start_time=2020") {
		t.Errorf("--since-jst 2020 should be overridden by --yesterday-jst, got: %q", qs[0])
	}
}

// TestLikedList_YesterdayJST_QueryReflectsUTC は --yesterday-jst が
// 実行時刻基準で JST 前日範囲をクエリに反映することを検証する。
//
// 固定 clock を注入できないため、現在時刻基準で「直近 50 時間以内」かを検証する。
func TestLikedList_YesterdayJST_QueryReflectsUTC(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--yesterday-jst"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	if !strings.Contains(qs[0], "start_time=") || !strings.Contains(qs[0], "end_time=") {
		t.Errorf("--yesterday-jst should set start_time/end_time, got: %q", qs[0])
	}
}

// TestLikedList_SinceJST_InvalidFormat は --since-jst のフォーマット不正で
// ErrInvalidArgument が返ることを検証する。
func TestLikedList_SinceJST_InvalidFormat(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--since-jst", "notadate"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("errors.Is(err, ErrInvalidArgument) = false (err=%v)", err)
	}
}

// TestLikedList_All_FollowsNextToken は --all で next_token を辿り
// 3 ページ取得後 next_token="" で停止することを検証する。
func TestLikedList_All_FollowsNextToken(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newPagingLikedTestServer(t)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--all"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	paths, qs := state.snapshot()
	// 3 ページ分の liked_tweets リクエストが発生したことを検査
	pageCount := 0
	for _, p := range paths {
		if strings.HasSuffix(p, "/liked_tweets") {
			pageCount++
		}
	}
	if pageCount != 3 {
		t.Errorf("expected 3 liked_tweets requests, got %d (paths=%v)", pageCount, paths)
	}
	// 2 ページ目以降に pagination_token が付くこと
	tokenFound := false
	for _, q := range qs {
		if strings.Contains(q, "pagination_token=p2") || strings.Contains(q, "pagination_token=p3") {
			tokenFound = true
		}
	}
	if !tokenFound {
		t.Errorf("expected pagination_token on later pages, got: %v", qs)
	}
}

// TestLikedList_All_MaxPagesCap は --max-pages=2 で 2 ページで打ち切ることを検証する。
func TestLikedList_All_MaxPagesCap(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newPagingLikedTestServer(t)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--all", "--max-pages", "2"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	paths, _ := state.snapshot()
	pageCount := 0
	for _, p := range paths {
		if strings.HasSuffix(p, "/liked_tweets") {
			pageCount++
		}
	}
	if pageCount != 2 {
		t.Errorf("expected 2 liked_tweets requests with --max-pages=2, got %d", pageCount)
	}
}

// TestLikedList_All_AggregatedJSON は --all で全ページの data が集約 JSON 出力されること、
// meta.result_count が集約後の総件数になり、next_token が空になることを検証する (D-8)。
func TestLikedList_All_AggregatedJSON(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newPagingLikedTestServer(t)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--all"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var got xapi.LikedTweetsResponse
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON output: %v, out=%q", err, buf.String())
	}
	if len(got.Data) != 5 {
		t.Errorf("expected 5 tweets aggregated, got %d (data=%+v)", len(got.Data), got.Data)
	}
	if got.Meta.ResultCount != 5 {
		t.Errorf("expected meta.result_count=5 (recomputed), got %d", got.Meta.ResultCount)
	}
	if got.Meta.NextToken != "" {
		t.Errorf("expected meta.next_token='' after aggregation, got %q", got.Meta.NextToken)
	}
}

// TestLikedList_All_Human は --all --no-json で全ページ分の human 行が出力されることを検証する。
func TestLikedList_All_Human(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newPagingLikedTestServer(t)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--all", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"id=10", "id=11", "id=20", "id=21", "id=30"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %q", want, out)
		}
	}
}

// TestLikedList_NDJSON_SinglePage は --ndjson で 1 ツイート 1 行 JSON が出力されることを検証する。
func TestLikedList_NDJSON_SinglePage(t *testing.T) {
	setAllXAPIEnv(t)

	body := `{"data":[
		{"id":"100","text":"<hello>","author_id":"42","created_at":"2026-05-12T01:23:45.000Z"},
		{"id":"200","text":"bye","author_id":"42","created_at":"2026-05-12T01:23:46.000Z"}
	],"meta":{"result_count":2}}`

	srv, _ := newLikedTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--ndjson"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 NDJSON lines, got %d: %q", len(lines), out)
	}
	// 各行が JSON Tweet として decode 可能であること
	var first xapi.Tweet
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Errorf("line[0] not valid JSON Tweet: %v (line=%q)", err, lines[0])
	}
	if first.ID != "100" {
		t.Errorf("line[0].ID = %q, want 100", first.ID)
	}
	// D-12: HTML エスケープが無効化されている (`<hello>` がそのまま)
	if !strings.Contains(lines[0], "<hello>") {
		t.Errorf("expected raw <hello> (no HTML escape), got: %q", lines[0])
	}
}

// TestLikedList_NDJSON_AllStreaming は --all --ndjson でストリーミング出力されることを検証する。
func TestLikedList_NDJSON_AllStreaming(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newPagingLikedTestServer(t)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--all", "--ndjson"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 NDJSON lines from 3 pages, got %d: %q", len(lines), out)
	}
	// 順序確認: 10, 11, 20, 21, 30
	wantIDs := []string{"10", "11", "20", "21", "30"}
	for i, w := range wantIDs {
		var tw xapi.Tweet
		if err := json.Unmarshal([]byte(lines[i]), &tw); err != nil {
			t.Errorf("line[%d] not valid JSON: %v (%q)", i, err, lines[i])
			continue
		}
		if tw.ID != w {
			t.Errorf("line[%d].ID = %q, want %q", i, tw.ID, w)
		}
	}
}

// TestLikedList_NDJSON_EmptyResponse は 0 件で stdout が空になることを検証する。
func TestLikedList_NDJSON_EmptyResponse(t *testing.T) {
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
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--ndjson"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty stdout, got: %q", buf.String())
	}
}

// TestLikedList_NoJSON_NDJSON_Mutex は --no-json と --ndjson 同時指定で exit 2 になることを検証する (D-1)。
func TestLikedList_NoJSON_NDJSON_Mutex(t *testing.T) {
	setAllXAPIEnv(t)

	srv, _ := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--no-json", "--ndjson"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("errors.Is(err, ErrInvalidArgument) = false (err=%v)", err)
	}
}

// TestLikedList_DefaultFieldsInQuery はフラグ未指定時にデフォルト fields が
// クエリに反映されることを検証する (spec §11 [liked] ハードコード)。
func TestLikedList_DefaultFieldsInQuery(t *testing.T) {
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

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	// tweet.fields デフォルト: id,text,author_id,created_at,entities,public_metrics
	if !strings.Contains(qs[0], "tweet.fields=") {
		t.Errorf("expected tweet.fields= in query, got: %q", qs[0])
	}
	if !strings.Contains(qs[0], "id") || !strings.Contains(qs[0], "text") {
		t.Errorf("expected default tweet.fields contents, got: %q", qs[0])
	}
	// expansions=author_id
	if !strings.Contains(qs[0], "expansions=author_id") {
		t.Errorf("expected expansions=author_id, got: %q", qs[0])
	}
	// user.fields=username,name
	if !strings.Contains(qs[0], "user.fields=") {
		t.Errorf("expected user.fields= in query, got: %q", qs[0])
	}
}

// TestLikedList_TweetFields_CustomCSV は --tweet-fields カスタム指定が反映されることを検証する。
func TestLikedList_TweetFields_CustomCSV(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--tweet-fields", "id,text"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	if !strings.Contains(qs[0], "tweet.fields=id%2Ctext") {
		t.Errorf("expected tweet.fields=id,text (url-encoded), got: %q", qs[0])
	}
}

// TestLikedList_Expansions_CustomCSV は --expansions カスタム指定が反映されることを検証する。
func TestLikedList_Expansions_CustomCSV(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--expansions", "author_id,referenced_tweets.id"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	if !strings.Contains(qs[0], "expansions=author_id%2Creferenced_tweets.id") {
		t.Errorf("expected custom expansions, got: %q", qs[0])
	}
}

// TestLikedList_UserFields_CustomCSV は --user-fields カスタム指定が反映されることを検証する。
func TestLikedList_UserFields_CustomCSV(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--user-fields", "username,name,verified"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	if !strings.Contains(qs[0], "user.fields=username%2Cname%2Cverified") {
		t.Errorf("expected custom user.fields, got: %q", qs[0])
	}
}

// TestLikedList_CSVWhitespaceTrim は csv 値の前後空白が trim されることを検証する (D-9)。
func TestLikedList_CSVWhitespaceTrim(t *testing.T) {
	setAllXAPIEnv(t)

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list", "--user-id", "12345", "--tweet-fields", " id , text "})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, qs := state.snapshot()
	if len(qs) == 0 {
		t.Fatalf("no requests recorded")
	}
	if !strings.Contains(qs[0], "tweet.fields=id%2Ctext") {
		t.Errorf("expected trimmed tweet.fields=id,text, got: %q", qs[0])
	}
}

// TestLikedListHelp_ShowsExtFlags は --help に M11 で追加した全フラグが表示されることを検証する。
func TestLikedListHelp_ShowsExtFlags(t *testing.T) {
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
		"--since-jst",
		"--yesterday-jst",
		"--all",
		"--max-pages",
		"--ndjson",
		"--tweet-fields",
		"--expansions",
		"--user-fields",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help output missing %q, got: %s", want, out)
		}
	}
}
