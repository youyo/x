# M09 詳細計画: CLI `x me` + 認証情報ローダー + main.go の exit code 写像

> Layer 2: M9 マイルストーンの詳細実装計画。
> 親ロードマップ: [plans/x-roadmap.md](./x-roadmap.md)
> スペック: [docs/specs/x-spec.md](../docs/specs/x-spec.md) §6 `x me`, §11 設定優先順位 (env > credentials.toml > config.toml > default)

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M9 |
| 範囲 | `internal/cli/me.go` (新規), `internal/cli/me_test.go` (新規), `internal/cli/auth_loader.go` (新規), `internal/cli/auth_loader_test.go` (新規), `internal/cli/root.go` (AddCommand 追加), `cmd/x/main.go` (exit code 写像拡張) |
| 前提 | M8 (xapi.likes) commit `2272f0b` |
| 完了条件 | `httptest` mock で `x me` が JSON 出力 / `--no-json` で human-readable / 401→exit3 / 403→exit4 / 404→exit5 / 認証情報欠落→exit3、`go test -race ./...` 全 pass、`golangci-lint run` 0 issues |
| TDD | Red → Green → Refactor 厳守 |

## Scope

### In Scope

1. **`internal/cli/auth_loader.go`** (新規)
   - `ErrCredentialsMissing` 番兵エラー (`errors.Is` 可能、`xapi.ErrAuthentication` を Unwrap で内包し exit 3 に写像)
   - `LoadCredentialsFromEnvOrFile() (*config.Credentials, error)` — env > credentials.toml の優先順位ロジック (**ファイル単位**)
     - 4 つの env (`X_API_KEY`, `X_API_SECRET`, `X_ACCESS_TOKEN`, `X_ACCESS_TOKEN_SECRET`) が**すべて非空** → env のみから構築して即返却 (file は読まない)
     - 1 つでも欠ける → `config.DefaultCredentialsPath()` から `config.LoadCredentials()` で読み込んで返却
       - file から取得した creds のいずれかが空 → `ErrCredentialsMissing`
       - `errors.Is(loadErr, config.ErrCredentialsNotFound)` → `ErrCredentialsMissing` に置き換える
       - その他の file 読み込みエラー (パース失敗、権限読み取りエラー等) → そのまま wrap して返却
   - 内部ヘルパ: `credentialsFromEnv() (*config.Credentials, bool)` — 4 つ揃っていれば creds+true、不足なら部分 creds+false
   - 内部ヘルパ: `credentialsComplete(c *config.Credentials) bool` — 4 フィールドすべて非空かを判定

2. **`internal/cli/me.go`** (新規)
   - `newMeCmd() *cobra.Command` factory
   - フラグ: `--no-json` (default false = JSON 出力)
   - `RunE`:
     1. `LoadCredentialsFromEnvOrFile()` で認証情報取得
     2. `xapi.NewClient(cmd.Context(), creds)` で Client 生成
     3. `client.GetUserMe(cmd.Context())` で User 取得 — `WithUserFields` は M9 では渡さない (デフォルト 3 フィールドのみ取得 / spec §6 例 `{"id","username","name"}` と整合)
     4. `--no-json` なら `id=... username=... name=...` を 1 行で出力
     5. それ以外なら `{"id":"...","username":"...","name":"..."}` を `json.Encoder.Encode` で出力 (末尾改行付与)
   - 出力先: `cmd.OutOrStdout()`
   - エラーは wrap せずそのまま返す (cmd/x/main.go の run() で `xapi.ExitCodeFor` + 番兵 Unwrap が判定する)

3. **`internal/cli/root.go`** (既存ファイル編集)
   - `root.AddCommand(newMeCmd())` を 1 行追加

4. **`cmd/x/main.go`** (既存ファイル編集)
   - `run()` 内のエラー → exit code 写像を拡張
   - 判定順序 (上から優先):
     1. `isArgumentError(err)` → `app.ExitArgumentError` (2) — 既存
     2. `errors.Is(err, xapi.ErrAuthentication)` → `app.ExitAuthError` (3)
     3. `errors.Is(err, xapi.ErrPermission)` → `app.ExitPermissionError` (4)
     4. `errors.Is(err, xapi.ErrNotFound)` → `app.ExitNotFoundError` (5)
     5. fallback → `app.ExitGenericError` (1)
   - `ErrCredentialsMissing` は内部で `ErrAuthentication` を Unwrap するので 2. の経路で exit 3 に写像される

5. **テスト**

   `internal/cli/auth_loader_test.go`:
   - env 4 つ揃い、credentials.toml 不在 → env から取得成功 (file は読まれない)
   - env 4 つ揃い、credentials.toml も完備 → **env が勝ち、file は読まれない**
   - env 部分欠落 (3 つしか設定されていない)、credentials.toml 完備 → file から取得 (env で部分上書きはしない)
   - env 部分欠落、credentials.toml が完備でない (1 フィールド空) → `ErrCredentialsMissing`
   - env 部分欠落、credentials.toml が `config.ErrCredentialsNotFound` → `ErrCredentialsMissing`
   - env まったく無し、credentials.toml 完備 → ファイルから取得成功
   - すべて欠落 → `ErrCredentialsMissing`、`errors.Is(err, xapi.ErrAuthentication)` も true
   - credentials.toml のパースエラー → wrap してそのまま返却 (exit 1、`xapi.ErrAuthentication` は Unwrap しない)
   - env="" (空文字明示) → 未設定と同じ扱い (= 4 つ揃わなければ file fallback)
   - **環境変数の差し替えは `t.Setenv` を使用、XDG_DATA_HOME も `t.Setenv` + `t.TempDir()` で隔離 (テスト並列実行時の汚染防止)**
   - **`t.Parallel()` は env 操作するテストでは使わない** (`t.Setenv` 内部で要求される制約)

   `internal/cli/me_test.go`:
   - 成功パス (200 OK + JSON 出力)
     - httptest サーバを起動し、テスト用に `xapi.WithBaseURL` を渡すルートを確保するため、`newMeCmd()` の RunE が `xapi.NewClient()` を直接呼ぶ実装では BaseURL 差し替えが難しい
     - **方針**: `internal/cli/me.go` 内で `clientFactory` 変数 (`func(ctx context.Context, creds *config.Credentials) (meClient, error)`) をパッケージ private で持ち、テストでは `t.Cleanup` で差し替える。`meClient` は `GetUserMe(ctx, opts...) (*xapi.User, error)` の interface を最小定義 (本番実装は `*xapi.Client` がそのまま満たす)。
     - テストでは env を `t.Setenv` で詰めて認証通過させる
   - `--no-json` 出力 (human-readable `id=... username=... name=...` 行)
   - 認証情報欠落 → `ErrCredentialsMissing` 返却 (caller 側で exit 3 写像)
   - 401 → `ErrAuthentication`
   - 404 → `ErrNotFound`
   - help 出力に `--no-json` が含まれる
   - **`x me --help` で usage が標準出力に流れる** (Cobra デフォルト挙動の確認)

   `cmd/x/main.go` の単体テストは追加しない (既存 `main_test.go` がない設計を踏襲。`run()` の動作は `internal/cli/*_test.go` の Execute 経路で間接的にカバーされる)。エラー判定ロジックの単独テストは将来必要なら別マイルストーンで切り出す。

### Out of Scope

- `--json` フラグ (M9 では JSON がデフォルト、`--no-json` で人間可読 — version サブコマンドと同じ流儀)
- `--user-fields` カスタマイズ (M9 ではデフォルト 3 フィールドのみ、必要なら将来追加)
- `config.toml` (非機密設定) の参照 — M9 の `x me` には影響しないため対象外。M10/M11 で `[cli].output` 等を解釈する際に必要になるが、本 M では触れない
- `--config` / `--credentials` フラグ (M12 `x configure` で導入予定)

## Design Decisions

### D-1: `ErrCredentialsMissing` は `xapi.ErrAuthentication` を Unwrap で内包する

理由:
- `cmd/x/main.go` の `errors.Is(err, xapi.ErrAuthentication)` 1 ルートで exit 3 にマップできるため、`run()` の分岐がシンプルになる
- spec §6 で 401 と auth-error は同じ exit code 3 に分類される (CLI が認証情報を持っていない状態は 401 と同等の意味)
- メッセージは固定値 (どのフィールドが欠落しているかは出さず一般化、情報露出最小化、テスト stable)

実装:
```go
var ErrCredentialsMissing = fmt.Errorf("%w: X API credentials missing (set X_API_KEY/X_API_SECRET/X_ACCESS_TOKEN/X_ACCESS_TOKEN_SECRET env vars or credentials.toml)", xapi.ErrAuthentication)
```

`fmt.Errorf("%w", xapi.ErrAuthentication)` で Unwrap 連鎖を作る。エラーメッセージは

```
xapi: authentication failed (401): X API credentials missing (set X_API_KEY/X_API_SECRET/X_ACCESS_TOKEN/X_ACCESS_TOKEN_SECRET env vars or credentials.toml)
```

のような形になり、`errors.Is(err, xapi.ErrAuthentication)` が true を返す。

### D-2: `xapi.Client` を直接呼ばず、クライアント生成を関数変数経由にする

理由:
- M9 のテストで `httptest.Server` の URL を `xapi.WithBaseURL` で注入したい
- しかし `newMeCmd()` の `RunE` が `xapi.NewClient(ctx, creds)` を直接呼ぶと URL 差し替えができない
- 関数変数 `newMeClient` で間接化することで、テスト側から `t.Cleanup` 経由で差し替え可能

検討した代替案:

1. **`X_API_BASE_URL` env を読んで `xapi.WithBaseURL` に渡す**
   - 利点: 新規抽象化が要らず、staging 環境にも使える
   - 欠点: 本番ユーザーに見える env を増やす (UX ノイズ)、エンドポイント自体は X API 固有でユーザーが変更する想定外、spec §11 の環境変数表に無いキーを CLI が暗黙対応するのは仕様逸脱
   - **却下** (spec 整合性 + ユーザー混乱回避)

2. **cobra command を struct でラップし、コンストラクタで client factory を注入**
   - 利点: パッケージ変数を消せる、複数 cmd で再利用しやすい
   - 欠点: M9 単発のために構造ファクトリを導入するのは過剰、`newMeCmd()` 1 つに留めて将来のリファクタ余地を残す方が KISS
   - **却下** (yagni)

3. **package-level `var newMeClient`** (採用)
   - 利点: 最小変更、`t.Cleanup` で確実に元に戻せる、テストが env 操作と独立してクライアントだけ差し替えられる
   - 欠点: グローバル可変状態が増える (テスト並列性の問題は R2 で対処)

実装:
```go
// internal/cli/me.go
type meClient interface {
    GetUserMe(ctx context.Context, opts ...xapi.UserFieldsOption) (*xapi.User, error)
}

var newMeClient = func(ctx context.Context, creds *config.Credentials) (meClient, error) {
    return xapi.NewClient(ctx, creds), nil
}
```

テスト側で:
```go
prev := newMeClient
t.Cleanup(func() { newMeClient = prev })
newMeClient = func(ctx context.Context, _ *config.Credentials) (meClient, error) {
    return xapi.NewClient(ctx, nil, xapi.WithBaseURL(srv.URL)), nil
}
```

### D-3: 認証情報の env 優先順位は「**ファイル単位** env > file」(タスク指示準拠)

理由:
- タスク指示書に明記されている挙動:
  - 「すべての env が揃っていれば env を使う」
  - 「不足する場合は credentials.toml を読む」
- フィールド単位 (env と file を 1 フィールドずつマージ) のほうが運用上柔軟だが、(i) タスク指示と異なる解釈になり、(ii) テストマトリクスが膨れ、(iii) 部分マージの仕様詳細 (env="" と env 未設定の区別など) を別途決める必要がある
- spec §11 「env > credentials.toml」もファイル単位の解釈と矛盾しない (env "ソース全体" が file "ソース全体" より優先)
- 将来 (M12 `x configure` 等) でユーザーから「フィールド単位で上書きしたい」要望があれば、その時点で再設計する

挙動定義:
1. `credentialsFromEnv()` で 4 つの env を取得
2. 4 つすべて非空 → そのまま返却 (file は読まない)
3. 1 つでも空 → `config.DefaultCredentialsPath()` を解決し `config.LoadCredentials(path)` を呼ぶ
   - 成功 → 4 フィールドの完備性チェック (`credentialsComplete`) → 完備なら返却、欠落なら `ErrCredentialsMissing`
   - `config.ErrCredentialsNotFound` → `ErrCredentialsMissing` に置換
   - その他のエラー (パース失敗、Stat 失敗) → `fmt.Errorf("auth loader: %w", err)` で wrap (exit 1)
4. env="" (空文字で明示的に設定) は **未設定と同じ扱い** (R7 と整合)

### D-4: `--no-json` を採用 (`--json` ではなく)

理由:
- `internal/cli/version.go` (M1) と同じ流儀 (default JSON、`--no-json` で human-readable)
- spec §6 では `x me [--json]` と書かれているが、ロードマップ §M9 完了条件は「JSON / human 出力 / `--no-json` で human」となっており version と統一されている
- ロードマップを優先

### D-5: テストでの env 操作と t.Parallel() の併用は避ける

理由:
- Go の `t.Setenv` は `t.Parallel()` と排他制御される (`t.Parallel()` を呼ぶと panic)
- 認証情報テストでは env を多用するため、`t.Parallel()` は **使わない**
- 既存 `root_test.go` / `version_test.go` は `t.Parallel()` を使っているが、それらは env 非依存なのでそのまま維持

### D-6: `LoadCredentialsFromEnvOrFile()` は `ctx` を受けない (タスク指示と差分)

タスク指示:
```
func LoadCredentialsFromEnvOrFile(ctx) (*config.Credentials, error)
```

本計画では `ctx` を取らない `LoadCredentialsFromEnvOrFile() (*config.Credentials, error)` を採用。

理由:
- env / TOML ファイル読み込みは I/O とはいえ context cancel を想定する必要がない (短時間で完結)
- 既存 `config.LoadCredentials(path)` も ctx を取らないため API 整合性
- 将来 (M21+ で sqlite 等の永続層を介すようになった場合) は ctx 付き API を別途追加するか、本関数を破壊的に変更する。M9 時点では未使用パラメータを増やさない

### D-7: パッケージ doc コメントは既存 `root.go` 上の 1 箇所のみ

理由:
- `revive` の `package-comments` ルールは「同一パッケージで複数ファイルに package コメントを書くと違反」になる可能性
- 既存 `internal/cli/root.go` 冒頭の `// Package cli は ...` 部分が唯一の package doc
- M9 で追加するファイル (me.go, auth_loader.go) には**ファイル冒頭にコメントは書かない** (ビルドコメントなどは別)
- ただし、ファイル先頭にエクスポート無しのコメントを書くと revive がパッケージコメント扱いするケースがある → **無コメント** で `package cli` から開始する (`version.go` と同じ書き方を採る)

### D-8: ヘルパ関数の所在

- `LoadCredentialsFromEnvOrFile` は `internal/config` ではなく `internal/cli` に置く
  - 理由: CLI モード固有 (MCP モードはファイル不使用 — spec §11)。`config` パッケージは store の薄いラッパ、CLI 層の優先順位ロジックは「presentation」 (cli) の関心事
  - `config.LoadCredentials` は file のみを扱い、env 由来は本 M で初導入される

### D-9: human-readable 出力フォーマット

```
id=1234567890 username=alice name=Alice
```

理由:
- key=value のスペース区切りは grep/awk しやすい
- 改行コード 1 つで終わる (Fprintln)
- 名前にスペースを含む場合は引用しないが、spec の例 `name=Naoto` と整合 (CJK や空白を含むケースは将来 `--no-json` の改善で対応)

### D-10: 出力時の改行

- `json.Encoder.Encode` は末尾改行を自動付与するので追加不要
- `fmt.Fprintln` も改行を自動付与
- どちらも 1 行 + 改行のみで終わる

## TDD Plan (Red → Green → Refactor)

### Step 1: `ErrCredentialsMissing` 定義のみ

- Red: `auth_loader_test.go` に「`ErrCredentialsMissing` は `xapi.ErrAuthentication` を Unwrap する」テスト
- Green: `auth_loader.go` に番兵エラー定義のみ

### Step 2: `credentialsFromEnv` の table-driven テスト

- Red: 4 つの env が揃っているケース / 部分欠落ケース / すべて欠落のケース
- Green: env 4 つを `os.Getenv` で読んで *Credentials を返す関数

### Step 3: `LoadCredentialsFromEnvOrFile` の優先順位テスト (ファイル単位)

- Red: env 完備、file 完備、両方完備 (env 勝ち、file 未読)、env 不足+file 完備、env 不足+file 欠落 (`ErrCredentialsMissing`)、file パース失敗
- Green: `credentialsFromEnv` → ok ならそのまま return / `config.DefaultCredentialsPath` → `LoadCredentials` → `credentialsComplete` のロジック

### Step 4: `newMeCmd` の RunE — 認証情報欠落

- Red: env / file 両方ない状態で `cmd.Execute()` を呼ぶと `ErrCredentialsMissing` を返す
- Green: `newMeCmd` の RunE 冒頭で `LoadCredentialsFromEnvOrFile()` を呼ぶ

### Step 5: `newMeCmd` の RunE — 成功パス (JSON)

- Red: httptest mock を立て、`newMeClient` を差し替えて env で creds を渡し、JSON 出力を assert
- Green: `GetUserMe` 呼び出し + JSON エンコーダ出力

### Step 6: `newMeCmd` の RunE — `--no-json`

- Red: `--no-json` フラグで `id=42 username=alice name=Alice` 形式の出力を assert
- Green: noJSON フラグの分岐実装

### Step 7: `newMeCmd` の RunE — エラー写像

- Red: 401 → `errors.Is(err, xapi.ErrAuthentication)`、404 → `errors.Is(err, xapi.ErrNotFound)`
- Green: GetUserMe のエラーをそのまま return (既に xapi 層で番兵が Unwrap 可能になっている)

### Step 8: `cmd/x/main.go` の `run()` 拡張

- ユニットテストは作らず、既存 `internal/cli/me_test.go` の Execute 経路で間接的に検証
  - 補足: 仮に `run()` を直接テストしたい場合は内部関数 `mapErrorToExitCode(err) int` を切り出すアプローチも可能だが、M9 ではスコープを限定する
- 実装: `errors.Is` チェックを 3 つ追加 (`xapi.ErrAuthentication` / `ErrPermission` / `ErrNotFound`)
- `xapi` パッケージを import に追加 (`github.com/youyo/x/internal/xapi`)

### Step 9: root.go の AddCommand

- Red: `TestRootHelpShowsMe` (root_test.go に追加) で `--help` に `me` が含まれる
- Green: `root.AddCommand(newMeCmd())`

### Step 10: Refactor

- 命名・コメント・gofumpt 整形
- `golangci-lint run` で 0 issues 確認
- `go vet`, `go test -race -count=1 ./...` で全 pass 確認

## Test Matrix

| # | テスト名 | 対象 | 概要 |
|---|---|---|---|
| 1 | TestErrCredentialsMissing_UnwrapsAuthError | auth_loader | `errors.Is(ErrCredentialsMissing, xapi.ErrAuthentication)` |
| 2 | TestCredentialsFromEnv_AllSet | auth_loader | 4 env 揃い |
| 3 | TestCredentialsFromEnv_PartialMissing | auth_loader | 1 個欠落 → ok=false |
| 4 | TestCredentialsFromEnv_AllMissing | auth_loader | 全部欠落 → ok=false、ゼロ値 creds |
| 5 | TestLoadCredentialsFromEnvOrFile_EnvOnly | auth_loader | env のみ、file 不在 → env |
| 6 | TestLoadCredentialsFromEnvOrFile_FileOnly | auth_loader | env 全空、file 完備 → file |
| 7 | TestLoadCredentialsFromEnvOrFile_EnvBeatsFile | auth_loader | 両方完備、env が勝ち file 未読 |
| 8 | TestLoadCredentialsFromEnvOrFile_PartialEnvFallsBackToFile | auth_loader | env 3 つだけ + file 完備 → file から取得 (env は無視) |
| 9 | TestLoadCredentialsFromEnvOrFile_Missing | auth_loader | env 欠落 + file 不在 → ErrCredentialsMissing |
| 10 | TestLoadCredentialsFromEnvOrFile_FileIncomplete | auth_loader | env 欠落 + file が 1 フィールド空 → ErrCredentialsMissing |
| 11 | TestLoadCredentialsFromEnvOrFile_ParseError | auth_loader | 不正 TOML → wrap error (xapi.ErrAuthentication は Unwrap しない) |
| 12 | TestMe_Success_JSON | me | 200 OK + 既定 JSON |
| 13 | TestMe_Success_NoJSON | me | --no-json human |
| 14 | TestMe_CredentialsMissing | me | env も file も無し → ErrCredentialsMissing |
| 15 | TestMe_401_AuthError | me | httptest で 401 |
| 16 | TestMe_404_NotFound | me | httptest で 404 |
| 17 | TestMe_HelpShowsNoJSON | me | --help に --no-json |
| 18 | TestRootHelpShowsMe | root | --help に me |

合計 18 テストケース。既存 100+ テストと合わせて 118+ になる見込み。

## Risks / Mitigations

| # | リスク | 対策 |
|---|---|---|
| R1 | `xapi.NewClient` の `WithBaseURL` が exported 済みか | client.go:80 で公開済み (`WithBaseURL`)、`go doc` で確認 |
| R2 | `clientFactory` 関数変数の差し替えはテスト並列性を破壊 | `t.Cleanup` で確実に元に戻す + me_test.go では `t.Parallel()` を使わない (auth_loader も同様) |
| R3 | `revive` の `package-comments` でファイル別の冒頭コメント違反 | 新規ファイルにはファイル先頭コメント書かない (`version.go` を踏襲) |
| R4 | `credentials.toml` のパーミッション警告が log.Printf で stderr ノイズ | テストで `t.Setenv("XDG_DATA_HOME", t.TempDir())` し、`SaveCredentials` で 0600 で作成すれば警告は出ない |
| R5 | `t.Setenv` + `t.Parallel` の組み合わせ panic | M9 で env 操作するテストはすべて非並列 (D-5) |
| R6 | HOME 環境変数依存の DefaultCredentialsPath | `t.Setenv("XDG_DATA_HOME", t.TempDir())` で隔離 |
| R7 | env で `X_API_KEY=""` のような空文字が設定されているケース | `os.Getenv` は空文字を返す。これを「未設定」と同じ扱いにする (CLI 仕様としてシンプル) |

## Verification Checklist

- [ ] `go test -race -count=1 ./...` 全 pass
- [ ] `go vet ./...` 0 issues
- [ ] `golangci-lint run ./...` 0 issues
- [ ] `go build -o /tmp/x ./cmd/x` が成功
- [ ] 手動: `XDG_DATA_HOME=/tmp/x-test-$$ X_API_KEY=fake ... x me` が exit 3 (実際には fake credentials で 401 になり exit 3)
- [ ] 手動: `x me --help` で `--no-json` が表示される
- [ ] 手動: `x --help` で `me` サブコマンドが一覧表示される
- [ ] M1〜M8 の既存テストが回帰しないこと

## Commit メッセージ

```
feat(cli): x me サブコマンドと認証情報ローダーを追加

- internal/cli/me.go: GetUserMe → JSON/human 出力、xapi 経由の Cobra サブコマンド
- internal/cli/auth_loader.go: env > credentials.toml の優先順位で X API クレデンシャルを解決 (ErrCredentialsMissing は xapi.ErrAuthentication を Unwrap)
- internal/cli/root.go: x me を AddCommand
- cmd/x/main.go: run() のエラー → exit code 写像に xapi.ErrAuthentication/ErrPermission/ErrNotFound を追加 (3/4/5)

spec §6 (x me) / §11 (env > credentials > config > default) に準拠。

Plan: plans/x-m09-cli-me.md
```

## Open Questions

(本計画時点で全解決済み)

## Changelog

| 日時 | 内容 |
|---|---|
| 2026-05-12 | M9 詳細計画初版 (planner) |
