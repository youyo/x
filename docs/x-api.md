# X API v2 リファレンス (本リポジトリ視点)

> `x` CLI / Remote MCP が利用する X (旧 Twitter) API v2 の認証・エンドポイント・レート制限・料金・エラーレスポンスを集約した開発者向けメモ。
> 数値は執筆時点 (2026-05-12) の公式ドキュメントに基づくため、本番運用前に [docs.x.com](https://docs.x.com/x-api) で最新値を確認すること。

## 1. 概要

X API v2 は X (旧 Twitter) が提供する公式 REST API。本リポジトリでは **OAuth 1.0a User Context** で以下 2 エンドポイントのみを利用する。

| エンドポイント | 本リポでの用途 |
|---|---|
| `GET /2/users/me` | `x me` / MCP `get_user_me` ツール |
| `GET /2/users/:id/liked_tweets` | `x liked list` / MCP `get_liked_tweets` ツール |

その他のエンドポイント (post / search / filter stream など) は対象外。

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

## 5. レート制限

公式: <https://docs.x.com/x-api/fundamentals/rate-limits> (執筆時点)

| エンドポイント | Per User | Per App |
|---|---|---|
| `GET /2/users/me` | 75 req / 15 min | — (User context のみ) |
| `GET /2/users/:id/liked_tweets` | 75 req / 15 min | 75 req / 15 min |

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
- [Rate Limits](https://docs.x.com/x-api/fundamentals/rate-limits)
- [Pricing (Developer Console 確認推奨)](https://docs.x.com/x-api/getting-started/pricing)
- 本リポジトリの実装:
  - `internal/xapi/oauth1.go` — OAuth 1.0a 署名 (`dghubble/oauth1` ラッパー)
  - `internal/xapi/client.go` — HTTP retry + rate-limit aware sleep + エラー写像
  - `internal/xapi/users.go` — `GetUserMe`
  - `internal/xapi/likes.go` — `ListLikedTweets` + `EachLikedPage`
