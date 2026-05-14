package xapi

// dm_test.go は M35 で追加された Direct Messages API ラッパーのテスト。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// =============================================================================
// GetDMEvents (paged, /2/dm_events)
// =============================================================================

func TestGetDMEvents_HitsCorrectEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"meta":{"result_count":0}}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.GetDMEvents(context.Background()); err != nil {
		t.Fatalf("GetDMEvents: %v", err)
	}
	if gotPath != "/2/dm_events" {
		t.Errorf("path = %q, want /2/dm_events", gotPath)
	}
}

func TestGetDMEvents_AllOptionsReflected(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetDMEvents(context.Background(),
		WithDMPagedMaxResults(50),
		WithDMPagedPaginationToken("ptoken"),
		WithDMPagedEventTypes("MessageCreate", "ParticipantsJoin"),
		WithDMPagedDMEventFields("id", "event_type", "text"),
		WithDMPagedExpansions("sender_id"),
		WithDMPagedUserFields("username"),
		WithDMPagedTweetFields("id"),
		WithDMPagedMediaFields("media_key", "type"),
	)
	if err != nil {
		t.Fatalf("GetDMEvents: %v", err)
	}
	want := []string{
		"max_results=50",
		"pagination_token=ptoken",
		"event_types=MessageCreate%2CParticipantsJoin",
		"dm_event.fields=id%2Cevent_type%2Ctext",
		"expansions=sender_id",
		"user.fields=username",
		"tweet.fields=id",
		"media.fields=media_key%2Ctype",
	}
	for _, w := range want {
		if !strings.Contains(gotQuery, w) {
			t.Errorf("query %q missing %q", gotQuery, w)
		}
	}
}

// TestGetDMEvents_EventTypesCSV は event_types が CSV で送信されることを pin する (M35 D-3)。
func TestGetDMEvents_EventTypesCSV(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetDMEvents(context.Background(),
		WithDMPagedEventTypes("MessageCreate", "ParticipantsLeave"),
	)
	if err != nil {
		t.Fatalf("GetDMEvents: %v", err)
	}
	if !strings.Contains(gotQuery, "event_types=MessageCreate%2CParticipantsLeave") {
		t.Errorf("query %q missing CSV-encoded event_types", gotQuery)
	}
	// 配列形式 (event_types=MessageCreate&event_types=...) ではないことを確認
	if strings.Count(gotQuery, "event_types=") != 1 {
		t.Errorf("event_types should appear exactly once (CSV), got query = %q", gotQuery)
	}
}

func TestGetDMEvents_DecodesData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data":[{
				"id":"1",
				"event_type":"MessageCreate",
				"text":"hello",
				"sender_id":"123",
				"dm_conversation_id":"123-456",
				"created_at":"2026-05-10T12:34:56.000Z"
			}],
			"meta":{"result_count":1}
		}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	resp, err := c.GetDMEvents(context.Background())
	if err != nil {
		t.Fatalf("GetDMEvents: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("Data len = %d, want 1", len(resp.Data))
	}
	e := resp.Data[0]
	if e.ID != "1" || e.EventType != "MessageCreate" || e.Text != "hello" ||
		e.SenderID != "123" || e.DMConversationID != "123-456" {
		t.Errorf("decoded event = %+v", e)
	}
}

func TestGetDMEvents_404_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found"}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetDMEvents(context.Background())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetDMEvents_InvalidJSON_NoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<<not json>>`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.GetDMEvents(context.Background()); err == nil {
		t.Fatal("want error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry on parse error)", got)
	}
}

func TestEachDMEventsPage_MultiPage_FullTraversal(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		w.WriteHeader(http.StatusOK)
		switch page {
		case 1:
			if r.URL.Query().Get("pagination_token") != "" {
				t.Errorf("page1: pagination_token should be empty, got %q", r.URL.Query().Get("pagination_token"))
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"1","event_type":"MessageCreate"}],"meta":{"next_token":"n1","result_count":1}}`))
		case 2:
			if r.URL.Query().Get("pagination_token") != "n1" {
				t.Errorf("page2: pagination_token = %q, want n1", r.URL.Query().Get("pagination_token"))
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"2","event_type":"MessageCreate"}],"meta":{"result_count":1}}`))
		default:
			t.Errorf("unexpected page = %d", page)
		}
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	var got []string
	err := c.EachDMEventsPage(context.Background(), func(p *DMEventsResponse) error {
		for _, e := range p.Data {
			got = append(got, e.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	if page != 2 {
		t.Errorf("page count = %d, want 2", page)
	}
	if len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Errorf("ids = %v", got)
	}
}

func TestEachDMEventsPage_RespectsMaxPages(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"x"}],"meta":{"next_token":"more"}}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	err := c.EachDMEventsPage(context.Background(), func(_ *DMEventsResponse) error { return nil },
		WithDMPagedMaxPages(1),
	)
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (max_pages=1)", got)
	}
}

// =============================================================================
// GetDMEvent (Lookup, /2/dm_events/:event_id)
// =============================================================================

func TestGetDMEvent_HitsCorrectEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"123","event_type":"MessageCreate"}}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	resp, err := c.GetDMEvent(context.Background(), "123")
	if err != nil {
		t.Fatalf("GetDMEvent: %v", err)
	}
	if gotPath != "/2/dm_events/123" {
		t.Errorf("path = %q, want /2/dm_events/123", gotPath)
	}
	if resp.Data == nil || resp.Data.ID != "123" {
		t.Errorf("Data = %+v", resp.Data)
	}
}

func TestGetDMEvent_EmptyID_Rejects(t *testing.T) {
	c := NewClient(context.Background(), nil)
	_, err := c.GetDMEvent(context.Background(), "")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "eventID must be non-empty") {
		t.Errorf("err = %v", err)
	}
}

func TestGetDMEvent_404_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found"}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetDMEvent(context.Background(), "123")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetDMEvent_OptionsReflected(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"123"}}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetDMEvent(context.Background(), "123",
		WithDMLookupDMEventFields("id", "text"),
		WithDMLookupExpansions("sender_id"),
		WithDMLookupUserFields("username"),
		WithDMLookupTweetFields("id"),
		WithDMLookupMediaFields("type"),
	)
	if err != nil {
		t.Fatalf("GetDMEvent: %v", err)
	}
	for _, w := range []string{
		"dm_event.fields=id%2Ctext",
		"expansions=sender_id",
		"user.fields=username",
		"tweet.fields=id",
		"media.fields=type",
	} {
		if !strings.Contains(gotQuery, w) {
			t.Errorf("query %q missing %q", gotQuery, w)
		}
	}
}

// =============================================================================
// GetDMConversation (/2/dm_conversations/:id/dm_events)
// =============================================================================

func TestGetDMConversation_HitsCorrectEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.GetDMConversation(context.Background(), "123-456"); err != nil {
		t.Fatalf("GetDMConversation: %v", err)
	}
	if gotPath != "/2/dm_conversations/123-456/dm_events" {
		t.Errorf("path = %q, want /2/dm_conversations/123-456/dm_events", gotPath)
	}
}

func TestGetDMConversation_GroupID_PathPreserved(t *testing.T) {
	// advisor #2: group:<id> 形式の conv_id をテストで pin
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.GetDMConversation(context.Background(), "group:abc"); err != nil {
		t.Fatalf("GetDMConversation: %v", err)
	}
	// `:` は unreserved in path だが url.PathEscape は `:` を残す。実際のサーバ実装に
	// 依存するが、ここでは Path 部分に `group:abc` がそのまま現れることを pin する。
	if !strings.Contains(gotPath, "group:abc") {
		t.Errorf("path = %q, want substring group:abc", gotPath)
	}
}

func TestGetDMConversation_EmptyID_Rejects(t *testing.T) {
	c := NewClient(context.Background(), nil)
	_, err := c.GetDMConversation(context.Background(), "")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "conversationID must be non-empty") {
		t.Errorf("err = %v", err)
	}
}

func TestEachDMConversationPage_MultiPage(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		page++
		w.WriteHeader(http.StatusOK)
		if page == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"a"}],"meta":{"next_token":"n"}}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"id":"b"}],"meta":{}}`))
		}
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	var ids []string
	err := c.EachDMConversationPage(context.Background(), "123-456", func(p *DMEventsResponse) error {
		for _, e := range p.Data {
			ids = append(ids, e.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Errorf("ids = %v", ids)
	}
}

func TestEachDMConversationPage_EmptyID_Rejects(t *testing.T) {
	c := NewClient(context.Background(), nil)
	err := c.EachDMConversationPage(context.Background(), "", func(_ *DMEventsResponse) error { return nil })
	if err == nil {
		t.Fatal("want error")
	}
}

// =============================================================================
// GetDMWithUser (/2/dm_conversations/with/:participant_id/dm_events)
// =============================================================================

func TestGetDMWithUser_HitsCorrectEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	if _, err := c.GetDMWithUser(context.Background(), "789"); err != nil {
		t.Fatalf("GetDMWithUser: %v", err)
	}
	if gotPath != "/2/dm_conversations/with/789/dm_events" {
		t.Errorf("path = %q, want /2/dm_conversations/with/789/dm_events", gotPath)
	}
}

func TestGetDMWithUser_EmptyID_Rejects(t *testing.T) {
	c := NewClient(context.Background(), nil)
	_, err := c.GetDMWithUser(context.Background(), "")
	if err == nil {
		t.Fatal("want error")
	}
}

func TestEachDMWithUserPage_MultiPage(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		page++
		w.WriteHeader(http.StatusOK)
		if page == 1 {
			_, _ = w.Write([]byte(`{"data":[{"id":"x"}],"meta":{"next_token":"t2"}}`))
		} else {
			_, _ = w.Write([]byte(`{"data":[{"id":"y"}],"meta":{}}`))
		}
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	var ids []string
	err := c.EachDMWithUserPage(context.Background(), "789", func(p *DMEventsResponse) error {
		for _, e := range p.Data {
			ids = append(ids, e.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Each: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("ids = %v", ids)
	}
}

func TestEachDMWithUserPage_EmptyID_Rejects(t *testing.T) {
	c := NewClient(context.Background(), nil)
	err := c.EachDMWithUserPage(context.Background(), "", func(_ *DMEventsResponse) error { return nil })
	if err == nil {
		t.Fatal("want error")
	}
}

// =============================================================================
// Includes.Media DTO のテスト (M35 D-5)
// =============================================================================

func TestIncludesMedia_DecodesMediaArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data":[{"id":"1","event_type":"MessageCreate","text":"img"}],
			"includes":{"media":[{"media_key":"mk1","type":"photo","url":"https://example.com/x.jpg","width":640,"height":480,"alt_text":"alt"}]},
			"meta":{"result_count":1}
		}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	resp, err := c.GetDMEvents(context.Background())
	if err != nil {
		t.Fatalf("GetDMEvents: %v", err)
	}
	if len(resp.Includes.Media) != 1 {
		t.Fatalf("Media len = %d, want 1", len(resp.Includes.Media))
	}
	m := resp.Includes.Media[0]
	if m.MediaKey != "mk1" || m.Type != "photo" || m.URL != "https://example.com/x.jpg" ||
		m.Width != 640 || m.Height != 480 || m.AltText != "alt" {
		t.Errorf("media = %+v", m)
	}
}
