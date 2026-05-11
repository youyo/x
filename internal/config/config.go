// Package config は x コマンドの設定ファイル (XDG Base Directory Specification 準拠) を読み込む。
//
// M3 (本マイルストーン) では非機密設定 (`~/.config/x/config.toml`) のみを対象とする。
// シークレット (`~/.local/share/x/credentials.toml`) と guard (シークレット混入検知) は
// 後続マイルストーン M4 で追加する。
//
// MCP モードでは config.toml / credentials.toml を一切読まず環境変数のみを使う設計
// (spec §11) だが、その分岐は呼び出し側の責務とする。
//
// spec 参照: docs/specs/x-spec.md §11 Configuration
package config

// CLIConfig は `~/.config/x/config.toml` の構造を表す (非機密設定のみ)。
//
// シークレット系のキー (api_key / access_token 等) はこの構造体に**入れない**。
// 万一読み込んでしまっても guard (M4) で検知してエラー終了する設計。
type CLIConfig struct {
	// CLI は [cli] セクションを表す。
	CLI CLISection `toml:"cli"`
	// Liked は [liked] セクションを表す。
	Liked LikedSection `toml:"liked"`
}

// CLISection は config.toml の [cli] セクションを表す。
type CLISection struct {
	// Output は出力形式 (json / ndjson / table)。
	Output string `toml:"output"`
	// LogLevel はログレベル (debug / info / warn / error)。
	LogLevel string `toml:"log_level"`
}

// LikedSection は config.toml の [liked] セクションを表す。
// すべて `x liked list` サブコマンドのデフォルト値として使用される。
type LikedSection struct {
	// DefaultMaxResults は --max-results フラグのデフォルト値 (1 ページあたり最大件数)。
	DefaultMaxResults int `toml:"default_max_results"`
	// DefaultMaxPages は --max-pages フラグのデフォルト値 (--all 時の最大ページ数)。
	DefaultMaxPages int `toml:"default_max_pages"`
	// DefaultTweetFields は X API tweet.fields パラメータのデフォルト値 (カンマ区切り)。
	DefaultTweetFields string `toml:"default_tweet_fields"`
	// DefaultExpansions は X API expansions パラメータのデフォルト値 (カンマ区切り)。
	DefaultExpansions string `toml:"default_expansions"`
	// DefaultUserFields は X API user.fields パラメータのデフォルト値 (カンマ区切り)。
	DefaultUserFields string `toml:"default_user_fields"`
}

// DefaultCLIConfig は spec §11 のテンプレに従ったデフォルト値を持つ *CLIConfig を返す。
// 呼び出しごとに新しいインスタンスを返すので、呼び出し側で安全にミューテーション可能。
func DefaultCLIConfig() *CLIConfig {
	return &CLIConfig{
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
}
