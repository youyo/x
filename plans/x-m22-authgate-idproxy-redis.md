# Plan: M22 — authgate idproxy redis store

> Layer 2: マイルストーン詳細計画。
> 親ロードマップ: [plans/x-roadmap.md](./x-roadmap.md) §M22
> M21 (sqlite) ハンドオフ準拠。spec §5 / §10 / §11 (REDIS_URL) 反映。

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M22: authgate idproxy redis store backend |
| 親ロードマップ | plans/x-roadmap.md |
| ステータス | Approved / 実装フェーズ着手可 |
| 作成日 | 2026-05-12 |
| 想定コミット粒度 | 1 コミット (`feat(authgate): idproxy redis store backend を追加`) |
| 前マイルストーン | M21 (sqlite store, commit: 7b50dd8) |
| 後続マイルストーン | M23 (dynamodb store) |

## ゴール

- spec §11 の環境変数 `STORE_BACKEND=redis` / `REDIS_URL` (例: `redis://localhost:6379/0`) に対応する authgate 層のヘルパ `authgate.NewRedisStore(url string) (idproxy.Store, error)` を提供する。
- `idproxy/store/redis.New(Options)` の薄いラッパーとし、URL パースは `github.com/redis/go-redis/v9` の `redis.ParseURL` を活用して自前実装を避ける。
- `gate.go` の `WithIDProxyStore(store idproxy.Store)` Option 経由で接続するため、`gate.go` と `idproxy.go` は変更しない。
- TDD: Red → Green → Refactor。テストは `miniredis/v2` を用いて in-process Redis でカバーし、リアル Redis を要求しない。

## 非ゴール / 制約

- リアル Redis サーバーを CI 必須にしない (miniredis で完結)。spec の `STORE_BACKEND=redis` E2E はロードマップ §M24 で評価する。
- 接続プール / Sentinel / Cluster の対応は範囲外 (将来必要なら別マイルストーン化)。
- TTL は `idproxy/store/redis` 内部が SET EX で自動付与済み (本マイルストーンで追加実装しない)。
- CLI 層 (`internal/cli/mcp.go`、M24) で `REDIS_URL` を環境変数から読む実装は範囲外。本マイルストーンでは authgate 層に sentinel と URL パースを置くのみ。

## 影響範囲

### 追加ファイル

| ファイル | 役割 |
|---|---|
| `internal/authgate/store_redis.go` | `NewRedisStore(url) (idproxy.Store, error)`, `ErrRedisURLRequired` |
| `internal/authgate/store_redis_test.go` | TDD: テーブル駆動テスト + miniredis 統合 |

### 変更ファイル

| ファイル | 変更内容 |
|---|---|
| `internal/authgate/doc.go` | M22 反映: redis store の概要・利用例追記 |
| `go.mod` / `go.sum` | `github.com/redis/go-redis/v9` (transitive → direct or indirect), `github.com/alicebob/miniredis/v2` (test) などの依存追加 |

### 変更しないファイル

- `internal/authgate/gate.go` (`WithIDProxyStore` Option 経由で十分)
- `internal/authgate/idproxy.go` (組み立てロジックは変更不要)
- `internal/authgate/store_memory.go` / `store_sqlite.go` (既存実装を保護)

## 公開 API 設計

```go
package authgate

// ErrRedisURLRequired は [NewRedisStore] に空文字を渡した場合に返るエラー。
// `STORE_BACKEND=redis` モードで [spec §11] の `REDIS_URL` が空のままだと
// authgate 層が組み立て前に弾く責務に徹するための sentinel。
var ErrRedisURLRequired = errors.New("authgate: redis url is required")

// NewRedisStore は spec §11 の `REDIS_URL` を解析し、
// idproxy.Store の Redis 実装を返す薄いラッパーである。
//
// 引数 rawURL (例):
//   - redis://localhost:6379/0
//   - redis://:password@host:6379/1
//   - rediss://host:6380/2 (TLS 自動有効化)
//
// 動作:
//   - 空文字 → [ErrRedisURLRequired]
//   - `redis.ParseURL(rawURL)` で Options を取得し、
//     `idproxy/store/redis.New(redisstore.Options{...})` を呼ぶ。
//   - KeyPrefix は固定で `"idproxy:"` (idproxy 同梱のキー構造仕様に従う)。
//   - パース失敗・接続/PING 失敗は fmt.Errorf でラップして返す。
//
// 戻り値の Store は利用者が不要になった時点で Close() を呼ぶ責務を負う。
func NewRedisStore(rawURL string) (idproxy.Store, error)
```

### KeyPrefix の選定根拠

`idproxy/store/redis.Store.k(ns, id) = prefix + ns + ":" + id` (store.go:86) なので、prefix は **末尾コロン付き** `"idproxy:"` を採用する。これにより最終キーが `idproxy:session:<id>` / `idproxy:authcode:<code>` 形式となり、共有 Redis 上の名前空間衝突を防ぐ。

### TLS 検出

`redis.ParseURL` は `rediss://` スキームを検出すると `Options.TLSConfig` を non-nil に設定する (options.go:535-540)。authgate 側では `redisstore.Options.TLS = (parsed.TLSConfig != nil)` で TLS フラグを伝達する。

> 注: `idproxy/store/redis.New` は `Options.TLS == true` で自前の `&tls.Config{MinVersion: TLS12}` を生成する (store.go:63)。`parsed.TLSConfig.ServerName` (rediss://host) は失われるが、idproxy 側がデフォルトで `ServerName=Addr` を見るため実害なし。本マイルストーンでは現状のままとし、将来 SNI が必要な要件が出た時点で再評価する。

## TDD

### Red フェーズ (テスト先行)

ファイル: `internal/authgate/store_redis_test.go` (package `authgate_test`)

| # | テスト名 | 検証内容 |
|---|---|---|
| 1 | `TestNewRedisStore_EmptyURL` | 空文字 → `errors.Is(err, ErrRedisURLRequired)`, 戻り値 store == nil |
| 2 | `TestNewRedisStore_InvalidURL` | `"::not-a-url::"` 等の不正 URL → エラー返却 (`ErrRedisURLRequired` ではない、原因をラップ) |
| 3 | `TestNewRedisStore_InvalidScheme` | `"http://example.com"` → エラー (`redis: invalid URL scheme` を wrap) |
| 4 | `TestNewRedisStore_ImplementsStore` | miniredis 経由で正常 URL → `idproxy.Store` 適合 + non-nil |
| 5 | `TestNewRedisStore_PingFailure` | 到達不能 host (TCP closed port) → エラー (PING 失敗ラップ) |
| 6 | `TestNewRedisStore_BasicSetGet` | miniredis で SetSession → GetSession ラウンドトリップ |
| 7 | `TestNewRedisStore_PersistsAcrossReopen` | miniredis 1 つに 2 つの Store を作り、SetSession (s1) → Close → s2 で GetSession 成功 (永続性 sanity check) |
| 8 | `TestNewRedisStore_KeyPrefix` | SetSession 後に miniredis の `Keys("*")` に `idproxy:session:<id>` が含まれる (prefix 確認) |
| 9 | `TestNewRedisStore_ParsesDBNumber` | URL `redis://host:port/3` → 別 DB に書く (miniredis では DB 番号サポートあり、SELECT 後に Keys が DB ごとに分離) |
| 10 | `TestNewRedisStore_ParsesPassword` | miniredis に `RequireAuth("secret")` を設定 → `redis://:secret@host:port` で接続成功、間違いパスワードで失敗 |
| 11 | `TestNewRedisStore_RedissScheme` | `rediss://host:port` で接続試行 → miniredis は plain TCP のため接続エラーが返ることを確認 (TLS 検出のみ確認、実 TLS は miniredis では検証不可なため省略) |

> 留意: テスト 11 は TLS の **検出** をスモークするのみ。実 TLS handshake は CI 制約で省略する (advisor 助言反映)。

### Green フェーズ (最小実装)

`internal/authgate/store_redis.go`:

```go
package authgate

import (
    "errors"
    "fmt"

    "github.com/redis/go-redis/v9"
    "github.com/youyo/idproxy"
    redisstore "github.com/youyo/idproxy/store/redis"
)

const redisKeyPrefix = "idproxy:"

var ErrRedisURLRequired = errors.New("authgate: redis url is required")

func NewRedisStore(rawURL string) (idproxy.Store, error) {
    if rawURL == "" {
        return nil, ErrRedisURLRequired
    }
    opts, err := redis.ParseURL(rawURL)
    if err != nil {
        return nil, fmt.Errorf("authgate: parse redis url: %w", err)
    }
    store, err := redisstore.New(redisstore.Options{
        Addr:      opts.Addr,
        Password:  opts.Password,
        DB:        opts.DB,
        TLS:       opts.TLSConfig != nil,
        KeyPrefix: redisKeyPrefix,
    })
    if err != nil {
        return nil, fmt.Errorf("authgate: open redis store: %w", err)
    }
    return store, nil
}
```

### Refactor フェーズ

- doc コメントを sqlite と対称に整形。
- 公開シンボルすべてに日本語 doc コメント。
- パッケージ doc コメントは `doc.go` 1 ファイル集約 (Go の規約)。`store_redis.go` には先頭にパッケージコメントを書かない。
- 関数内変数のシャドーイング回避、`if err != nil` 早期 return 厳守。

## doc.go の修正方針

既存の M21 sqlite 例示の直後に、以下スニペットを追加する:

```go
//	// idproxy モード (redis store, 軽量サーバー向け)
//	store, err := authgate.NewRedisStore(os.Getenv("REDIS_URL"))
//	if err != nil { /* ErrRedisURLRequired 等を処理 */ }
//	defer store.Close()
//	mw, err = authgate.New(authgate.ModeIDProxy,
//	    authgate.WithOIDCIssuer(os.Getenv("OIDC_ISSUER")),
//	    authgate.WithOIDCClientID(os.Getenv("OIDC_CLIENT_ID")),
//	    authgate.WithOIDCClientSecret(os.Getenv("OIDC_CLIENT_SECRET")),
//	    authgate.WithCookieSecret(os.Getenv("COOKIE_SECRET")),
//	    authgate.WithExternalURL(os.Getenv("EXTERNAL_URL")),
//	    authgate.WithIDProxyStore(store),
//	)
```

冒頭の "M21 までで memory / sqlite を実装済み、redis / dynamodb は M22–M23 で順次追加する計画" の記述を「M22 までで memory / sqlite / redis を実装済み、dynamodb は M23 で追加する計画」に更新する。

## 依存追加 (go.mod)

- 直接依存に格上げされうるもの:
  - `github.com/redis/go-redis/v9` (現在 idproxy 経由の indirect → x の直接 import で direct に昇格)
- テスト専用依存 (新規追加):
  - `github.com/alicebob/miniredis/v2`
  - これは `// indirect` ではなく test imports なので、`go.mod` 上で直接依存となる (Go 1.17+ の lazy module loading 規約)。

検証手順:

1. `go get github.com/redis/go-redis/v9@latest`
2. `go get github.com/alicebob/miniredis/v2@latest`
3. `go mod tidy`
4. `go build ./...` `go test ./...`
5. `golangci-lint run ./...` で direct/indirect の警告がないことを確認

> idproxy v0.4.2 が依存する go-redis のバージョンと合致させる (`go.mod` の `require` で同一行に揃える)。バージョン不整合があれば idproxy 側に合わせる。

## golangci-lint 配慮

- `errcheck`: `store.Close()` の戻り値を握りつぶす場合は `_ = store.Close() //nolint:errcheck` で抑制 (sqlite と対称)。
- `gosec`: 本実装には security 観点の特殊配慮なし (TLS verification は go-redis のデフォルトに従う)。
- `lll`: doc コメントの長さに注意。120 文字以内に整形。
- `revive`: 公開シンボルの doc コメントは「シンボル名 + ...」形式で記述。

## 完了条件

- [ ] `internal/authgate/store_redis.go` を追加し `NewRedisStore` + `ErrRedisURLRequired` を公開
- [ ] `internal/authgate/store_redis_test.go` を追加し 11 テスト全 pass
- [ ] `internal/authgate/doc.go` に redis store の利用例を追記、M22 までを反映
- [ ] `go mod tidy` 済み、`go build -o /tmp/x ./cmd/x` 成功
- [ ] `go test -race -count=1 ./...` 全 pass
- [ ] `golangci-lint run ./...` 0 issues
- [ ] `go vet ./...` 0 issues
- [ ] git commit: `feat(authgate): idproxy redis store backend を追加`、フッターに `Plan: plans/x-m22-authgate-idproxy-redis.md`

## リスク

| リスク | 影響 | 対策 |
|---|---|---|
| miniredis が DB SELECT 未対応 | 中 (テスト 9 失敗) | miniredis v2.30+ は SELECT サポート済み。失敗時はテスト 9 を skip し、Note を残す |
| go-redis のバージョンが idproxy と不一致 | 中 (build エラー) | `go mod tidy` 後に `go.mod` を確認、idproxy の require と一致させる |
| miniredis の TLS スキーム検証不可 | 低 (テスト 11) | TLS 検出のみ確認、実 handshake は省略 (advisor 助言準拠) |
| key prefix の trailing colon 漏れ | 高 (key 衝突) | 定数 `redisKeyPrefix = "idproxy:"` で固定し、テスト 8 で検証 |

## 参考実装 / 引用

- `/Users/youyo/src/github.com/youyo/idproxy/store/redis/store.go` (公開 API、KeyPrefix の挙動)
- `/Users/youyo/src/github.com/youyo/logvalet/internal/cli/mcp_auth.go:62-73` (redis 統合パターン)
- `/Users/youyo/src/github.com/youyo/x/internal/authgate/store_sqlite.go` (対称な薄ラッパー実装)
- `/Users/youyo/pkg/mod/github.com/redis/go-redis/v9@v9.19.0/options.go:496-543` (`ParseURL` 実装)

## Changelog

| 日時 | 種別 | 内容 |
|---|---|---|
| 2026-05-12 | 作成 | 計画初版 (advisor 助言反映: ParseURL 活用 / KeyPrefix 末尾コロン / 依存追加注意) |
