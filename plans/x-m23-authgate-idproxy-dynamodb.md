# Plan: M23 — authgate idproxy dynamodb store

> Layer 2: マイルストーン詳細計画。
> 親ロードマップ: [plans/x-roadmap.md](./x-roadmap.md) §M23
> M22 (redis) ハンドオフ準拠。spec §5 / §10 / §11 (`STORE_BACKEND=dynamodb` / `DYNAMODB_TABLE_NAME` / `AWS_REGION`) 反映。

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M23: authgate idproxy dynamodb store backend |
| 親ロードマップ | plans/x-roadmap.md |
| ステータス | Approved / 実装フェーズ着手可 |
| 作成日 | 2026-05-12 |
| 想定コミット粒度 | 1 コミット (`feat(authgate): idproxy dynamodb store backend を追加`) |
| 前マイルストーン | M22 (redis store, commit: 491bb30) |
| 後続マイルストーン | M24 (CLI x mcp + E2E) |

## ゴール

- spec §11 の環境変数 `STORE_BACKEND=dynamodb` / `DYNAMODB_TABLE_NAME` / `AWS_REGION` に対応する authgate 層のヘルパ `authgate.NewDynamoDBStore(tableName, region string) (idproxy.Store, error)` を提供する。
- 上流 `github.com/youyo/idproxy/store.NewDynamoDBStore(tableName, region)` の薄いラッパーとし、`config.LoadDefaultConfig` / `dynamodb.NewFromConfig` などの AWS SDK plumbing は **上流に委譲する**。
- 空 `tableName` / 空 `region` は `errors.Is` で識別可能な sentinel で弾く (defaulting は CLI 層 M24 の責務)。
- `gate.go` の `WithIDProxyStore(store idproxy.Store)` Option 経由で接続するため、`gate.go` と `idproxy.go` は変更しない。
- TDD: Red → Green → Refactor。テストは AWS API 接続不要 (credential lazy 解決を活用し、credential ダミー env で実 API call を発生させない) 。

## 非ゴール / 制約

- **`aws-sdk-go-v2` を direct 依存に格上げしない**。authgate は idproxy 経由で間接的にしか触れないため、go.mod 上の indirect 状態を維持する。
- **`fakeDynamoDBClient` / smithy-go MockClient を authgate に置かない**。上流 `idproxy/store/dynamodb_test.go` で既にカバー済みの責務であり、authgate 側で再実装すると無駄な複雑性となる。
- **DynamoDB Local (docker) や LocalStack の docker 統合テストは追加しない**。authgate 層の役割は薄ラッパー + sentinel 提供のみで、ストア本体の動作検証は上流の conformance test に委ねる。spec §11 の E2E は M24 で評価する。
- `SetSession` / `GetSession` 等の実通信テストは authgate 層では実行しない (実 API call は credential & AWS account が必要)。

## 影響範囲

### 追加ファイル

| ファイル | 役割 |
|---|---|
| `internal/authgate/store_dynamodb.go` | `NewDynamoDBStore(tableName, region) (idproxy.Store, error)`, `ErrDynamoDBTableRequired`, `ErrDynamoDBRegionRequired` |
| `internal/authgate/store_dynamodb_test.go` | TDD: 空 args / 正常 args / `idproxy.Store` interface 適合 |

### 変更ファイル

| ファイル | 変更内容 |
|---|---|
| `internal/authgate/doc.go` | M23 反映: dynamodb store の概要・利用例追記、「4 store backend 完成」を明示 |

### 変更しないファイル

| ファイル | 理由 |
|---|---|
| `internal/authgate/gate.go` | `WithIDProxyStore` Option 経由で接続するため変更不要 |
| `internal/authgate/idproxy.go` | 同上 |
| `go.mod` / `go.sum` | `aws-sdk-go-v2/*` は idproxy v0.4.2 経由の indirect 依存として既に登録済み (go.mod の indirect ブロックを参照)。direct 格上げは不要 |

## 設計詳細

### 公開 API

```go
// internal/authgate/store_dynamodb.go (抜粋イメージ)

// ErrDynamoDBTableRequired は NewDynamoDBStore に空の tableName を渡した場合に
// 返るエラー。errors.Is で判定すること。
var ErrDynamoDBTableRequired = errors.New("authgate: dynamodb table name is required")

// ErrDynamoDBRegionRequired は NewDynamoDBStore に空の region を渡した場合に
// 返るエラー。errors.Is で判定すること。
var ErrDynamoDBRegionRequired = errors.New("authgate: dynamodb region is required")

// NewDynamoDBStore は idproxy/store.NewDynamoDBStore を呼んで dynamodb 実装を
// 返す薄いラッパーである。
func NewDynamoDBStore(tableName, region string) (idproxy.Store, error) {
    if tableName == "" {
        return nil, ErrDynamoDBTableRequired
    }
    if region == "" {
        return nil, ErrDynamoDBRegionRequired
    }
    s, err := store.NewDynamoDBStore(tableName, region)
    if err != nil {
        return nil, fmt.Errorf("authgate: open dynamodb store: %w", err)
    }
    return s, nil
}
```

### 設計の根拠

1. **薄ラッパー戦略**: `store_memory.go` (1 行委譲) と同レベルの薄さを維持。`store_sqlite.go` の chmod / mkdir のような OS 固有処理は dynamodb には不要。
2. **空 args 早期 reject**: spec §11 で `STORE_BACKEND=dynamodb` 時の defaulting は CLI 層の責務。authgate 層は空文字を sentinel で識別可能に弾く責務に徹する。
3. **credential lazy 解決の活用**: `config.LoadDefaultConfig` は credential を解決せず config 構造体だけ返す (実 credential 取得は最初の API call 時)。これによりテストは「コンストラクタが non-nil store を返す」までを credential 不要で検証できる。
4. **TTL は上流で実装済み**: idproxy/store/dynamodb は DynamoDB ネイティブ TTL + Get 時の二重チェックを実装済み (源: `/Users/youyo/pkg/mod/github.com/youyo/idproxy@v0.4.2/store/dynamodb.go:101-194`)。authgate 側で追加実装しない。
5. **ConsistentRead も上流で実装済み**: session / authcode の Get は ConsistentRead=true 固定 (源: 同 dynamodb.go:250, 301)。

## テスト戦略

### TDD: Red → Green → Refactor

| # | テスト | Red (失敗確認) | Green (実装) | 検証内容 |
|---|---|---|---|---|
| T1 | `TestNewDynamoDBStore_EmptyTableName` | 関数未実装 → コンパイルエラー | sentinel + 早期 return | `errors.Is(err, ErrDynamoDBTableRequired)` |
| T2 | `TestNewDynamoDBStore_EmptyRegion` | 同上 | sentinel + 早期 return | `errors.Is(err, ErrDynamoDBRegionRequired)` |
| T3 | `TestNewDynamoDBStore_ImplementsStore` | 同上 | `store.NewDynamoDBStore` 委譲 | non-nil & `idproxyStoreCompileCheck` 適合 |

advisor フィードバック反映: 「両方空 args の sentinel 優先順位テスト」は API 契約として過剰特定なので含めない (将来 joined error 化等で容易に壊れる)。 T1/T2 が個別に sentinel マッチを検証していれば十分。

### テスト環境の隔離

`t.Setenv` で開発者ローカルの SSO profile / IMDS lookup を遮断し、CI / ローカル環境差を吸収する。SDK v2 では shared config が常時ロードされるため、最低限以下の 3 つで足りる:

```go
t.Setenv("AWS_ACCESS_KEY_ID", "test")
t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
```

`AWS_PROFILE=""` や `AWS_SDK_LOAD_CONFIG=false` は SDK v2 では no-op / 曖昧挙動のため含めない (advisor フィードバック反映)。

実機確認済み: 上記設定下で `store.NewDynamoDBStore("x-test", "us-east-1")` は ~11ms で non-nil store を返す (探索テストで検証済み、本実装には残さない)。

### テスト構造 (store_memory_test.go を踏襲)

```go
// internal/authgate/store_dynamodb_test.go
package authgate_test

import (
    "errors"
    "testing"

    "github.com/youyo/x/internal/authgate"
)

func setDynamoDBTestEnv(t *testing.T) {
    t.Helper()
    // 開発者ローカルの SSO profile / IMDS lookup を遮断する最低限の env 3 つ。
    // SDK v2 では AWS_PROFILE="" / AWS_SDK_LOAD_CONFIG=false は no-op のため含めない。
    t.Setenv("AWS_ACCESS_KEY_ID", "test")
    t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
    t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}

func TestNewDynamoDBStore_EmptyTableName(t *testing.T) {
    t.Parallel()
    s, err := authgate.NewDynamoDBStore("", "us-east-1")
    if err == nil {
        if s != nil {
            _ = s.Close()
        }
        t.Fatal("expected error, got nil")
    }
    if !errors.Is(err, authgate.ErrDynamoDBTableRequired) {
        t.Fatalf("err = %v, want errors.Is ErrDynamoDBTableRequired", err)
    }
    if s != nil {
        t.Fatalf("returned non-nil store: %v", s)
    }
}

func TestNewDynamoDBStore_EmptyRegion(t *testing.T) {
    t.Parallel()
    s, err := authgate.NewDynamoDBStore("x-test", "")
    if err == nil {
        if s != nil {
            _ = s.Close()
        }
        t.Fatal("expected error, got nil")
    }
    if !errors.Is(err, authgate.ErrDynamoDBRegionRequired) {
        t.Fatalf("err = %v, want errors.Is ErrDynamoDBRegionRequired", err)
    }
}

func TestNewDynamoDBStore_ImplementsStore(t *testing.T) {
    t.Parallel()
    setDynamoDBTestEnv(t)

    s, err := authgate.NewDynamoDBStore("x-test", "us-east-1")
    if err != nil {
        t.Fatalf("NewDynamoDBStore failed: %v", err)
    }
    if s == nil {
        t.Fatal("NewDynamoDBStore returned nil store")
    }
    t.Cleanup(func() {
        _ = s.Close()
    })

    _ = idproxyStoreCompileCheck(s)
}
```

### 既存 `idproxyStoreCompileCheck` ヘルパーの再利用

`store_memory_test.go:34` で定義済みのため、同じ `package authgate_test` 内で再利用できる (redis / sqlite のテストと同じパターン)。

## 実装手順 (TDD)

1. **Red 1**: テストファイル `store_dynamodb_test.go` を最初に書く。`authgate.NewDynamoDBStore` / `authgate.ErrDynamoDBTableRequired` / `authgate.ErrDynamoDBRegionRequired` が未定義のためコンパイルエラー。`go test ./internal/authgate/...` で確認。
2. **Green 1 (T1, T2)**: `store_dynamodb.go` に sentinel 2 つと、空 args チェックのみの最小実装 (`store.NewDynamoDBStore` 呼び出しは未実装でもよい)。テスト T1-T2 が pass することを確認。
3. **Green 2 (T3)**: `store.NewDynamoDBStore` への委譲を追加して T3 を pass。
4. **Refactor**: doc コメントの日本語化、エラーラップメッセージ統一、import の整理。
5. **doc.go 更新**: 既存の future-tense 文 2 か所 (`doc.go:9-11` の「dynamodb は M23 で追加する」、`doc.go:17-20` の M21/M22 の実装履歴と「dynamodb は M23 で追加する」) を M23 完了後の文言に書き換える。`[NewDynamoDBStore]` リンクを追記し、「4 store backend 完成」を明記する。

## 完了条件 (Acceptance Criteria)

| # | 確認項目 | 検証方法 |
|---|---|---|
| C1 | `go test -race -count=1 ./...` が全 pass | コマンド実行 |
| C2 | `golangci-lint run ./...` が 0 issues | コマンド実行 |
| C3 | `go vet ./...` が clean | コマンド実行 |
| C4 | `go build -o /tmp/x ./cmd/x` が成功 | コマンド実行 |
| C5 | `NewDynamoDBStore("", "r")` で `ErrDynamoDBTableRequired` を sentinel として返す | T1 |
| C6 | `NewDynamoDBStore("t", "")` で `ErrDynamoDBRegionRequired` を sentinel として返す | T2 |
| C7 | 正常 args で non-nil `idproxy.Store` を返す | T3 |
| C8 | go.mod の `aws-sdk-go-v2/*` が direct に格上げされていない (indirect 維持) | `cat go.mod` で確認 |
| C9 | `gate.go` / `idproxy.go` が無変更 | `git diff` で確認 |
| C10 | doc.go の future-tense 文 2 か所 (M22 時点の「dynamodb は M23 で追加する」) が M23 完了後の文言に書き換えられ、`[NewDynamoDBStore]` リンクが追加され、「4 store backend 完成」が明記される | レビュー |

## リスクと対応

| リスク | 影響 | 緩和策 |
|---|---|---|
| `config.LoadDefaultConfig` が CI で AWS profile を探索して遅延 / エラー | 中 | `setDynamoDBTestEnv` ヘルパーで env を完全に分離。事前 probe (~11ms) 済み |
| 上流 `idproxy/store.NewDynamoDBStore` API が将来変わる | 低 | 上流 v0.4.2 に pin、変更時は go.mod の version bump で吸収。テスト T4 が壊れれば早期検知 |
| go.mod の `aws-sdk-go-v2` indirect が将来 direct 化を求められる | 低 | M24 で CLI 層が直接 SDK を import する場合のみ direct 化 (本マイルストーンの範囲外) |
| logvalet と同じ `store.NewDynamoDBStore` を使うが、x プロジェクトの想定環境差 | 低 | logvalet の `mcp_auth.go:51` で実績あり (同一 API、同一引数) |

## ハンドオフ (M24 へ)

- 4 つの store backend が出揃った: `NewMemoryStore()` / `NewSQLiteStore(path)` / `NewRedisStore(url)` / `NewDynamoDBStore(table, region)`。
- M24 の CLI 層 (`internal/cli/mcp.go` 等) はこの 4 関数を `STORE_BACKEND` 環境変数で switch するだけ。logvalet の `internal/cli/mcp_auth.go:46-77` がほぼそのまま参考になる (`buildIDProxyStore` パターン)。
- spec §11 環境変数: `STORE_BACKEND` / `SQLITE_PATH` / `REDIS_URL` / `DYNAMODB_TABLE_NAME` / `AWS_REGION` の defaulting / 検証は M24 で実装。
- 各 store ヘルパーは `idproxy.Store` を返すので `WithIDProxyStore(s)` Option で `authgate.New` に注入する。
- E2E テスト (LocalStack / DynamoDB Local 利用、または mock) は M24 のスコープ。

## 参考

- 上流実装: `/Users/youyo/pkg/mod/github.com/youyo/idproxy@v0.4.2/store/dynamodb.go`
- logvalet 統合例: `/Users/youyo/src/github.com/youyo/logvalet/internal/cli/mcp_auth.go:46-77`
- 既存薄ラッパー: `internal/authgate/store_memory.go` (1 行委譲)、`internal/authgate/store_redis.go` (URL パース)、`internal/authgate/store_sqlite.go` (chmod / mkdir 強化)
- spec §11 環境変数表: `docs/specs/x-spec.md` 399-403 行
