package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gomcp "github.com/mark3labs/mcp-go/mcp"

	mcpinternal "github.com/youyo/x/internal/mcp"
	"github.com/youyo/x/internal/xapi"
)

// callLikedHandler は引数 map で handler を 1 回呼ぶテストヘルパ。
func callLikedHandler(t *testing.T, client *xapi.Client, args map[string]any) *gomcp.CallToolResult {
	t.Helper()
	handler := mcpinternal.NewGetLikedTweetsHandler(client)
	req := gomcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned protocol-level error: %v", err)
	}
	if res == nil {
		t.Fatal("handler returned nil result")
	}
	return res
}

// likedNoopHandler は X API を呼ばない単純な 200 OK ハンドラ (バリデーション失敗テスト用)。
func likedNoopHandler(_ http.ResponseWriter, _ *http.Request) {}

// likedJSONHandler は固定 JSON ボディを返す httptest ハンドラを生成する。
func likedJSONHandler(t *testing.T, body string, captured *string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			*captured = r.URL.String()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}
}

// ---------- Group A: 引数バリデーション ----------

func TestGetLikedTweetsHandler_Validation_InvalidSinceJST(t *testing.T) {
	t.Parallel()

	client := newTestXAPIClient(t, likedNoopHandler)
	res := callLikedHandler(t, client, map[string]any{
		"user_id":   "42",
		"since_jst": "2026/05/11", // 不正フォーマット (YYYY-MM-DD 期待)
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true; content=%s", extractTextContent(t, res))
	}
}

func TestGetLikedTweetsHandler_Validation_InvalidStartTime(t *testing.T) {
	t.Parallel()

	client := newTestXAPIClient(t, likedNoopHandler)
	res := callLikedHandler(t, client, map[string]any{
		"user_id":    "42",
		"start_time": "not-rfc3339",
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
}

func TestGetLikedTweetsHandler_Validation_MaxResultsOutOfRange(t *testing.T) {
	t.Parallel()

	client := newTestXAPIClient(t, likedNoopHandler)
	cases := []struct {
		name string
		val  float64
	}{
		{"zero", 0},
		{"above_100", 101},
		{"negative", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := callLikedHandler(t, client, map[string]any{
				"user_id":     "42",
				"max_results": tc.val,
			})
			if !res.IsError {
				t.Errorf("IsError = false, want true for max_results=%v", tc.val)
			}
		})
	}
}

func TestGetLikedTweetsHandler_Validation_MaxResultsFractional(t *testing.T) {
	t.Parallel()

	client := newTestXAPIClient(t, likedNoopHandler)
	res := callLikedHandler(t, client, map[string]any{
		"user_id":     "42",
		"max_results": 50.5,
	})
	if !res.IsError {
		t.Fatalf("IsError = false, want true for fractional max_results")
	}
}

func TestGetLikedTweetsHandler_Validation_TypeMismatch(t *testing.T) {
	t.Parallel()

	client := newTestXAPIClient(t, likedNoopHandler)
	cases := []struct {
		name string
		args map[string]any
	}{
		{
			name: "max_results_string",
			args: map[string]any{"user_id": "42", "max_results": "50"},
		},
		{
			name: "yesterday_jst_string",
			args: map[string]any{"user_id": "42", "yesterday_jst": "true"},
		},
		{
			name: "tweet_fields_string_not_array",
			args: map[string]any{"user_id": "42", "tweet_fields": "id,text"},
		},
		{
			name: "tweet_fields_array_with_non_string",
			args: map[string]any{"user_id": "42", "tweet_fields": []any{"id", 123}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := callLikedHandler(t, client, tc.args)
			if !res.IsError {
				t.Errorf("IsError = false, want true; args=%+v", tc.args)
			}
		})
	}
}

func TestGetLikedTweetsHandler_NilClient(t *testing.T) {
	t.Parallel()

	handler := mcpinternal.NewGetLikedTweetsHandler(nil)
	res, err := handler(context.Background(), gomcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handler returned protocol-level error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("IsError = false, want true for nil client")
	}
}

// ---------- Group B: 時間窓決定 ----------

func TestYesterdayJSTRange_Deterministic(t *testing.T) {
	t.Parallel()

	// 固定時刻 2026-05-12T03:00:00+09:00 (JST) → 前日 = 2026-05-11
	jst := time.FixedZone("JST", 9*3600)
	fixed := time.Date(2026, 5, 12, 3, 0, 0, 0, jst)
	start, end, err := mcpinternal.YesterdayJSTRangeForTest(fixed)
	if err != nil {
		t.Fatalf("YesterdayJSTRange: %v", err)
	}
	// start は JST 2026-05-11T00:00:00、end は JST 2026-05-11T23:59:59
	wantStart := time.Date(2026, 5, 11, 0, 0, 0, 0, jst)
	wantEnd := time.Date(2026, 5, 11, 23, 59, 59, 0, jst)
	if !start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", end, wantEnd)
	}
}

func TestGetLikedTweetsHandler_TimeWindow_SinceJST(t *testing.T) {
	t.Parallel()

	var captured string
	client := newTestXAPIClient(t, likedJSONHandler(t,
		`{"data":[],"meta":{"result_count":0}}`, &captured,
	))
	res := callLikedHandler(t, client, map[string]any{
		"user_id":   "42",
		"since_jst": "2026-05-11",
	})
	if res.IsError {
		t.Fatalf("IsError = true, want false; content=%s", extractTextContent(t, res))
	}
	// JST 2026-05-11 00:00:00 → UTC 2026-05-10T15:00:00Z
	// JST 2026-05-11 23:59:59 → UTC 2026-05-11T14:59:59Z
	if !strings.Contains(captured, "start_time=2026-05-10T15%3A00%3A00Z") {
		t.Errorf("captured URL missing expected start_time: %s", captured)
	}
	if !strings.Contains(captured, "end_time=2026-05-11T14%3A59%3A59Z") {
		t.Errorf("captured URL missing expected end_time: %s", captured)
	}
}

func TestGetLikedTweetsHandler_TimeWindow_StartEnd(t *testing.T) {
	t.Parallel()

	var captured string
	client := newTestXAPIClient(t, likedJSONHandler(t,
		`{"data":[],"meta":{"result_count":0}}`, &captured,
	))
	res := callLikedHandler(t, client, map[string]any{
		"user_id":    "42",
		"start_time": "2026-05-10T00:00:00Z",
		"end_time":   "2026-05-10T23:59:59Z",
	})
	if res.IsError {
		t.Fatalf("IsError = true, want false; content=%s", extractTextContent(t, res))
	}
	if !strings.Contains(captured, "start_time=2026-05-10T00%3A00%3A00Z") {
		t.Errorf("captured URL missing start_time: %s", captured)
	}
	if !strings.Contains(captured, "end_time=2026-05-10T23%3A59%3A59Z") {
		t.Errorf("captured URL missing end_time: %s", captured)
	}
}

func TestGetLikedTweetsHandler_TimeWindow_Priority_SinceJSTOverStartEnd(t *testing.T) {
	t.Parallel()

	var captured string
	client := newTestXAPIClient(t, likedJSONHandler(t,
		`{"data":[],"meta":{"result_count":0}}`, &captured,
	))
	res := callLikedHandler(t, client, map[string]any{
		"user_id":    "42",
		"since_jst":  "2026-05-11",
		"start_time": "2025-01-01T00:00:00Z", // 上書きされる
		"end_time":   "2025-01-02T00:00:00Z", // 上書きされる
	})
	if res.IsError {
		t.Fatalf("IsError = true, want false")
	}
	// since_jst の範囲が勝つ
	if !strings.Contains(captured, "start_time=2026-05-10T15%3A00%3A00Z") {
		t.Errorf("since_jst should win over start_time, got: %s", captured)
	}
	if strings.Contains(captured, "2025-01-01") {
		t.Errorf("start_time should be overridden, got: %s", captured)
	}
}

// ---------- Group C: シングルページ取得 ----------

func TestGetLikedTweetsHandler_Single_Success(t *testing.T) {
	t.Parallel()

	body := `{
		"data":[
			{"id":"1","text":"t1"},
			{"id":"2","text":"t2"},
			{"id":"3","text":"t3"},
			{"id":"4","text":"t4"},
			{"id":"5","text":"t5"}
		],
		"includes":{"users":[{"id":"42","username":"alice","name":"Alice"}]},
		"meta":{"result_count":5}
	}`
	client := newTestXAPIClient(t, likedJSONHandler(t, body, nil))
	res := callLikedHandler(t, client, map[string]any{"user_id": "42"})
	if res.IsError {
		t.Fatalf("IsError = true, want false; content=%s", extractTextContent(t, res))
	}
	structured, ok := res.StructuredContent.(*xapi.LikedTweetsResponse)
	if !ok {
		t.Fatalf("StructuredContent type = %T, want *xapi.LikedTweetsResponse", res.StructuredContent)
	}
	if got := len(structured.Data); got != 5 {
		t.Errorf("Data len = %d, want 5", got)
	}
	if structured.Meta.ResultCount != 5 {
		t.Errorf("Meta.ResultCount = %d, want 5", structured.Meta.ResultCount)
	}
}

func TestGetLikedTweetsHandler_Single_DefaultUserID_ResolvesSelf(t *testing.T) {
	t.Parallel()

	// 2 段階リクエスト: 最初に /2/users/me、続いて /2/users/42/liked_tweets
	var gotPaths []string
	var mu atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := mu.Add(1)
		gotPaths = append(gotPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		switch idx {
		case 1:
			// GetUserMe
			_, _ = w.Write([]byte(`{"data":{"id":"42","username":"alice","name":"Alice"}}`))
		default:
			// ListLikedTweets
			_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
		}
	}))
	t.Cleanup(srv.Close)
	client := xapi.NewClient(context.Background(), nil, xapi.WithBaseURL(srv.URL))

	res := callLikedHandler(t, client, map[string]any{}) // user_id 未指定
	if res.IsError {
		t.Fatalf("IsError = true, want false; content=%s", extractTextContent(t, res))
	}
	if len(gotPaths) < 2 {
		t.Fatalf("expected 2 requests, got %d: %+v", len(gotPaths), gotPaths)
	}
	if gotPaths[0] != "/2/users/me" {
		t.Errorf("1st request path = %s, want /2/users/me", gotPaths[0])
	}
	if gotPaths[1] != "/2/users/42/liked_tweets" {
		t.Errorf("2nd request path = %s, want /2/users/42/liked_tweets", gotPaths[1])
	}
}

func TestGetLikedTweetsHandler_Single_TextContent_HasSpecKeys(t *testing.T) {
	t.Parallel()

	body := `{"data":[{"id":"1","text":"t1"}],"includes":{"users":[]},"meta":{"result_count":1}}`
	client := newTestXAPIClient(t, likedJSONHandler(t, body, nil))
	res := callLikedHandler(t, client, map[string]any{"user_id": "42"})
	text := extractTextContent(t, res)
	// spec §6 の出力キーがすべて含まれる
	for _, key := range []string{`"data"`, `"includes"`, `"meta"`} {
		if !strings.Contains(text, key) {
			t.Errorf("TextContent missing %s: %s", key, text)
		}
	}
}

// ---------- Group D: 全件取得 (all=true) ----------

func TestGetLikedTweetsHandler_All_AggregatesThreePages(t *testing.T) {
	t.Parallel()

	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := count.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		switch idx {
		case 1:
			_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"a"}],"meta":{"result_count":1,"next_token":"t1"}}`))
		case 2:
			_, _ = w.Write([]byte(`{"data":[{"id":"2","text":"b"}],"meta":{"result_count":1,"next_token":"t2"}}`))
		case 3:
			_, _ = w.Write([]byte(`{"data":[{"id":"3","text":"c"}],"meta":{"result_count":1}}`))
		default:
			t.Errorf("unexpected request %d", idx)
		}
	}))
	t.Cleanup(srv.Close)
	client := xapi.NewClient(context.Background(), nil, xapi.WithBaseURL(srv.URL))

	res := callLikedHandler(t, client, map[string]any{
		"user_id": "42",
		"all":     true,
	})
	if res.IsError {
		t.Fatalf("IsError = true, want false; content=%s", extractTextContent(t, res))
	}
	structured, ok := res.StructuredContent.(*xapi.LikedTweetsResponse)
	if !ok {
		t.Fatalf("StructuredContent type = %T", res.StructuredContent)
	}
	if got := len(structured.Data); got != 3 {
		t.Errorf("aggregated Data len = %d, want 3", got)
	}
	if structured.Meta.ResultCount != 3 {
		t.Errorf("Meta.ResultCount = %d, want 3 (rebuilt)", structured.Meta.ResultCount)
	}
	if structured.Meta.NextToken != "" {
		t.Errorf("Meta.NextToken = %q, want empty (rebuilt)", structured.Meta.NextToken)
	}
}

func TestGetLikedTweetsHandler_All_MaxPagesTruncates(t *testing.T) {
	t.Parallel()

	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		idx := count.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		switch idx {
		case 1:
			_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"a"}],"meta":{"result_count":1,"next_token":"t1"}}`))
		case 2:
			_, _ = w.Write([]byte(`{"data":[{"id":"2","text":"b"}],"meta":{"result_count":1,"next_token":"t2"}}`))
		default:
			t.Errorf("max_pages=2 should not allow page %d", idx)
		}
	}))
	t.Cleanup(srv.Close)
	client := xapi.NewClient(context.Background(), nil, xapi.WithBaseURL(srv.URL))

	res := callLikedHandler(t, client, map[string]any{
		"user_id":   "42",
		"all":       true,
		"max_pages": float64(2),
	})
	if res.IsError {
		t.Fatalf("IsError = true, want false; content=%s", extractTextContent(t, res))
	}
	if got := count.Load(); got != 2 {
		t.Errorf("request count = %d, want 2 (truncated at max_pages)", got)
	}
}

func TestGetLikedTweetsHandler_All_SinglePageNoNextToken(t *testing.T) {
	t.Parallel()

	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"1","text":"a"}],"meta":{"result_count":1}}`))
	}))
	t.Cleanup(srv.Close)
	client := xapi.NewClient(context.Background(), nil, xapi.WithBaseURL(srv.URL))

	res := callLikedHandler(t, client, map[string]any{
		"user_id": "42",
		"all":     true,
	})
	if res.IsError {
		t.Fatalf("IsError = true, want false")
	}
	if got := count.Load(); got != 1 {
		t.Errorf("request count = %d, want 1", got)
	}
}

// ---------- Group E: クエリパラメータ反映 ----------

func TestGetLikedTweetsHandler_QueryParams_Custom(t *testing.T) {
	t.Parallel()

	var captured string
	client := newTestXAPIClient(t, likedJSONHandler(t,
		`{"data":[],"meta":{"result_count":0}}`, &captured,
	))
	res := callLikedHandler(t, client, map[string]any{
		"user_id":      "42",
		"tweet_fields": []any{"id", "text"},
		"expansions":   []any{"author_id"},
		"user_fields":  []any{"username"},
		"max_results":  float64(50),
	})
	if res.IsError {
		t.Fatalf("IsError = true, want false; content=%s", extractTextContent(t, res))
	}
	// URL エンコード後の文字列で確認 (`.` → `.` のまま、`,` → `%2C`)
	for _, want := range []string{
		"tweet.fields=id%2Ctext",
		"expansions=author_id",
		"user.fields=username",
		"max_results=50",
	} {
		if !strings.Contains(captured, want) {
			t.Errorf("captured URL missing %q: %s", want, captured)
		}
	}
}

func TestGetLikedTweetsHandler_QueryParams_Defaults(t *testing.T) {
	t.Parallel()

	var captured string
	client := newTestXAPIClient(t, likedJSONHandler(t,
		`{"data":[],"meta":{"result_count":0}}`, &captured,
	))
	res := callLikedHandler(t, client, map[string]any{"user_id": "42"})
	if res.IsError {
		t.Fatalf("IsError = true, want false")
	}
	// spec §11 のデフォルト値が URL に反映される (max_results=100 / tweet.fields / expansions / user.fields)
	for _, want := range []string{
		"max_results=100",
		"tweet.fields=id", // 一部一致で十分
		"expansions=author_id",
		"user.fields=username",
	} {
		if !strings.Contains(captured, want) {
			t.Errorf("default URL missing %q: %s", want, captured)
		}
	}
}

// ---------- Group F: X API エラー → IsError ----------

func TestGetLikedTweetsHandler_Error_401(t *testing.T) {
	t.Parallel()

	client := newTestXAPIClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401}`))
	})
	res := callLikedHandler(t, client, map[string]any{"user_id": "42"})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
}

func TestGetLikedTweetsHandler_Error_404(t *testing.T) {
	t.Parallel()

	client := newTestXAPIClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found","status":404}`))
	})
	res := callLikedHandler(t, client, map[string]any{"user_id": "missing"})
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
}

func TestGetLikedTweetsHandler_Error_SelfResolveFails(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /2/users/me → 401
		if strings.Contains(r.URL.Path, "/users/me") {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401}`))
			return
		}
		t.Errorf("should not reach liked_tweets when self resolve fails: %s", r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	client := xapi.NewClient(context.Background(), nil, xapi.WithBaseURL(srv.URL), xapi.WithMaxRetries(0))

	res := callLikedHandler(t, client, map[string]any{}) // user_id 未指定
	if !res.IsError {
		t.Fatalf("IsError = false, want true")
	}
}

// ---------- Group G: 出力スキーマ ----------

func TestGetLikedTweetsHandler_Output_TextJSONShape(t *testing.T) {
	t.Parallel()

	body := `{"data":[{"id":"1","text":"hello"}],"includes":{"users":[{"id":"42","username":"alice","name":"Alice"}]},"meta":{"result_count":1}}`
	client := newTestXAPIClient(t, likedJSONHandler(t, body, nil))
	res := callLikedHandler(t, client, map[string]any{"user_id": "42"})
	text := extractTextContent(t, res)

	var got xapi.LikedTweetsResponse
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("TextContent is not valid JSON: %v; text=%s", err, text)
	}
	if len(got.Data) != 1 || got.Data[0].ID != "1" {
		t.Errorf("decoded Data unexpected: %+v", got.Data)
	}
	if got.Meta.ResultCount != 1 {
		t.Errorf("decoded Meta.ResultCount = %d, want 1", got.Meta.ResultCount)
	}
	if len(got.Includes.Users) != 1 || got.Includes.Users[0].ID != "42" {
		t.Errorf("decoded Includes.Users unexpected: %+v", got.Includes.Users)
	}
}
