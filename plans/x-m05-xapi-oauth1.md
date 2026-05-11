# M5 詳細計画: `internal/xapi/oauth1.go` (dghubble/oauth1 ラッパー + 署名検証)

> Layer 2: M5 の実装詳細計画。Roadmap [plans/x-roadmap.md](./x-roadmap.md) の Phase B 起点。
> スペック: [docs/specs/x-spec.md](../docs/specs/x-spec.md) §5/§10/§11

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M5 |
| タイトル | `internal/xapi/oauth1.go` (dghubble/oauth1 ラッパー + 署名検証) |
| 前提マイルストーン | M1-M4 完了 |
| 推定 LOC | 実装 ~60 行 / テスト ~180 行 |
| 推定テストケース | 7 ケース |
| 作成日 | 2026-05-12 |
| ステータス | 計画中 |

## 目的とスコープ

M6 (`xapi/client.go`) で X API v2 への HTTP リクエストに OAuth 1.0a 署名を付与するための基盤を提供する。M4 で確立した `config.Credentials` 型 (APIKey / APISecret / AccessToken / AccessTokenSecret) を入力に取り、`dghubble/oauth1` の `*oauth1.Config` および署名済み `*http.Client` を返す薄いラッパーを実装する。

スコープ内:
- `internal/xapi` パッケージの新規作成
- `dghubble/oauth1` v0.7.3 を依存追加
- `NewOAuth1Config(creds *config.Credentials) *oauth1.Config`
- `NewHTTPClient(ctx context.Context, creds *config.Credentials) *http.Client`
- TDD: フィールドマッピング検証 + httptest による Authorization ヘッダ検証

スコープ外:
- HTTP リクエスト本体 (retry / rate limit) → M6
- DTO 型 / users.go / likes.go → M7-M8

## 設計判断

### D-1: 関数 API (struct ではない)

入力 `*config.Credentials` を引数で受け取り、副作用なく `*oauth1.Config` / `*http.Client` を返す純粋関数として設計する。
- 理由: `dghubble/oauth1` 自体が値型ベースで設計されており、ラッパー側で状態を持つ必然性がない。M6 の `client.go` でも関数呼び出し 1 行で済む。
- 却下案: `OAuth1Signer` struct を作って `Signer.HTTPClient()` で返す形式 → 過剰設計。

### D-2: nil 引数の扱い

`creds == nil` の場合、`NewOAuth1Config` / `NewHTTPClient` どちらも **panic させず**、空文字列 4 つを入れた zero Config / Client を返す。
- 理由: dghubble/oauth1 自体は ConsumerKey="" でも `Config` を組み立てるし `Client` も返す (実 API 呼び出し時に X API 側が 401 を返すだけ)。Go 標準ライブラリ流の堅牢性 (`http.DefaultTransport` 等) に倣い、構築時 panic より「使えるが認証失敗する Client」を返す方が呼び出し側で扱いやすい。
- 却下案 A: `(*..., error)` シグネチャに変更 → 呼び出し側がエラー処理を強いられ、M6/M7/M9 で `if err != nil` が散らばる。実態として「ロード時にエラーになる入力」は存在しない (config.Credentials の zero value も valid)。
- 却下案 B: nil で panic → M6 以降で nil-check を強要し、設計簡潔さを損なう。代わりに M9 で「環境変数 + credentials.toml いずれもないなら exit 3」を行う方針で責務分離する。
- 実装: 内部で `creds = &config.Credentials{}` にフォールバック。

### D-3: パッケージ doc コメントの配置

`internal/xapi` は新規パッケージ。本マイルストーンで作成する `oauth1.go` の冒頭にパッケージ doc を 1 箇所だけ配置する。M6 以降で `client.go` を作る際にはパッケージ doc を**書かない** (重複違反になるため)。

```go
// Package xapi は X (Twitter) API v2 のクライアントを提供する。
//
// OAuth 1.0a (HMAC-SHA1) 署名は dghubble/oauth1 のラッパーを介して付与する。
// HTTP retry / rate-limit handling は client.go (M6) で実装する。
package xapi
```

### D-4: context.Context の伝播

`NewHTTPClient(ctx, creds)` の `ctx` は `oauth1.Config.Client(ctx, token)` にそのまま渡す。ctx の具体的な利用ポリシーは dghubble/oauth1 v0.7.3 の実装に委ね、本パッケージとしては「シグネチャに含めてライブラリへ伝播するだけ」とする。
- spec §5/§6 で「context propagation」が暗黙的に要求されるため、シグネチャに含める。
- M6 で実際に transport カスタマイズが必要になった時点で挙動を再確認し doc 更新する。

### D-5: dghubble/oauth1 のバージョン pin

`go get github.com/dghubble/oauth1@v0.7.3` で固定する。
- v0.7.3 (2024-02-26) が最新 stable。v0.8 系はまだ存在しない (2026-05 時点でも未リリースの想定)。
- spec §10 で「v0.7.x」と幅指定されている範囲内。

## 実装ファイル詳細

### `internal/xapi/oauth1.go` (新規)

```go
// Package xapi は X (Twitter) API v2 のクライアントを提供する。
//
// OAuth 1.0a (HMAC-SHA1) 署名は dghubble/oauth1 のラッパーを介して付与する。
// HTTP retry / rate-limit handling は client.go (M6) で実装する。
package xapi

import (
	"context"
	"net/http"

	"github.com/dghubble/oauth1"

	"github.com/youyo/x/internal/config"
)

// NewOAuth1Config は config.Credentials を dghubble/oauth1.Config に変換する。
//
// マッピング:
//   - Credentials.APIKey    → oauth1.Config.ConsumerKey
//   - Credentials.APISecret → oauth1.Config.ConsumerSecret
//
// AccessToken / AccessTokenSecret は oauth1.Token として別途扱われるため
// 本関数の返り値には含まれない (NewHTTPClient 内部で oauth1.NewToken に渡す)。
//
// creds が nil の場合は空文字列 4 つを持つゼロ値 Credentials として扱う
// (panic させず、利用時に X API 側で 401 を返させる方針)。
func NewOAuth1Config(creds *config.Credentials) *oauth1.Config {
	c := safeCredentials(creds)
	return oauth1.NewConfig(c.APIKey, c.APISecret)
}

// NewHTTPClient は ctx と config.Credentials から OAuth 1.0a 署名済み *http.Client を返す。
//
// 用途: M6 (`client.go`) で X API v2 の各エンドポイントに対するリクエストに
// 利用する。返却される Client はリクエストごとに oauth_nonce / oauth_timestamp /
// oauth_signature を再計算し Authorization ヘッダに付与する。
//
// ctx は dghubble/oauth1.Config.Client にそのまま渡す。通常は context.Background()
// で良い。transport カスタマイズが必要になった場合の挙動は M6 で再確認する。
//
// creds が nil の場合は空 Credentials として扱う (D-2 参照)。
func NewHTTPClient(ctx context.Context, creds *config.Credentials) *http.Client {
	c := safeCredentials(creds)
	cfg := oauth1.NewConfig(c.APIKey, c.APISecret)
	token := oauth1.NewToken(c.AccessToken, c.AccessTokenSecret)
	return cfg.Client(ctx, token)
}

// safeCredentials は nil の場合に空 Credentials を返すヘルパ。
func safeCredentials(creds *config.Credentials) *config.Credentials {
	if creds == nil {
		return &config.Credentials{}
	}
	return creds
}
```

### `internal/xapi/oauth1_test.go` (新規)

7 テストケース構成:

```go
package xapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// TestNewOAuth1Config_MapsConsumerCredentials は Credentials.APIKey/APISecret が
// oauth1.Config.ConsumerKey/ConsumerSecret に正しくマッピングされることを確認する。
func TestNewOAuth1Config_MapsConsumerCredentials(t *testing.T) {
	creds := &config.Credentials{
		APIKey:    "test-consumer-key",
		APISecret: "test-consumer-secret",
	}
	cfg := xapi.NewOAuth1Config(creds)
	if cfg == nil {
		t.Fatalf("NewOAuth1Config returned nil")
	}
	if cfg.ConsumerKey != "test-consumer-key" {
		t.Errorf("ConsumerKey = %q, want %q", cfg.ConsumerKey, "test-consumer-key")
	}
	if cfg.ConsumerSecret != "test-consumer-secret" {
		t.Errorf("ConsumerSecret = %q, want %q", cfg.ConsumerSecret, "test-consumer-secret")
	}
}

// TestNewOAuth1Config_NilCredentials は nil 入力でも panic せず空文字 Config を返すことを確認する。
func TestNewOAuth1Config_NilCredentials(t *testing.T) {
	cfg := xapi.NewOAuth1Config(nil)
	if cfg == nil {
		t.Fatalf("NewOAuth1Config(nil) returned nil")
	}
	if cfg.ConsumerKey != "" || cfg.ConsumerSecret != "" {
		t.Errorf("expected empty consumer key/secret, got key=%q secret=%q", cfg.ConsumerKey, cfg.ConsumerSecret)
	}
}

// TestNewHTTPClient_AuthorizationHeader は httptest サーバーに対して
// NewHTTPClient で作った Client がリクエストを送ると、Authorization ヘッダに
// OAuth スキームと必須パラメータ群が含まれることを確認する。
func TestNewHTTPClient_AuthorizationHeader(t *testing.T) {
	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	creds := &config.Credentials{
		APIKey:            "ck",
		APISecret:         "cs",
		AccessToken:       "at",
		AccessTokenSecret: "ats",
	}
	client := xapi.NewHTTPClient(context.Background(), creds)
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	_ = resp.Body.Close()

	auth := captured.Get("Authorization")
	if !strings.HasPrefix(auth, "OAuth ") {
		t.Fatalf("Authorization header does not start with %q: %q", "OAuth ", auth)
	}
	// OAuth 1.0a (RFC 5849) の必須パラメータ群が全て含まれること。
	// oauth_version は OPTIONAL なので必須リストには含めない。
	requiredParams := []string{
		`oauth_consumer_key="ck"`,
		`oauth_token="at"`,
		`oauth_signature_method="HMAC-SHA1"`,
		"oauth_nonce=",
		"oauth_timestamp=",
		"oauth_signature=",
	}
	for _, p := range requiredParams {
		if !strings.Contains(auth, p) {
			t.Errorf("Authorization header missing %q: %s", p, auth)
		}
	}
	// oauth_version は OPTIONAL だが、もし emit されていれば値は "1.0" であること。
	if strings.Contains(auth, "oauth_version=") && !strings.Contains(auth, `oauth_version="1.0"`) {
		t.Errorf(`oauth_version present but not "1.0": %s`, auth)
	}
}

// TestNewHTTPClient_EmptyCredentials は空 Credentials でも Client が作成され、
// リクエストに Authorization ヘッダが付与されることを確認する (値は意味なし)。
func TestNewHTTPClient_EmptyCredentials(t *testing.T) {
	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := xapi.NewHTTPClient(context.Background(), &config.Credentials{})
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	_ = resp.Body.Close()

	auth := captured.Get("Authorization")
	if !strings.HasPrefix(auth, "OAuth ") {
		t.Fatalf("expected OAuth scheme even with empty credentials, got %q", auth)
	}
}

// TestNewHTTPClient_NilCredentials は nil 入力でも panic せず Client を返すことを確認する。
func TestNewHTTPClient_NilCredentials(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("NewHTTPClient(nil) panicked: %v", r)
		}
	}()
	client := xapi.NewHTTPClient(context.Background(), nil)
	if client == nil {
		t.Fatalf("NewHTTPClient(nil) returned nil")
	}
}

// TestNewHTTPClient_DifferentNoncePerRequest は同一 Client から複数リクエストを
// 送ると oauth_nonce / oauth_timestamp / oauth_signature がリクエストごとに
// 再計算されることを確認する (replay 防止)。
func TestNewHTTPClient_DifferentNoncePerRequest(t *testing.T) {
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	creds := &config.Credentials{APIKey: "ck", APISecret: "cs", AccessToken: "at", AccessTokenSecret: "ats"}
	client := xapi.NewHTTPClient(context.Background(), creds)
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("client.Do[%d]: %v", i, err)
		}
		// time.Sleep を入れず nonce のみで差分が出ることを期待
		_ = resp.Body.Close()
	}
	if len(auths) != 2 {
		t.Fatalf("expected 2 authorizations, got %d", len(auths))
	}
	if auths[0] == auths[1] {
		t.Errorf("Authorization headers should differ between requests due to nonce/timestamp: %q", auths[0])
	}
}

// TestNewHTTPClient_RealRequestPath は path 付き URL でも Authorization ヘッダが
// 正しく付与されること (署名 base string にパスを含めても破綻しないこと) を確認する。
func TestNewHTTPClient_RealRequestPath(t *testing.T) {
	var capturedPath string
	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	creds := &config.Credentials{APIKey: "ck", APISecret: "cs", AccessToken: "at", AccessTokenSecret: "ats"}
	client := xapi.NewHTTPClient(context.Background(), creds)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/2/users/me", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	_ = resp.Body.Close()
	if capturedPath != "/2/users/me" {
		t.Errorf("path = %q, want /2/users/me", capturedPath)
	}
	if !strings.HasPrefix(capturedAuth, "OAuth ") {
		t.Errorf("Authorization scheme broken: %q", capturedAuth)
	}
}
```

## TDD ステップ

Red → Green → Refactor を以下の順で進める。

### Step 1 (Red): 失敗テスト追加
1. `internal/xapi/` ディレクトリ作成
2. `internal/xapi/oauth1_test.go` 上記 7 ケースを記述
3. `go test ./internal/xapi/...` → コンパイルエラー (`xapi` パッケージ未定義) で失敗を確認

### Step 2 (Green): 依存追加と最小実装
1. `go get github.com/dghubble/oauth1@v0.7.3`
2. `internal/xapi/oauth1.go` を上記コード通り実装
3. `go test -race -count=1 ./internal/xapi/...` → 全 7 ケース pass を確認
4. `go test -race -count=1 ./...` → 既存 50+ テストと合わせて regression なし確認

### Step 3 (Refactor): 品質チェック
1. `golangci-lint run ./...` → 違反 0
2. `go vet ./...` → 警告 0
3. `gofumpt -l .` → 差分 0 (フォーマット確認、必要なら `gofumpt -w internal/xapi/`)
4. doc コメントレビュー: パッケージ doc / 全公開シンボルに日本語コメントあるか

## 検証コマンド (DoD)

```bash
cd /Users/youyo/src/github.com/youyo/x

# 1. テスト全 pass
go test -race -count=1 ./...

# 2. 静的解析 0 違反
golangci-lint run ./...
go vet ./...

# 3. ビルド成功 (M6 以降の同パッケージ追加に備える)
go build ./...

# 4. 単体検証 (M5 範囲)
go test -race -count=1 -run TestNewOAuth1Config ./internal/xapi/
go test -race -count=1 -run TestNewHTTPClient ./internal/xapi/
```

## リスク

| # | リスク | 影響 | 対策 |
|---|---|---|---|
| R-1 | dghubble/oauth1 v0.7.3 の API が想定と異なる (e.g. `Config.Client` のシグネチャ変更) | 中 | WebFetch で README を確認済み (NewConfig / NewToken / Config.Client 仕様一致)。go.mod に @v0.7.3 で pin。 |
| R-2 | nil creds 受け入れ方針が呼び出し側の意図と齟齬 | 低 | doc コメントに明示。M9 で「実認証失敗時 exit 3」を別途実装。 |
| R-3 | golangci-lint v2 で `oauth1.NoContext` の使用が deprecated 警告 | 低 | 本実装では NoContext を使わず context.Context 引数を受け取る設計。 |
| R-4 | パッケージ doc コメントの重複違反 (M6 で client.go 追加時) | 中 | 本計画 D-3 に明記。M6 のハンドオフで「oauth1.go に既に存在する」と伝達。 |
| R-5 | httptest サーバーへの実リクエストで TLS 関連の挙動が混入 | 低 | httptest.NewServer (HTTP) のみ使用、NewTLSServer は使わない。 |
| R-6 | dghubble/oauth1 が内部で生成する nonce が同一時刻リクエストで衝突 | 低 | テストでは for-loop による連続実行で nonce 差異を確認 (TestNewHTTPClient_DifferentNoncePerRequest)。実装は dghubble 任せ。 |

## ロールバック手順

実装に失敗した場合:
1. `git checkout -- internal/xapi/ go.mod go.sum` で差分破棄
2. `go mod tidy` で依存掃除
3. 計画を見直し (本ファイルに修正履歴を追加)

## ハンドオフ (M6 への引き継ぎ事項)

- `internal/xapi/oauth1.go` の冒頭に**パッケージ doc コメント済み** → M6 で `client.go` を作るときは package コメントを書かないこと
- `NewHTTPClient(ctx, creds) *http.Client` が M6 の HTTP client base になる
  - retry / rate-limit middleware は `http.RoundTripper` を wrap する形で client.Transport に注入する設計を推奨
- `context.Context` の意味: dghubble/oauth1 内部の transport カスタマイズ用 (`oauth2.HTTPClient`)
  - M6 で `client.Do(req.WithContext(reqCtx))` のように request scope の ctx を使う際は、`NewHTTPClient` 構築時の ctx と別物として扱う
- 依存: `github.com/dghubble/oauth1 v0.7.3` 追加済み (M6 では追加不要)

## 完了定義 (Definition of Done)

- [ ] `internal/xapi/oauth1.go` が上記設計通りに存在
- [ ] `internal/xapi/oauth1_test.go` の 7 ケース全 pass
- [ ] `go test -race -count=1 ./...` 全 pass
- [ ] `golangci-lint run ./...` 違反 0
- [ ] `go vet ./...` 警告 0
- [ ] パッケージ doc コメント 1 箇所 (oauth1.go)
- [ ] 全公開シンボル (`NewOAuth1Config`, `NewHTTPClient`) に日本語 doc コメント
- [ ] `go.mod` に `github.com/dghubble/oauth1 v0.7.3` が記録
- [ ] Conventional Commits 日本語コミット (feat(xapi): OAuth 1.0a 署名ラッパー (dghubble/oauth1) を追加)
- [ ] コミットメッセージに `Plan: plans/x-m05-xapi-oauth1.md` フッター
