# M4: `internal/config` credentials.toml + guard

> Layer 2: M4 マイルストーン詳細計画。Layer 1 ロードマップは [plans/x-roadmap.md](./x-roadmap.md) を参照。
> 前マイルストーン: [plans/x-m03-config-xdg.md](./x-m03-config-xdg.md) (commit 2b2a804)
> 後続マイルストーン: M5 (`internal/xapi/oauth1.go`)

## 1. Overview

| 項目 | 値 |
|---|---|
| マイルストーン | M4 |
| ステータス | Planned |
| バージョン | 0.0.4 (CLI シークレットローダー + ガード) |
| 依存 | M3 (`config.Dir()` / `DataDir()` / BurntSushi/toml) |
| 対象モジュール | `github.com/youyo/x/internal/config` (拡張) |
| 追加コード依存 | なし (BurntSushi/toml は M3 で追加済み) |
| 検証方式 | TDD (Red → Green → Refactor) + table-driven + race + lint 0 |
| 対象ファイル数 (新規) | 4 ファイル (credentials.go / credentials_test.go / guard.go / guard_test.go) |
| LOC 目安 | ~400 (実装 ~180 / テスト ~220) |
| 参考 | spec §10 セキュリティ / §11 Configuration (`docs/specs/x-spec.md`) |

### M4 で**やること**
- `internal/config/credentials.go`
  - `Credentials` 型 (4 フィールド: APIKey / APISecret / AccessToken / AccessTokenSecret)
  - `LoadCredentials(path string) (*Credentials, error)` — ファイル不在で `ErrCredentialsNotFound`、パーミッション緩い場合はログ警告だが値は返す (spec §10「警告」準拠)
  - `SaveCredentials(path string, c *Credentials) error` — ディレクトリ 0700 / ファイル 0600 を**強制** (perm 引数を取らない設計、後述「設計判断」参照)。書き込みは tmp + rename で TOC-TOU を最小化
  - `DefaultCredentialsPath() (string, error)` — `filepath.Join(DataDir(), "credentials.toml")` を返す
  - `CheckPermissions(path string) error` — Stat + Mode().Perm() で「group / other に r/w/x が無いこと」を検証 (0400/0500/0600/0700 を許容、後述「設計判断」参照)。緩ければ `ErrPermissionsTooOpen`、ファイル不在は `ErrCredentialsNotFound`

### 設計判断 (Phase 2 タスク指示からの意識的逸脱)

1. **`SaveCredentials` シグネチャ**: タスク指示は `SaveCredentials(path, c, perm)` の 3 引数だったが、本マイルストーンは「**perm 0600 強制**」が主目的であり、外部から perm を受け取ると誤って緩い値を渡される事故リスクが残る。よって **perm 引数を取らず 0600 をハードコード**する。spec §11 「ファイルを 0600 で作成」「既存ファイルのパーミッションが緩い場合は警告」の方針とも一致する。
2. **フィールド名**: タスク指示は `XAPIKey/XAPISecret/...` だったが、Go の idiomatic な命名 (`APIKey` / `APISecret` 等) と TOML テーブル名 `[xapi]` をラッパ構造体 `credentialsFile{XAPI Credentials}` で表現する 2 段構造を採用。これにより `Credentials` 単体は X API 依存を意識せずに OAuth1 / 環境変数とも組み合わせ可能 (M5/M9 でメリットが出る)。
3. **`CheckPermissions` の許容範囲**: spec §10 は「0600 でなければ警告」と書かれているが、これを文字通り "exactly 0600" と読むと `0400` (owner read only) のような**より厳しい**設定も警告対象になり不自然。本実装は「**group / other に何のビットも立っていないこと**」と解釈し `mode & 0o077 != 0` で判定する (0400/0500/0600/0700 は許容)。doc コメントに明記する。
- `internal/config/guard.go`
  - `CheckConfigNoSecrets(path string) error` — config.toml を生 TOML として再パースし、`[xapi]` セクションまたは `api_key` / `api_secret` / `access_token` / `access_token_secret` キーが存在したら `ErrSecretInConfig` を返す
  - エラー定数: `ErrSecretInConfig`
- TDD: `t.TempDir()` + `os.Chmod` + `runtime.GOOS != "windows"` の skip + table-driven

### M4 で**やらないこと** (将来マイルストーン)
- 環境変数 override (`X_API_KEY` 等) (→ M5 / M9 で xapi クライアント初期化時に統合)
- `x configure` 対話モード (→ M12)
- credentials.toml の書き込みフローのテンプレ化 (→ M12)
- MCP モードでの credentials.toml 不使用ロジック (呼び出し側の責務、M24 で組み込む)

## 2. Goal

### 機能要件
- [ ] `Credentials` 構造体: `APIKey` / `APISecret` / `AccessToken` / `AccessTokenSecret` 4 フィールド (TOML タグ: `api_key` / `api_secret` / `access_token` / `access_token_secret`、TOML テーブル名は `[xapi]`)
- [ ] `LoadCredentials(path)`:
  - ファイル不在 → `ErrCredentialsNotFound` 返却 (M3 の `LoadCLI` と異なり、シークレットは必須リソース扱い)
  - パーミッションが 0600 でない (POSIX のみ) → `log.Printf` で警告、値は返す (spec §10「警告」)
  - TOML 構文エラー → エラー返却 (wrap)
  - 全フィールド空 (decoded.IsZero()) → そのまま返す。必須チェックは呼び出し側 (M9 で実装) に委ねる
- [ ] `SaveCredentials(path, c)`:
  - 親ディレクトリが存在しなければ `0700` で作成
  - tmp ファイル (`<path>.tmp-<pid>-<nano>`) に書き込んでから `os.Rename(tmp, path)`
  - 書き込み後、`os.Chmod(path, 0o600)` で最終強制 (umask 対策)
- [ ] `DefaultCredentialsPath()`: `filepath.Join(DataDir(), credentialsFileName)` を返す。`credentialsFileName = "credentials.toml"` 定数
- [ ] `CheckPermissions(path)`:
  - Stat → `errors.Is(fs.ErrNotExist)` → `ErrCredentialsNotFound`
  - Mode().Perm() & 0o077 ≠ 0 → `ErrPermissionsTooOpen` (group / other に r/w/x がある)
  - Windows (`runtime.GOOS == "windows"`) → 常に nil (POSIX 限定)
- [ ] `CheckConfigNoSecrets(path)`:
  - ファイル不在 → nil (config.toml は任意ファイル)
  - TOML 生パース (`toml.DecodeFile` を `map[string]any` に対して実行)
  - `[xapi]` キーが存在 → `ErrSecretInConfig`
  - トップレベルに `api_key` / `api_secret` / `access_token` / `access_token_secret` がある (any セクション含む再帰探索) → `ErrSecretInConfig`
  - エラーメッセージにはどのキーが検出されたかを含める (但しトークン値自体はログに出さない)

### 非機能要件
- [ ] `go test -race -count=1 ./...` 全 pass (M1+M2+M3 既存 41 テスト + M4 追加分)
- [ ] `golangci-lint run ./...` 0 issues
- [ ] `go vet ./...` 警告ゼロ
- [ ] 全公開シンボル (型 / 関数 / 構造体フィールド / エクスポート定数 / エラー) に**日本語** doc コメント
- [ ] パッケージ doc コメントは M3 で書いた `config.go` の 1 箇所のみ。新規ファイル (`credentials.go` / `guard.go`) には書かない
- [ ] credentials の値 (api_key 等) を**ログに絶対出さない**。エラーメッセージに含めるのはキー名のみ

## 3. Spec Mapping

| spec §10 / §11 規定 | M4 実装箇所 |
|---|---|
| credentials.toml の保存先 (`${XDG_DATA_HOME:-~/.local/share}/x/credentials.toml`) | `DefaultCredentialsPath()` |
| `[xapi] api_key/api_secret/access_token/access_token_secret` | `Credentials` 構造体 |
| ディレクトリ 0700 / ファイル 0600 | `SaveCredentials` |
| 起動時パーミッション検査、0600 でなければ警告 | `LoadCredentials` 内で `CheckPermissions` 呼び出し、緩ければ log 警告 |
| シークレットを config.toml に書こうとしたら拒否 | `CheckConfigNoSecrets` (M12 の `x configure` から呼ばれる) |
| MCP モードではファイル不使用 | M4 では呼び出し側責務とし、credential ファイルロジックは CLI 用途のみと doc コメントに明記 |

## 4. ファイル設計

### 4.1 `internal/config/credentials.go`

```go
package config

import (
    "errors"
    "fmt"
    "io/fs"
    "log"
    "os"
    "path/filepath"
    "runtime"

    "github.com/BurntSushi/toml"
)

const credentialsFileName = "credentials.toml"

// ErrCredentialsNotFound は credentials.toml が存在しない場合に返される。
var ErrCredentialsNotFound = errors.New("credentials.toml が見つかりません")

// ErrPermissionsTooOpen は credentials.toml のパーミッションが 0600 より緩い場合に返される。
var ErrPermissionsTooOpen = errors.New("credentials.toml のパーミッションが緩すぎます (0600 推奨)")

// Credentials は ~/.local/share/x/credentials.toml の [xapi] セクションを表す。
// CLI モード専用。MCP モードでは環境変数からのみ取得し、本構造体は使わない。
type Credentials struct {
    APIKey            string `toml:"api_key"`
    APISecret         string `toml:"api_secret"`
    AccessToken       string `toml:"access_token"`
    AccessTokenSecret string `toml:"access_token_secret"`
}

// credentialsFile は credentials.toml のトップレベル構造ラッパ ([xapi] テーブル)。
type credentialsFile struct {
    XAPI Credentials `toml:"xapi"`
}

// DefaultCredentialsPath は DataDir()/credentials.toml を返す。
func DefaultCredentialsPath() (string, error) {
    dir, err := DataDir()
    if err != nil {
        return "", err
    }
    return filepath.Join(dir, credentialsFileName), nil
}

// LoadCredentials は path から Credentials を読み込む。
//
//   - ファイル不在 → ErrCredentialsNotFound
//   - パーミッション緩い (POSIX のみ) → log.Printf で警告して継続
//   - TOML 構文エラー → wrap してエラー返却
func LoadCredentials(path string) (*Credentials, error) {
    if err := CheckPermissions(path); err != nil {
        switch {
        case errors.Is(err, ErrCredentialsNotFound):
            return nil, err
        case errors.Is(err, ErrPermissionsTooOpen):
            log.Printf("warning: %v: %s", err, path)
            // 続行して値は返す
        default:
            return nil, err
        }
    }

    var file credentialsFile
    if _, err := toml.DecodeFile(path, &file); err != nil {
        return nil, fmt.Errorf("credentials.toml のデコードに失敗 (%s): %w", path, err)
    }
    creds := file.XAPI
    return &creds, nil
}

// SaveCredentials は c を path に書き込む。
//
//   - 親ディレクトリは 0700 で作成 (存在すれば触らない)
//   - tmp ファイル → rename で半端な書き込みを避ける
//   - 最終的に os.Chmod で 0600 を強制 (umask の影響を打ち消す)
func SaveCredentials(path string, c *Credentials) error {
    if c == nil {
        return errors.New("credentials が nil です")
    }
    dir := filepath.Dir(path)
    if err := os.MkdirAll(dir, 0o700); err != nil {
        return fmt.Errorf("ディレクトリ作成に失敗 (%s): %w", dir, err)
    }
    // MkdirAll は既存ディレクトリの mode を変更しないため、明示的に 0700 を強制する
    // (spec §11「ディレクトリを 0700 で作成」を既存パスでも適用)。
    if err := os.Chmod(dir, 0o700); err != nil {
        return fmt.Errorf("ディレクトリのパーミッション設定に失敗 (%s): %w", dir, err)
    }

    tmp := fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
    f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
    if err != nil {
        return fmt.Errorf("一時ファイル作成に失敗 (%s): %w", tmp, err)
    }
    enc := toml.NewEncoder(f)
    if encErr := enc.Encode(credentialsFile{XAPI: *c}); encErr != nil {
        _ = f.Close()
        _ = os.Remove(tmp)
        return fmt.Errorf("credentials.toml のエンコードに失敗: %w", encErr)
    }
    if closeErr := f.Close(); closeErr != nil {
        _ = os.Remove(tmp)
        return fmt.Errorf("一時ファイルのクローズに失敗: %w", closeErr)
    }
    if err := os.Rename(tmp, path); err != nil {
        _ = os.Remove(tmp)
        return fmt.Errorf("credentials.toml のリネームに失敗 (%s → %s): %w", tmp, path, err)
    }
    if err := os.Chmod(path, 0o600); err != nil {
        return fmt.Errorf("credentials.toml のパーミッション設定に失敗 (%s): %w", path, err)
    }
    return nil
}

// CheckPermissions は path のパーミッションが 0600 (group / other に何も無し) であることを検証する。
//
//   - Windows は POSIX パーミッションが当てにならないため常に nil
//   - ファイル不在 → ErrCredentialsNotFound
//   - mode & 0o077 ≠ 0 → ErrPermissionsTooOpen
func CheckPermissions(path string) error {
    info, err := os.Stat(path)
    if err != nil {
        if errors.Is(err, fs.ErrNotExist) {
            return fmt.Errorf("%w: %s", ErrCredentialsNotFound, path)
        }
        return fmt.Errorf("credentials.toml の状態取得に失敗 (%s): %w", path, err)
    }
    if runtime.GOOS == "windows" {
        return nil
    }
    if info.Mode().Perm()&0o077 != 0 {
        return fmt.Errorf("%w: mode=%o path=%s", ErrPermissionsTooOpen, info.Mode().Perm(), path)
    }
    return nil
}
```

### 4.2 `internal/config/guard.go`

```go
package config

import (
    "errors"
    "fmt"
    "io/fs"
    "os"
    "sort"
    "strings"

    "github.com/BurntSushi/toml"
)

// ErrSecretInConfig は config.toml にシークレット系のキーが含まれていた場合に返される。
var ErrSecretInConfig = errors.New("config.toml にシークレットが含まれています (credentials.toml に分離してください)")

// secretKeys は config.toml に書いてはいけないキー名 (小文字、トップレベル / セクション内問わず)。
var secretKeys = map[string]struct{}{
    "api_key":             {},
    "api_secret":          {},
    "access_token":        {},
    "access_token_secret": {},
}

// secretSections は config.toml にあってはいけないセクション名 (小文字)。
var secretSections = map[string]struct{}{
    "xapi": {},
}

// CheckConfigNoSecrets は config.toml を生 TOML として再パースし、
// シークレット系のキー / セクションが含まれている場合 ErrSecretInConfig を返す。
//
// ファイル不在は nil (config.toml はオプショナル)。
func CheckConfigNoSecrets(path string) error {
    if _, err := os.Stat(path); err != nil {
        if errors.Is(err, fs.ErrNotExist) {
            return nil
        }
        return fmt.Errorf("config.toml の状態取得に失敗 (%s): %w", path, err)
    }

    var raw map[string]any
    if _, err := toml.DecodeFile(path, &raw); err != nil {
        return fmt.Errorf("config.toml のデコードに失敗 (%s): %w", path, err)
    }

    found := collectSecretKeys(raw)
    if len(found) == 0 {
        return nil
    }
    sort.Strings(found)
    return fmt.Errorf("%w: 検出されたキー [%s]", ErrSecretInConfig, strings.Join(found, ", "))
}

// collectSecretKeys は decoded を再帰探索し、secret 系キー / セクション名を見つけた path を返す。
func collectSecretKeys(node map[string]any) []string {
    var hits []string
    walk(node, "", &hits)
    return hits
}

// walk は node を再帰的に巡回し secretKeys / secretSections に該当するキーを hits に追加する。
// map と arrays-of-tables ([]map[string]any) の両方に再帰する (BurntSushi/toml は
// [[xapi]] のような array of tables を後者にデコードする)。
func walk(node map[string]any, prefix string, hits *[]string) {
    for k, v := range node {
        lower := strings.ToLower(k)
        full := lower
        if prefix != "" {
            full = prefix + "." + lower
        }
        if _, isSecret := secretKeys[lower]; isSecret {
            *hits = append(*hits, full)
        }
        if _, isSecretSection := secretSections[lower]; isSecretSection {
            *hits = append(*hits, full)
        }
        switch child := v.(type) {
        case map[string]any:
            walk(child, full, hits)
        case []map[string]any:
            for _, item := range child {
                walk(item, full, hits)
            }
        }
    }
}
```

## 5. テスト設計 (TDD)

### 5.1 `internal/config/credentials_test.go`

**`TestDefaultCredentialsPath`** (`t.Setenv` で `XDG_DATA_HOME`):
- xdg_set → `/tmp/xdg/x/credentials.toml`
- xdg_unset + HOME=/tmp/home → `/tmp/home/.local/share/x/credentials.toml`

**`TestCheckPermissions`** (`t.TempDir()` + `os.Chmod`):
- POSIX 限定 (`runtime.GOOS == "windows"` で `t.Skip`)
- ケース:
  - `not_exist` — path 不在 → `ErrCredentialsNotFound`
  - `mode_0600` — 0600 → nil
  - `mode_0644` — 0644 → `ErrPermissionsTooOpen`
  - `mode_0640` — 0640 (group 読み取り) → `ErrPermissionsTooOpen`
  - `mode_0400` — 0400 (owner read only、other なし) → nil (より厳しいのは OK)

**`TestLoadCredentials_FileNotFound`** → `ErrCredentialsNotFound`

**`TestLoadCredentials_Roundtrip`**:
1. `SaveCredentials(tmp, &Credentials{APIKey: "k", ...})`
2. `LoadCredentials(tmp)` → 値が一致
3. `os.Stat(tmp).Mode().Perm() == 0o600` (POSIX のみ)

**`TestLoadCredentials_PermissionsWarning`** (POSIX 限定):
1. `SaveCredentials`
2. `os.Chmod(tmp, 0o644)` で意図的に緩める
3. `log.SetOutput(&buf)` でログ出力を捕捉 (テスト終了時に `t.Cleanup` で元に戻す)
4. `LoadCredentials(tmp)` がエラーなく値を返す
5. `buf.String()` に `"warning"` (substring) を含むことをアサート (warn-and-continue ブランチを実テストでカバー)

**`TestLoadCredentials_InvalidTOML`**:
- `os.WriteFile(tmp, []byte("[xapi\n"), 0o600)` → デコードエラー

**`TestSaveCredentials_CreatesDir`**:
- 存在しない深いパス `subdir/x/credentials.toml` を渡す
- ディレクトリが 0700 で作られる (POSIX)、ファイルが 0600

**`TestSaveCredentials_NilCredentials`** → エラー

### 5.2 `internal/config/guard_test.go`

**`TestCheckConfigNoSecrets`** (table-driven, `t.TempDir()` + `os.WriteFile`):

| ケース | 内容 | 期待 |
|---|---|---|
| `file_not_exist` | path 不在 | nil |
| `clean_config` | spec §11 のテンプレ ([cli]/[liked] のみ) | nil |
| `xapi_section` | `[xapi]\napi_key="x"` | `ErrSecretInConfig`、message に "xapi" |
| `top_level_api_key` | `api_key = "x"` (セクションなし) | `ErrSecretInConfig`、message に "api_key" |
| `nested_access_token` | `[cli]\naccess_token="x"` | `ErrSecretInConfig`、message に "cli.access_token" |
| `multiple_secrets` | `[xapi]\napi_key="x"\napi_secret="y"` | `ErrSecretInConfig`、message に複数キー (sort 済み) |
| `case_insensitive` | `[XAPI]` or `API_KEY = "x"` | `ErrSecretInConfig` (大文字キーも検出) |
| `invalid_toml` | 構文不正 | デコードエラー (NOT `ErrSecretInConfig`) |

各ケースで `errors.Is(err, ErrSecretInConfig)` の判定も実施。

## 6. 実装手順 (TDD ステップ)

### Step 1: Red — credentials_test.go (CheckPermissions / DefaultCredentialsPath)
- 上記のテストケースを書く (型・関数は未実装でコンパイル不可)

### Step 2: Green — credentials.go 雛形
- `Credentials` 型 + `credentialsFile` ラッパ + 定数 + エラー定義
- `DefaultCredentialsPath` / `CheckPermissions` 実装
- テスト pass を確認

### Step 3: Red — credentials_test.go (LoadCredentials / SaveCredentials)
- ラウンドトリップ / ファイル不在 / パーミッション警告 / 不正 TOML / nil 引数のテスト追加

### Step 4: Green — credentials.go の Load/Save 実装
- 上記の設計通りに実装
- テスト pass を確認

### Step 5: Red — guard_test.go
- 8 ケースのテーブル駆動テスト

### Step 6: Green — guard.go 実装
- `CheckConfigNoSecrets` + `collectSecretKeys` + `walk` 実装
- テスト pass を確認

### Step 7: Refactor
- 重複削除、ヘルパ抽出、コメント追加
- `golangci-lint run ./...` 0 issues
- `go vet ./...` 警告ゼロ
- `go test -race -count=1 ./...` 全 pass

### Step 8: コミット
```
feat(config): credentials.toml ローダーと config.toml シークレットガードを追加

- internal/config/credentials.go: Credentials 型 + LoadCredentials / SaveCredentials / DefaultCredentialsPath / CheckPermissions
- internal/config/guard.go: CheckConfigNoSecrets で config.toml への secret 混入を検知
- パーミッションは 0700 ディレクトリ / 0600 ファイルを強制 (POSIX)
- TDD で網羅: ラウンドトリップ / パーミッション / TOML 構文 / セクション横断 secret 検出
- spec §10 セキュリティ / §11 Configuration に準拠

Plan: plans/x-m04-credentials-guard.md
```

## 7. リスク評価

| リスク | 影響 | 対策 |
|---|---|---|
| Windows パーミッション挙動 | 低 | `runtime.GOOS == "windows"` で `CheckPermissions` は常に nil 返却 / 関連テストは `t.Skip` |
| `os.Chmod` の TOC-TOU (シンボリックリンク経由) | 中 | tmp + rename パターンで minimize。further hardening (`O_NOFOLLOW`) は M12 で再評価 |
| BurntSushi/toml `DecodeFile` が generic map のとき値を `int64` / `string` / `map[string]any` に分解 | 低 | `walk` は `map[string]any` のみ再帰、それ以外は値を見ない (key だけ判定) |
| `log.Printf` がデフォルトで stderr に出る → CLI 出力 (stdout JSON 等) を汚染しない | 低 | `log` の標準出力先は stderr なので JSON stdout は汚染されない |
| credentials の値がエラーメッセージに混入 | 高 | エラーメッセージにはキー名 / パス / mode のみ。値は載せない |
| `errcheck` で `f.Close()` / `os.Remove(tmp)` が拾われる | 中 | エラーパスでは `_ = f.Close()` / `_ = os.Remove(tmp)` のように明示的に破棄 |
| `revive:exported` で型・関数・フィールド全部に doc コメント要求 | 中 | 全公開シンボルに日本語コメント記述 |
| `gocritic:singleCaseSwitch` 警告 | 低 | `LoadCredentials` の switch は 3 case あるので問題なし |
| ファイル書き込みテスト時の umask 影響 | 中 | `SaveCredentials` の最後に `os.Chmod(path, 0o600)` で umask を打ち消し、テストで `Stat` 結果を直接アサート |

## 8. Definition of Done

- [ ] `internal/config/{credentials,guard}.go` + 各 `_test.go` 計 4 ファイル追加
- [ ] `go test -race -count=1 ./...` で M1+M2+M3 既存テスト + M4 追加分が全 pass
- [ ] `golangci-lint run ./...` で issues 0
- [ ] `go vet ./...` で警告ゼロ
- [ ] `go.mod` 変更なし (依存追加なし)
- [ ] 1 commit にまとめ、message に Plan フッターを記載、push しない
- [ ] CYCLE_RESULT で commit SHA / 追加テスト数 / 後続マイルストーン (M5) へのハンドオフを記述

## 9. M5 へのハンドオフ (予告)

M5 (`internal/xapi/oauth1.go`) が知るべき情報:

- `Credentials` 型のフィールド名: `APIKey` / `APISecret` / `AccessToken` / `AccessTokenSecret` (4 文字列)
- `LoadCredentials(path string) (*Credentials, error)` シグネチャ
- `DefaultCredentialsPath() (string, error)` で標準パス取得
- `ErrCredentialsNotFound` / `ErrPermissionsTooOpen` / `ErrSecretInConfig` の 3 エラー定数を `errors.Is` で判別可能
- CLI モードでは env > credentials.toml の優先順位を M9 で実装する (M4 では credentials.toml の I/O のみ提供)
- MCP モードでは credentials.toml を**読まない**: 呼び出し側 (M24 で実装) で分岐させる
- パッケージ doc コメントは引き続き `config.go` の 1 箇所のみ
