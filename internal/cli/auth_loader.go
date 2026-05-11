package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/youyo/x/internal/config"
	"github.com/youyo/x/internal/xapi"
)

// ErrCredentialsMissing は CLI 実行時に X API クレデンシャルが env / credentials.toml のいずれにも
// 揃っていない状態を表す番兵エラーである。
//
// xapi.ErrAuthentication を Unwrap で内包しており、cmd/x/main.go の run() では
// errors.Is(err, xapi.ErrAuthentication) 経由で exit code 3 (ExitAuthError) に
// 写像される (spec §6 エラーハンドリングポリシー)。
//
// 設計判断 (plans/x-m09-cli-me.md D-1) によりエラーメッセージは欠落フィールド名を
// 出さず一般化する。情報露出最小化とテスト stable の両立が狙い。
var ErrCredentialsMissing = fmt.Errorf(
	"%w: X API credentials missing (set X_API_KEY/X_API_SECRET/X_ACCESS_TOKEN/X_ACCESS_TOKEN_SECRET env vars or credentials.toml)",
	xapi.ErrAuthentication,
)

// LoadCredentialsFromEnvOrFile は CLI モードの認証情報を env > credentials.toml の優先順位で解決する。
//
// 解決アルゴリズム (ファイル単位):
//  1. 4 つの環境変数 (X_API_KEY / X_API_SECRET / X_ACCESS_TOKEN / X_ACCESS_TOKEN_SECRET) を読む
//  2. すべて非空であれば env から構築した *config.Credentials を返す (file は読まない)
//  3. 1 つでも欠けていれば credentials.toml をデフォルトパスから読み込む
//     - file 完備 → file の値を返却
//     - file が ErrCredentialsNotFound → ErrCredentialsMissing
//     - file 完備でない (1 フィールドでも空) → ErrCredentialsMissing
//     - その他の読み込み / パースエラー → wrap して返却 (exit 1)
//
// env 上で空文字 ("X_API_KEY=") を明示的に設定したケースは「未設定」と同じ扱いとする
// (シンプル化、CLI 仕様としての一貫性のため)。
//
// 本関数は context を受け取らない。ファイル I/O は短時間で完結し、ctx cancel を
// 必要としないため (詳細は plans/x-m09-cli-me.md D-6)。
func LoadCredentialsFromEnvOrFile() (*config.Credentials, error) {
	if c, ok := credentialsFromEnv(); ok {
		return c, nil
	}

	path, err := config.DefaultCredentialsPath()
	if err != nil {
		return nil, fmt.Errorf("auth loader: resolve credentials path: %w", err)
	}
	c, err := config.LoadCredentials(path)
	if err != nil {
		if errors.Is(err, config.ErrCredentialsNotFound) {
			return nil, ErrCredentialsMissing
		}
		return nil, fmt.Errorf("auth loader: load credentials.toml: %w", err)
	}
	if !credentialsComplete(c) {
		return nil, ErrCredentialsMissing
	}
	return c, nil
}

// credentialsFromEnv は環境変数から *config.Credentials を構築する。
//
// 4 つの env のいずれかが空文字 ("") または未設定であれば
// ok=false を返し、部分的にセットされた値を含む creds を返す
// (呼び出し側で破棄される想定だが、テスト容易性のため返却はする)。
//
// 「空文字明示」と「未設定」を区別しないのは plans/x-m09-cli-me.md D-3 の決定に従う。
func credentialsFromEnv() (*config.Credentials, bool) {
	c := &config.Credentials{
		APIKey:            os.Getenv("X_API_KEY"),
		APISecret:         os.Getenv("X_API_SECRET"),
		AccessToken:       os.Getenv("X_ACCESS_TOKEN"),
		AccessTokenSecret: os.Getenv("X_ACCESS_TOKEN_SECRET"),
	}
	return c, credentialsComplete(c)
}

// credentialsComplete は c の 4 フィールドすべてが非空であるかを判定する。
// nil の場合は false を返す。
func credentialsComplete(c *config.Credentials) bool {
	if c == nil {
		return false
	}
	return c.APIKey != "" && c.APISecret != "" && c.AccessToken != "" && c.AccessTokenSecret != ""
}
