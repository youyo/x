# M10 詳細計画: CLI `x liked list` 基本フラグ

> Layer 2: M10 マイルストーンの詳細実装計画。
> 親ロードマップ: [plans/x-roadmap.md](./x-roadmap.md)
> スペック: [docs/specs/x-spec.md](../docs/specs/x-spec.md) §6 `x liked list` / §11 設定優先順位 / フロー1 (§8)
> 前マイルストーン: M9 (commit `7723177`) — auth_loader / meClient interface / newMeClient swap / WithBaseURL 注入 / --no-json 慣習 / exit code 写像

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M10 |
| 範囲 | `internal/cli/liked.go` (新規) / `internal/cli/liked_test.go` (新規) / `internal/cli/root.go` (AddCommand 追加) / `cmd/x/main.go` (`ErrInvalidArgument` 番兵を判定に追加) / `internal/cli/errors.go` (新規, `ErrInvalidArgument` 番兵) |
| 前提 | M9 (commit `7723177`) |
| 完了条件 | `httptest` mock で `x liked list` がシングルページ JSON 出力 / `--no-json` で human 1 行/ツイート / `--user-id` 指定/未指定の両系統 / `--start-time` `--end-time` `--max-results` `--pagination-token` 反映 / `--start-time` フォーマット不正 → exit 2 / `--max-results` 範囲外 → exit 2 / 401→3, 403→4, 404→5。`go test -race -count=1 ./...` 全 pass、`golangci-lint run ./...` 0 issues、`go vet ./...` clean、`go build -o /tmp/x ./cmd/x` 成功 |
| TDD | Red → Green → Refactor 厳守 |

## Scope

### In Scope

1. **`internal/cli/errors.go`** (新規)
   - `ErrInvalidArgument` 番兵エラー
   - シグネチャ:
     ```go
     var ErrInvalidArgument = errors.New("invalid argument")
     ```
   - `cmd/x/main.go` の switch で `errors.Is(err, cli.ErrInvalidArgument) → app.ExitArgumentError (2)` に写像。
   - 理由: 既存 `isArgumentError` は Cobra 自身の "unknown command/flag" 文字列マッチのみで、`RunE` 内のバリデーション失敗 (時刻フォーマット不正 / `--max-results` 範囲外) を捕捉できない (advisor 指摘 #1)。

2. **`internal/cli/liked.go`** (新規)
   - `newLikedCmd() *cobra.Command` factory — 親コマンド (`x liked`)、サブコマンド無しでは help を出す
   - `newLikedListCmd() *cobra.Command` factory — 実体 (`x liked list`)
   - `likedClient` interface (newMeClient swap と同じ流儀):
     ```go
     type likedClient interface {
         GetUserMe(ctx context.Context, opts ...xapi.UserFieldsOption) (*xapi.User, error)
         ListLikedTweets(ctx context.Context, userID string, opts ...xapi.LikedTweetsOption) (*xapi.LikedTweetsResponse, error)
     }
     ```
   - `newLikedClient` パッケージ var (テスト差し替え):
     ```go
     var newLikedClient = func(ctx context.Context, creds *config.Credentials) (likedClient, error) {
         return xapi.NewClient(ctx, creds), nil
     }
     ```
   - フラグ:
     | フラグ | 型 | デフォルト | 説明 |
     |---|---|---|---|
     | `--user-id` | string | `""` (空 = self) | 取得対象ユーザー ID。未指定なら GetUserMe で self の ID を解決 |
     | `--start-time` | string | `""` | RFC3339 文字列。空なら未指定 |
     | `--end-time` | string | `""` | RFC3339 文字列。空なら未指定 |
     | `--max-results` | int | `100` | 1〜100。範囲外なら exit 2 |
     | `--pagination-token` | string | `""` | X API の next_token 続きから取得 |
     | `--no-json` | bool | `false` | true なら human、false (default) なら JSON |
   - `RunE` ロジック:
     1. `LoadCredentialsFromEnvOrFile()` で認証情報取得
     2. `newLikedClient(ctx, creds)` でクライアント生成
     3. **バリデーション** (順序固定):
        - `--max-results` が `1..100` の範囲外 → `fmt.Errorf("%w: --max-results must be in 1..100, got %d", cli.ErrInvalidArgument, n)`
        - `--start-time` が非空かつ `time.Parse(time.RFC3339, ...)` 失敗 → `fmt.Errorf("%w: --start-time: %v", cli.ErrInvalidArgument, parseErr)`
        - `--end-time` が非空かつ `time.Parse(time.RFC3339, ...)` 失敗 → 同上
     4. `--user-id` 未指定なら `client.GetUserMe(ctx)` で `User.ID` を取得し置換
     5. オプション組み立て (順序は last-wins 性質を踏まえ重複なし):
        - 常に `xapi.WithMaxResults(n)` を呼ぶ (n は default=100。**0 を流さない**; advisor 指摘 #3 + likes.go:88 docstring)
        - `start-time`/`end-time` が非空ならパース済み time を `xapi.WithStartTime` / `xapi.WithEndTime` で渡す
        - `--pagination-token` が非空なら `xapi.WithPaginationToken`
     6. `client.ListLikedTweets(ctx, userID, opts...)` を呼ぶ
     7. 出力:
        - `--no-json=false` (default): `json.NewEncoder(cmd.OutOrStdout()).Encode(resp)` で `*xapi.LikedTweetsResponse` 全体 (`{data, includes, meta}`) を出力 (末尾改行付与) — MCP 出力スキーマと整合 (advisor 指摘 #4)
        - `--no-json=true`: `resp.Data` を 1 ツイート 1 行で human フォーマット出力
   - human フォーマット (`--no-json` 時、advisor 指摘 #5):
     - 行ごとに `id=<id>\tauthor=<author_id>\tcreated=<created_at>\ttext=<text>`
     - `text` は改行 (`\n` / `\r`) を半角スペースに置換、`\t` も半角スペースに置換、合計 UTF-8 ルーン数 80 を超えたら 79 ルーン + `…` (U+2026) で truncate
     - 0 件のときは何も出さない (改行のみも出さない; 後段 pipe に優しい)
   - エラーは wrap せず呼び出し側 (cmd/x/main.go) に伝搬。`xapi.ErrAuthentication` / `ErrPermission` / `ErrNotFound` の番兵は xapi が付けるので CLI 層では二重 wrap しない。

3. **`internal/cli/root.go`** (既存ファイル編集)
   - `root.AddCommand(newLikedCmd())` を 1 行追加 (newMeCmd 直後)

4. **`cmd/x/main.go`** (既存ファイル編集)
   - `run()` の switch に `errors.Is(err, cli.ErrInvalidArgument) → app.ExitArgumentError (2)` を追加
   - 判定順序 (上から優先):
     1. `isArgumentError(err)` → `ExitArgumentError` (2) — 既存
     2. `errors.Is(err, cli.ErrInvalidArgument)` → `ExitArgumentError` (2) — **新規**
     3. `errors.Is(err, xapi.ErrAuthentication)` → `ExitAuthError` (3) — 既存
     4. `errors.Is(err, xapi.ErrPermission)` → `ExitPermissionError` (4) — 既存
     5. `errors.Is(err, xapi.ErrNotFound)` → `ExitNotFoundError` (5) — 既存
     6. fallback → `ExitGenericError` (1)

### Out of Scope (M11 以降)

- `--since-jst <YYYY-MM-DD>` / `--yesterday-jst` (時刻簡易指定) — M11
- `--all` / `--max-pages` (next_token 自動辿り; `EachLikedPage` 利用) — M11
- `--ndjson` (NDJSON 出力切替) — M11
- `--tweet-fields` / `--expansions` / `--user-fields` カスタマイズ — M11 (デフォルトは X API 既定、CLI からは渡さない)
- `config.toml` の `[liked]` セクション参照 — M11 (`default_max_results` 等)
- `--config` / `--credentials` フラグ — M12

## Design Decisions

### D-1: `ErrInvalidArgument` を `internal/cli/errors.go` に置く

- 配置先候補: (a) `internal/app/exit.go` に Sentinel 型として追加 / (b) `internal/cli/errors.go` 新規。
- 採用: (b)。
  - 理由 1: `internal/app` は exit code 定数のみを公開する責務に絞られている。エラー型を増やすとパッケージの役割が膨らむ
  - 理由 2: 引数バリデーション失敗は CLI 層の関心事 (xapi 層は HTTP ステータスに対応する `ErrAuthentication` 等を持つ)。同じレイヤで番兵を定義したほうが層責務が明確
  - 理由 3: 将来 MCP 層が同種の "request validation error" を必要としても、CLI 専用と独立に置ける

### D-2: `--user-id` 未指定 → `GetUserMe` で self 解決

- 仕様 §6: `--user-id <id> (default: me)` の "me" は文字列リテラルではなく self 解決を意味する (フロー1 §8 段階 3 で明示)
- "me" を `/2/users/:id/liked_tweets` の :id にそのまま渡すことは X API では不可 (このエンドポイントは数値 ID 必須)
- M9 の `meClient` を再利用せず、`likedClient` に `GetUserMe` を含める (advisor 指摘 #2)
- `--user-id` 指定時は `GetUserMe` を呼ばない (リクエスト最小化、rate-limit 節約)

### D-3: `WithMaxResults` は常に呼ぶ (`0` を流さない)

- `internal/xapi/likes.go:88` の docstring が明示:「CLI 層 (M10/M11) で default_max_results = 100 (spec §11) を必ずセットする責務を負う」
- Cobra フラグの default=100 を活用し、0 が渡る経路を作らない (advisor 指摘 #3)
- 将来 `config.toml [liked].default_max_results` を反映する場合も、CLI 層で値を確定させてから `WithMaxResults(n)` を呼ぶ

### D-4: JSON 出力は `*xapi.LikedTweetsResponse` 全体を出す

- 候補: (a) `resp.Data` のみ (配列) / (b) `{data, includes, meta}` 全体。
- 採用: (b)。
  - 理由 1: spec §6 MCP tool `get_liked_tweets` の出力スキーマと完全一致 (CLI と MCP の出力が乖離しないと M18 の実装/テストが楽になる, advisor 指摘 #4)
  - 理由 2: `meta.next_token` がトップレベルにあるため、`x liked list --max-results 100 | jq -r '.meta.next_token'` で次ページの token を取り出せる (CLI 利用者の典型ユースケース)
  - 理由 3: M11 で `--ndjson` を実装する際、トップレベルが配列ではなく構造体だと NDJSON 化のため `.data[]` 抽出が必要だが、それは M11 で対応

### D-5: human 出力フォーマットの確定仕様

- `--no-json` 時の text は改行を含むことが多いため、grep / awk しやすいよう **1 ツイート 1 行に揃える**
- 区切り文字: タブ (`\t`) — スペース区切りより text 中の半角スペースと衝突しない
- text 整形手順 (順番):
  1. `\r\n` → ` `、`\n` → ` `、`\r` → ` `、`\t` → ` ` (制御文字 4 種を半角スペースに置換)
  2. UTF-8 ルーン数 80 超なら 79 ルーン + `…` (U+2026) で truncate
- フィールド順: `id=<id>\tauthor=<author_id>\tcreated=<created_at>\ttext=<text>`
- 0 件: 何も出さない (`fmt.Fprintln` も呼ばない)

### D-6: `time.Parse` のレイアウトは `time.RFC3339`

- `time.RFC3339` (`2006-01-02T15:04:05Z07:00`) は fractional seconds なしのフォーマット
- 実装上 `time.Parse(time.RFC3339, "2026-05-11T15:00:00.123Z")` は fractional 部分も受理する (Go 1.x の time パーサの拡張挙動。advisor 指摘 #6)
- テストで `"2026-05-11T15:00:00Z"` と `"2026-05-11T15:00:00.123Z"` の両方を流して許容範囲を文書化
- ローカルタイム (`2026-05-11T15:00:00+09:00`) も受理 — xapi 層が `t.UTC()` で正規化する (likes.go:72)

### D-7: バリデーションは `RunE` 内で行い、`PreRunE` を使わない

- Cobra の `PreRunE` は `RunE` と返り値が独立しているため、エラー型をテストする際に複雑化する
- M9 (me.go) と同じく単一 `RunE` 関数内で順序付きバリデーション → 認証情報ロード → 呼び出し、を一連で書く
- ただし「認証情報ロード」と「引数バリデーション」の順序は **引数バリデーション先行**:
  - 認証情報無しでも引数エラーは検出できる
  - 認証情報ロードは file I/O が走るため軽くない
  - exit code の優先順位が `2 > 3` で揃う (引数エラーが先にユーザに見える)

### D-8: `--user-id` の前段で GetUserMe を呼ぶタイミング

- バリデーション完了 → 認証情報ロード → クライアント生成 → `--user-id` 未指定なら `GetUserMe` → `ListLikedTweets`、の順
- `--user-id` 指定時は `GetUserMe` をスキップ (節約)
- テスト方針: httptest サーバが両方のパス (`/2/users/me` と `/2/users/:id/liked_tweets`) を 1 つのハンドラで分岐 (`switch r.URL.Path`)

### D-9: `xapi.NewClient(ctx, nil)` の安全性

- M5 で `NewOAuth1Config(nil)` が nil 安全に作られているため、テスト stub で `creds` が nil でも httptest サーバ向けには問題なく動作する
- ただし M9 の `stubMeClientFactory` と同様、test ヘルパで `xapi.WithBaseURL(srv.URL)` を渡す

## Test Plan

### `internal/cli/liked_test.go`

すべて httptest + `newLikedClient` 差し替えパターン。M9 の `stubMeClientFactory` と同じ流儀で `stubLikedClientFactory(t, baseURL)` ヘルパを用意する。`cmd.SetOut(buf)` と `cmd.SetErr(buf)` を **同じ buf** に向け、help / error が stdout / stderr どちらに流れても assert 可能にする (advisor 補足 #2)。

#### 成功系

1. **`x liked list` (--user-id 未指定 → GetUserMe で解決 → liked_tweets 取得)**
   - 期待: `/2/users/me` → ID=42 が返る / `/2/users/42/liked_tweets` が呼ばれる / 結果が JSON で stdout
   - 出力検証: `json.Unmarshal` で `resp.Data` `resp.Meta.ResultCount` を read
2. **`x liked list --user-id 12345`**
   - 期待: `/2/users/me` は呼ばれない / `/2/users/12345/liked_tweets` が呼ばれる
   - 検証: httptest ハンドラで `gotPaths []string` に記録、`/2/users/me` 未含有を assert
3. **`x liked list --max-results 50`**
   - 期待: クエリに `max_results=50` が含まれる
4. **`x liked list --start-time 2026-05-11T15:00:00Z --end-time 2026-05-12T14:59:59Z`**
   - 期待: クエリに両 RFC3339 が含まれる (UTC 正規化済み)
5. **`x liked list --start-time 2026-05-11T15:00:00.123Z`** (RFC3339Nano 互換性確認)
   - 期待: パース成功 / クエリは `start_time=2026-05-11T15:00:00Z` (xapi が UTC + 秒精度で format するため fractional は落ちる、D-6 / advisor 補足 #3)
6. **`x liked list --pagination-token abc`**
   - 期待: クエリに `pagination_token=abc`
7. **`x liked list --no-json`**
   - 期待: 各行が `id=...\tauthor=...\tcreated=...\ttext=...` の形式 / JSON ではない / 改行を含む text が 1 行に折りたたまれている / 80 ルーン超の text が `…` で truncate
8. **`x liked list --no-json` で 0 件レスポンス**
   - 期待: stdout が空文字列

#### バリデーション失敗系 (`ErrInvalidArgument`)

5 ケースを **table-driven 1 関数** で記述 (advisor 補足 #4):

9. **`x liked list --max-results 0`** → `errors.Is(err, cli.ErrInvalidArgument)`、`errors.Is(err, xapi.ErrAuthentication)` は false (= exit 2 経路)
10. **`x liked list --max-results 101`** → 同上
11. **`x liked list --max-results -1`** → 同上
12. **`x liked list --start-time invalid-date`** → 同上
13. **`x liked list --end-time 2026-05-11`** (date only) → 同上 (RFC3339 ではない)

#### 認証情報欠落 (`ErrCredentialsMissing` 経路, exit 3)

14. env 4 つ未設定 / credentials.toml 不在 → `errors.Is(err, ErrCredentialsMissing)` true、`errors.Is(err, xapi.ErrAuthentication)` true

#### HTTP エラー系

15. `/2/users/12345/liked_tweets` が 401 → `errors.Is(err, xapi.ErrAuthentication)` true (exit 3)
16. `/2/users/12345/liked_tweets` が 403 → `errors.Is(err, xapi.ErrPermission)` true (exit 4)
17. `/2/users/12345/liked_tweets` が 404 → `errors.Is(err, xapi.ErrNotFound)` true (exit 5)

#### help / 構造系

18. `x liked --help` → "list" が含まれる (サブコマンドが見える)
19. `x liked list --help` → 全フラグ (`--user-id`, `--start-time`, `--end-time`, `--max-results`, `--pagination-token`, `--no-json`) が含まれる
20. `x --help` → "liked" が含まれる
21. `x liked` (サブコマンド省略) → Cobra デフォルトの help が stdout に流れる (`run()` が 0 を返す挙動。`SilenceUsage:true` でも RootCmd 直下の Run なしコマンドは help 表示が標準)

### `cmd/x/main.go` のテスト追加なし

- 既存 M9 の方針を踏襲。`ErrInvalidArgument` の exit code 写像は `liked_test.go` から `cmd.Execute()` 経路を通すと cobra.Command の RunE が直接 error を返すだけなので、`os.Exit` まで通すなら別途 `main_test.go` (exec-based) が必要 → M10 では実施しない
- `errors.Is(err, cli.ErrInvalidArgument)` の正/誤が `liked_test.go` で検証されていれば main.go の switch は自明な配線

## Implementation Steps (TDD: Red → Green → Refactor)

### Step 1: `internal/cli/errors.go` 新規 (定義のみ)

- 番兵 `var ErrInvalidArgument = errors.New("invalid argument")` を定義 (doc コメント付与)
- 専用 sanity test は不要 (advisor 補足 #1): `liked_test.go` の case #9〜13 が `errors.Is` 経由で検出を担保する

### Step 2: `cmd/x/main.go` の switch 拡張

- Red: 既存テストは全通る (新規分岐は到達しない) ことを確認
- Green: `errors.Is(err, cli.ErrInvalidArgument) → app.ExitArgumentError` を switch に追加
- Refactor: コメント (doc) を更新

### Step 3: `internal/cli/liked.go` factory 骨格 + flag 定義 (Red 群)

- `liked_test.go` に 21 テストケースを **すべて Red 状態で先に追加** (ファイル全体作成)
- `liked.go` を空の factory として作る (`newLikedCmd()` 返却 `&cobra.Command{Use:"liked"}` のみ) → コンパイル成功 + テスト失敗 (RED) を確認

### Step 4: `newLikedListCmd` 実装 (Green)

- フラグ宣言 (`Flags().StringVar` 等)
- バリデーション 3 項目 (max-results 範囲 / start-time / end-time パース)
- `LoadCredentialsFromEnvOrFile` 呼び出し
- `newLikedClient` でクライアント生成
- `--user-id` 未指定なら `GetUserMe`
- `ListLikedTweets` 呼び出し
- JSON 出力 / human 出力分岐
- root.go に `AddCommand(newLikedCmd())` 追加
- ここでテスト 21 個全パス (GREEN)

### Step 5: Refactor

- `formatHumanLine(tweet xapi.Tweet) string` ヘルパを切り出し (text 整形ロジック)
- truncate 関数 `truncateRunes(s string, max int) string` を独立化
- newLikedClient 差し替え用 test helper を me_test.go と整合的なネーミングに揃える (`stubLikedClientFactory`)
- doc コメント (日本語) を全公開シンボル / 番兵に付与
- golangci-lint v2 + go vet + go test -race を通す

### Step 6: 動作確認

- `go build -o /tmp/x ./cmd/x` で実バイナリ生成
- `/tmp/x liked --help` / `/tmp/x liked list --help` が正しい usage を表示
- 実認証情報がないので X API は叩かないが、`/tmp/x liked list --max-results 999` が exit 2 になることだけ確認 (`echo $?`)

### Step 7: コミット

```
feat(cli): x liked list 基本フラグを追加

- internal/cli/liked.go: newLikedCmd + newLikedListCmd factory
- フラグ: --user-id / --start-time / --end-time / --max-results / --pagination-token / --no-json
- internal/cli/errors.go: ErrInvalidArgument 番兵を新設
- cmd/x/main.go: ErrInvalidArgument を exit 2 に写像
- TDD: 21 テストケース、httptest mock + likedClient stub swap

Plan: plans/x-m10-cli-liked-basic.md
```

## Risks

| リスク | 影響 | 対策 |
|---|---|---|
| `time.RFC3339` のフラクショナル秒受理が Go バージョン依存 | 低 | テストで fractional 入りと無しの両方を確認 (D-6)。実装は層を超えず Parse → xapi に渡すだけなので、xapi 側の `t.UTC().Format(time.RFC3339)` で秒精度に丸まる |
| `--user-id` "me" を文字列リテラルで渡す利用者が出る可能性 | 低 | ドキュメント (cobra Long) で「未指定なら self」と明記。"me" 文字列が来たら X API 404 で素直にエラーになる (本マイルストーンでは追加処理しない、M11 検討) |
| human フォーマットの仕様が将来 NDJSON と整合しない可能性 | 中 | D-4 で JSON はトップレベル構造体に確定。M11 で `--ndjson` を追加する際に `--no-json` と `--ndjson` の関係 (`--no-json --ndjson` をどう扱うか) を再設計する余地を残す (M11 計画書で確定) |
| `newLikedClient` interface の GetUserMe が xapi.WithUserFields の variadic を要求するため、stub 実装でも variadic を持つ必要 | 低 | M9 の `stubMeClient` と同じパターンで `func(...UserFieldsOption) (*User, error)` を実装する (interface 要件) |
| help テストで Cobra のヘルプ出力先が SilenceUsage の影響を受ける | 低 | M9 me_test.go と同じ `cmd.SetOut(buf)` パターンで stdout を捕捉できることを確認済み |

## Definition of Done

- [ ] `internal/cli/errors.go` 新規追加 (1 番兵 + doc コメント)
- [ ] `internal/cli/liked.go` 新規追加 (newLikedCmd / newLikedListCmd / likedClient / newLikedClient)
- [ ] `internal/cli/liked_test.go` 新規追加 (21 テストケース)
- [ ] `internal/cli/root.go` に `AddCommand(newLikedCmd())` 追加
- [ ] `cmd/x/main.go` switch に `cli.ErrInvalidArgument` 判定追加
- [ ] `go test -race -count=1 ./...` 全 pass (M1〜M9 既存テスト維持)
- [ ] `golangci-lint run ./...` 0 issues
- [ ] `go vet ./...` clean
- [ ] `go build -o /tmp/x ./cmd/x` 成功
- [ ] `/tmp/x liked list --max-results 999` が exit code 2 を返す (`echo $?` 検証)
- [ ] `/tmp/x liked list --help` の usage に 6 フラグすべて表示
- [ ] git commit: `feat(cli): x liked list 基本フラグを追加` (Plan フッター付き)
