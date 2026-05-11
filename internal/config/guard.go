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

// ErrSecretInConfig は config.toml にシークレット系のキーが含まれていた場合に返される番兵エラー。
// spec §11「シークレット系のキーが config.toml に書かれている場合はエラー終了する」に対応。
var ErrSecretInConfig = errors.New("config.toml にシークレットが含まれています (credentials.toml に分離してください)")

// secretKeys は config.toml に書いてはいけないキー名 (小文字、セクション位置を問わない)。
// 大文字小文字混在は呼び出し側で ToLower 後に判定する。
var secretKeys = map[string]struct{}{
	"api_key":             {},
	"api_secret":          {},
	"access_token":        {},
	"access_token_secret": {},
}

// secretSections は config.toml にあってはいけないセクション (テーブル) 名 (小文字)。
// 中身が空でもセクションキーが存在する時点で拒否する。
var secretSections = map[string]struct{}{
	"xapi": {},
}

// CheckConfigNoSecrets は path の config.toml をパースし、シークレット系のキー / セクションが
// 含まれている場合 ErrSecretInConfig を返す。
//
// 挙動:
//   - ファイル不在 → nil (config.toml はオプショナル)
//   - TOML 構文エラー → wrap してエラー返却 (ErrSecretInConfig ではない)
//   - secret 検出 → ErrSecretInConfig を wrap、エラーメッセージに検出されたキーのパス (sort 済み) を含む
//
// セキュリティ: 検出されたキーの「値」は絶対にエラーメッセージへ含めない。
// 値が漏れる経路を遮断するため、本関数は値を一切参照せずキー名のみで判定する。
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

// collectSecretKeys は decoded を再帰探索し、secret 系キー / セクション名を見つけたパス
// (例: "cli.access_token") のスライスを返す。順序は保証しない (呼び出し側で sort)。
func collectSecretKeys(node map[string]any) []string {
	var hits []string
	walkTOML(node, "", &hits)
	return hits
}

// walkTOML は node を再帰的に巡回し、secretKeys / secretSections に該当するキーを
// hits に追加する。BurntSushi/toml はテーブルを map[string]any、配列テーブルを
// []map[string]any にデコードするため両方に再帰する。
func walkTOML(node map[string]any, prefix string, hits *[]string) {
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
			walkTOML(child, full, hits)
		case []map[string]any:
			for _, item := range child {
				walkTOML(item, full, hits)
			}
		}
	}
}
