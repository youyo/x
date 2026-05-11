# M3: `internal/config` XDG loader (非機密設定のみ)

> Layer 2: M3 マイルストーン詳細計画。Layer 1 ロードマップは [plans/x-roadmap.md](./x-roadmap.md) を参照。
> 前マイルストーン: [plans/x-m02-ci-baseline.md](./x-m02-ci-baseline.md) (commit 41ae9ad)
> 後続マイルストーン: M4 (`internal/config` credentials.toml + guard)

## 1. Overview

| 項目 | 値 |
|---|---|
| マイルストーン | M3 |
| ステータス | Planned |
| バージョン | 0.0.3 (CLI 設定ローダー) |
| 依存 | M1 (Cobra スケルトン) / M2 (CI + lint v2 厳格構成) |
| 対象モジュール | `github.com/youyo/x/internal/config` (新規) |
| 追加コード依存 | `github.com/BurntSushi/toml v1.x` |
| 検証方式 | TDD (Red → Green → Refactor) + table-driven + race + lint 0 |
| 対象ファイル数 (新規) | 6 ファイル (xdg.go / config.go / loader_cli.go + 各 _test.go) |
| LOC 目安 | ~350 (実装 ~150 / テスト ~200) |
| 参考 | spec §11 Configuration (`docs/specs/x-spec.md`) |

### M3 で**やること**
- `internal/config/xdg.go` — `Dir()` / `DataDir()` を提供。`$XDG_CONFIG_HOME` / `$XDG_DATA_HOME` を尊重し、未設定時は `$HOME/.config/x` / `$HOME/.local/share/x` に解決する (`Dir` は revive:exported の stutter 警告回避のため。`config.Dir()` = config パッケージの Dir → 「config dir」を素直に表現)
- `internal/config/config.go` — `CLIConfig` / `LikedConfig` 型定義 + `DefaultCLIConfig()` ヘルパ + パッケージ doc コメント
- `internal/config/loader_cli.go` — `LoadCLI(path string) (*CLIConfig, error)` (BurntSushi/toml 読み込み、ゼロ値補完、ファイル不在時はデフォルト)
- 各ファイルに対応する `_test.go` を TDD で先行作成

### M3 で**やらないこと** (将来マイルストーン)
- `credentials.toml` 読み書き (→ M4)
- `guard.go` (`config.toml` へのシークレット混入検知) (→ M4)
- 環境変数 override / CLI flag override (→ M9 / M10 で Cobra フラグ経由)
- `x configure` 対話モード (→ M12)
- TOML 書き込み (→ M12)

## 2. Goal

### 機能要件
- [ ] `config.Dir()` が以下の優先順位でディレクトリを返す:
  1. `$XDG_CONFIG_HOME` が非空 → `$XDG_CONFIG_HOME/x`
  2. それ以外 → `$HOME/.config/x` (spec §11 「`${XDG_CONFIG_HOME:-~/.config}/x/config.toml`」に厳密準拠)
  3. `$HOME` も解決できない極端な状況 (CI でも稀) → エラー返却
- [ ] `config.DataDir()` が同様に `$XDG_DATA_HOME/x` / `$HOME/.local/share/x` を返す
- [ ] `CLIConfig` 構造体が `[cli]` と `[liked]` セクションを表現する (spec §11 のテンプレ準拠)
- [ ] `DefaultCLIConfig()` がスペック既定値を持つ `*CLIConfig` を返す
- [ ] `LoadCLI(path)` が以下を満たす:
  - ファイル不在 → `DefaultCLIConfig()` をそのまま返し、エラーなし
  - 不正 TOML → エラー返却 (パッケージ呼び出し側が exit 6 (`UsageError`) にマップできる)
  - 存在 → TOML をデコードし、空フィールドはデフォルト値で補完
- [ ] `DefaultCLIConfigPath()` が `filepath.Join(Dir(), "config.toml")` を返す (M9/M10 用ヘルパ)

### 非機能要件
- [ ] `go test -race -count=1 ./...` 全 pass (M1+M2 既存 25 テスト + M3 追加分)
- [ ] `golangci-lint run ./...` 0 issues (errcheck / revive:exported / package-comments / gocritic 等)
- [ ] `go vet ./...` 警告ゼロ
- [ ] 全公開シンボル (関数 / 構造体フィールド / エクスポート定数) に**日本語**の doc コメント
- [ ] パッケージ doc コメントは `config.go` の冒頭 1 箇所に集約 (M1/M2 の前例に倣う。`xdg.go` / `loader_cli.go` / `_test.go` には付けない)

## 3. Spec Mapping

| spec §11 規定 | M3 実装箇所 |
|---|---|
| `${XDG_CONFIG_HOME:-~/.config}/x/config.toml` | `Dir()` |
| `${XDG_DATA_HOME:-~/.local/share}/x/credentials.toml` のディレクトリ部 | `DataDir()` (M4 で `credentials.toml` ファイル名と組み合わせる) |
| `[cli] output / log_level` | `CLIConfig.CLI.Output` / `CLIConfig.CLI.LogLevel` |
| `[liked] default_max_results` | `CLIConfig.Liked.DefaultMaxResults` |
| `[liked] default_max_pages` | `CLIConfig.Liked.DefaultMaxPages` |
| `[liked] default_tweet_fields` | `CLIConfig.Liked.DefaultTweetFields` |
| `[liked] default_expansions` | `CLIConfig.Liked.DefaultExpansions` |
| `[liked] default_user_fields` | `CLIConfig.Liked.DefaultUserFields` |

### Default 値 (spec §11 のテンプレ準拠)

```go
&CLIConfig{
    CLI: CLISection{
        Output:   "json",
        LogLevel: "info",
    },
    Liked: LikedSection{
        DefaultMaxResults:  100,
        DefaultMaxPages:    50,
        DefaultTweetFields: "id,text,author_id,created_at,entities,public_metrics",
        DefaultExpansions:  "author_id",
        DefaultUserFields:  "username,name",
    },
}
```

## 4. ファイル設計

### 4.1 `internal/config/config.go`

```go
// Package config は x コマンドの設定ファイル (XDG 準拠) を読み込む。
//
// M3 (本マイルストーン) では非機密設定 (config.toml) のみを対象とする。
// シークレット (credentials.toml) と guard (シークレット混入検知) は M4 で追加する。
// MCP モードでは config.toml / credentials.toml は一切読まず、環境変数のみを使う
// 設計だが、その分岐は呼び出し側の責務とする。
//
// spec 参照: docs/specs/x-spec.md §11 Configuration
package config

// CLIConfig は ~/.config/x/config.toml の構造を表す (非機密設定のみ)。
type CLIConfig struct {
    CLI   CLISection   `toml:"cli"`
    Liked LikedSection `toml:"liked"`
}

// CLISection は config.toml の [cli] セクションを表す。
type CLISection struct {
    Output   string `toml:"output"`
    LogLevel string `toml:"log_level"`
}

// LikedSection は config.toml の [liked] セクションを表す。
type LikedSection struct {
    DefaultMaxResults  int    `toml:"default_max_results"`
    DefaultMaxPages    int    `toml:"default_max_pages"`
    DefaultTweetFields string `toml:"default_tweet_fields"`
    DefaultExpansions  string `toml:"default_expansions"`
    DefaultUserFields  string `toml:"default_user_fields"`
}

// DefaultCLIConfig は spec §11 のテンプレに従ったデフォルト値を持つ CLIConfig を返す。
func DefaultCLIConfig() *CLIConfig { ... }
```

### 4.2 `internal/config/xdg.go`

```go
package config

import (
    "errors"
    "os"
    "path/filepath"
)

// appSubDir は XDG ベースディレクトリ配下に作成するサブディレクトリ名。
const appSubDir = "x"

// ErrHomeNotResolved は $HOME も解決できなかった場合に返す番兵エラー。
var ErrHomeNotResolved = errors.New("HOME ディレクトリを解決できませんでした")

// Dir は CLI 非機密設定ファイルの保存ディレクトリ ($XDG_CONFIG_HOME/x など) を返す。
// (関数名は revive:exported の stutter 警告回避のため `ConfigDir` ではなく `Dir`。)
//
// 優先順位:
//  1. 環境変数 $XDG_CONFIG_HOME (非空) → 末尾に "/x"
//  2. $HOME → 末尾に "/.config/x"
//  3. いずれも解決できなければ ErrHomeNotResolved
func Dir() (string, error) { ... }

// DataDir は CLI シークレットファイルの保存ディレクトリ ($XDG_DATA_HOME/x など) を返す。
// 解決ロジックは Dir と対称 ("Data" は "config" と被らないので stutter にならず維持)。
func DataDir() (string, error) { ... }

// DefaultCLIConfigPath は Dir()/config.toml を返す (M9/M10 から使用)。
func DefaultCLIConfigPath() (string, error) { ... }
```

**$HOME の解決方法**: `os.UserHomeDir()` を使う (`HOME` 環境変数を見るが、未設定時は `os/user` パッケージで補完する Go 標準の挙動)。`os.UserConfigDir()` は macOS で `~/Library/Application Support` を返すため、spec §11 (`${XDG_CONFIG_HOME:-~/.config}`) と齟齬が出る。よって `os.UserConfigDir()` は**使わず**、明示的に `$XDG_CONFIG_HOME` か `$HOME/.config` のいずれかに解決する。

### 4.3 `internal/config/loader_cli.go`

```go
package config

import (
    "errors"
    "fmt"
    "io/fs"
    "os"

    "github.com/BurntSushi/toml"
)

// LoadCLI は path から CLIConfig を読み込む。
//
// 挙動:
//   - ファイルが存在しない (os.IsNotExist) → DefaultCLIConfig() をそのまま返し、エラー nil
//   - TOML 構文エラー → エラー返却 (呼び出し側が UsageError に変換可能)
//   - 読み込み成功 → 値がゼロ値のフィールドだけデフォルト値で補完
func LoadCLI(path string) (*CLIConfig, error) {
    cfg := DefaultCLIConfig()
    if _, err := os.Stat(path); err != nil {
        if errors.Is(err, fs.ErrNotExist) {
            return cfg, nil
        }
        return nil, fmt.Errorf("config.toml の状態取得に失敗: %w", err)
    }

    var decoded CLIConfig
    if _, err := toml.DecodeFile(path, &decoded); err != nil {
        return nil, fmt.Errorf("config.toml のデコードに失敗: %w", err)
    }
    applyDefaults(&decoded, cfg)
    return &decoded, nil
}

// applyDefaults は decoded のゼロ値フィールドを defaults 値で埋める。
func applyDefaults(decoded, defaults *CLIConfig) { ... }
```

**`applyDefaults` の補完ルール**:
- 文字列フィールド: `""` のときデフォルト値で上書き
- 数値フィールド: `0` のときデフォルト値で上書き (spec の `default_max_results: 100` などは正の値前提)

**設計判断 (ゼロ値 = 未設定の限界)**: 「ユーザーが意図的に `default_max_results = 0` と書きたい」ケースとは区別できない。spec §11 ではこれらの値は正の値前提なので OK。将来 M9/M10 で env override / CLI flag override を被せるときに、この区別が必要なら `*int` (ポインタ型) への切り替えを検討する。

## 5. テスト設計 (TDD: Red → Green → Refactor)

### 5.1 `internal/config/xdg_test.go`

**テーブル駆動 + `t.Setenv`** で環境変数を制御する。テストは以下のケースを網羅:

| ケース | XDG_CONFIG_HOME | HOME | 期待結果 |
|---|---|---|---|
| `xdg_set` | `/tmp/xdg` | `/tmp/home` | `/tmp/xdg/x` |
| `xdg_unset` | (未設定) | `/tmp/home` | `/tmp/home/.config/x` |
| `xdg_empty` | `""` | `/tmp/home` | `/tmp/home/.config/x` (空は未設定扱い) |

`DataDir` も同様に 3 ケース (`XDG_DATA_HOME` / `HOME`)。

`DefaultCLIConfigPath` は ConfigDir に依存する 2 ケース (`xdg_set` 通過 / 末尾が `config.toml`)。

**`ErrHomeNotResolved` 経路について**: 当初は `XDG_*` と `HOME` の両方を未設定にしてエラー経路を検証する予定だったが、`os.UserHomeDir()` は Linux/macOS 上で `/etc/passwd` の pw_dir に fallback するため、開発機・CI ともにエラーを返さない。「常に `t.Skip` するテスト」は無いほうがマシ (false coverage を生む) なので、**`ErrHomeNotResolved` 経路はテストで網羅せず、コードレビューでカバー**する。`ErrHomeNotResolved` のエラー定義自体はディフェンシブに残す。

### 5.2 `internal/config/config_test.go`

- `DefaultCLIConfig()` の戻り値が spec §11 のテンプレと一致することを表明 (`cmp.Diff` ではなく `reflect.DeepEqual` で標準ライブラリ縛り)
- `CLIConfig` の TOML タグが期待通りであることは loader テストで間接的に検証されるので、ここでは個別に行わない

### 5.3 `internal/config/loader_cli_test.go`

`t.TempDir()` でテスト専用ディレクトリを切り、TOML ファイルを作って読み込む table-driven テスト:

| ケース | ファイル状態 | 期待 |
|---|---|---|
| `file_not_found` | path 不在 | `DefaultCLIConfig()` と等価、err == nil |
| `empty_file` | 空ファイル | デフォルト値が全フィールドに入る |
| `partial_file` | `[cli] output = "ndjson"` のみ | output=ndjson、それ以外はデフォルト |
| `mixed_section` | `[cli] output="ndjson"` + `[liked] default_max_pages = 200` | output=ndjson / max_pages=200 / その他はデフォルト (section 横断補完を検証) |
| `full_file` | spec §11 テンプレを全部書く | すべての値が読み込まれる |
| `invalid_toml` | 構文不正 (例: `[cli` 未閉) | err != nil、err にデコードメッセージを含む |
| `unknown_keys_ignored` | `[cli] foo = "bar"` のみ | err nil、フィールドはデフォルト (BurntSushi/toml は不明キーを無視する) |

各ケースで `os.WriteFile(filepath.Join(t.TempDir(), "config.toml"), []byte(...), 0o644)` でファイルを作成。

`applyDefaults` の挙動は loader_cli_test の `partial_file` ケースで間接的にカバーされる。

## 6. 実装手順 (TDD ステップ)

### Step 1: 依存追加
```
go get github.com/BurntSushi/toml@latest
```
go.mod / go.sum を更新。

### Step 2: Red — xdg_test.go 作成
- 上記のテーブル駆動テストを書く (型・関数は未実装なのでコンパイル不可)

### Step 3: Green — xdg.go 実装
- `ConfigDir` / `DataDir` / `DefaultCLIConfigPath` / `ErrHomeNotResolved` を実装
- 内部ヘルパ `xdgDir(envName, homeRel string) (string, error)` を private で抽出 (DRY)
- `go test -race -count=1 ./internal/config/...` 全 pass を確認

### Step 4: Red — config_test.go 作成
- `DefaultCLIConfig()` の中身を期待する `reflect.DeepEqual` テスト

### Step 5: Green — config.go 実装
- パッケージ doc コメント + 全 struct + `DefaultCLIConfig()` 実装
- 各 struct フィールドに doc コメント (revive:exported 対策)

### Step 6: Red — loader_cli_test.go 作成
- 6 ケースのテーブル駆動テスト

### Step 7: Green — loader_cli.go 実装
- `LoadCLI` + `applyDefaults` 実装

### Step 8: Refactor
- 重複削除、ヘルパ抽出、コメント追加
- `golangci-lint run ./...` を実行して 0 issues
- `go vet ./...` 警告ゼロ
- `go test -race -count=1 ./...` 全 pass (M1+M2 既存 25 テスト + M3 追加分)

### Step 9: コミット
```
feat(config): XDG 準拠の config.toml ローダーを追加 (非機密設定のみ)

- internal/config パッケージ新規作成
- ConfigDir / DataDir で XDG_CONFIG_HOME / XDG_DATA_HOME を解決
- CLIConfig / LikedConfig 型と DefaultCLIConfig ヘルパ
- LoadCLI で BurntSushi/toml を使った設定ファイル読み込み
- ファイル不在時はデフォルト値を返し、ゼロ値フィールドは自動補完
- TDD で網羅: xdg / config / loader_cli の table-driven テスト

Plan: plans/x-m03-config-xdg.md
```

push は M3 では行わない (タスク指示: 「git commit (push しない)」)。

## 7. リスク評価

| リスク | 影響 | 対策 |
|---|---|---|
| `os.UserHomeDir()` が `/etc/passwd` に fallback してエラー経路に到達しない | 低 | `ErrHomeNotResolved` 経路はテストで網羅せずコードレビューでカバー (§5.1 参照)。エラー定義自体はディフェンシブに残す |
| BurntSushi/toml の不明キー扱い | 低 | `toml.Decoder.DisallowUnknownFields` は使わず、不明キーは無視する (将来 spec 拡張時の前方互換性) |
| `golangci-lint v2` の `gocritic:emptyStringTest` 等で `s == ""` が警告される可能性 | 低 | `len(s) == 0` を使う or `disabled-checks` に追加する。まずは実装後に確認 |
| `revive:exported` で各構造体フィールドに doc コメント要求 | 中 | 全フィールドに `// XYZ は ...` 形式の日本語コメントを記述 |
| `errcheck` で `os.WriteFile` / `toml.DecodeFile` のエラーを取り逃す | 低 | テスト中の `os.WriteFile` は `if err != nil { t.Fatal(err) }` で必ず処理 |
| `golangci-lint` で `package-comments` がテストファイルでも警告 | 中 | `.golangci.yml` 既存の `exclusions.rules` で `_test\.go` は対象外。追加対応不要 |
| Dockerfile builder の `go mod download` キャッシュ失効 | 低 | M2 ハンドオフ通り許容 |

## 8. Definition of Done

- [ ] `internal/config/{xdg,config,loader_cli}.go` + 各 `_test.go` 計 6 ファイル作成
- [ ] `go test -race -count=1 ./...` で M1+M2 既存 25 テスト + M3 追加分 (推定 12〜15) が全 pass
- [ ] `golangci-lint run ./...` で issues 0
- [ ] `go vet ./...` で警告ゼロ
- [ ] `go.mod` / `go.sum` に `github.com/BurntSushi/toml` 追加
- [ ] 1 commit にまとめ、message に Plan フッターを記載、push しない
- [ ] CYCLE_RESULT で commit SHA / 追加テスト数 / 後続マイルストーン (M4) へのハンドオフを記述

## 9. M4 へのハンドオフ (予告)

- M3 で確立する `ConfigDir()` / `DataDir()` を M4 がそのまま使う
- M4 は `internal/config/credentials.go` (`DataDir()/credentials.toml` のパーミッション 0600 強制 R/W) と `internal/config/guard.go` (`CLIConfig` を見てシークレット混入を拒否する関数) を追加
- パッケージ doc コメントは M3 で `config.go` に置いた 1 箇所のみ。M4 で追加するファイルにはコメントを書かない (前例維持)
- `applyDefaults` の補完ロジックは M9/M10 で env override / CLI flag override を被せる際の土台になる
