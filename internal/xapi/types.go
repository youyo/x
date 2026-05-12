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
// M7 で最小フィールド (id / text / author_id / created_at) を導入し、
// M8 (likes.go) で entities / public_metrics / referenced_tweets を追加した。
//
// CreatedAt は X API が返す ISO 8601 文字列 ("2026-05-12T12:00:00.000Z" 形式) を
// そのまま保持する。time.Time への型化は CLI/MCP 層 (M11/M18) で必要に応じて行う。
type Tweet struct {
	// ID は X が払い出すツイートの数値 ID (文字列表現)。
	ID string `json:"id"`
	// Text はツイート本文 (URL 短縮あり、改行込み)。
	Text string `json:"text"`
	// AuthorID は投稿者の User.ID。`tweet.fields=author_id` または既定で付与される。
	AuthorID string `json:"author_id,omitempty"`
	// CreatedAt は投稿日時の ISO 8601 文字列。`tweet.fields=created_at` が必要。
	CreatedAt string `json:"created_at,omitempty"`
	// Entities は本文中の URL / Hashtag / Mention / Annotation の抽出結果。
	// `tweet.fields=entities` が必要。指定無し時は nil。
	Entities *TweetEntities `json:"entities,omitempty"`
	// PublicMetrics はリツイート数等の公開メトリクス。
	// `tweet.fields=public_metrics` が必要。指定無し時は nil。
	// User 側の `public_metrics` (UserPublicMetrics) とは項目が異なるため別型としている。
	PublicMetrics *TweetPublicMetrics `json:"public_metrics,omitempty"`
	// ReferencedTweets はリツイート / 引用 / リプライ先の関連ツイート ID。
	// `tweet.fields=referenced_tweets` が必要。空配列または nil。
	// 各要素の Type は "retweeted" / "quoted" / "replied_to" のいずれか。
	// 関連ツイート本体を取得するには `expansions=referenced_tweets.id` を併用する
	// (Includes.Tweets に詰められる)。
	ReferencedTweets []ReferencedTweet `json:"referenced_tweets,omitempty"`
}

// TweetPublicMetrics はツイートの公開メトリクスを表す DTO である。
//
// `tweet.fields=public_metrics` 指定時にレスポンスに含まれる。
// User 側の `public_metrics` (UserPublicMetrics) とは項目が完全に異なるため別型としている。
//
// BookmarkCount / ImpressionCount はオーナー特権メトリクスであり、
// OAuth 1.0a の認証ユーザー自身のツイートのみ返却される
// (他人のツイートをレスポンス内に取得した場合は 0 になることがある)。
type TweetPublicMetrics struct {
	// RetweetCount はリツイート数。
	RetweetCount int `json:"retweet_count"`
	// ReplyCount はリプライ数。
	ReplyCount int `json:"reply_count"`
	// LikeCount は Like 数。
	LikeCount int `json:"like_count"`
	// QuoteCount は引用ツイート数。
	QuoteCount int `json:"quote_count"`
	// BookmarkCount はブックマーク数 (オーナー特権メトリクス)。
	BookmarkCount int `json:"bookmark_count,omitempty"`
	// ImpressionCount はインプレッション数 (オーナー特権メトリクス)。
	ImpressionCount int `json:"impression_count,omitempty"`
}

// TweetEntities はツイート本文内のエンティティ抽出結果を表す DTO である。
//
// `tweet.fields=entities` 指定時にレスポンスに含まれる。
// 各 slice は要素 0 の場合 nil/空のまま (omitempty で省略される)。
type TweetEntities struct {
	// URLs は本文中の URL エンティティ。
	URLs []TweetURL `json:"urls,omitempty"`
	// Hashtags は本文中のハッシュタグエンティティ。
	Hashtags []TweetTag `json:"hashtags,omitempty"`
	// Mentions は本文中の @メンションエンティティ。
	Mentions []TweetMention `json:"mentions,omitempty"`
	// Annotations は X が自動付与する意味エンティティ (固有名詞抽出等)。
	Annotations []TweetAnnotation `json:"annotations,omitempty"`
}

// TweetURL は本文中の URL エンティティを表す DTO である。
type TweetURL struct {
	// Start は本文中の開始オフセット (UTF-16 code unit)。
	Start int `json:"start"`
	// End は本文中の終了オフセット (UTF-16 code unit, exclusive)。
	End int `json:"end"`
	// URL は本文中の短縮 URL (t.co)。
	URL string `json:"url"`
	// ExpandedURL は短縮を展開した実 URL。
	ExpandedURL string `json:"expanded_url,omitempty"`
	// DisplayURL はユーザーに表示される短縮表示 URL。
	DisplayURL string `json:"-"`
}

// TweetTag は本文中のハッシュタグエンティティを表す DTO である。
type TweetTag struct {
	// Start は本文中の開始オフセット (UTF-16 code unit)。
	Start int `json:"start"`
	// End は本文中の終了オフセット (UTF-16 code unit, exclusive)。
	End int `json:"end"`
	// Tag は `#` を除いたタグ文字列 (例: "golang")。
	Tag string `json:"tag"`
}

// TweetMention は本文中の @メンションエンティティを表す DTO である。
type TweetMention struct {
	// Start は本文中の開始オフセット (UTF-16 code unit)。
	Start int `json:"start"`
	// End は本文中の終了オフセット (UTF-16 code unit, exclusive)。
	End int `json:"end"`
	// Username は `@` を除いたスクリーンネーム (例: "alice")。
	Username string `json:"username"`
	// ID はメンション先ユーザーの User.ID (X API が解決した場合のみ)。
	ID string `json:"id,omitempty"`
}

// TweetAnnotation は本文中の意味エンティティ (X 自動抽出) を表す DTO である。
type TweetAnnotation struct {
	// Start は本文中の開始オフセット (UTF-16 code unit)。
	Start int `json:"start"`
	// End は本文中の終了オフセット (UTF-16 code unit, exclusive)。
	End int `json:"end"`
	// Probability はエンティティ抽出の確度 (0.0 - 1.0)。
	Probability float64 `json:"probability,omitempty"`
	// Type はエンティティタイプ ("Person", "Place", "Organization", "Product", "Other" など)。
	Type string `json:"type,omitempty"`
	// NormalizedText は正規化済みのテキスト (表記揺れを統一したもの)。
	NormalizedText string `json:"normalized_text,omitempty"`
}

// ReferencedTweet は関連ツイートへの参照を表す DTO である。
//
// Tweet.ReferencedTweets の要素として用いる。Type には次のいずれかが入る:
//   - "retweeted"   リツイート元
//   - "quoted"      引用元
//   - "replied_to"  リプライ先
type ReferencedTweet struct {
	// Type は参照種別 ("retweeted" / "quoted" / "replied_to")。
	Type string `json:"type"`
	// ID は参照先ツイートの Tweet.ID。
	ID string `json:"id"`
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
// `expansions=author_id` 指定時に `users` 配列が、
// `expansions=referenced_tweets.id` 指定時に `tweets` 配列が含まれる。
type Includes struct {
	// Users は expansion で取得された関連ユーザー。
	Users []User `json:"users,omitempty"`
	// Tweets は expansion で取得された関連ツイート (referenced_tweets.id 等)。
	Tweets []Tweet `json:"tweets,omitempty"`
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
