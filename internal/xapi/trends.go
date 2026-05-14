package xapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// trends.go は X API v2 の Trends 系 2 エンドポイントをラップする (M34)。
//
//   - GET /2/trends/by/woeid/:id          → GetTrends (max_trends 1..50, trend.fields)
//   - GET /2/users/personalized_trends    → GetPersonalizedTrends (personalized_trend.fields のみ)
//
// 設計判断 (詳細は plans/x-m34-spaces-trends.md):
//   - Option 型を **2 種類に分離** (TrendWoeidOption / TrendPersonalOption) — M34 D-4
//     2 endpoint でパラメータ名・上限・fields 値が異なるため型レベルで誤用を防ぐ。
//   - Trend DTO は両 endpoint の union 構造体 — M34 D-3
//     woeid: trend_name / tweet_count、personal: trend_name / category / post_count / trending_since。
//   - パッケージ doc は既存 oauth1.go に集約 — M34 D-9

// =============================================================================
// DTOs
// =============================================================================

// Trend は X API v2 の Trends endpoint が返すトレンドオブジェクトを表す DTO である (M34)。
//
// woeid endpoint は `trend_name` / `tweet_count` を返す。
// personalized_trends endpoint は `trend_name` / `category` / `post_count` / `trending_since` を返す。
//
// 両者の union として 1 つの構造体に集約。各 endpoint が返さないフィールドは
// omitempty により Marshal 時に省略される (M34 D-3)。
type Trend struct {
	TrendName     string `json:"trend_name,omitempty"`
	TweetCount    int    `json:"tweet_count,omitempty"`
	Category      string `json:"category,omitempty"`
	PostCount     int    `json:"post_count,omitempty"`
	TrendingSince string `json:"trending_since,omitempty"`
}

// TrendsResponse は GetTrends / GetPersonalizedTrends の配列レスポンス本体である (M34)。
type TrendsResponse struct {
	Data []Trend `json:"data,omitempty"`
}

// =============================================================================
// Option types — 2 種類に分離 (M34 D-4)
// =============================================================================

// TrendWoeidOption は GetTrends 用の関数オプションである。
// X API は本 endpoint で `max_trends` (≠ max_results) と `trend.fields` を受け付ける (M34 D-18)。
type TrendWoeidOption func(*trendWoeidConfig)

type trendWoeidConfig struct {
	maxTrends   int // 0 は no-op (X API デフォルト 20 に任せる)
	trendFields []string
}

// WithTrendWoeidMaxTrends は X API の `max_trends` を設定する (1..50, default 20)。
// 0 は no-op。CLI 層で範囲チェックを担う。
func WithTrendWoeidMaxTrends(n int) TrendWoeidOption {
	return func(c *trendWoeidConfig) { c.maxTrends = n }
}

// WithTrendWoeidTrendFields は X API の `trend.fields` を設定する。空引数は no-op。
// 有効値: `trend_name`, `tweet_count`。
func WithTrendWoeidTrendFields(fields ...string) TrendWoeidOption {
	return func(c *trendWoeidConfig) {
		if len(fields) == 0 {
			return
		}
		c.trendFields = append([]string(nil), fields...)
	}
}

// TrendPersonalOption は GetPersonalizedTrends 用の関数オプションである。
// X API は本 endpoint で `personalized_trend.fields` のみ受け付ける (M34 D-19)。
type TrendPersonalOption func(*trendPersonalConfig)

type trendPersonalConfig struct {
	personalizedTrendFields []string
}

// WithTrendPersonalFields は X API の `personalized_trend.fields` を設定する。空引数は no-op。
// 有効値: `category`, `post_count`, `trend_name`, `trending_since`。
func WithTrendPersonalFields(fields ...string) TrendPersonalOption {
	return func(c *trendPersonalConfig) {
		if len(fields) == 0 {
			return
		}
		c.personalizedTrendFields = append([]string(nil), fields...)
	}
}

// =============================================================================
// GetTrends / GetPersonalizedTrends
// =============================================================================

// GetTrends は X API v2 `GET /2/trends/by/woeid/:woeid` を呼び出す (M34)。
//
// woeid は Where On Earth ID (Yahoo WOEID 体系): 東京=1118370 / 日本=23424856 / 全世界=1。
// woeid <= 0 は ErrInvalidArgument ではなく通常 error を返す (CLI 層が wrap する)。
func (c *Client) GetTrends(ctx context.Context, woeid int, opts ...TrendWoeidOption) (*TrendsResponse, error) {
	if woeid <= 0 {
		return nil, fmt.Errorf("xapi: GetTrends: woeid must be positive, got %d", woeid)
	}
	cfg := newTrendWoeidConfig(opts)
	endpoint := buildTrendWoeidURL(c.BaseURL(), woeid, &cfg)
	body, err := c.fetchJSON(ctx, endpoint, "GetTrends")
	if err != nil {
		return nil, err
	}
	out := &TrendsResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetTrends response: %w", err)
	}
	return out, nil
}

// GetPersonalizedTrends は X API v2 `GET /2/users/personalized_trends` を呼び出す (M34)。
//
// 認証ユーザー固定 (X API 仕様で user_id 引数を受け付けない、M34 D-17)。
func (c *Client) GetPersonalizedTrends(ctx context.Context, opts ...TrendPersonalOption) (*TrendsResponse, error) {
	cfg := newTrendPersonalConfig(opts)
	endpoint := buildTrendPersonalURL(c.BaseURL(), &cfg)
	body, err := c.fetchJSON(ctx, endpoint, "GetPersonalizedTrends")
	if err != nil {
		return nil, err
	}
	out := &TrendsResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("xapi: decode GetPersonalizedTrends response: %w", err)
	}
	return out, nil
}

// =============================================================================
// URL builders
// =============================================================================

// buildTrendWoeidURL は GetTrends の完全 URL を組み立てる。
func buildTrendWoeidURL(baseURL string, woeid int, cfg *trendWoeidConfig) string {
	path := "/2/trends/by/woeid/" + strconv.Itoa(woeid)
	values := url.Values{}
	if cfg.maxTrends > 0 {
		values.Set("max_trends", strconv.Itoa(cfg.maxTrends))
	}
	if len(cfg.trendFields) > 0 {
		values.Set("trend.fields", strings.Join(cfg.trendFields, ","))
	}
	if q := values.Encode(); q != "" {
		return baseURL + path + "?" + q
	}
	return baseURL + path
}

// buildTrendPersonalURL は GetPersonalizedTrends の完全 URL を組み立てる。
func buildTrendPersonalURL(baseURL string, cfg *trendPersonalConfig) string {
	path := "/2/users/personalized_trends"
	values := url.Values{}
	if len(cfg.personalizedTrendFields) > 0 {
		values.Set("personalized_trend.fields", strings.Join(cfg.personalizedTrendFields, ","))
	}
	if q := values.Encode(); q != "" {
		return baseURL + path + "?" + q
	}
	return baseURL + path
}

// =============================================================================
// config builders
// =============================================================================

func newTrendWoeidConfig(opts []TrendWoeidOption) trendWoeidConfig {
	cfg := trendWoeidConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

func newTrendPersonalConfig(opts []TrendPersonalOption) trendPersonalConfig {
	cfg := trendPersonalConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}
