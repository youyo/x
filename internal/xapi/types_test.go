package xapi

import (
	"encoding/json"
	"testing"
)

// TestUser_Unmarshal_Minimal は X API v2 /2/users/me の最小レスポンス JSON
// (id/username/name のみ) が User 構造体に正しくデコードされることを確認する。
func TestUser_Unmarshal_Minimal(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"id":"42","username":"alice","name":"Alice"}`)
	var u User
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if u.ID != "42" {
		t.Errorf("ID = %q, want %q", u.ID, "42")
	}
	if u.Username != "alice" {
		t.Errorf("Username = %q, want %q", u.Username, "alice")
	}
	if u.Name != "Alice" {
		t.Errorf("Name = %q, want %q", u.Name, "Alice")
	}
	if u.Verified {
		t.Errorf("Verified = true, want false (omitted)")
	}
	if u.Description != "" {
		t.Errorf("Description = %q, want empty", u.Description)
	}
	if u.PublicMetrics != nil {
		t.Errorf("PublicMetrics = %+v, want nil (omitted)", u.PublicMetrics)
	}
}

// TestUser_Unmarshal_WithFields は user.fields=verified,description,public_metrics
// 指定時のオプショナルフィールドが正しくデコードされることを確認する。
func TestUser_Unmarshal_WithFields(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id":"42",
		"username":"alice",
		"name":"Alice",
		"verified":true,
		"description":"hello world",
		"public_metrics":{
			"followers_count":1234,
			"following_count":56,
			"tweet_count":789,
			"listed_count":12
		}
	}`)
	var u User
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !u.Verified {
		t.Errorf("Verified = false, want true")
	}
	if u.Description != "hello world" {
		t.Errorf("Description = %q, want %q", u.Description, "hello world")
	}
	if u.PublicMetrics == nil {
		t.Fatal("PublicMetrics = nil, want non-nil")
	}
	if u.PublicMetrics.FollowersCount != 1234 {
		t.Errorf("FollowersCount = %d, want 1234", u.PublicMetrics.FollowersCount)
	}
	if u.PublicMetrics.FollowingCount != 56 {
		t.Errorf("FollowingCount = %d, want 56", u.PublicMetrics.FollowingCount)
	}
	if u.PublicMetrics.TweetCount != 789 {
		t.Errorf("TweetCount = %d, want 789", u.PublicMetrics.TweetCount)
	}
	if u.PublicMetrics.ListedCount != 12 {
		t.Errorf("ListedCount = %d, want 12", u.PublicMetrics.ListedCount)
	}
}

// TestTweet_Unmarshal_Minimal は X API v2 Tweet オブジェクトの最小フィールドが
// Tweet 構造体にデコードされることを確認する (M8 で拡張する基盤テスト)。
func TestTweet_Unmarshal_Minimal(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"id":"1","text":"hello","author_id":"42","created_at":"2026-05-12T12:00:00.000Z"}`)
	var tw Tweet
	if err := json.Unmarshal(raw, &tw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if tw.ID != "1" {
		t.Errorf("ID = %q, want %q", tw.ID, "1")
	}
	if tw.Text != "hello" {
		t.Errorf("Text = %q, want %q", tw.Text, "hello")
	}
	if tw.AuthorID != "42" {
		t.Errorf("AuthorID = %q, want %q", tw.AuthorID, "42")
	}
	if tw.CreatedAt != "2026-05-12T12:00:00.000Z" {
		t.Errorf("CreatedAt = %q, want %q", tw.CreatedAt, "2026-05-12T12:00:00.000Z")
	}
}

// TestMeta_Unmarshal は Meta DTO の result_count / next_token がデコードされることを確認する。
func TestMeta_Unmarshal(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"result_count":42,"next_token":"7140dibdnow9c7btw3z2qj4hp9pcq8q"}`)
	var m Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m.ResultCount != 42 {
		t.Errorf("ResultCount = %d, want 42", m.ResultCount)
	}
	if m.NextToken != "7140dibdnow9c7btw3z2qj4hp9pcq8q" {
		t.Errorf("NextToken = %q, want %q", m.NextToken, "7140dibdnow9c7btw3z2qj4hp9pcq8q")
	}
}

// TestIncludes_Unmarshal は Includes.Users が X API v2 のレスポンス
// `{"includes":{"users":[...]}}` の `users` 部分を取り込めることを確認する。
func TestIncludes_Unmarshal(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"users":[{"id":"42","username":"alice","name":"Alice"}]}`)
	var inc Includes
	if err := json.Unmarshal(raw, &inc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(inc.Users) != 1 {
		t.Fatalf("Users len = %d, want 1", len(inc.Users))
	}
	if inc.Users[0].ID != "42" {
		t.Errorf("Users[0].ID = %q, want %q", inc.Users[0].ID, "42")
	}
}

// TestErrorResponse_Unmarshal は X API v2 のエラーレスポンス JSON が
// ErrorResponse 構造体にデコードされることを確認する。
func TestErrorResponse_Unmarshal(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"title":"Unauthorized",
		"detail":"Unauthorized",
		"type":"about:blank",
		"status":401,
		"errors":[
			{"message":"missing oauth_token","parameters":{"oauth_token":["required"]}}
		]
	}`)
	var er ErrorResponse
	if err := json.Unmarshal(raw, &er); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if er.Title != "Unauthorized" {
		t.Errorf("Title = %q, want Unauthorized", er.Title)
	}
	if er.Detail != "Unauthorized" {
		t.Errorf("Detail = %q, want Unauthorized", er.Detail)
	}
	if er.Type != "about:blank" {
		t.Errorf("Type = %q, want about:blank", er.Type)
	}
	if er.Status != 401 {
		t.Errorf("Status = %d, want 401", er.Status)
	}
	if len(er.Errors) != 1 {
		t.Fatalf("Errors len = %d, want 1", len(er.Errors))
	}
	if er.Errors[0].Message != "missing oauth_token" {
		t.Errorf("Errors[0].Message = %q, want %q", er.Errors[0].Message, "missing oauth_token")
	}
	if er.Errors[0].Parameters == nil {
		t.Error("Errors[0].Parameters = nil, want non-nil")
	}
}
