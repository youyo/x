package xapi

// User は X API v2 のユーザーオブジェクトを表す DTO である。
//
// `/2/users/me` のレスポンスや `expansions=author_id` で取得される
// `includes.users` 配列の要素として用いられる。
//
// 必須フィールド (id / username / name) は X API がデフォルトで返却する。
// それ以外 (Verified / Description / PublicMetrics) は `user.fields`
// クエリパラメータで明示的に要求した場合のみ返却される。
// レスポンスに含まれないフィールドは `omitempty` により Marshal 時に省略される。
//
// 将来 X API が新フィールドを追加した場合は本構造体を拡張する。
type User struct {
	// ID は X が払い出すユーザーの数値 ID (文字列表現)。
	ID string `json:"id"`
	// Username は @ を除いたスクリーンネーム (例: "alice")。
	Username string `json:"username"`
	// Name は表示名 (X 上のプロフィール名)。
	Name string `json:"name"`
	// Verified は認証済みアカウントかどうか。`user.fields=verified` が必要。
	Verified bool `json:"verified,omitempty"`
	// Description はプロフィール文。`user.fields=description` が必要。
	Description string `json:"description,omitempty"`
	// PublicMetrics はフォロワー数等の公開メトリクス。
	// `user.fields=public_metrics` が必要。指定無し時は nil。
	PublicMetrics *UserPublicMetrics `json:"public_metrics,omitempty"`
}

// UserPublicMetrics はユーザーの公開メトリクスを表す DTO である。
//
// X API v2 の `user.fields=public_metrics` 指定時にレスポンスに含まれる。
// Tweet 側の public_metrics は項目が異なるため別型 TweetPublicMetrics (M8 で追加予定)
// で表現する。
type UserPublicMetrics struct {
	// FollowersCount はフォロワー数。
	FollowersCount int `json:"followers_count"`
	// FollowingCount はフォロー数。
	FollowingCount int `json:"following_count"`
	// TweetCount は投稿数 (Post 数)。
	TweetCount int `json:"tweet_count"`
	// ListedCount は登録されているリスト数。
	ListedCount int `json:"listed_count"`
}

// Tweet は X API v2 のツイート (Post) オブジェクトを表す DTO である。
//
// M7 では最小フィールド (id / text / author_id / created_at) のみ定義する。
// M8 (likes.go) で entities / public_metrics / referenced_tweets 等を拡張する。
//
// CreatedAt は X API が返す ISO 8601 文字列 ("2026-05-12T12:00:00.000Z" 形式) を
// そのまま保持する。time.Time への型化は M8 で検討する。
type Tweet struct {
	// ID は X が払い出すツイートの数値 ID (文字列表現)。
	ID string `json:"id"`
	// Text はツイート本文 (URL 短縮あり、改行込み)。
	Text string `json:"text"`
	// AuthorID は投稿者の User.ID。`tweet.fields=author_id` または既定で付与される。
	AuthorID string `json:"author_id,omitempty"`
	// CreatedAt は投稿日時の ISO 8601 文字列。`tweet.fields=created_at` が必要。
	CreatedAt string `json:"created_at,omitempty"`
}

// Meta は X API v2 リストエンドポイントの `meta` オブジェクトを表す DTO である。
//
// `/2/users/:id/liked_tweets` 等のページネーション付きエンドポイントで使う。
// `/2/users/me` のレスポンスには Meta は含まれない。
type Meta struct {
	// ResultCount は本ページに含まれる要素数。
	ResultCount int `json:"result_count,omitempty"`
	// NextToken は次ページ取得用トークン。空文字列なら最終ページ。
	NextToken string `json:"next_token,omitempty"`
}

// Includes は X API v2 のレスポンス `includes` オブジェクトを表す DTO である。
//
// `expansions=author_id` 指定時に `users` 配列が、`expansions=referenced_tweets.id`
// 指定時に `tweets` 配列が (M8 で追加予定) 含まれる。
type Includes struct {
	// Users は expansion で取得された関連ユーザー。
	Users []User `json:"users,omitempty"`
}

// ErrorResponse は X API v2 がエラー時に返す JSON 本体を表す DTO である。
//
// 利用例: `xapi.APIError.Body` を `json.Unmarshal` してレスポンスの詳細を読み取る。
//
//	var apiErr *xapi.APIError
//	if errors.As(err, &apiErr) {
//	    var er xapi.ErrorResponse
//	    _ = json.Unmarshal(apiErr.Body, &er)
//	    log.Printf("X API error: %s (%s)", er.Title, er.Detail)
//	}
type ErrorResponse struct {
	// Title はエラータイトル (例: "Unauthorized")。
	Title string `json:"title,omitempty"`
	// Detail はエラー詳細。
	Detail string `json:"detail,omitempty"`
	// Type は RFC 7807 problem+json の type URI ("about:blank" 等)。
	Type string `json:"type,omitempty"`
	// Status は HTTP ステータスコード (レスポンスヘッダと一致する)。
	Status int `json:"status,omitempty"`
	// Errors は問題詳細のリスト。
	Errors []APIErrorPayload `json:"errors,omitempty"`
}

// APIErrorPayload は ErrorResponse.Errors の各要素を表す DTO である。
type APIErrorPayload struct {
	// Message は人間可読なエラーメッセージ。
	Message string `json:"message,omitempty"`
	// Parameters は X API がエラー文脈で返す任意の構造化情報。
	// 例: `{ "oauth_token": ["required"] }`。
	Parameters map[string]any `json:"parameters,omitempty"`
}
