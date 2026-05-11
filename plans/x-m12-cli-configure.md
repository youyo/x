# M12: CLI `x configure` + config.toml [liked] 連携 実装計画

## 目的

M11 までで X API クライアント / CLI の主要機能 (`x me`, `x liked list`) が完成し、
`config.toml` / `credentials.toml` の R/W も `internal/config` 層に揃った。
本マイルストーンでは:

1. **`x configure` サブコマンドを実装する** (初期セットアップを CLI から完結させる)
   - 対話モード (引数なし): 4 つの X API トークンを読み取り `credentials.toml` に保存
   - `--print-paths`: `config.toml` / `credentials.toml` / `data_dir` のパスを出力
   - `--check`: 既存ファイルのパーミッション / 必須フィールド / シークレット混入を検証
2. **`x liked list` のハードコード定数を `config.toml [liked]` 連携に置き換える**
   - M11 ハンドオフ: `likedDefaultTweetFields` / `likedDefaultExpansions` /
     `likedDefaultUserFields` / `likedDefaultMaxPages` を spec §11 優先順位
     (env > config.toml > default) に従って動的に決定する

完了条件: `x configure` で初期セットアップが完結し、`x configure --check` で構成検証可能、
`x liked list` のデフォルト値が `config.toml [liked]` から読み込まれる。

## 前提 / ハンドオフ確認

- 既存 API (M3/M4 で実装済):
  - `config.LoadCredentials(path) (*Credentials, error)`
  - `config.SaveCredentials(path, c) error` (perm 0600 強制, dir 0700, tmp + rename, chmod)
  - `config.DefaultCredentialsPath() (string, error)`
  - `config.CheckPermissions(path) error` (Windows nil, group/other 0 のとき nil)
  - `config.CheckConfigNoSecrets(path) error` (`ErrSecretInConfig` を返す)
  - `config.LoadCLI(path) (*CLIConfig, error)` (ファイル不在時は DefaultCLIConfig を返す)
  - `config.DefaultCLIConfigPath() (string, error)`
  - `config.DataDir() (string, error)` / `config.Dir() (string, error)`
- `internal/cli/liked.go` の **ハードコード定数** 4 つ:
  - `likedDefaultTweetFields` / `likedDefaultExpansions` / `likedDefaultUserFields` (string)
  - `likedDefaultMaxPages` (int = 50)
  - **削除しないが、Default*Section が空の場合のフォールバック** として残す方針
    (理由: spec §11 デフォルト保証、設定ファイル / 環境破損時の最終防衛線)
- パッケージ doc は `internal/cli/root.go` のみ (他ファイルに `// Package cli` を書かない)
- TDD パターン: `cmd.SetArgs` + `cmd.SetOut(buf)` + `httptest` mock
- 既存テストは t.Setenv("XDG_DATA_HOME", ...) で隔離している
- 環境変数の優先順位は spec §11 で `env > credentials.toml > config.toml > default` だが、
  本マイルストーン (liked デフォルト値) は **CLI flag > config.toml > 組み込みデフォルト** のみ
  (env で `X_LIKED_*` を読む仕様は spec §11 環境変数一覧に無いため対象外)

## 設計判断 (Decisions)

### D-1: 対話モードでパスワード入力を echo オフにするか
- **採用**: `golang.org/x/term` の `term.IsTerminal(int(os.Stdin.Fd()))` で TTY 判定
  - TTY: `term.ReadPassword(fd)` で echo オフ
  - 非 TTY (テスト / パイプ): `bufio.Reader.ReadString('\n')` で通常入力
- 理由: シークレットを画面 / ログに残さない (`api_secret` / `access_token_secret` は機密)
  - `api_key` / `access_token` も保守的に同じ扱い (4 つすべてシークレット相当)
- 代替案検討: `github.com/AlecAivazis/survey/v2` → 外部依存増、CI の TTY 模擬が複雑
- 代替案検討: `bufio` のみ → 画面に echo されて screen recording / shoulder surfing リスク
- TTY 判定の fd: `os.Stdin.Fd()` (cobra の `cmd.InOrStdin()` は io.Reader なので fd 取れない)
  - テストでは cmd.SetIn を使うため、cmd.InOrStdin() が *os.File でない場合は非 TTY 扱い

### D-2: 既存 credentials.toml がある場合の保護
- **採用**: 対話モード突入時に存在チェックし、見つかれば `"overwrite existing credentials.toml? [y/N]: "`
  プロンプトを表示。`y` または `Y` 以外なら `ErrConfigureCancelled` を返却して exit 1
- spec §11「ディレクトリ 0700 / ファイル 0600 で作成、既存ファイルのパーミッションが緩い場合は警告」
  + spec §6「対話形式」を組み合わせ、誤上書きから保護
- `--force` フラグは M12 では追加しない (YAGNI: 必要になったタイミングで追加)
- ErrConfigureCancelled は cli 層の番兵エラーとして errors.go に追加せず、
  `errors.New("configure cancelled by user")` で文字列ベース (exit 1 へ fall back)

### D-3: `--check` の出力フォーマット
- **採用**: `{"ok": bool, "issues": [...string]}` の単一 JSON (NDJSON ではない)
  - 各 issue は人間可読な英語 1 行 (例: "credentials.toml not found at /path",
    "credentials.toml permission too open (mode=0644)", "credentials.toml missing field: api_secret",
    "config.toml contains secret keys: [xapi.api_key]")
- `--no-json` フラグで human フォーマット: 1 行目に `ok: true` または `ok: false`、
  以降 1 issue 1 行で `  - <issue>`
- exit code: ok=false でも exit 0 (チェック結果はあくまで情報として返す)
  - 理由: スクリプトから `jq '.ok'` で判定可能、shell スクリプト連携が容易
- 代替案検討: ok=false で exit 1 → CI で「設定ミスは failure」運用にしたい場合に有用だが、
  M12 では情報出力に専念。M13 以降の利用者要望次第で `--strict` フラグ追加余地

### D-4: `--print-paths` の出力フォーマット
- **採用**: `{"config": "...", "credentials": "...", "data_dir": "..."}` JSON
- `--no-json` で human: `config=...`, `credentials=...`, `data_dir=...` (1 行ずつ)
- XDG 環境変数は **解決時の値** を出力 (`config.DefaultCLIConfigPath()` などを呼ぶ)
  - 環境変数 `XDG_CONFIG_HOME` / `XDG_DATA_HOME` が設定されていれば反映される
- ファイル存在の有無は出力しない (ただパスのみ。`--check` と責務分離)

### D-5: `--print-paths` / `--check` 同時指定の扱い
- **採用**: 排他にしない。`--print-paths` が先に評価され、出力後 return。`--check` も指定されていたら無視
  - 実装: cobra RunE 内で `if printPaths { ...; return }` → `if check { ...; return }` → 対話モード
- 代替案検討: 排他フラグにする → cobra の MarkFlagsMutuallyExclusive で実装可能だが、
  両方指定で `--print-paths` が勝つだけのほうがシンプル

### D-6: 対話モード入力の trim 仕様
- **採用**: `strings.TrimSpace` を全フィールドに適用 (前後の空白 / 改行 / タブを除去)
- 空文字 (TrimSpace 後) が 1 つでもあれば `ErrInvalidArgument` で wrap → exit 2
  - エラーメッセージ: `"%w: api_key cannot be empty"` のようにフィールド名は出す
  - シークレット値そのものは絶対に出さない
- 改行は LF / CRLF 両対応 (bufio.Reader.ReadString('\n') は \n 含む文字列を返すので TrimSpace で除去)

### D-7: liked のデフォルト値解決の責務配置
- **採用**: `internal/cli/liked.go` 内に `loadLikedDefaults()` 関数を新設
  - 戻り値: `(tweetFields, expansions, userFields string, maxPages int)`
  - 実装:
    1. `config.DefaultCLIConfigPath()` → `config.LoadCLI(path)` を呼ぶ
       - エラー (ErrHomeNotResolved 等) は warning ログを出してデフォルトに fallback
       - ファイル不在は DefaultCLIConfig が返るので透過的に処理
    2. `cfg.Liked.DefaultTweetFields` 等を返す (LoadCLI 内 applyDefaults で必ず非空)
- `newLikedListCmd()` 内で `loadLikedDefaults()` を呼び、`pflag.StringVar(..., default, ...)` の
  デフォルト値として使用
  - **タイミングが重要**: cobra の Flags 定義は RunE 呼び出し前 (init 時) に実行される
  - 但し `newLikedListCmd()` 自体は `NewRootCmd()` から呼ばれるため、毎回 evaluate される
  - テストで `t.Setenv("XDG_CONFIG_HOME", ...)` + `config.toml` を書いてから `NewRootCmd()` を
    呼べば、その config が反映される
- フォールバック定数 (`likedDefaultTweetFields` 等) は残し、`config.DefaultCLIConfig()` の
  値と一致していることを単体テストで検証 (構造的に同じ値)
- 代替案検討: ハードコード定数を完全削除 → DefaultCLIConfig が config パッケージ内なので
  cli から呼べば良いが、LoadCLI が一度走るので結局効率は同じ。シンプル化を優先
- **修正方針**: ハードコード定数 4 つは **削除** し、`config.DefaultCLIConfig()` のみを
  fallback 源として使う (二重管理を避ける)

### D-8: liked デフォルト解決時の i/o エラー扱い
- **採用**: `LoadCLI` がエラーを返したら **stderr に warning** を出してデフォルトに fallback
  - 理由: CLI 起動時の "fail fast" は厳しすぎる (XDG_CONFIG_HOME パーミッション問題等で
    全コマンドが死ぬ)。設定読み込みは best-effort
- warning は `os.Stderr` に直接書くのではなく、`log.Printf` を使う
  - 既存 `LoadCredentials` 内の `log.Printf("warning: ...")` と同じ流儀
- 但し warning が大量に出ると stderr が汚染されるため、`config.toml` の TOML 構文エラー
  だけは fatal にする選択肢もある
  - **採用**: M12 では best-effort 一択。構文エラーは LoadCLI が wrap して返すが、
    呼び出し側で log.Printf してから default に fallback する
  - 代替案検討: TOML 構文エラーは exit 2 → ユーザー体験を損なう (一度誤った toml を書くと
    全コマンドが落ちる)。M13 以降で見直す

### D-9: `--check` の network / context dependency
- **採用**: 完全に file I/O のみ。X API を呼ばない (認証情報の "有効性" は確認しない)
  - 理由: spec §6 「パーミッション・必須キーの存在をチェック」と限定的な記述
  - X API ping は `x me` で別途実施可能
- `--check` で検査する項目:
  1. credentials.toml の存在 (`os.Stat`)
  2. credentials.toml のパーミッション (`config.CheckPermissions`)
  3. credentials.toml の 4 フィールド存在 (`config.LoadCredentials` + `credentialsComplete` 相当)
     - api_key / api_secret / access_token / access_token_secret すべて非空
  4. config.toml の存在 (オプション、不在は ok)
  5. config.toml のシークレット混入 (`config.CheckConfigNoSecrets`)

### D-10: configure サブコマンドの位置付け
- root の AddCommand で `newConfigureCmd()` を追加
- M11 までに existed: version / me / liked
- M13 (README/CHANGELOG/LICENSE/GoReleaser) で完全な v0.1.0 になる
- 本マイルストーン後の `x` バイナリ: `x configure`, `x configure --print-paths`,
  `x configure --check`, `x version`, `x me`, `x liked list`

### D-11: golang.org/x/term の追加
- `go get golang.org/x/term@latest` で追加
- 既に dghubble/oauth1 が transitive で golang.org/x/crypto を引っ張っている可能性あり
- term は軽量で安定 (Go 公式準拠の補助パッケージ)

### D-12: 対話モード入力プロンプトの言語
- **採用**: 英語で統一 (既存 CLI の Long/Short はすべて英語)
  - 例: `"X API Consumer Key (api_key): "`
- 国際化は M13 以降の検討事項

### D-13: テスト時の TTY 判定回避
- `cmd.SetIn(bytes.NewBufferString(...))` で stdin を差し替えると、
  `os.Stdin.Fd()` は依然プロセス全体の stdin を指す
- **採用**: configure.go 内で `cmd.InOrStdin()` から読み、`(*os.File)` への型アサーションで
  TTY 判定。bytes.Buffer の場合は型アサーション失敗 → 非 TTY 扱いで通常 read
- `term.ReadPassword(fd)` は本物の TTY が必要なので、テストでは通らないパス

## ファイル変更

### 新規ファイル

**`internal/cli/configure.go`**:
- `newConfigureCmd() *cobra.Command` factory
- フラグ:
  - `--print-paths` (bool): パス情報を JSON / human で出力
  - `--check` (bool): 構成検証を JSON / human で出力
  - `--no-json` (bool): JSON ではなく human-readable で出力
- RunE 内分岐:
  1. `printPaths` true → `runPrintPaths(cmd, noJSON)` → return
  2. `check` true → `runCheck(cmd, noJSON)` → return
  3. それ以外 → `runInteractive(cmd)` 対話モード
- `runPrintPaths(cmd, noJSON) error`:
  - `config.DefaultCLIConfigPath()` / `config.DefaultCredentialsPath()` / `config.DataDir()`
  - JSON or human で出力
- `runCheck(cmd, noJSON) error`:
  - 各検査を順に実行し、issues スライスに append
  - JSON or human で `{ok, issues}` を出力
- `runInteractive(cmd) error`:
  - credentials.toml が存在 → 上書き確認プロンプト
  - 4 つのフィールドを順に読み (`promptSecret`)、`config.SaveCredentials` で保存
  - 完了メッセージ: `"saved credentials to <path>\n"` を stdout
- `promptSecret(cmd, label) (string, error)`:
  - `cmd.InOrStdin()` を取り、`*os.File` なら fd 取って `term.IsTerminal` 判定
  - TTY なら `term.ReadPassword(fd)`、非 TTY なら `bufio.Reader.ReadString('\n')`
  - 前後 trim、空ならエラー
  - プロンプト文字列は `cmd.ErrOrStderr()` に出す (TTY echo オフでも見える)

**`internal/cli/configure_test.go`**:
- `TestConfigure_PrintPaths_JSON` / `..._Human`: JSON / human フォーマット出力検証
- `TestConfigure_Check_AllGood`: パーミッション 0600 + 4 フィールド全て揃った credentials.toml + secret 混入なし config.toml で `{ok: true, issues: []}`
- `TestConfigure_Check_CredentialsMissing`: credentials.toml 不在で `ok: false, issues: ["credentials.toml not found at ..."]`
- `TestConfigure_Check_PermissionsTooOpen`: 0644 の credentials.toml で `ok: false`
  - **Windows スキップ**: `runtime.GOOS == "windows"` なら t.Skip
- `TestConfigure_Check_MissingField`: api_secret が空の credentials.toml で `ok: false, issues: [".../missing field: api_secret"]`
- `TestConfigure_Check_SecretInConfig`: config.toml に `[xapi] api_key=...` を書いて `ok: false`
- `TestConfigure_Interactive_Success`: stdin に 4 行流し込み、credentials.toml が perm 0600 で作成されることを検証
- `TestConfigure_Interactive_EmptyField`: 空行を含むと ErrInvalidArgument
- `TestConfigure_Interactive_Overwrite_Yes`: 既存ファイルがある状態で "y\n" を先頭に流し込み、上書き成功
- `TestConfigure_Interactive_Overwrite_No`: "N\n" で cancel エラー (exit 1 相当)
- `TestConfigure_NoJSON_Check`: human フォーマット出力検証 (1 行目 ok:true/false)

### 変更ファイル

**`internal/cli/root.go`**:
- `root.AddCommand(newConfigureCmd())` を追加 (順序は version → me → liked → configure)

**`internal/cli/liked.go`**:
- ハードコード定数 4 つを削除:
  - `likedDefaultTweetFields`, `likedDefaultExpansions`, `likedDefaultUserFields`, `likedDefaultMaxPages`
- 代わりに `loadLikedDefaults()` 関数を新設:
  ```go
  func loadLikedDefaults() (tweetFields, expansions, userFields string, maxPages int) {
      defaults := config.DefaultCLIConfig().Liked
      path, err := config.DefaultCLIConfigPath()
      if err != nil {
          log.Printf("warning: cannot resolve config.toml path: %v", err)
          return defaults.DefaultTweetFields, defaults.DefaultExpansions, defaults.DefaultUserFields, defaults.DefaultMaxPages
      }
      cfg, err := config.LoadCLI(path)
      if err != nil {
          log.Printf("warning: cannot load config.toml (%s): %v", path, err)
          return defaults.DefaultTweetFields, defaults.DefaultExpansions, defaults.DefaultUserFields, defaults.DefaultMaxPages
      }
      return cfg.Liked.DefaultTweetFields, cfg.Liked.DefaultExpansions, cfg.Liked.DefaultUserFields, cfg.Liked.DefaultMaxPages
  }
  ```
- `newLikedListCmd()` 内:
  - 関数頭で `dfTweet, dfExp, dfUser, dfMaxPages := loadLikedDefaults()` を呼ぶ
  - pflag のデフォルト値として使う

**`internal/cli/liked_test.go`** (既存テストとの互換性):
- 既存テストは `XDG_CONFIG_HOME` を設定していない場合、`HOME` が一時ディレクトリ前提に
  なっていない場合がある → config.toml が見つからず DefaultCLIConfig が返るため、
  既存テストはハードコード値と DefaultCLIConfig 値が一致していれば動作継続
  - 検証: `config.DefaultCLIConfig().Liked.DefaultTweetFields == "id,text,author_id,created_at,entities,public_metrics"` (一致)
  - `DefaultExpansions == "author_id"` (一致)
  - `DefaultUserFields == "username,name"` (一致)
  - `DefaultMaxPages == 50` (一致)
- **新規テスト** `TestLikedList_ConfigToml_Overrides`:
  - t.Setenv("XDG_CONFIG_HOME", t.TempDir()) で config.toml を書く
  - `[liked] default_tweet_fields = "id,text"` のような上書き
  - `cmd.SetArgs([]string{"liked", "list"})` でフラグ未指定実行
  - httptest が記録した query から `tweet.fields=id%2Ctext` を検出

### 依存追加

- `go.mod` に `golang.org/x/term` を追加
- `go get golang.org/x/term@latest` で取得
- `go mod tidy` で go.sum を更新

## TDD 実装順序

### 1. liked.go の config.toml 連携 (Red → Green → Refactor)

1.1. `TestLikedList_ConfigToml_Overrides` を書く (Red)
1.2. `loadLikedDefaults()` を実装、`newLikedListCmd()` を修正 (Green)
1.3. 既存テストが green を維持していることを確認
1.4. ハードコード定数を削除、`config.DefaultCLIConfig()` から取得するよう refactor

### 2. configure.go の --print-paths

2.1. `TestConfigure_PrintPaths_JSON` を書く (Red)
2.2. `newConfigureCmd()` 骨格 + `--print-paths` を実装 (Green)
2.3. `TestConfigure_PrintPaths_Human` (`--no-json`) を追加 → 実装

### 3. configure.go の --check

3.1. `TestConfigure_Check_AllGood` を書く (Red)
3.2. `runCheck` を実装 (Green)
3.3. 各 issue ケースを 1 つずつ追加 (CredentialsMissing → PermissionsTooOpen → MissingField → SecretInConfig)

### 4. configure.go の対話モード

4.1. `TestConfigure_Interactive_Success` を書く (Red, stdin に 4 行流し込み)
4.2. `runInteractive` + `promptSecret` (非 TTY パス) を実装 (Green)
4.3. EmptyField / Overwrite_Yes / Overwrite_No を追加

### 5. ルート統合

5.1. `root.AddCommand(newConfigureCmd())` を追加
5.2. 既存 TestNewRootCmd_HasSubcommands で configure を期待リストに追加

### 6. 動作確認

6.1. `go test -race -count=1 ./...` 全 pass
6.2. `golangci-lint run ./...` 0 issues (v2)
6.3. `go vet ./...` clean
6.4. `go build -o /tmp/x ./cmd/x` 成功
6.5. 手動確認: `XDG_CONFIG_HOME=/tmp/cfg XDG_DATA_HOME=/tmp/data /tmp/x configure --print-paths`

## リスク / 留意

1. **対話モードのテスト容易性**: `term.ReadPassword` は TTY 必須なので、テスト時は
   `cmd.InOrStdin()` の型アサーション結果が non-TTY → bufio.Reader 経路を通る。
   `*os.File` の場合のみ TTY 判定する分岐で隔離。

2. **既存テストの XDG_CONFIG_HOME 汚染** (重要):
   既存 liked_test.go の一部は `XDG_CONFIG_HOME` を設定していない。
   開発機 (`youyo`) に既存の `~/.config/x/config.toml` がある場合、
   `loadLikedDefaults` 経由でその設定値が読み込まれ、既存テストの query 期待値が
   ずれて silently fail する可能性がある (CI では XDG_CONFIG_HOME が空 + HOME 配下に
   ファイルが無いので問題ないが、ローカルでは bite する)。
   - **採用**: テスト先頭で `XDG_CONFIG_HOME` も `t.TempDir()` で隔離するヘルパを追加
     - `isolateXDG(t)` (仮称) を `auth_loader_test.go` か新規 `helpers_test.go` に定義:
       ```go
       func isolateXDG(t *testing.T) {
           t.Helper()
           t.Setenv("XDG_CONFIG_HOME", t.TempDir())
           t.Setenv("XDG_DATA_HOME", t.TempDir())
       }
       ```
     - すべての liked_test.go テスト先頭で呼ぶ (setAllXAPIEnv の直後 / 直前)
     - configure_test.go の全テストでも呼ぶ
   - 代替案: ハードコード定数を残して LoadCLI のオーバーライドのみで対応 → D-7 で
     完全削除に決めたので、テスト隔離側で対応するほうが整合的
   - **実装順序**: liked.go の修正前に `isolateXDG` を入れ、既存テストすべてに適用してから
     ハードコード定数を削除する (Red → Green の途中で既存テストが落ちないように)

3. **対話モードでの上書き確認プロンプト**: 非 TTY 時に "[y/N]: " の文字列を stderr 出力 →
   bufio で 1 行読む。テストでは "y\n" や "n\n" を先頭に流し込む順序を厳密に守る必要

4. **`log.Printf` の標準ログ汚染**: `loadLikedDefaults` の warning が `os.Stderr` に出る
   と、テストが `cmd.ErrOrStderr()` を期待しているとマッチしない可能性。
   - 対策: `log.Printf` のフォーマットを警告のみに抑え、テストは `cmd.SetErr(buf)` で
     cobra のエラーのみを期待。`log` パッケージのデフォルト出力は `os.Stderr` だが、
     テストでは設定ファイルが無いケースなので警告自体出ない (LoadCLI は ErrNotExist
     で nil error を返す)

5. **golang.org/x/term 依存追加**: M12 で新規依存。CI のキャッシュ無効化リスクは限定的。
   既に dghubble/oauth1 → x/crypto を transitive で持っているため、追加コストは小

## 完了条件

- [ ] `internal/cli/configure.go` 新規作成
- [ ] `internal/cli/configure_test.go` 新規作成 (--print-paths / --check / 対話モード)
- [ ] `internal/cli/root.go` に `newConfigureCmd()` を AddCommand
- [ ] `internal/cli/liked.go` のハードコード定数 4 つを `loadLikedDefaults()` 経由に置換
- [ ] `internal/cli/liked_test.go` に `TestLikedList_ConfigToml_Overrides` を追加
- [ ] `go.mod` に `golang.org/x/term` を追加
- [ ] `go test -race -count=1 ./...` 全 pass
- [ ] `golangci-lint run ./...` 0 issues
- [ ] `go vet ./...` clean
- [ ] `go build -o /tmp/x ./cmd/x` 成功

## 次マイルストーン (M13) 引き継ぎ

- M12 で `x` CLI の v0.1.0 機能セットが揃う:
  - `x version` (M1)
  - `x me [--no-json]` (M9)
  - `x liked list [...]` (M10/M11, config.toml 連携あり M12)
  - `x configure [--print-paths|--check]` (M12)
  - `x completion {bash,zsh,fish,powershell}` (cobra 自動)
- M13 は README / CHANGELOG / LICENSE / GoReleaser
- パッケージ doc は `internal/cli/root.go` のみ (M12 でも継続)
- 番兵エラー: `ErrCredentialsMissing` / `ErrInvalidArgument` / 各 xapi エラー
- TDD パターンと httptest mock は M9-M12 で確立済
