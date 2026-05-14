# X API v2 リファレンス (本リポジトリ視点)

> `x` CLI / Remote MCP が利用する X (旧 Twitter) API v2 の認証・エンドポイント・レート制限・料金・エラーレスポンスを集約した開発者向けメモ。
> 数値は執筆時点 (2026-05-12) の公式ドキュメントに基づくため、本番運用前に [docs.x.com](https://docs.x.com/x-api) で最新値を確認すること。

## 1. 概要

X API v2 は X (旧 Twitter) が提供する公式 REST API。本リポジトリでは **OAuth 1.0a User Context** で以下のエンドポイントを利用する。

| エンドポイント | 本リポでの用途 | 導入 |
|---|---|---|
| `GET /2/users/me` | `x me` / MCP `get_user_me` ツール | v0.1.0 |
| `GET /2/users/:id/liked_tweets` | `x liked list` / MCP `get_liked_tweets` ツール | v0.1.0 |
| `GET /2/tweets/:id` | `x tweet get <ID\|URL>` | v0.4.0 (M29) |
| `GET /2/tweets?ids=...` | `x tweet get --ids ID1,ID2,...` (バッチ取得 1..100 件) | v0.4.0 (M29) |
| `GET /2/tweets/:id/liking_users` | `x tweet liking-users` | v0.4.0 (M29) |
| `GET /2/tweets/:id/retweeted_by` | `x tweet retweeted-by` | v0.4.0 (M29) |
| `GET /2/tweets/:id/quote_tweets` | `x tweet quote-tweets` | v0.4.0 (M29) |
| `GET /2/tweets/search/recent` | `x tweet search <query>` / `x tweet thread <ID\|URL>` (Basic tier 以上必須) | v0.5.0 (M30) |
| `GET /2/users/:id/tweets` | `x timeline tweets [<ID>]` | v0.5.0 (M31) |
| `GET /2/users/:id/mentions` | `x timeline mentions [<ID>]` | v0.5.0 (M31) |
| `GET /2/users/:id/timelines/reverse_chronological` | `x timeline home` (OAuth 1.0a 必須、認証ユーザー固定) | v0.5.0 (M31) |

その他のエンドポイント (post / filter stream / search/all / counts など) は対象外。

### 1.1 note_tweet (ロングツイート, 280 字超)

`tweet.fields=note_tweet` を指定すると、ロングツイート (280 字超の投稿) のレスポンス
`data` (または `includes.tweets`) に `note_tweet` オブジェクトが含まれる。スキーマ:

```jsonc
{
  "note_tweet": {
    "text": "<完全な本文 (truncate なし)>",
    "entities": { "urls": [...], "hashtags": [...], "mentions": [...] }
  }
}
```

- 旧 280 字以内のツイートでは `note_tweet` 自体が返らない (CLI 上は `omitempty`)
- M29 以降、`x liked list` および `x tweet get` は既定でこれを取得 (`note_tweet` を tweet.fields に含める)
- `--no-json` 出力時は `note_tweet.text` が非空ならそれを 80 ルーン truncate 表示し、`tw.text` (短縮版) より優先する
- JSON / NDJSON 出力は無変更 (note_tweet と text の両方を含む後方互換)

### 1.2 `liked_tweets` の `max_results` 下限 (= 5)

`GET /2/users/:id/liked_tweets` は `max_results` に 5 以上を要求する (4 以下は X API が 400 エラーを返す)。
M29 から CLI `x liked list --max-results n` は次のように振る舞う:

- **n=5..100**: そのまま X API に渡す
- **n=1..4 かつ `--all=false`**: X API に 5 を投げて応答を `[:n]` で絞る
- **n=1..4 かつ `--all=true`**: UX 混乱回避のため `ErrInvalidArgument` (exit 2) で拒否

### 1.3 `search/recent` の Tier 要件・期間・下限 (M30)

`GET /2/tweets/search/recent` は次の制約を持つ:

| 項目 | 制約 |
|---|---|
| **Tier 要件** | **X API v2 Basic 以上必須**。Free tier では `403 Forbidden` が返り、CLI は `exit 4` (`xapi.ErrPermission`) で終了する |
| 検索範囲 | **過去 7 日間のみ**。7 日より前の `start_time` を指定すると X API が `400` を返す (CLI は exit 1 で素通り) |
| `max_results` 下限 | **10**。CLI `x tweet search --max-results n` は次の挙動:<br>・**n=10..100**: そのまま X API に渡す<br>・**n=1..9 かつ `--all=false`**: X API に 10 を投げて応答を `[:n]` で絞る<br>・**n=1..9 かつ `--all=true`**: `ErrInvalidArgument` (exit 2) で拒否 |
| Rate Limit | Basic tier: 60 req / 15 min / user (執筆時点、最新は [docs.x.com](https://docs.x.com/x-api/fundamentals/rate-limits) で確認)。CLI は `remaining ≤ 2` で reset まで sleep (`--all` 時) |

#### `conversation_id:<id>` 演算子 (スレッド取得)

X 検索演算子 `conversation_id:<root_tweet_id>` を使うと、指定ツイートのスレッド (会話) を構成するツイート群を取得できる。`x tweet thread <ID|URL>` はこの仕組みを利用する:

1. `GET /2/tweets/:id?tweet.fields=conversation_id` でルートツイートの `conversation_id` を取得 (= 1 リクエスト消費)
2. `GET /2/tweets/search/recent?query=conversation_id:<convID>` で構成ツイートを取得 (`--all` 時は複数ページ消費)

注意点:
- ルートツイートが **過去 7 日より前** の場合、`search/recent` 範囲外のためスレッドが部分的にしか取得できない (ルート自身が結果に含まれないケースあり)
- `conversation_id:<id>` の検索結果には **ルートツイート自身も含まれる** (X 仕様)。CLI 側では別途 prepend しない
- `x tweet thread --author-only` はルートツイートの `author_id` で CLI 層フィルタを行い、スレッド作成者の発言のみ表示する

#### 検索クエリのエンコード

CLI は `query` を `url.Values.Set("query", q)` 経由で X API に送出する。これにより演算子内のコロン (`from:`, `conversation_id:`, `lang:` 等) は **`%3A`** にパーセントエンコードされる。X API は両表現 (`from:youyo` および `from%3Ayouyo`) を等価に受け付けるため動作上の差異はない。

> v0.5.0 リリース前に実機 smoke test (`x tweet search "from:youyo" --max-results 10`) で `%3A` エンコードが期待通り 200 を返すことを確認する。

### 1.4 User Timelines の `max_results` 非対称性と `exclude` サポート (M31)

`x timeline {tweets,mentions,home}` が叩く 3 つのエンドポイントは、X API 仕様で per-page 下限と
`exclude` サポート、認証ユーザー制約が異なる:

| エンドポイント | `max_results` 仕様 | `exclude` | 認証ユーザー制約 |
|---|---|---|---|
| `GET /2/users/:id/tweets` | **5..100** | あり (`retweets` / `replies`) | 任意のユーザー |
| `GET /2/users/:id/mentions` | **5..100** | **なし** (X API 仕様で非サポート) | 任意のユーザー |
| `GET /2/users/:id/timelines/reverse_chronological` | **1..100** | あり (`retweets` / `replies`) | **認証ユーザー固定** (他人不可) |

CLI 挙動 (M31 D-1 / D-4 / D-9):

- **`tweets` / `mentions` の `--max-results n`**:
  - `n=5..100` → そのまま X API に渡す
  - `n=1..4 かつ --all=false` → X API に 5 を投げて応答を `[:n]` で truncate
  - `n=1..4 かつ --all=true` → `ErrInvalidArgument` (exit 2) で拒否 (liked / search と同形)
- **`home` の `--max-results n`**: 下限 1 のため補正なし。`n=1..100` をそのまま X API に渡す
- **`mentions` の `--exclude`**: CLI フラグ自体を登録しない (X API 仕様で非サポートのため誤解を防ぐ)
- **`home` の `--user-id`**: CLI フラグ自体を登録しない (X API 仕様で認証ユーザー固定のため)。
  毎回 `GetUserMe` で self ID を自動解決する

その他の共通仕様 (3 サブコマンド共通):

- `--yesterday-jst > --since-jst > --start-time/--end-time` の優先順位 (liked / search と同形)
- `--since-id` / `--until-id` は時間窓と独立に併用可能 (X API 仕様、CLI で排他チェックしない、M31 D-14)
- `--all` で `EachUser*Page` / `EachHomeTimelinePage` を辿り、`--max-pages` (default 50) で上限
- `--no-json` / `--ndjson` 排他、`--ndjson + --all` はストリーミング (集約せず逐次出力)

公式リファレンス:
- <https://docs.x.com/x-api/posts/user-posts-timeline-by-user-id>
- <https://docs.x.com/x-api/posts/user-mention-timeline-by-user-id>
- <https://docs.x.com/x-api/posts/user-home-timeline-by-user-id>

### 1.5 Users Extended エンドポイント (lookup / search / graph、M32)

`x user {get,search,following,followers,blocking,muting}` は X API v2 の users 系 9 endpoint を
カバーする。各 endpoint で `max_results` 仕様、ページネーションキー、認証ユーザー制約が異なる:

| エンドポイント | `max_results` | ページネーション | 認証制約 |
|---|---|---|---|
| `GET /2/users/:id` | — | — | OAuth 1.0a |
| `GET /2/users` (`?ids=`) | — (最大 100 IDs/call) | — | OAuth 1.0a |
| `GET /2/users/by/username/:username` | — | — | OAuth 1.0a |
| `GET /2/users/by` (`?usernames=`) | — (最大 100 names/call) | — | OAuth 1.0a |
| `GET /2/users/search` | **1..1000** (default 100) | **`next_token`** | OAuth 1.0a / Bearer |
| `GET /2/users/:id/following` | 1..1000 | `pagination_token` | OAuth 1.0a / Bearer |
| `GET /2/users/:id/followers` | 1..1000 | `pagination_token` | OAuth 1.0a / Bearer |
| `GET /2/users/:id/blocking` | 1..1000 | `pagination_token` | **OAuth 1.0a self only** |
| `GET /2/users/:id/muting` | 1..1000 | `pagination_token` | **OAuth 1.0a self only** |

CLI 設計上の判断 (M32):

- **3 サブコマンドに分離**: `--ids` (数値 ID) / `--usernames` (username 文字列) / 位置引数 (ID/@username/URL) で
  対象 endpoint が変わるため、`x user get` 内で三者排他にして処理する (M32 D-8)
- **`blocking` / `muting`**: X API 仕様で self のみ参照可能なため、CLI 側で `--user-id` フラグを公開せず
  毎回 `GetUserMe` で self を解決する (M32 D-5)
- **`following` / `followers` の `@username`/URL 位置引数**: `GetUserByUsername` で先に ID 解決 → graph 呼び出し (2 API call、M32 D-7)
- **search/users と graph のページネーションキー差**: X API 公式 docs で確認済 — search は `next_token`、graph は `pagination_token`。`xapi` 層は別実装で吸収 (M32 D-3)
- **`extractUserRef`**: 位置引数のみで使用。`@alice` / `https://x.com/alice` / `12345` を判別。
  予約パス (`/i/`, `/home`, `/explore`, `/messages`, `/notifications`, `/search`, `/compose`, `/settings`, `/login`, `/signup`, `/intent`, `/share`) は拒否し、username は `^[A-Za-z0-9_]{1,15}$` で厳格検証 (M32 D-4)

公式リファレンス:
- <https://docs.x.com/x-api/users/users-lookup-by-id>
- <https://docs.x.com/x-api/users/users-lookup-by-username>
- <https://docs.x.com/x-api/users/search-for-users>
- <https://docs.x.com/x-api/users/following-by-user-id>
- <https://docs.x.com/x-api/users/followers-by-user-id>
- <https://docs.x.com/x-api/users/blocking-by-user-id>
- <https://docs.x.com/x-api/users/muting-by-user-id>

## 2. アカウント取得と App 設定

1. [X Developer Portal](https://developer.x.com/) にサインインする。
2. **Project** と **App** を作成する (個人用の Liked Posts 取得には pay-per-usage 体系で十分)。
3. App 設定 (User authentication settings) で **OAuth 1.0a** を有効化し、権限は **Read** のみに絞る。
4. App 種別はデフォルトの **Web App, Automated App or Bot** のままで問題ない。

> プラン階層 (Free / Basic / Pro 等) は公式ドキュメントが pay-per-usage を中心に説明する形に整理されており、最新の単価・利用枠は [Developer Console](https://developer.x.com/) で確認することが推奨されている。

## 3. OAuth 1.0a User Context — 4 シークレットの取得

X API v2 のうち本リポで使うエンドポイントは **User context** が必要 (App-only Bearer Token では `users/me` が呼べない)。手元に揃える 4 値は以下。

| 環境変数 | X Developer Portal 上の表記 | 用途 |
|---|---|---|
| `X_API_KEY` | **Consumer Keys → API Key** | OAuth 1.0a Consumer Key |
| `X_API_SECRET` | **Consumer Keys → API Key Secret** | OAuth 1.0a Consumer Secret |
| `X_ACCESS_TOKEN` | **Authentication Tokens → Access Token and Secret** | User Access Token |
| `X_ACCESS_TOKEN_SECRET` | **Authentication Tokens → Access Token and Secret** | User Access Token Secret |

### 取得手順

1. App の **Keys and tokens** タブを開く。
2. **Consumer Keys** セクションで `API Key` と `API Key Secret` を **Regenerate** する (初回は `Generate`)。表示されるのは **その瞬間だけ** なので必ずコピーする。
3. **Authentication Tokens** セクションの **Access Token and Secret** → **Generate** を押す。これは **App のオーナー自身** の User context で発行されるトークンで、自分の Liked Posts を取得するのにそのまま使える。
4. 上記 4 値を `x configure` の対話モードに投入するか、`X_API_KEY` / `X_API_SECRET` / `X_ACCESS_TOKEN` / `X_ACCESS_TOKEN_SECRET` 環境変数として export する。

> 別ユーザーの Liked Posts を取得する場合は OAuth 1.0a 3-legged または OAuth 2.0 PKCE の認可フローを別途実装する必要がある。本リポは User context の **静的トークン** のみをサポートする。

## 4. エンドポイント詳細

### 4.1 `GET /2/users/me`

自分自身 (Access Token に紐付くユーザー) の情報を返す。

- **必須パラメータ**: なし
- **主要オプション**:
  - `user.fields` — 例: `username,name`
- **戻り値 (主要フィールド)**:
  - `data.id` (string) — Snowflake ID (`x` 内部では `user_id` にリネームして公開)
  - `data.username` — `@` なしのハンドル名
  - `data.name` — 表示名

公式リファレンス: <https://docs.x.com/x-api/users/users-lookup-me>

### 4.2 `GET /2/users/:id/liked_tweets`

指定ユーザーが Like した Post の一覧を返す。`:id` は対象ユーザーの数値 ID。

- **必須パラメータ**: なし (`:id` のみ)
- **主要オプション**:
  - `max_results` — 1 ページあたりの件数 (5-100、デフォルト 100)
  - `pagination_token` — 次ページ取得用トークン (`meta.next_token` の値)
  - `start_time` / `end_time` — RFC 3339 / ISO 8601 UTC 形式 (秒精度、`Z` サフィックス)
  - `tweet.fields` — 例: `id,text,author_id,created_at,entities,public_metrics`
  - `expansions` — 例: `author_id`
  - `user.fields` — `expansions=author_id` と組み合わせて利用 (例: `username,name`)
- **戻り値 (主要フィールド)**:
  - `data[]` — Tweet オブジェクト配列
  - `includes.users[]` — `expansions` 指定時のユーザー情報
  - `meta.next_token` — 次ページがある場合のトークン
  - `meta.result_count` — このページの件数

公式リファレンス: <https://docs.x.com/x-api/posts/likes-lookup/users-id-liked-tweets>

### 4.3 `GET /2/tweets/search/recent` (M30)

過去 7 日間のキーワード検索を実行する。**X API v2 Basic tier 以上必須**。

- **必須パラメータ**: `query` (X 検索演算子をサポート)
- **主要オプション**:
  - `max_results` — 10..100 (デフォルト 10)
  - `pagination_token` — 次ページ取得用トークン
  - `start_time` / `end_time` — RFC 3339 UTC 形式 (過去 7 日以内)
  - `tweet.fields` — 例: `id,text,author_id,created_at,entities,public_metrics,note_tweet,conversation_id`
  - `expansions` — 例: `author_id`
  - `user.fields` / `media.fields`
- **戻り値**:
  - `data[]` — Tweet オブジェクト配列
  - `includes.users[]` / `includes.tweets[]` — `expansions` 指定時
  - `meta.next_token` — 次ページがある場合
  - `meta.result_count` — このページの件数

#### 主要な検索演算子

| 演算子 | 例 | 効果 |
|---|---|---|
| `from:` | `from:youyo` | 指定ユーザーの投稿のみ |
| `to:` | `to:youyo` | 指定ユーザー宛て返信のみ |
| `@` | `@youyo` | メンション含む投稿 |
| `lang:` | `lang:ja` | 言語フィルタ |
| `conversation_id:` | `conversation_id:1234` | 指定スレッドの構成ツイート |
| `is:retweet` / `-is:retweet` | `-is:retweet` | リツイート除外 |
| `has:links` / `has:media` | `has:media` | メディア付きのみ |
| `url:` | `url:github.com` | 指定 URL を含む投稿 |

複数演算子は AND 結合。OR は `(a OR b)` の括弧表記。

公式リファレンス: <https://docs.x.com/x-api/posts/recent-search>

## 5. レート制限

公式: <https://docs.x.com/x-api/fundamentals/rate-limits> (執筆時点)

| エンドポイント | Per User | Per App | Tier |
|---|---|---|---|
| `GET /2/users/me` | 75 req / 15 min | — (User context のみ) | Free 以上 |
| `GET /2/users/:id/liked_tweets` | 75 req / 15 min | 75 req / 15 min | Free 以上 |
| `GET /2/tweets/:id` / `GET /2/tweets` | 15 req / 15 min | 450 req / 15 min | Free 以上 |
| `GET /2/tweets/:id/liking_users` | 75 req / 15 min | 75 req / 15 min | Free 以上 |
| `GET /2/tweets/:id/retweeted_by` | 75 req / 15 min | 75 req / 15 min | Free 以上 |
| `GET /2/tweets/:id/quote_tweets` | 75 req / 15 min | 75 req / 15 min | Free 以上 |
| `GET /2/tweets/search/recent` | 60 req / 15 min | 60 req / 15 min | **Basic 以上必須** (Free 不可) |
| `GET /2/users/:id/tweets` | 1500 req / 15 min | — | Free 以上 |
| `GET /2/users/:id/mentions` | 450 req / 15 min | — | Free 以上 |
| `GET /2/users/:id/timelines/reverse_chronological` | 180 req / 15 min | — (OAuth 1.0a 必須、App-only 不可) | Free 以上 |
| `GET /2/users/:id` / `GET /2/users` | 100 req / 24 h / user | 500 req / 24 h | Free 以上 |
| `GET /2/users/by/username/:username` / `GET /2/users/by` | 100 req / 24 h / user | 500 req / 24 h | Free 以上 |
| `GET /2/users/search` | 900 req / 15 min | — | Free 以上 |
| `GET /2/users/:id/following` | 15 req / 15 min | 15 req / 15 min | Free 以上 |
| `GET /2/users/:id/followers` | 15 req / 15 min | 15 req / 15 min | Free 以上 |
| `GET /2/users/:id/blocking` | 15 req / 15 min | — (OAuth 1.0a 必須) | Free 以上 |
| `GET /2/users/:id/muting` | 15 req / 15 min | — (OAuth 1.0a 必須) | Free 以上 |

> User Timelines / Users Extended のレートは likes / search よりも緩い場合と厳しい場合があるが、`x` は保守的に
> `remaining ≤ 2` (likes/search と同値) で reset まで sleep する (M31 D-6 / M32 D-1)。
> Users Extended のレートは X API 公式ドキュメントの値 (時期により変動の可能性あり)、実機確認で更新する。

### レスポンスヘッダ

X API は以下のヘッダで現在のレート制限状態を返す。`x` クライアントは `x-rate-limit-remaining` と `x-rate-limit-reset` を `internal/xapi/client.go` で常時パースし、ページネーション時の暴走を抑制する。

| ヘッダ | 意味 |
|---|---|
| `x-rate-limit-limit` | この窓内で許可されている総リクエスト数 |
| `x-rate-limit-remaining` | この窓内で残っているリクエスト数 |
| `x-rate-limit-reset` | レート制限がリセットされる UNIX epoch 秒 |

### `x` の自動ウェイト挙動

`x liked list --all` (または MCP `get_liked_tweets all=true`) でページネーション中:

- **`remaining <= 2`** のとき、`reset` 時刻まで **context-aware sleep** する (最大 15 分)。
- `reset` ヘッダが過去/欠落している場合は **200 ms フォールバック sleep** + 続行。
- ページ間最小スリープは **200 ms** (バーストで `429` を踏まないように)。
- `--max-pages` (default 50) に到達したら正常終了する (打ち切りシグナルは現状なし)。

## 6. 料金 (Owned Reads)

公式: <https://docs.x.com/x-api/fundamentals/rate-limits>

- X API は **pay-per-usage (従量課金)** モデル。
- 「**Owned Reads**」: 自分が所有する Like / Post を取得する操作は **1 リソース (Tweet) あたり $0.001** (1,000 件で $1)。
- `x` のハードリミット計算例:
  - `--max-pages 50` (default) × `max_results=100/page` = **最大 5,000 Posts ≈ $5 / 実行**
  - 毎日 Routine から呼び出す前提でも、前日分の Like 件数が数十件なら **月額 $1 未満** に収まる。
- 厳密な最新単価は [Developer Console](https://developer.x.com/) または [docs.x.com/x-api/getting-started/pricing](https://docs.x.com/x-api/getting-started/pricing) を参照。

## 7. エラーレスポンス

`x` の `internal/xapi` パッケージは X API からのエラーステータスを Go の番兵エラーに写像し、最終的に `cmd/x/main.go` で **exit code** に変換する。MCP サーバー側も同じ写像を共用する。

| HTTP | エラー (Go) | exit code | 主な原因 |
|---|---|---|---|
| `401` | `xapi.ErrAuthentication` | `3` | OAuth 1.0a 署名失敗 / Access Token 失効 / Consumer Keys 不正 |
| `403` | `xapi.ErrPermission` | `4` | App の権限不足 (Read 権限がない、suspended アカウント等) |
| `404` | `xapi.ErrNotFound` | `5` | 存在しないユーザー / 削除済み Post |
| `429` | `xapi.ErrRateLimit` | `1` (リトライ後の最終失敗時) | レート制限超過。`x` は exp backoff で最大 3 回まで自動 retry |
| `5xx` | `xapi.APIError` (status コード保持) | `1` | X 側の一時障害。`x` は exp backoff で自動 retry |

### エラーレスポンスの JSON 構造 (X API v2)

```json
{
  "errors": [
    {
      "title": "Unauthorized",
      "detail": "Unauthorized",
      "type": "about:blank",
      "status": 401
    }
  ],
  "title": "Unauthorized",
  "detail": "Unauthorized",
  "type": "about:blank",
  "status": 401
}
```

`xapi.APIError.Errors[]` は上記 `errors[]` 配列を保持する。複数エラーが含まれる場合があるため、ログ出力時は全要素を出すのが望ましい。

## 8. 参考リンク

- [X API v2 公式ドキュメント (トップ)](https://docs.x.com/x-api)
- [OAuth 1.0a User Context 概要](https://docs.x.com/resources/fundamentals/authentication/oauth-1-0a/overview)
- [GET /2/users/me](https://docs.x.com/x-api/users/users-lookup-me)
- [GET /2/users/:id/liked_tweets](https://docs.x.com/x-api/posts/likes-lookup/users-id-liked-tweets)
- [GET /2/tweets/search/recent](https://docs.x.com/x-api/posts/recent-search)
- [X Search Operators](https://docs.x.com/x-api/posts/search/integrate/build-a-query)
- [Rate Limits](https://docs.x.com/x-api/fundamentals/rate-limits)
- [Pricing (Developer Console 確認推奨)](https://docs.x.com/x-api/getting-started/pricing)
- 本リポジトリの実装:
  - `internal/xapi/oauth1.go` — OAuth 1.0a 署名 (`dghubble/oauth1` ラッパー)
  - `internal/xapi/client.go` — HTTP retry + rate-limit aware sleep + エラー写像
  - `internal/xapi/users.go` — `GetUserMe` + Users Extended 9 endpoint (M32)
  - `internal/xapi/likes.go` — `ListLikedTweets` + `EachLikedPage`
  - `internal/xapi/tweets.go` — `GetTweet` / `GetTweets` / `GetLikingUsers` / `GetRetweetedBy` / `GetQuoteTweets` / `SearchRecent` / `EachSearchPage` (M29 / M30)
  - `internal/xapi/timeline.go` — `GetUserTweets` / `GetUserMentions` / `GetHomeTimeline` + `Each*TimelinePage` (M31)
  - `internal/xapi/pagination.go` — `computeInterPageWait` 共通化 (M29 T7)
