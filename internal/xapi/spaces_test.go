package xapi

// spaces_test.go は M34 で追加された Spaces API ラッパーのテスト。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// =============================================================================
// GetSpace
// =============================================================================

func TestGetSpace_HitsCorrectEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"S1","state":"live","title":"Hello"}}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	resp, err := c.GetSpace(context.Background(), "S1")
	if err != nil {
		t.Fatalf("GetSpace: %v", err)
	}
	if gotPath != "/2/spaces/S1" {
		t.Errorf("path = %q, want /2/spaces/S1", gotPath)
	}
	if resp.Data == nil || resp.Data.ID != "S1" {
		t.Errorf("Data = %+v, want ID=S1", resp.Data)
	}
}

func TestGetSpace_AllOptionsReflected(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"S1"}}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetSpace(context.Background(), "S1",
		WithSpaceLookupSpaceFields("state", "title"),
		WithSpaceLookupExpansions("host_ids"),
		WithSpaceLookupUserFields("username"),
		WithSpaceLookupTopicFields("name"),
	)
	if err != nil {
		t.Fatalf("GetSpace: %v", err)
	}
	want := []string{
		"space.fields=state%2Ctitle",
		"expansions=host_ids",
		"user.fields=username",
		"topic.fields=name",
	}
	for _, w := range want {
		if !strings.Contains(gotQuery, w) {
			t.Errorf("query %q missing %q", gotQuery, w)
		}
	}
}

func TestGetSpace_EmptyID_RejectsArgument(t *testing.T) {
	c := NewClient(context.Background(), nil)
	_, err := c.GetSpace(context.Background(), "")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "spaceID must be non-empty") {
		t.Errorf("err = %v", err)
	}
}

func TestGetSpace_404_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found Error"}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetSpace(context.Background(), "S1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetSpace_InvalidJSON_NoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetSpace(context.Background(), "S1")
	if err == nil {
		t.Fatal("want error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry on decode error)", got)
	}
}

// =============================================================================
// GetSpaces (batch ids)
// =============================================================================

func TestGetSpaces_BatchIDs_HitsCorrectEndpoint(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"S1"},{"id":"S2"}]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	resp, err := c.GetSpaces(context.Background(), []string{"S1", "S2", "S3"})
	if err != nil {
		t.Fatalf("GetSpaces: %v", err)
	}
	if gotPath != "/2/spaces" {
		t.Errorf("path = %q, want /2/spaces", gotPath)
	}
	if !strings.Contains(gotQuery, "ids=S1%2CS2%2CS3") {
		t.Errorf("query %q missing ids=S1,S2,S3", gotQuery)
	}
	if len(resp.Data) != 2 {
		t.Errorf("Data len = %d, want 2", len(resp.Data))
	}
}

func TestGetSpaces_EmptyIDs_Rejects(t *testing.T) {
	c := NewClient(context.Background(), nil)
	_, err := c.GetSpaces(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "spaceIDs must be non-empty") {
		t.Errorf("err = %v", err)
	}
}

func TestGetSpaces_TooManyIDs_Rejects(t *testing.T) {
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = fmt.Sprintf("S%d", i)
	}
	c := NewClient(context.Background(), nil)
	_, err := c.GetSpaces(context.Background(), ids)
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("err = %v", err)
	}
}

// =============================================================================
// SearchSpaces
// =============================================================================

func TestSearchSpaces_HitsCorrectEndpoint(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"S1"}]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.SearchSpaces(context.Background(), "AI")
	if err != nil {
		t.Fatalf("SearchSpaces: %v", err)
	}
	if gotPath != "/2/spaces/search" {
		t.Errorf("path = %q, want /2/spaces/search", gotPath)
	}
	if !strings.Contains(gotQuery, "query=AI") {
		t.Errorf("query %q missing query=AI", gotQuery)
	}
}

func TestSearchSpaces_EmptyQuery_Rejects(t *testing.T) {
	c := NewClient(context.Background(), nil)
	_, err := c.SearchSpaces(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "query must be non-empty") {
		t.Errorf("err = %v", err)
	}
}

func TestSearchSpaces_MaxResultsStateReflected(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.SearchSpaces(context.Background(), "AI",
		WithSpaceSearchMaxResults(50),
		WithSpaceSearchState("live"),
		WithSpaceSearchSpaceFields("state", "title"),
	)
	if err != nil {
		t.Fatalf("SearchSpaces: %v", err)
	}
	for _, w := range []string{"max_results=50", "state=live", "space.fields=state%2Ctitle"} {
		if !strings.Contains(gotQuery, w) {
			t.Errorf("query %q missing %q", gotQuery, w)
		}
	}
}

func TestSearchSpaces_QueryEscaped(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.SearchSpaces(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("SearchSpaces: %v", err)
	}
	if !strings.Contains(gotQuery, "query=hello+world") {
		t.Errorf("query %q missing query=hello+world", gotQuery)
	}
}

// =============================================================================
// GetSpacesByCreatorIDs
// =============================================================================

func TestGetSpacesByCreatorIDs_HitsCorrectEndpoint(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"S1"}]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetSpacesByCreatorIDs(context.Background(), []string{"U1", "U2"})
	if err != nil {
		t.Fatalf("GetSpacesByCreatorIDs: %v", err)
	}
	if gotPath != "/2/spaces/by/creator_ids" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "user_ids=U1%2CU2") {
		t.Errorf("query %q missing user_ids=U1,U2", gotQuery)
	}
}

func TestGetSpacesByCreatorIDs_EmptyIDs_Rejects(t *testing.T) {
	c := NewClient(context.Background(), nil)
	_, err := c.GetSpacesByCreatorIDs(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "creatorIDs must be non-empty") {
		t.Errorf("err = %v", err)
	}
}

func TestGetSpacesByCreatorIDs_TooManyIDs_Rejects(t *testing.T) {
	ids := make([]string, 101)
	for i := range ids {
		ids[i] = fmt.Sprintf("U%d", i)
	}
	c := NewClient(context.Background(), nil)
	_, err := c.GetSpacesByCreatorIDs(context.Background(), ids)
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Errorf("err = %v", err)
	}
}

// =============================================================================
// GetSpaceTweets / EachSpaceTweetsPage
// =============================================================================

func TestGetSpaceTweets_HitsCorrectEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"T1","text":"hi"}],"meta":{"result_count":1}}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	resp, err := c.GetSpaceTweets(context.Background(), "S1")
	if err != nil {
		t.Fatalf("GetSpaceTweets: %v", err)
	}
	if gotPath != "/2/spaces/S1/tweets" {
		t.Errorf("path = %q", gotPath)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "T1" {
		t.Errorf("Data = %+v", resp.Data)
	}
}

func TestGetSpaceTweets_AllOptionsReflected(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetSpaceTweets(context.Background(), "S1",
		WithSpaceTweetsMaxResults(50),
		WithSpaceTweetsPaginationToken("P0"),
		WithSpaceTweetsTweetFields("text", "author_id"),
		WithSpaceTweetsUserFields("username"),
		WithSpaceTweetsExpansions("author_id"),
		WithSpaceTweetsMediaFields("type"),
	)
	if err != nil {
		t.Fatalf("GetSpaceTweets: %v", err)
	}
	for _, w := range []string{
		"max_results=50",
		"pagination_token=P0",
		"tweet.fields=text%2Cauthor_id",
		"user.fields=username",
		"expansions=author_id",
		"media.fields=type",
	} {
		if !strings.Contains(gotQuery, w) {
			t.Errorf("query %q missing %q", gotQuery, w)
		}
	}
}

func TestGetSpaceTweets_PathEscape(t *testing.T) {
	var gotRequestURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// RequestURI は HTTP 行をそのまま保持しエスケープを失わない。
		gotRequestURI = r.RequestURI
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetSpaceTweets(context.Background(), "S 1")
	if err != nil {
		t.Fatalf("GetSpaceTweets: %v", err)
	}
	if !strings.Contains(gotRequestURI, "/2/spaces/S%201/tweets") {
		t.Errorf("requestURI = %q, want path containing /2/spaces/S%%201/tweets", gotRequestURI)
	}
}

func TestEachSpaceTweetsPage_MultiPage_FullTraversal(t *testing.T) {
	var seenTokens []string
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := atomic.AddInt32(&callCount, 1) - 1
		seenTokens = append(seenTokens, r.URL.Query().Get("pagination_token"))
		var body string
		switch idx {
		case 0:
			body = `{"data":[{"id":"T1","text":"a"}],"meta":{"result_count":1,"next_token":"P1"}}`
		case 1:
			body = `{"data":[{"id":"T2","text":"b"}],"meta":{"result_count":1}}`
		default:
			t.Errorf("unexpected call count %d", idx+1)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	var collected []Tweet
	err := c.EachSpaceTweetsPage(context.Background(), "S1", func(p *SpaceTweetsResponse) error {
		collected = append(collected, p.Data...)
		return nil
	})
	if err != nil {
		t.Fatalf("EachSpaceTweetsPage: %v", err)
	}
	if len(collected) != 2 {
		t.Errorf("collected = %d, want 2", len(collected))
	}
	if got := atomic.LoadInt32(&callCount); got != 2 {
		t.Errorf("callCount = %d, want 2", got)
	}
	if len(seenTokens) != 2 || seenTokens[0] != "" || seenTokens[1] != "P1" {
		t.Errorf("seenTokens = %v, want ['','P1']", seenTokens)
	}
}

func TestEachSpaceTweetsPage_PaginationParamName(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		// 1 ページのみ
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	err := c.EachSpaceTweetsPage(context.Background(), "S1", func(_ *SpaceTweetsResponse) error { return nil },
		WithSpaceTweetsPaginationToken("StartFromHere"))
	if err != nil {
		t.Fatalf("EachSpaceTweetsPage: %v", err)
	}
	if !strings.Contains(gotQuery, "pagination_token=StartFromHere") {
		t.Errorf("query %q missing pagination_token=StartFromHere (param name pin)", gotQuery)
	}
}
