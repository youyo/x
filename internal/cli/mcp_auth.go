package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/youyo/idproxy"

	"github.com/youyo/x/internal/authgate"
	"github.com/youyo/x/internal/config"
)

// loadMCPCredentials は MCP モード専用に環境変数のみから X API 認証情報を読み込む。
//
// spec §11 の不変条件 (MCP モードでは config.toml / credentials.toml を一切読まない) を pin する
// ための関数である。CLI モードの [LoadCredentialsFromEnvOrFile] と紛らわしいため
// 関数名 prefix `loadMCP*` で識別性を担保している。
//
// 4 つの env (X_API_KEY / X_API_SECRET / X_ACCESS_TOKEN / X_ACCESS_TOKEN_SECRET) のうち
// 1 つでも欠ければ [ErrCredentialsMissing] を返す。`ErrCredentialsMissing` は
// [xapi.ErrAuthentication] を Unwrap で内包しており、cmd/x/main.go の run() で exit code 3 に
// 写像される (M9 と同じ流儀)。
func loadMCPCredentials() (*config.Credentials, error) {
	c := &config.Credentials{
		APIKey:            os.Getenv("X_API_KEY"),
		APISecret:         os.Getenv("X_API_SECRET"),
		AccessToken:       os.Getenv("X_ACCESS_TOKEN"),
		AccessTokenSecret: os.Getenv("X_ACCESS_TOKEN_SECRET"),
	}
	if c.APIKey == "" || c.APISecret == "" || c.AccessToken == "" || c.AccessTokenSecret == "" {
		return nil, ErrCredentialsMissing
	}
	return c, nil
}

// buildIDProxyStore は spec §11 の STORE_BACKEND 環境変数に応じて idproxy.Store を生成する。
//
// STORE_BACKEND の値 (小文字化 + trim 後) に応じて authgate の factory 関数を呼び分ける:
//
//   - "" / "memory" → [authgate.NewMemoryStore] (デフォルト)
//   - "sqlite"     → [authgate.NewSQLiteStore] (SQLITE_PATH または XDG_DATA_HOME 既定)
//   - "redis"      → [authgate.NewRedisStore] (REDIS_URL)
//   - "dynamodb"   → [authgate.NewDynamoDBStore] (DYNAMODB_TABLE_NAME, AWS_REGION)
//   - その他       → [ErrInvalidArgument] でラップしたエラー (exit 2)
//
// SQLITE_PATH 未指定時は spec §11 の既定値 `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db` を
// [config.DataDir] 経由で解決する。authgate.NewSQLiteStore が親ディレクトリを自動作成する
// ため、ここで mkdir は行わない。
func buildIDProxyStore() (idproxy.Store, error) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("STORE_BACKEND")))
	switch backend {
	case "", "memory":
		return authgate.NewMemoryStore(), nil
	case "sqlite":
		path := os.Getenv("SQLITE_PATH")
		if path == "" {
			dataDir, err := config.DataDir()
			if err != nil {
				return nil, fmt.Errorf("resolve SQLITE_PATH default: %w", err)
			}
			path = filepath.Join(dataDir, "idproxy.db")
		}
		return authgate.NewSQLiteStore(path)
	case "redis":
		return authgate.NewRedisStore(os.Getenv("REDIS_URL"))
	case "dynamodb":
		return authgate.NewDynamoDBStore(os.Getenv("DYNAMODB_TABLE_NAME"), os.Getenv("AWS_REGION"))
	default:
		return nil, fmt.Errorf(
			"%w: STORE_BACKEND must be one of memory|sqlite|redis|dynamodb, got %q",
			ErrInvalidArgument, backend,
		)
	}
}
