package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/youyo/x/internal/authgate"
	mcpinternal "github.com/youyo/x/internal/mcp"
	transporthttp "github.com/youyo/x/internal/transport/http"
	"github.com/youyo/x/internal/version"
	"github.com/youyo/x/internal/xapi"
)

// MCP モード関連環境変数 (spec §11) のキー定数。リテラル散在を避け、テストとの照合を容易にする。
const (
	envMCPHost = "X_MCP_HOST"
	envMCPPort = "X_MCP_PORT"
	envMCPPath = "X_MCP_PATH"
	envMCPAuth = "X_MCP_AUTH"

	defaultMCPHost      = "127.0.0.1"
	defaultMCPPort      = 8080
	defaultMCPPath      = "/mcp"
	defaultMCPAuth      = "idproxy"
	defaultAPIKeyEnvVar = "X_MCP_API_KEY"
)

// newMcpCmd は `x mcp` サブコマンドを生成する factory である (spec §6)。
//
// フラグデフォルト値は本関数の呼び出し時点で env (`X_MCP_*`) を反映するため、
// テスト時は `t.Setenv("X_MCP_*", ...)` を **必ず本 factory 呼び出しの前** に行う必要がある
// (plans/x-m24-cli-mcp-e2e.md D-8 / advisor non-blocker #1)。
//
// `--apikey-env` のデフォルト値はリテラル `X_MCP_API_KEY` 固定で、env 連動はしない
// (plans D-1 / advisor 指摘 #1)。`X_MCP_API_KEY` は共有シークレットの**値**を保持する env で
// あり、flag デフォルトを env 上書きする用途ではない。
//
// 起動フロー (RunE):
//  1. `--auth` 値検証 (none|apikey|idproxy)
//  2. [loadMCPCredentials] で X API 認証情報を env-only ロード (spec §11 不変条件)
//  3. [xapi.NewClient] で X API クライアント生成
//  4. [buildAuthMiddleware] で authgate.Middleware 構築 (モード別 env 読み込み)
//  5. [mcpinternal.NewServer] で MCP サーバー生成
//  6. [transporthttp.NewServer] で HTTP サーバー生成
//  7. [signal.NotifyContext] で SIGINT/SIGTERM を ctx に統合
//  8. [transporthttp.Server.Run] で listen 開始 → ctx 終了で graceful shutdown
func newMcpCmd() *cobra.Command {
	var (
		host      string
		port      int
		path      string
		authMode  string
		apikeyEnv string
	)

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "start the X MCP server (Streamable HTTP)",
		Long: "Start the X MCP server in Streamable HTTP mode.\n" +
			"Credentials are loaded from environment variables only (X_API_KEY/X_API_SECRET/X_ACCESS_TOKEN/X_ACCESS_TOKEN_SECRET); credentials.toml is NOT read in MCP mode.\n" +
			"Auth modes: none (no authentication, local dev only), apikey (Bearer token via X_MCP_API_KEY), idproxy (OIDC cookie session).\n" +
			"STORE_BACKEND (memory|sqlite|redis|dynamodb) selects the idproxy persistence layer.\n" +
			"Exit codes: 0 success, 1 generic, 2 argument error, 3 auth error.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMcp(cmd, host, port, path, authMode, apikeyEnv)
		},
	}

	cmd.Flags().StringVar(&host, "host", envOrDefaultString(envMCPHost, defaultMCPHost),
		"bind host (env: X_MCP_HOST)")
	cmd.Flags().IntVar(&port, "port", envOrDefaultInt(envMCPPort, defaultMCPPort),
		"bind port (env: X_MCP_PORT)")
	cmd.Flags().StringVar(&path, "path", envOrDefaultString(envMCPPath, defaultMCPPath),
		"MCP endpoint path (env: X_MCP_PATH)")
	cmd.Flags().StringVar(&authMode, "auth", envOrDefaultString(envMCPAuth, defaultMCPAuth),
		"auth mode: none|apikey|idproxy (env: X_MCP_AUTH)")
	cmd.Flags().StringVar(&apikeyEnv, "apikey-env", defaultAPIKeyEnvVar,
		"env var name holding the shared secret (apikey mode only); the value is read from this env var at startup")

	return cmd
}

// runMcp は newMcpCmd の RunE 本体である。
//
// host / port / path / authMode / apikeyEnv は flag で確定済みの値を受け取る。
// MCP モード起動の全責務 (認証情報ロード → middleware 構築 → MCP/HTTP server 起動 → signal handling)
// を一気に実行する。エラーは wrap せず呼び出し側 (cmd/x/main.go run()) に伝搬する。
func runMcp(cmd *cobra.Command, host string, port int, path, authMode, apikeyEnv string) error {
	// 1. authMode 値検証 (大文字小文字許容、trim)
	mode, err := normalizeAuthMode(authMode)
	if err != nil {
		return err
	}

	// 2. env-only credentials ロード (spec §11 不変条件: credentials.toml は読まない)
	creds, err := loadMCPCredentials()
	if err != nil {
		return err
	}

	// 3. xapi.Client 生成 (ctx は cobra から渡される)
	ctx := cmd.Context()
	xapiClient := xapi.NewClient(ctx, creds)

	// 4. authgate.Middleware 構築 (モード別 env 読み込み)
	mw, err := buildAuthMiddleware(mode, apikeyEnv)
	if err != nil {
		return err
	}

	// 5. MCP server 構築
	mcpSrv := mcpinternal.NewServer(xapiClient, version.String())

	// 6. HTTP server 構築
	addr := fmt.Sprintf("%s:%d", host, port)
	httpSrv := transporthttp.NewServer(
		mcpSrv,
		transporthttp.WithAddr(addr),
		transporthttp.WithPath(path),
		transporthttp.WithHandlerMiddleware(mw.Wrap),
	)

	// 7. signal context 統合
	runCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 8. 起動メッセージを stderr に書いて Run (ブロッキング、ctx 終了で graceful shutdown)
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
		"starting x mcp server on http://%s%s (auth=%s)\n", addr, path, mode)
	return httpSrv.Run(runCtx)
}

// normalizeAuthMode は --auth フラグ値を正規化して [authgate.Mode] にマップする。
//
// 大文字小文字は無視し、前後空白を trim する。`none` / `apikey` / `idproxy` 以外は
// [ErrInvalidArgument] でラップしたエラーを返し、cmd/x/main.go の run() で exit 2 に写像される。
func normalizeAuthMode(s string) (authgate.Mode, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	switch authgate.Mode(v) {
	case authgate.ModeNone, authgate.ModeAPIKey, authgate.ModeIDProxy:
		return authgate.Mode(v), nil
	default:
		return "", fmt.Errorf(
			"%w: --auth must be one of none|apikey|idproxy, got %q",
			ErrInvalidArgument, s,
		)
	}
}

// buildAuthMiddleware は authgate.Mode に応じて Middleware を構築する。
//
// 各モードで必要な env (apikey の値、idproxy の OIDC_* / COOKIE_SECRET / EXTERNAL_URL /
// STORE_BACKEND 系) を読み込み、[authgate.New] の Option として渡す。
//
//   - none:    引数なし
//   - apikey:  WithAPIKey(os.Getenv(apikeyEnv)) — apikeyEnv は --apikey-env で指定された env 名
//   - idproxy: OIDC_ISSUER / OIDC_CLIENT_ID / OIDC_CLIENT_SECRET / COOKIE_SECRET / EXTERNAL_URL
//   - STORE_BACKEND に応じた idproxy.Store ([buildIDProxyStore])
func buildAuthMiddleware(mode authgate.Mode, apikeyEnv string) (authgate.Middleware, error) {
	switch mode {
	case authgate.ModeNone:
		return authgate.New(mode)
	case authgate.ModeAPIKey:
		return authgate.New(mode, authgate.WithAPIKey(os.Getenv(apikeyEnv)))
	case authgate.ModeIDProxy:
		store, err := buildIDProxyStore()
		if err != nil {
			return nil, err
		}
		return authgate.New(
			mode,
			authgate.WithOIDCIssuer(os.Getenv("OIDC_ISSUER")),
			authgate.WithOIDCClientID(os.Getenv("OIDC_CLIENT_ID")),
			authgate.WithOIDCClientSecret(os.Getenv("OIDC_CLIENT_SECRET")),
			authgate.WithCookieSecret(os.Getenv("COOKIE_SECRET")),
			authgate.WithExternalURL(os.Getenv("EXTERNAL_URL")),
			authgate.WithIDProxyStore(store),
		)
	default:
		// normalizeAuthMode を通っているため到達不能だが防御的に保護する。
		return nil, fmt.Errorf("%w: unsupported auth mode %q", ErrInvalidArgument, mode)
	}
}

// envOrDefaultString は env が非空ならその値、空なら fallback を返す。
// 空文字 ("") を明示設定したケースは「未設定」と同じ扱い (M9 の credentialsFromEnv と同方針)。
func envOrDefaultString(envName, fallback string) string {
	if v := os.Getenv(envName); v != "" {
		return v
	}
	return fallback
}

// envOrDefaultInt は env が非空かつ整数として解釈可能ならその値、それ以外は fallback を返す。
//
// パース失敗時は警告を出さず fallback にフォールバックする (cobra のフラグ default 計算時には
// stderr を使わない方針)。RunE 実行時の host:port 組み立てで明示的にエラーになるため、
// 安全側の挙動として silent fallback を採用する。
func envOrDefaultInt(envName string, fallback int) int {
	if v := os.Getenv(envName); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
