package xapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// tweetLookupConfig は GetTweet / GetTweets のオプション集約構造体である。
type tweetLookupConfig struct {
	tweetFields []string
	expansions  []string
	userFields  []string
	mediaFields []string
}

// TweetLookupOption は GetTweet / GetTweets の挙動を変更する関数オプションである。
// 同じキーで複数回呼ばれた場合は最後の呼び出しが勝つ (last-wins)。
type TweetLookupOption func(*tweetLookupConfig)

// WithGetTweetFields は X API の `tweet.fields` クエリパラメータを設定する。
// 空引数は no-op。
func WithGetTweetFields(fields ...string) TweetLookupOption {
	return func(c *tweetLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.tweetFields = append([]string(nil), fields...)
	}
}

// WithGetTweetExpansions は X API の `expansions` クエリパラメータを設定する。
// 空引数は no-op。
func WithGetTweetExpansions(exp ...string) TweetLookupOption {
	return func(c *tweetLookupConfig) {
		if len(exp) == 0 {
			return
		}
		c.expansions = append([]string(nil), exp...)
	}
}

// WithGetTweetUserFields は X API の `user.fields` クエリパラメータを設定する。
// 空引数は no-op。
func WithGetTweetUserFields(fields ...string) TweetLookupOption {
	return func(c *tweetLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.userFields = append([]string(nil), fields...)
	}
}

// WithGetTweetMediaFields は X API の `media.fields` クエリパラメータを設定する。
// 空引数は no-op。
func WithGetTweetMediaFields(fields ...string) TweetLookupOption {
	return func(c *tweetLookupConfig) {
		if len(fields) == 0 {
			return
		}
		c.mediaFields = append([]string(nil), fields...)
	}
}

// TweetResponse は GetTweet が返す単一ツイートレスポンス本体である。
//
// X API v2 `GET /2/tweets/:id` のレスポンス `{ data, includes }` を構造化する。
type TweetResponse struct {
	// Data は取得したツイート本体。X API がツイートを返さなかった場合は nil。
	Data *Tweet `json:"data,omitempty"`
	// Includes は expansions で取得された関連リソース (users / tweets)。
	Includes Includes `json:"includes,omitempty"`
}

// TweetsResponse は GetTweets が返すバッチ取得レスポンス本体である。
//
// X API v2 `GET /2/tweets?ids=...` のレスポンス `{ data, includes, errors }` を構造化する。
// `errors` は一部 ID が見つからない等の partial error が入る (D-9, TweetLookupError 参照)。
type TweetsResponse struct {
	// Data は取得できたツイートの配列 (0 件以上)。
	Data []Tweet `json:"data,omitempty"`
	// Includes は expansions で取得された関連リソース (users / tweets)。
	Includes Includes `json:"includes,omitempty"`
	// Errors はバッチ取得時の partial error (一部 ID が見つからなかった等)。
	// HTTP ステータスは 200 OK のまま、応答 JSON にこれが入ることがある。
	Errors []TweetLookupError `json:"errors,omitempty"`
}

// TweetLookupError は GetTweets の partial error を表す DTO である (M29 D-9)。
//
// X API v2 のバッチ partial error スキーマは既存 ErrorResponse / APIErrorPayload と
// フィールドが異なるため別型を用意する。
//
// 代表例 (`Could not find tweet with ids: [<id>].`):
//
//	{
//	  "value":         "<id>",
//	  "detail":        "Could not find tweet with ids: [<id>].",
//	  "title":         "Not Found Error",
//	  "resource_type": "tweet",
//	  "parameter":     "ids",
//	  "resource_id":   "<id>",
//	  "type":          "https://api.twitter.com/2/problems/resource-not-found"
//	}
type TweetLookupError struct {
	Value        string `json:"value,omitempty"`
	Detail       string `json:"detail,omitempty"`
	Title        string `json:"title,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	Parameter    string `json:"parameter,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	Type         string `json:"type,omitempty"`
}

// GetTweet は X API v2 `GET /2/tweets/:id` を呼び出し、単一ツイートを返す。
//
// 認証は NewClient 時に渡した *config.Credentials の OAuth 1.0a 署名で行われる。
// opts で `tweet.fields` / `expansions` / `user.fields` / `media.fields` をカスタマイズ可能。
//
// エラーの分類は M6 Client.Do と同じ:
//   - errors.Is(err, ErrAuthentication) → 401
//   - errors.Is(err, ErrPermission)     → 403
//   - errors.Is(err, ErrNotFound)       → 404
//   - errors.Is(err, ErrRateLimit)      → 429 リトライ枯渇
//
// レスポンスの JSON 形式: `{"data": {...}, "includes": {...}}`。
// `data` フィールドが配列等の型不一致だった場合は decode エラーを返し、リトライしない。
func (c *Client) GetTweet(
	ctx context.Context,
	tweetID string,
	opts ...TweetLookupOption,
) (*TweetResponse, error) {
	cfg := applyTweetLookupOpts(opts)
	endpoint := buildGetTweetURL(c.BaseURL(), tweetID, &cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build GetTweet request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read GetTweet response: %w", err)
	}
	out := &TweetResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetTweet response: %w", err)
	}
	return out, nil
}

// GetTweets は X API v2 `GET /2/tweets?ids=ID1,ID2,...` を呼び出し、複数ツイートを返す。
//
// ids は 1〜100 件の範囲で X API に渡される。
//   - 0 件 → "ids must be non-empty" エラー (CLI 層でラップ)
//   - 101 件以上 → "ids must be at most 100" エラー
//
// partial error (一部 ID が見つからない等) は TweetsResponse.Errors に入る。HTTP は 200 OK。
func (c *Client) GetTweets(
	ctx context.Context,
	ids []string,
	opts ...TweetLookupOption,
) (*TweetsResponse, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("xapi: GetTweets: ids must be non-empty")
	}
	if len(ids) > 100 {
		return nil, fmt.Errorf("xapi: GetTweets: ids must be at most 100, got %d", len(ids))
	}
	cfg := applyTweetLookupOpts(opts)
	endpoint := buildGetTweetsURL(c.BaseURL(), ids, &cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build GetTweets request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read GetTweets response: %w", err)
	}
	out := &TweetsResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetTweets response: %w", err)
	}
	return out, nil
}

// applyTweetLookupOpts は opts を tweetLookupConfig に適用して返す。
func applyTweetLookupOpts(opts []TweetLookupOption) tweetLookupConfig {
	cfg := tweetLookupConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// buildGetTweetURL は `GET /2/tweets/:id` の完全 URL を組み立てる (path escape あり)。
func buildGetTweetURL(baseURL, tweetID string, cfg *tweetLookupConfig) string {
	path := "/2/tweets/" + url.PathEscape(tweetID)
	values := tweetLookupValues(cfg)
	if q := values.Encode(); q != "" {
		return baseURL + path + "?" + q
	}
	return baseURL + path
}

// buildGetTweetsURL は `GET /2/tweets?ids=...` の完全 URL を組み立てる。
func buildGetTweetsURL(baseURL string, ids []string, cfg *tweetLookupConfig) string {
	values := tweetLookupValues(cfg)
	values.Set("ids", strings.Join(ids, ","))
	return baseURL + "/2/tweets?" + values.Encode()
}

// tweetLookupValues は cfg の各 fields/expansions を url.Values に詰める。
func tweetLookupValues(cfg *tweetLookupConfig) url.Values {
	values := url.Values{}
	if len(cfg.tweetFields) > 0 {
		values.Set("tweet.fields", strings.Join(cfg.tweetFields, ","))
	}
	if len(cfg.expansions) > 0 {
		values.Set("expansions", strings.Join(cfg.expansions, ","))
	}
	if len(cfg.userFields) > 0 {
		values.Set("user.fields", strings.Join(cfg.userFields, ","))
	}
	if len(cfg.mediaFields) > 0 {
		values.Set("media.fields", strings.Join(cfg.mediaFields, ","))
	}
	return values
}
