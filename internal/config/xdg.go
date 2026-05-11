package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// appSubDir は XDG ベースディレクトリ配下に作成するアプリ専用サブディレクトリ名。
// spec §11 で `~/.config/x/` / `~/.local/share/x/` を指定しているのに対応する。
const appSubDir = "x"

// configFileName は CLI 非機密設定ファイルのファイル名。
const configFileName = "config.toml"

// ErrHomeNotResolved は $XDG_CONFIG_HOME / $XDG_DATA_HOME と $HOME のいずれも
// 解決できなかった極端な状況で返す番兵エラー。CI / 開発機では通常到達しない。
var ErrHomeNotResolved = errors.New("HOME ディレクトリを解決できませんでした")

// Dir は CLI 非機密設定ファイルを置くディレクトリ ($XDG_CONFIG_HOME/x など) を返す。
//
// 優先順位:
//  1. 環境変数 $XDG_CONFIG_HOME が非空 → "<XDG_CONFIG_HOME>/x"
//  2. それ以外 → "<HOME>/.config/x" (os.UserHomeDir で HOME を解決)
//  3. いずれも解決不能 → ErrHomeNotResolved
//
// macOS でも os.UserConfigDir() (~/Library/Application Support) は使わず、
// spec §11 の `${XDG_CONFIG_HOME:-~/.config}` に厳密準拠する。
func Dir() (string, error) {
	return xdgDir("XDG_CONFIG_HOME", ".config")
}

// DataDir は CLI シークレット (credentials.toml) や idproxy sqlite DB を置くディレクトリ
// ($XDG_DATA_HOME/x など) を返す。優先順位は Dir と対称。
func DataDir() (string, error) {
	return xdgDir("XDG_DATA_HOME", filepath.Join(".local", "share"))
}

// DefaultCLIConfigPath は Dir()/config.toml を返す。
// M9/M10 等の呼び出し側が config.toml のフルパスを得るためのヘルパ。
func DefaultCLIConfigPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

// xdgDir は XDG_*_HOME 環境変数 envName と HOME 配下のサブパス homeRel を引数に
// アプリ用サブディレクトリ ("/x") までを連結して返す内部ヘルパ。
func xdgDir(envName, homeRel string) (string, error) {
	if v := os.Getenv(envName); v != "" {
		return filepath.Join(v, appSubDir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("%w: %v", ErrHomeNotResolved, err)
	}
	return filepath.Join(home, homeRel, appSubDir), nil
}
