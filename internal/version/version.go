// Package version は x コマンドのバージョン情報を保持し提供する。
//
// 3 つの公開変数 Version / Commit / Date は ldflags で書き換える前提で
// var キーワードで定義する。GoReleaser や手動ビルド時には
//
//	go build -ldflags "-X github.com/youyo/x/internal/version.Version=v0.1.0 \
//	                   -X github.com/youyo/x/internal/version.Commit=abcdef0 \
//	                   -X github.com/youyo/x/internal/version.Date=2026-05-12"
//
// のように上書きする。ビルド時に注入されなかった場合はそれぞれ
// "dev" / "none" / "unknown" がデフォルト値として使われる。
package version

import "fmt"

// Version はビルド時に ldflags で書き込まれるセマンティックバージョン文字列。
// 未注入時のデフォルトは "dev"。
var Version = "dev"

// Commit はビルド時に ldflags で書き込まれる Git コミット SHA。
// 未注入時のデフォルトは "none"。
var Commit = "none"

// Date はビルド時に ldflags で書き込まれる ISO8601 形式のビルド日時。
// 未注入時のデフォルトは "unknown"。
var Date = "unknown"

// Info はバージョン情報を JSON シリアライズ可能な形でまとめた構造体。
// CLI の `x version` (default: JSON 出力) でそのまま json.Marshal される。
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// NewInfo は現在のパッケージ変数 (Version/Commit/Date) のスナップショットを
// Info 構造体として返す。テスト時に変数を書き換えてから NewInfo() を呼ぶことで
// 一貫したスナップショットを得られる。
func NewInfo() Info {
	return Info{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
	}
}

// String は human-readable な単一行のバージョン文字列を返す。
// 例: "dev (commit: none, built: unknown)" / "v0.1.0 (commit: abcdef0, built: 2026-05-12)"
func String() string {
	return fmt.Sprintf("%s (commit: %s, built: %s)", Version, Commit, Date)
}
