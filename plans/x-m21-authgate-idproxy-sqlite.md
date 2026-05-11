# M21: authgate idproxy sqlite store 実装計画

## 概要

`STORE_BACKEND=sqlite` モードを実装する。`github.com/youyo/idproxy/store/sqlite`
(internal: `modernc.org/sqlite`、pure Go) を用いた薄いラッパーを authgate 層に追加し、
M20 で確立した `WithIDProxyStore(idproxy.Store)` Option 経由で
`internal/authgate/gate.go` に差し込む。本マイルストーンは M20 と同じパターンで、
**gate.go / idproxy.go の本体には触れない**。

- **対象**:
  - `internal/authgate/store_sqlite.go` (新規)
  - `internal/authgate/store_sqlite_test.go` (新規)
  - `internal/authgate/doc.go` (修正: M21 反映)
  - `go.mod` / `go.sum` (`github.com/youyo/idproxy/store/sqlite` + transitive `modernc.org/sqlite` 追加)
- **スペック根拠**:
  - §5 architecture: `internal/authgate/store_sqlite.go` (modernc.org/sqlite, ローカル開発向け)
  - §10 セキュリティ: credentials 系ファイルは perm 0600
  - §11 環境変数: `STORE_BACKEND=sqlite` / `SQLITE_PATH` (default: `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db`)
- **roadmap**: M21 (Phase F)

## 設計方針

### 1. authgate.NewSQLiteStore(path) は path:string のみを受ける

**結論: 公開 API は `func NewSQLiteStore(path string) (idproxy.Store, error)` の
1 シグネチャに統一する。XDG 環境変数の解決やデフォルトパスの組み立ては CLI 層 (M24) の
責務とする。**

- spec §11 SQLITE_PATH のデフォルトは `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db`
  だが、これは CLI が viper/env を組み立てる際に解決する値。authgate 層は受け取った
  パス文字列を `idproxy/store/sqlite.New(path)` に渡すだけの薄いラッパーに徹する
- M20 ハンドオフに従い「authgate.NewSQLiteStore(path) は path:string を受けるだけ」を
  維持。XDG/ホーム展開はやらない
- 空文字 `path` は `ErrSQLitePathRequired` で reject (誤検知防止: `:memory:` は
  テスト用途として明示的に許可するため、空文字とそれ以外を区別する)

### 2. perm 0600 の強制 (spec §10, WAL サイドカー含む)

**結論: `idproxy/store/sqlite.New(path)` が成功した直後に、メインの `path` に加えて
WAL モードで生成されるサイドカー (`<path>-wal`, `<path>-shm`) も 0600 に締め直す。
`:memory:` の場合は skip する。**

理由:
- spec §10 「起動時に credentials.toml のパーミッションを検査し、`0600` でなければ
  警告」とあり、SQLite DB ファイルもセッショントークン・認証コードを格納する
  シークレット相当のため、credentials.toml と同等の扱いとする
- `idproxy/store/sqlite` の `New` は DSN で `_pragma=journal_mode(WAL)` を有効化
  しているため、初回書き込み (`ExecContext(schema)`) 後にサイドカーファイル
  (`<path>-wal` / `<path>-shm`) が生成される。WAL ファイルには未チェックポイント
  状態のセッションデータが平文で含まれるため、メインの `.db` だけ 0600 にしても
  穴が空く (advisor 指摘事項)
- `idproxy/store/sqlite` の `New` 内部では `os.Chmod` を実行しておらず、初期作成時
  に OS umask 依存となる。authgate 層で明示的に 3 ファイル全てに 0600 を保証する
- 既存ファイルが緩い権限 (例: 0644) で残っていた場合も、起動時に 0600 へ強制締め直す
- Windows / 非 POSIX 環境では `os.Chmod` は限定的にしか機能しないが、エラーとせず
  best-effort で続行する (spec §10 の「POSIX のみ」表記に整合)
- WAL/SHM ファイルがまだ生成されていない (lazy 生成) 場合は ENOENT で no-op になる
  ため、エラーを無視するだけで十分

実装手順:
```go
s, err := sqlitestore.New(path)
if err != nil {
    return nil, fmt.Errorf("authgate: open sqlite store: %w", err)
}
if path != ":memory:" {
    // POSIX: best-effort で 0600 へ。WAL モードの -wal / -shm サイドカーも対象。
    // 存在しないファイルや Windows での失敗は ENOENT/EPERM を返すが致命的でないため無視する。
    for _, p := range []string{path, path + "-wal", path + "-shm"} {
        _ = os.Chmod(p, 0o600)
    }
}
return s, nil
```

**判断**: chmod 失敗時の挙動は **静かに無視** (=best-effort)。spec §10 が
「警告」止まりであり、また `idproxy.Store` 自体はファイル権限を知らないため、
authgate 層から logger を呼ばず err も無視する。これにより Windows での運用が
スムーズになる。代わりに `doc.go` で「POSIX 環境では起動時にメイン DB ファイルおよび
WAL サイドカー (`-wal` / `-shm`) を 0600 へ chmod する」旨を明文化する。

### 3. 親ディレクトリの自動作成

**結論: `path` の親ディレクトリが存在しない場合、`os.MkdirAll(parent, 0o700)` で
自動作成する。**

理由:
- spec §11 のデフォルトパス `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db` は
  初回起動時に `~/.local/share/x/` が存在しない可能性が高い
- credentials.toml と同様に親ディレクトリは 0700 で作成する (spec §11 「ディレクトリを
  `0700`、ファイルを `0600` で作成」に整合)
- 既存ディレクトリの権限は変更しない (利用者が意図的に 0755 にしている可能性を尊重)
- `:memory:` は親ディレクトリ計算自体を skip
- `path` が絶対/相対両方を許容する (`filepath.Dir` で自然に処理可能)
- 親ディレクトリ作成失敗 (権限不足等) は `ErrSQLitePathRequired` ではなく
  `fmt.Errorf("authgate: create sqlite parent dir: %w", err)` でラップして返す

### 4. スキーマ初期化は idproxy 側に委譲

`idproxy/store/sqlite.New(path)` 内部で `CREATE TABLE IF NOT EXISTS` を実行するため、
authgate 層では migrate を呼ばない。M21 では idproxy v0.4.2 の現行スキーマ
(sessions / auth_codes / access_tokens / refresh_tokens / family_revocations /
clients の 6 テーブル) をそのまま利用する。

### 5. cleanup goroutine の管理

`idproxy/store/sqlite.New` は内部で 5 分間隔の cleanup goroutine を起動する。
ラッパー側で `Close()` を呼べば停止されるため、特別な対応は不要。

`NewWithCleanupInterval(path, 0)` を使えば cleanup を完全に無効化できるが、本番
用途では不要 (M21 のスコープ外)。テストでは `:memory:` を使うため通常の `New` で
問題ない (テスト後の `Close()` で goroutine も終了)。

### 6. エラー設計

新規 sentinel エラー:

| エラー | 用途 |
|--------|------|
| `ErrSQLitePathRequired` | `path == ""` を弾く |

既存パターン (`ErrIDProxyConfigInvalid` 等) と同じく `var Err... = errors.New(...)` を
パッケージ変数として公開し、`errors.Is` で判定可能にする。

親ディレクトリ作成失敗・sqlite open 失敗は sentinel を持たず、`fmt.Errorf` で
ラップして返す (M19/M20 と同じ方針: 致命的なエラーは sentinel を出さずにラップ)。

### 7. doc.go の更新

`internal/authgate/doc.go` の以下を修正:
- M20 までで 3 モード全実装済み → "memory / sqlite の 2 store backend を実装済み、
  redis / dynamodb は M22–M23 で順次追加" に書き換え

## TDD: テストケース一覧

ファイル: `internal/authgate/store_sqlite_test.go`、`package authgate_test`、table-driven 中心。

| # | テスト名 | 入力 | 期待 | 検証要素 |
|---|---------|------|------|---------|
| 1 | `TestNewSQLiteStore_EmptyPath` | path="" | `errors.Is(err, ErrSQLitePathRequired)` | 空 path reject |
| 2 | `TestNewSQLiteStore_ImplementsStore` | path=t.TempDir()+"x.db" | non-nil + interface 適合 | コンパイル時 + runtime 検証 |
| 3 | `TestNewSQLiteStore_CreatesParentDir` | path=t.TempDir()+"nested/sub/x.db" | err==nil + ディレクトリ存在 + DB ファイル存在 | 親ディレクトリ自動作成 |
| 4 | `TestNewSQLiteStore_SetsPerm0600` | path=t.TempDir()+"x.db" | main DB と `-wal` / `-shm` (存在すれば) の `os.Stat().Mode().Perm() == 0o600` | perm 0600 強制 (POSIX only, runtime.GOOS で skip)。サイドカーは存在チェック後に assert |
| 5 | `TestNewSQLiteStore_OverridesLoosePerm` | 事前に 0o644 で空ファイル作成 → NewSQLiteStore | perm 0600 に締め直される | 既存ファイルの権限緊縮 (POSIX only) |
| 6 | `TestNewSQLiteStore_PersistsAcrossReopen` | (a) Set → Close、(b) New 再オープン → Get | 同じ session が取得できる | 永続性検証 (file path 使用) |
| 7 | `TestNewSQLiteStore_BasicSetGet` | NewSQLiteStore → Set → Get | session 取得成功 | sanity check (薄ラッパーの呼び出し方向) |
| 8 | `TestNewSQLiteStore_MemoryPath` | path=":memory:" | err==nil + Set/Get 成功 | `:memory:` の特殊扱い (chmod skip + 親ディレクトリ skip) |

### 補足: Windows での扱い

- テスト #4, #5 は `runtime.GOOS == "windows"` の場合 `t.Skip("POSIX only")` で skip。
- それ以外のテストはクロスプラットフォームで動く (sqlite ファイル作成自体は問題なし)。

### 補助関数

`idproxyStoreCompileCheck` は store_memory_test.go ですでに定義済み。同じ
`package authgate_test` 内なので再利用する (重複定義しない)。

## 実装手順 (TDD)

### Red フェーズ
1. `internal/authgate/store_sqlite_test.go` を書く (上記 8 ケース)
2. `go test ./internal/authgate/...` で全テスト失敗を確認

### Green フェーズ
3. `go get github.com/youyo/idproxy/store/sqlite` で依存追加
   - transitive で `modernc.org/sqlite` が入る (pure Go, CGO 不要)
4. `internal/authgate/store_sqlite.go` を実装
   ```go
   package authgate

   import (
       "errors"
       "fmt"
       "os"
       "path/filepath"

       "github.com/youyo/idproxy"
       sqlitestore "github.com/youyo/idproxy/store/sqlite"
   )

   // ErrSQLitePathRequired は NewSQLiteStore に空のパスを渡した場合に返るエラー。
   var ErrSQLitePathRequired = errors.New("authgate: sqlite path is required")

   // NewSQLiteStore は idproxy.Store の sqlite 実装を返す。
   //
   // (詳細な日本語 doc コメント...)
   func NewSQLiteStore(path string) (idproxy.Store, error) {
       if path == "" {
           return nil, ErrSQLitePathRequired
       }
       if path != ":memory:" {
           if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
               return nil, fmt.Errorf("authgate: create sqlite parent dir: %w", err)
           }
       }
       s, err := sqlitestore.New(path)
       if err != nil {
           return nil, fmt.Errorf("authgate: open sqlite store: %w", err)
       }
       if path != ":memory:" {
           // POSIX: best-effort で 0600 へ。WAL サイドカー (-wal / -shm) も含む。
           // 存在しないファイルや Windows の失敗は無視。
           for _, p := range []string{path, path + "-wal", path + "-shm"} {
               _ = os.Chmod(p, 0o600)
           }
       }
       return s, nil
   }
   ```
5. `go test ./internal/authgate/...` でテスト全 pass を確認

### Refactor フェーズ
6. doc.go を更新 (M21 反映)
7. `golangci-lint run ./...` で 0 issues 確認
8. `go vet ./...` 確認
9. `go test -race -count=1 ./...` 全 pass 確認
10. `go build -o /tmp/x ./cmd/x` 成功確認

## 品質ゲート

- [ ] `go test -race -count=1 ./...` 全 pass
- [ ] `golangci-lint run ./...` 0 issues
- [ ] `go vet ./...` clean
- [ ] `go build -o /tmp/x ./cmd/x` 成功
- [ ] 全公開シンボル (`NewSQLiteStore`, `ErrSQLitePathRequired`) に日本語 doc コメント
- [ ] パッケージ doc は doc.go 1 ファイルに集約 (store_sqlite.go にパッケージ doc を書かない)
- [ ] perm 0600 が POSIX で確実に設定されることを `os.Stat` で確認
- [ ] 永続性 (再オープンで session 復元) が確認できる
- [ ] Windows テストは適切に skip される (runtime.GOOS チェック)

## リスク と対策

| リスク | 影響 | 対策 |
|--------|------|------|
| `modernc.org/sqlite` のクロスコンパイルでビルド時間が長い | 中 | logvalet で実績あり。CI で wait 増加は許容範囲 |
| Windows での perm 0600 が機能しない | 低 | spec §10「POSIX のみ」で明文化済み。テストは runtime.GOOS で skip |
| `os.Chmod` の chmod 失敗を握りつぶす設計 | 低 | doc.go に「best-effort」と明記。将来 logger 配線時に warning 出力を検討 |
| `idproxy/store/sqlite` のスキーマが将来変更される | 中 | idproxy バージョンを go.mod で pinned。upstream の breaking change は dependabot で検知 |
| sqlite cleanup goroutine が test ハングの原因になる | 低 | テスト後の `Close()` で停止する仕様を確認済み。`t.Cleanup` で必ず呼ぶ |
| transitive で `modernc.org/sqlite` の依存が増える (ビルドサイズ) | 低 | distroless image でも問題ないことを logvalet で確認済み |

## 完了条件

1. `internal/authgate/store_sqlite.go` + テスト 8 ケースが pass
2. 既存テスト (M1-M20) も全 pass、回帰なし
3. `golangci-lint run ./...` 0 issues、`go vet` clean
4. `git commit` メッセージ: `feat(authgate): idproxy sqlite store backend を追加`
   フッター: `Plan: plans/x-m21-authgate-idproxy-sqlite.md`
5. doc.go の M21 反映

## M22 へのハンドオフ

- `NewSQLiteStore(path)` と同じパターンで `NewRedisStore(url string)` を追加
- redis 公開 API は `github.com/youyo/idproxy/store/redis` の `New(Options)` 形式
  (logvalet `mcp_auth.go` で使用例あり)
- `REDIS_URL` (例: `redis://localhost:6379/0`) を `redisstore.Options{Addr, Password, DB, TLS, KeyPrefix}` に
  分解するパース処理が必要 (CLI 層 M24 の責務とするか authgate 層に置くかは M22 で判断)
- TTL は idproxy 側が SET EX 経由で自動付与するので追加実装不要
- 接続テストは CI に redis コンテナを足すか、go-redis のテスト用 miniredis を使うか M22 で決定
