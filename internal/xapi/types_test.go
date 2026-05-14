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

// TestTweet_Unmarshal_WithExtendedFields は M8 で追加した拡張フィールド
// (entities / public_metrics / referenced_tweets) が Tweet にデコードされることを確認する。
func TestTweet_Unmarshal_WithExtendedFields(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id":"1",
		"text":"hello #golang @alice https://t.co/abc",
		"author_id":"42",
		"created_at":"2026-05-12T12:00:00.000Z",
		"entities":{
			"urls":[{"start":21,"end":44,"url":"https://t.co/abc","expanded_url":"https://example.com/article","display_url":"example.com/article"}],
			"hashtags":[{"start":6,"end":13,"tag":"golang"}],
			"mentions":[{"start":14,"end":20,"username":"alice","id":"42"}],
			"annotations":[{"start":6,"end":12,"probability":0.95,"type":"Other","normalized_text":"Go"}]
		},
		"public_metrics":{
			"retweet_count":3,
			"reply_count":4,
			"like_count":100,
			"quote_count":2,
			"bookmark_count":7,
			"impression_count":1234
		},
		"referenced_tweets":[
			{"type":"retweeted","id":"999"}
		]
	}`)
	var tw Tweet
	if err := json.Unmarshal(raw, &tw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Entities
	if tw.Entities == nil {
		t.Fatal("Entities = nil, want non-nil")
	}
	if got := len(tw.Entities.URLs); got != 1 {
		t.Fatalf("Entities.URLs len = %d, want 1", got)
	}
	if u := tw.Entities.URLs[0]; u.URL != "https://t.co/abc" || u.ExpandedURL != "https://example.com/article" {
		t.Errorf("URLs[0] = %+v", u)
	}
	if got := len(tw.Entities.Hashtags); got != 1 || tw.Entities.Hashtags[0].Tag != "golang" {
		t.Errorf("Hashtags = %+v", tw.Entities.Hashtags)
	}
	if got := len(tw.Entities.Mentions); got != 1 || tw.Entities.Mentions[0].Username != "alice" {
		t.Errorf("Mentions = %+v", tw.Entities.Mentions)
	}
	if got := len(tw.Entities.Annotations); got != 1 || tw.Entities.Annotations[0].NormalizedText != "Go" {
		t.Errorf("Annotations = %+v", tw.Entities.Annotations)
	}

	// PublicMetrics
	if tw.PublicMetrics == nil {
		t.Fatal("PublicMetrics = nil")
	}
	pm := tw.PublicMetrics
	if pm.RetweetCount != 3 || pm.ReplyCount != 4 || pm.LikeCount != 100 || pm.QuoteCount != 2 {
		t.Errorf("PublicMetrics core counts = %+v", pm)
	}
	if pm.BookmarkCount != 7 || pm.ImpressionCount != 1234 {
		t.Errorf("PublicMetrics extra counts = %+v", pm)
	}

	// ReferencedTweets
	if got := len(tw.ReferencedTweets); got != 1 {
		t.Fatalf("ReferencedTweets len = %d, want 1", got)
	}
	if r := tw.ReferencedTweets[0]; r.Type != "retweeted" || r.ID != "999" {
		t.Errorf("ReferencedTweets[0] = %+v", r)
	}
}

// TestTweet_Unmarshal_Minimal_DoesNotPopulateExtended は M7 の最小レスポンスでも
// M8 拡張フィールドが nil/空のまま (omitempty で互換性維持) であることを確認する。
func TestTweet_Unmarshal_Minimal_DoesNotPopulateExtended(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"id":"1","text":"hello"}`)
	var tw Tweet
	if err := json.Unmarshal(raw, &tw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if tw.Entities != nil {
		t.Errorf("Entities = %+v, want nil", tw.Entities)
	}
	if tw.PublicMetrics != nil {
		t.Errorf("PublicMetrics = %+v, want nil", tw.PublicMetrics)
	}
	if tw.ReferencedTweets != nil {
		t.Errorf("ReferencedTweets = %+v, want nil", tw.ReferencedTweets)
	}
}

// TestIncludes_Unmarshal_WithTweets は M8 で追加した Includes.Tweets が
// referenced_tweets.id expansion レスポンスをデコードできることを確認する。
func TestIncludes_Unmarshal_WithTweets(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"users":[{"id":"42","username":"alice","name":"Alice"}],
		"tweets":[{"id":"999","text":"original tweet","author_id":"7"}]
	}`)
	var inc Includes
	if err := json.Unmarshal(raw, &inc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(inc.Tweets) != 1 {
		t.Fatalf("Tweets len = %d, want 1", len(inc.Tweets))
	}
	if got := inc.Tweets[0]; got.ID != "999" || got.Text != "original tweet" || got.AuthorID != "7" {
		t.Errorf("Tweets[0] = %+v", got)
	}
}

// TestTweet_Unmarshal_WithNoteTweet は M29 で追加した note_tweet フィールド
// (text のみ) を Tweet.NoteTweet にデコードできることを確認する。
func TestTweet_Unmarshal_WithNoteTweet(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id":"1",
		"text":"truncated…",
		"note_tweet":{"text":"This is the full long-form note tweet body."}
	}`)
	var tw Tweet
	if err := json.Unmarshal(raw, &tw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if tw.NoteTweet == nil {
		t.Fatal("NoteTweet = nil, want non-nil")
	}
	if tw.NoteTweet.Text != "This is the full long-form note tweet body." {
		t.Errorf("NoteTweet.Text = %q", tw.NoteTweet.Text)
	}
	if tw.NoteTweet.Entities != nil {
		t.Errorf("NoteTweet.Entities = %+v, want nil (omitted)", tw.NoteTweet.Entities)
	}
}

// TestTweet_Unmarshal_WithNoteTweetEntities は note_tweet.entities が
// 既存 TweetEntities 型 (D-6) として読めることを確認する。
func TestTweet_Unmarshal_WithNoteTweetEntities(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id":"1",
		"text":"truncated…",
		"note_tweet":{
			"text":"Long body with #tag and https://t.co/xyz",
			"entities":{
				"urls":[{"start":24,"end":40,"url":"https://t.co/xyz","expanded_url":"https://example.com"}],
				"hashtags":[{"start":15,"end":19,"tag":"tag"}],
				"mentions":[{"start":0,"end":0,"username":"bob"}]
			}
		}
	}`)
	var tw Tweet
	if err := json.Unmarshal(raw, &tw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if tw.NoteTweet == nil || tw.NoteTweet.Entities == nil {
		t.Fatal("NoteTweet.Entities = nil")
	}
	if len(tw.NoteTweet.Entities.URLs) != 1 || tw.NoteTweet.Entities.URLs[0].ExpandedURL != "https://example.com" {
		t.Errorf("NoteTweet.Entities.URLs = %+v", tw.NoteTweet.Entities.URLs)
	}
	if len(tw.NoteTweet.Entities.Hashtags) != 1 || tw.NoteTweet.Entities.Hashtags[0].Tag != "tag" {
		t.Errorf("NoteTweet.Entities.Hashtags = %+v", tw.NoteTweet.Entities.Hashtags)
	}
	if len(tw.NoteTweet.Entities.Mentions) != 1 || tw.NoteTweet.Entities.Mentions[0].Username != "bob" {
		t.Errorf("NoteTweet.Entities.Mentions = %+v", tw.NoteTweet.Entities.Mentions)
	}
}

// TestTweet_Unmarshal_WithoutNoteTweet は note_tweet が無いレスポンスで
// Tweet.NoteTweet が nil のままになることを確認する (omitempty)。
func TestTweet_Unmarshal_WithoutNoteTweet(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"id":"1","text":"hello"}`)
	var tw Tweet
	if err := json.Unmarshal(raw, &tw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if tw.NoteTweet != nil {
		t.Errorf("NoteTweet = %+v, want nil", tw.NoteTweet)
	}
}

// TestTweet_Unmarshal_WithConversationID は conversation_id が
// Tweet.ConversationID に読み込まれることを確認する。
func TestTweet_Unmarshal_WithConversationID(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"id":"100","text":"reply","conversation_id":"42"}`)
	var tw Tweet
	if err := json.Unmarshal(raw, &tw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if tw.ConversationID != "42" {
		t.Errorf("ConversationID = %q, want %q", tw.ConversationID, "42")
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
