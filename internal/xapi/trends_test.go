package xapi

// trends_test.go は M34 で追加された Trends API ラッパーのテスト。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGetTrends_HitsCorrectEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"trend_name":"#golang","tweet_count":1234}]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	resp, err := c.GetTrends(context.Background(), 1118370)
	if err != nil {
		t.Fatalf("GetTrends: %v", err)
	}
	if gotPath != "/2/trends/by/woeid/1118370" {
		t.Errorf("path = %q", gotPath)
	}
	if len(resp.Data) != 1 || resp.Data[0].TrendName != "#golang" || resp.Data[0].TweetCount != 1234 {
		t.Errorf("Data = %+v", resp.Data)
	}
}

func TestGetTrends_MaxTrendsReflected(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetTrends(context.Background(), 1, WithTrendWoeidMaxTrends(10))
	if err != nil {
		t.Fatalf("GetTrends: %v", err)
	}
	// クエリ名が max_trends であることを pin (max_results ではない)
	if !strings.Contains(gotQuery, "max_trends=10") {
		t.Errorf("query %q missing max_trends=10 (param name pin)", gotQuery)
	}
	if strings.Contains(gotQuery, "max_results=") {
		t.Errorf("query %q contains max_results, but X API uses max_trends", gotQuery)
	}
}

func TestGetTrends_TrendFieldsReflected(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetTrends(context.Background(), 1,
		WithTrendWoeidTrendFields("trend_name", "tweet_count"))
	if err != nil {
		t.Fatalf("GetTrends: %v", err)
	}
	if !strings.Contains(gotQuery, "trend.fields=trend_name%2Ctweet_count") {
		t.Errorf("query %q missing trend.fields=trend_name,tweet_count", gotQuery)
	}
}

func TestGetTrends_InvalidWoeid_Rejects(t *testing.T) {
	c := NewClient(context.Background(), nil)
	for _, w := range []int{0, -1, -1118370} {
		_, err := c.GetTrends(context.Background(), w)
		if err == nil {
			t.Errorf("woeid=%d: want error", w)
			continue
		}
		if !strings.Contains(err.Error(), "must be positive") {
			t.Errorf("woeid=%d: err = %v", w, err)
		}
	}
}

func TestGetTrends_404_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"Not Found Error"}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetTrends(context.Background(), 1)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetTrends_InvalidJSON_NoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetTrends(context.Background(), 1)
	if err == nil {
		t.Fatal("want error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry)", got)
	}
}

func TestGetPersonalizedTrends_HitsCorrectEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"trend_name":"#golang","category":"Technology","post_count":42,"trending_since":"2026-05-15T00:00:00.000Z"}]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	resp, err := c.GetPersonalizedTrends(context.Background())
	if err != nil {
		t.Fatalf("GetPersonalizedTrends: %v", err)
	}
	if gotPath != "/2/users/personalized_trends" {
		t.Errorf("path = %q", gotPath)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("Data len = %d", len(resp.Data))
	}
	d := resp.Data[0]
	if d.TrendName != "#golang" || d.Category != "Technology" || d.PostCount != 42 {
		t.Errorf("Data[0] = %+v", d)
	}
}

func TestGetPersonalizedTrends_PersonalizedTrendFieldsReflected(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(t, srv)
	_, err := c.GetPersonalizedTrends(context.Background(),
		WithTrendPersonalFields("trend_name", "category", "post_count", "trending_since"))
	if err != nil {
		t.Fatalf("GetPersonalizedTrends: %v", err)
	}
	// クエリ名が personalized_trend.fields であることを pin (trend.fields ではない)
	if !strings.Contains(gotQuery, "personalized_trend.fields=trend_name%2Ccategory%2Cpost_count%2Ctrending_since") {
		t.Errorf("query %q missing personalized_trend.fields=... (param name pin)", gotQuery)
	}
	if strings.Contains(gotQuery, "trend.fields=") && !strings.Contains(gotQuery, "personalized_trend.fields=") {
		t.Errorf("query %q contains trend.fields but should use personalized_trend.fields", gotQuery)
	}
}
