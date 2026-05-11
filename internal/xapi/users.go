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

// UserFieldsOption は GetUserMe のクエリパラメータを設定する関数オプションである。
//
// 同じキーで複数回呼ばれた場合は最後の呼び出しが勝つ (url.Values.Set 挙動)。
//
// 将来的 (M8 likes) には RequestOption へ統合し本型はエイリアスとして残す予定。
type UserFieldsOption func(*url.Values)

// WithUserFields は X API v2 の `user.fields` クエリパラメータを設定する。
//
// 例: `WithUserFields("username", "name", "verified", "public_metrics")` は
// URL に `?user.fields=username,name,verified,public_metrics` を付与する。
//
// 空引数で呼ばれた場合はクエリパラメータを変更しない (no-op)。
// 既に同名のフィールドが設定されている場合は上書きする (last-wins)。
//
// 利用可能なフィールド名は X API v2 ドキュメントを参照すること。
// 代表例: id / username / name / verified / description / public_metrics /
// created_at / profile_image_url / protected。
func WithUserFields(fields ...string) UserFieldsOption {
	return func(v *url.Values) {
		if len(fields) == 0 {
			return
		}
		v.Set("user.fields", strings.Join(fields, ","))
	}
}

// GetUserMe は X API v2 `GET /2/users/me` を呼び出し、認証ユーザーの User 情報を返す。
//
// 認証は NewClient 時に渡した *config.Credentials の OAuth 1.0a 署名で行われる。
// opts でクエリパラメータをカスタマイズできる (例: WithUserFields)。
//
// エラーの分類は M6 Client.Do と同じ:
//   - errors.Is(err, ErrAuthentication) → 401
//   - errors.Is(err, ErrPermission)     → 403
//   - errors.Is(err, ErrNotFound)       → 404
//   - errors.Is(err, ErrRateLimit)      → 429 リトライ枯渇
//   - errors.As(err, &apiErr)           → APIError から Body/Header/StatusCode 取得
//   - errors.Is(err, context.Canceled)  → context cancel
//
// レスポンスの JSON 形式: `{"data": {"id": "...", "username": "...", ...}}`。
// `data` フィールドが文字列等の型不一致だった場合は decode エラー
// (xapi: decode GetUserMe response) を返し、リトライはしない。
func (c *Client) GetUserMe(ctx context.Context, opts ...UserFieldsOption) (*User, error) {
	values := url.Values{}
	for _, opt := range opts {
		opt(&values)
	}
	endpoint := c.BaseURL() + "/2/users/me"
	if q := values.Encode(); q != "" {
		endpoint += "?" + q
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xapi: build GetUserMe request: %w", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xapi: read GetUserMe response: %w", err)
	}
	var env struct {
		Data User `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("xapi: decode GetUserMe response: %w", err)
	}
	return &env.Data, nil
}
