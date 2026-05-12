package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

// LoadCLI は path から CLIConfig を読み込んで返す。
//
// 挙動:
//   - path のファイルが存在しない (fs.ErrNotExist) → DefaultCLIConfig() を返し、エラー nil
//   - その他の Stat エラー → エラー返却
//   - TOML 構文エラー → エラー返却 (呼び出し側が exit code (UsageError 等) にマップ可能)
//   - 読み込み成功 → デコード結果のうちゼロ値フィールドだけデフォルト値で補完
//
// シークレットの混入検知 (guard) は M4 で追加する。本マイルストーンでは
// 非機密設定の読み込みに専念する。
func LoadCLI(path string) (*CLIConfig, error) {
	defaults := DefaultCLIConfig()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return defaults, nil
		}
		return nil, fmt.Errorf("stat config.toml (%s): %w", path, err)
	}

	var decoded CLIConfig
	if _, err := toml.DecodeFile(path, &decoded); err != nil {
		return nil, fmt.Errorf("decode config.toml (%s): %w", path, err)
	}
	applyDefaults(&decoded, defaults)
	return &decoded, nil
}

// applyDefaults は decoded のゼロ値フィールドを defaults 値で補完する。
//
// 文字列は空文字列 "" を、数値は 0 をゼロ値とみなし、それぞれ defaults の値で上書きする。
// spec §11 の数値フィールド (default_max_results / default_max_pages) は正の値前提のため、
// 「ユーザーが意図的に 0 を指定した」ケースとは区別しない設計とする。
func applyDefaults(decoded, defaults *CLIConfig) {
	if decoded.CLI.Output == "" {
		decoded.CLI.Output = defaults.CLI.Output
	}
	if decoded.CLI.LogLevel == "" {
		decoded.CLI.LogLevel = defaults.CLI.LogLevel
	}
	if decoded.Liked.DefaultMaxResults == 0 {
		decoded.Liked.DefaultMaxResults = defaults.Liked.DefaultMaxResults
	}
	if decoded.Liked.DefaultMaxPages == 0 {
		decoded.Liked.DefaultMaxPages = defaults.Liked.DefaultMaxPages
	}
	if decoded.Liked.DefaultTweetFields == "" {
		decoded.Liked.DefaultTweetFields = defaults.Liked.DefaultTweetFields
	}
	if decoded.Liked.DefaultExpansions == "" {
		decoded.Liked.DefaultExpansions = defaults.Liked.DefaultExpansions
	}
	if decoded.Liked.DefaultUserFields == "" {
		decoded.Liked.DefaultUserFields = defaults.Liked.DefaultUserFields
	}
}
