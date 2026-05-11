# Plan: M24 — CLI `x mcp` サブコマンド + E2E

> Layer 2: マイルストーン詳細計画。
> 親ロードマップ: [plans/x-roadmap.md](./x-roadmap.md) §M24
> M23 (dynamodb store) ハンドオフ準拠。spec §6 (`x mcp` フラグ) / §11 (X_MCP_* / OIDC_* / STORE_BACKEND 全環境変数) 反映。

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M24: CLI `x mcp` サブコマンド + 3 モード E2E テスト |
| 親ロードマップ | plans/x-roadmap.md |
| ステータス | Approved / 実装フェーズ着手可 |
| 作成日 | 2026-05-12 |
| 想定コミット粒度 | 1 コミット (`feat(cli): x mcp サブコマンドと 3 モード E2E テストを追加 (v0.2.0 機能完成)`) |
| 前マイルストーン | M23 (dynamodb store, commit: 2ffa62c) |
| 後続マイルストーン | M25 (v0.2.0 README / CHANGELOG / タグ) |

## ゴール

- spec §6 の `x mcp` サブコマンドフラグ (`--host` / `--port` / `--path` / `--auth` / `--apikey-env`) と spec §11 の MCP モード環境変数 (`X_MCP_HOST` / `X_MCP_PORT` / `X_MCP_PATH` / `X_MCP_AUTH` / `X_MCP_API_KEY` / `OIDC_*` / `COOKIE_SECRET` / `EXTERNAL_URL` / `STORE_BACKEND` / `SQLITE_PATH` / `REDIS_URL` / `DYNAMODB_TABLE_NAME` / `AWS_REGION`) を全実装する。
- 認証情報は **環境変数のみ** から読み込み、credentials.toml / config.toml を一切読まない (spec §11 の不変条件)。
- 3 認証モード × 2 MCP tools = 6 シナリオを E2E テストで検証する (httptest + mark3labs/mcp-go client)。
- M15-M23 までの既存パッケージ (xapi / mcp / transport/http / authgate) を **無改修** で接続する (ファサード層追加のみ)。
- TDD: Red → Green → Refactor。signal.NotifyContext によるシグナルハンドリングは構築 / 接続のテストで検証する (実 SIGINT 発火テストは Go の制約から省略)。

## 非ゴール / 制約

- **idproxy E2E は memory store + cookie 偽装のみ**。Google OIDC との実通信は手動確認に委ねる (CI で実 OIDC を叩かない)。
- **graceful shutdown の SIGTERM 実発火テストは行わない**。Cobra RunE / signal.NotifyContext / http.Server.Shutdown は内部実装の信頼可能な部品であり、ctx cancel での graceful shutdown 経路は M15 (`TestRun_ContextCancel_GracefulShutdown`) で既に証明済み。本マイルストーンでは Cobra command 構築層のテストに留める。
- **OAuth 2.1 AS (Bearer JWT 検証) は対象外**。M19-M23 の認証は cookie session ベースのみで、機械実行向けは API Key モードで担う。
- **新規外部依存は追加しない**。spec §10 の dependency セット (cobra / mcp-go / oauth1 / idproxy / aws-sdk-go-v2 / go-redis / modernc.org/sqlite) のみで完結。

## 影響範囲

### 追加ファイル

| ファイル | 役割 |
|---|---|
| `internal/cli/mcp.go` | `newMcpCmd() *cobra.Command` + フラグ定義 + RunE (env defaulting / 認証情報ロード / authgate 構築 / mcp.NewServer / transport/http.NewServer / signal.NotifyContext + Run) |
| `internal/cli/mcp_auth.go` | `loadMCPCredentials() (*config.Credentials, error)` (env-only) + `buildIDProxyStore() (idproxy.Store, error)` (STORE_BACKEND 分岐 + SQLITE_PATH デフォルト) |
| `internal/cli/mcp_test.go` | Unit: フラグ解析 / env defaulting / `--auth` バリデーション / `loadMCPCredentials` / `buildIDProxyStore` (host/port/path/auth/apikey-env defaulting と SQLITE_PATH XDG_DATA_HOME default を網羅) |
| `internal/cli/mcp_e2e_test.go` | E2E: httptest で X API モック + `transport/http.Server.Run` 起動 + mark3labs/mcp-go client で 3 モード × 2 tools = 6 シナリオ + apikey 不正/欠落の負系統 2 シナリオ |

### 変更ファイル

| ファイル | 変更内容 |
|---|---|
| `internal/cli/root.go` | `NewRootCmd` 内 `AddCommand(newMcpCmd())` を 1 行追加 + 既存パッケージ doc コメントに `mcp` を追記 |

### 変更しないファイル

| ファイル | 理由 |
|---|---|
| `internal/mcp/server.go` | M15 で確立した `NewServer(client, version)` をそのまま利用 |
| `internal/transport/http/server.go` | M16 で確立した Option (`WithAddr` / `WithPath` / `WithHandlerMiddleware` / `WithShutdownTimeout`) をそのまま利用 |
| `internal/authgate/*.go` | M16-M23 で確立した `authgate.New(mode, opts...)` + `WithAPIKey` / `WithOIDC*` / `WithIDProxyStore` 等をそのまま利用 |
| `internal/xapi/client.go` | `xapi.NewClient(ctx, creds, opts...)` をそのまま利用 |
| `cmd/x/main.go` | 既存 exit code 写像をそのまま利用 (mcp サブコマンドのエラーも `ErrInvalidArgument` / `xapi.ErrAuthentication` で写像) |
| `internal/cli/auth_loader.go` | CLI モード専用 (env + file) のため流用しない。MCP は env-only の別関数 `loadMCPCredentials` を持つ (spec §11) |

## 設計詳細

### CLI フラグ定義 (cobra)

```go
// internal/cli/mcp.go (抜粋イメージ)

var (
    host       string
    port       int
    path       string
    authMode   string
    apikeyEnv  string
)

cmd := &cobra.Command{
    Use:   "mcp",
    Short: "start the X MCP server (Streamable HTTP)",
    Long:  "...",
    RunE:  runMcp,
}

cmd.Flags().StringVar(&host, "host", envOrDefault("X_MCP_HOST", "127.0.0.1"),
    "bind host (env: X_MCP_HOST)")
cmd.Flags().IntVar(&port, "port", envIntOrDefault("X_MCP_PORT", 8080),
    "bind port (env: X_MCP_PORT)")
cmd.Flags().StringVar(&path, "path", envOrDefault("X_MCP_PATH", "/mcp"),
    "MCP endpoint path (env: X_MCP_PATH)")
cmd.Flags().StringVar(&authMode, "auth", envOrDefault("X_MCP_AUTH", "idproxy"),
    "auth mode: none|apikey|idproxy (env: X_MCP_AUTH)")
cmd.Flags().StringVar(&apikeyEnv, "apikey-env", "X_MCP_API_KEY",
    "env var name holding the shared secret (apikey mode only)")
```

> **重要**: `--apikey-env` の **デフォルト値は env 変数名のリテラル `X_MCP_API_KEY`** であり、env 連動は行わない。`X_MCP_API_KEY` env はあくまで「共有シークレットの値」を保持する場所であって「flag を上書きするための名前指定 env」ではない (spec §6 / §11 と整合)。env 連動させると `X_MCP_API_KEY=hunter2` を設定したときに flag 値が `"hunter2"` になり、`os.Getenv("hunter2")` で空文字を取得して常に 401 になる、というバグになる (advisor 指摘 #1)。

#### env defaulting 仕様

| flag | 値の決定順序 |
|---|---|
| `--host` | CLI flag > `X_MCP_HOST` > `127.0.0.1` |
| `--port` | CLI flag > `X_MCP_PORT` (atoi) > `8080` |
| `--path` | CLI flag > `X_MCP_PATH` > `/mcp` |
| `--auth` | CLI flag > `X_MCP_AUTH` > `idproxy` |
| `--apikey-env` | CLI flag > **リテラル** `X_MCP_API_KEY` (env 連動なし、D-1 参照) |

**cobra のフラグデフォルト値設定**は cmd 生成時の 1 回限りで決まるため、env を flag default に直接反映するヘルパ (`envOrDefault`) を `RunE` 経由ではなく `newMcpCmd()` 内のローカル変数で計算する。これによりテスト時は `os.Setenv` → `newMcpCmd()` の順序で env defaulting を検証できる。

### authMode 値検証

`runMcp` 冒頭で `--auth` 値を `none` / `apikey` / `idproxy` のいずれかに正規化する (小文字化 + trim)。それ以外は `ErrInvalidArgument` で wrap して exit 2 に写像する。

```go
mode := strings.ToLower(strings.TrimSpace(authMode))
switch authgate.Mode(mode) {
case authgate.ModeNone, authgate.ModeAPIKey, authgate.ModeIDProxy:
    // ok
default:
    return fmt.Errorf("%w: --auth must be one of none|apikey|idproxy, got %q", ErrInvalidArgument, authMode)
}
```

### `loadMCPCredentials() (*config.Credentials, error)`

```go
// internal/cli/mcp_auth.go

// loadMCPCredentials は MCP モード専用に env-only で X API 認証情報をロードする。
// CLI モードと違い credentials.toml は読まない (spec §11 不変条件)。
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
```

`ErrCredentialsMissing` (M9 で auth_loader.go に既存) を再利用する。これは `xapi.ErrAuthentication` を Unwrap で内包するため、cmd/x/main.go の既存 switch で exit 3 に写像される。

### `buildIDProxyStore() (idproxy.Store, error)`

```go
// internal/cli/mcp_auth.go

// buildIDProxyStore は spec §11 の STORE_BACKEND に応じて idproxy.Store を生成する。
// 各 STORE_BACKEND に対応する env からパラメータを取り、authgate の factory 関数に委譲する。
func buildIDProxyStore() (idproxy.Store, error) {
    backend := strings.ToLower(strings.TrimSpace(os.Getenv("STORE_BACKEND")))
    switch backend {
    case "", "memory":
        return authgate.NewMemoryStore(), nil
    case "sqlite":
        path := os.Getenv("SQLITE_PATH")
        if path == "" {
            // XDG_DATA_HOME default: ${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db
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
        return nil, fmt.Errorf("%w: STORE_BACKEND must be one of memory|sqlite|redis|dynamodb, got %q",
            ErrInvalidArgument, backend)
    }
}
```

- `config.DataDir()` は M3 で実装済み。XDG_DATA_HOME を解決して `/x` サブディレクトリ込みで返す (確認済み: `~/.local/share/x` を返す)。よって `filepath.Join(dataDir, "idproxy.db")` で OK。
- `STORE_BACKEND` が空文字 (env 未設定) の場合は spec §11 の default (`memory`) として扱う。

### `runMcp` 統合フロー

```go
func runMcp(cmd *cobra.Command, _ []string) error {
    // 1. authMode 値検証 (上述)
    // 2. env-only credentials ロード
    creds, err := loadMCPCredentials()
    if err != nil { return err }

    // 3. xapi.Client 生成
    ctx := cmd.Context()
    xapiClient := xapi.NewClient(ctx, creds)

    // 4. authgate.Middleware 構築 (モード別 opts)
    mw, err := buildAuthMiddleware(authgate.Mode(mode), apikeyEnv)
    if err != nil { return err }

    // 5. MCP server 構築
    mcpSrv := mcpinternal.NewServer(xapiClient, version.String())

    // 6. HTTP server 構築
    addr := fmt.Sprintf("%s:%d", host, port)
    httpSrv := transporthttp.NewServer(mcpSrv,
        transporthttp.WithAddr(addr),
        transporthttp.WithPath(path),
        transporthttp.WithHandlerMiddleware(mw.Wrap),
    )

    // 7. signal context 統合 (Cobra から渡される ctx + SIGINT/SIGTERM)
    runCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
    defer stop()

    // 8. 起動 + Run はブロッキング + graceful shutdown
    _, _ = fmt.Fprintf(cmd.ErrOrStderr(),
        "starting x mcp server on http://%s%s (auth=%s)\n", addr, path, mode)
    return httpSrv.Run(runCtx)
}
```

### `buildAuthMiddleware`

```go
func buildAuthMiddleware(mode authgate.Mode, apikeyEnv string) (authgate.Middleware, error) {
    switch mode {
    case authgate.ModeNone:
        return authgate.New(mode)
    case authgate.ModeAPIKey:
        return authgate.New(mode, authgate.WithAPIKey(os.Getenv(apikeyEnv)))
    case authgate.ModeIDProxy:
        store, err := buildIDProxyStore()
        if err != nil { return nil, err }
        return authgate.New(mode,
            authgate.WithOIDCIssuer(os.Getenv("OIDC_ISSUER")),
            authgate.WithOIDCClientID(os.Getenv("OIDC_CLIENT_ID")),
            authgate.WithOIDCClientSecret(os.Getenv("OIDC_CLIENT_SECRET")),
            authgate.WithCookieSecret(os.Getenv("COOKIE_SECRET")),
            authgate.WithExternalURL(os.Getenv("EXTERNAL_URL")),
            authgate.WithIDProxyStore(store),
        )
    default:
        return nil, fmt.Errorf("%w: unsupported auth mode %q", ErrInvalidArgument, mode)
    }
}
```

### root.go の更新

```go
// internal/cli/root.go
root.AddCommand(newMcpCmd())
```

パッケージ doc の `// MCP サーバーモードは `x mcp` で起動する。` を 1 行追加する程度。

## テスト戦略

### TDD: Red → Green → Refactor

#### Phase 1: Red — unit (mcp_test.go)

1. **`TestNewMcpCmd_Defaults`**: env 未設定で newMcpCmd() を生成し、`--host` default = `127.0.0.1` / `--port` default = `8080` / `--path` default = `/mcp` / `--auth` default = `idproxy` / `--apikey-env` default = `X_MCP_API_KEY` を検証
2. **`TestNewMcpCmd_EnvDefaulting`**: `t.Setenv("X_MCP_HOST", "0.0.0.0")` / `X_MCP_PORT=9090` / `X_MCP_PATH=/foo` / `X_MCP_AUTH=apikey` を設定して default 値が env を反映することを検証 (**注: `t.Setenv` は必ず `newMcpCmd()` 呼び出しの前に行うこと** — D-8 参照、advisor non-blocker #1)
3. **`TestNewMcpCmd_AuthModeValidation`**: `--auth invalid` で `ErrInvalidArgument` (exit 2 ターゲット) を返すことを RunE で検証 (`cmd.SetArgs` + `cmd.ExecuteContext`)
4. **`TestLoadMCPCredentials_MissingEnv`**: 4 env のうち 1 つでも欠ければ `ErrCredentialsMissing` を返す (`errors.Is(err, xapi.ErrAuthentication)` も真であること)
5. **`TestLoadMCPCredentials_AllEnvSet`**: 4 env を `t.Setenv` で揃えると non-nil の `*config.Credentials` が返る
6. **`TestLoadMCPCredentials_IgnoresFile`**: credentials.toml が存在する状態 (M9 と同等の setup) でも env が無ければ `ErrCredentialsMissing` (= file を読まない不変条件) を返す
7. **`TestBuildIDProxyStore_DefaultMemory`**: `STORE_BACKEND` 未設定で `authgate.NewMemoryStore()` 同等の値が返る (`idproxy.Store` interface 検証 + nil でない)
8. **`TestBuildIDProxyStore_SQLitePathDefault`**: `STORE_BACKEND=sqlite` + `SQLITE_PATH` 未設定 + `XDG_DATA_HOME=<tmp>` で `<tmp>/x/idproxy.db` 配下が生成されること
9. **`TestBuildIDProxyStore_SQLiteExplicitPath`**: `STORE_BACKEND=sqlite` + `SQLITE_PATH=<tmp>/custom.db` で指定パスに生成されること
10. **`TestBuildIDProxyStore_RedisURLRequired`**: `STORE_BACKEND=redis` + `REDIS_URL` 未設定 → `errors.Is(err, authgate.ErrRedisURLRequired)`
11. **`TestBuildIDProxyStore_DynamoDBTableRequired`**: `STORE_BACKEND=dynamodb` + `DYNAMODB_TABLE_NAME` 未設定 → `errors.Is(err, authgate.ErrDynamoDBTableRequired)`
12. **`TestBuildIDProxyStore_UnknownBackend`**: `STORE_BACKEND=foo` → `errors.Is(err, ErrInvalidArgument)`

#### Phase 2: Green — minimal 実装

上記テストを通す最小限の `mcp.go` / `mcp_auth.go` を実装する。

#### Phase 3: E2E (mcp_e2e_test.go)

6 シナリオ (3 モード × 2 tools) + 2 負系統。各シナリオは:

1. httptest.Server で X API モックを起動 (`/2/users/me`, `/2/users/:id/liked_tweets`)
2. xapi.NewClient(ctx, creds, WithBaseURL(mockURL)) を構築するため、テスト内では `mcp.go` 経由ではなく `mcp.NewServer(client, version)` を直接呼ぶ (環境変数注入だけで MCP モード起動するのは難しいため、内部関数を `MCPServerForTest(...)` 等で export することは避け、テストは E2E の **接続層** までを検証する)
3. `transporthttp.NewServer(mcpSrv, WithAddr(freePort), WithPath("/mcp"), WithHandlerMiddleware(mw.Wrap))` を起動
4. `client.NewStreamableHttpClient(serverURL, transport.WithHTTPHeaders({"Authorization": "Bearer ..."}))` で接続
5. `client.Initialize` → `client.CallTool(name=get_user_me|get_liked_tweets, args={})` → 結果検証

シナリオ一覧:

| # | mode | tool | 期待 |
|---|---|---|---|
| 1 | none | get_user_me | success, IsError=false, user_id 非空 |
| 2 | none | get_liked_tweets | success, IsError=false, data 配列 |
| 3 | apikey (正しい Bearer) | get_user_me | success |
| 4 | apikey (正しい Bearer) | get_liked_tweets | success |
| 5 | apikey (不正 Bearer) | get_user_me | client.Initialize 段階で 401 (`Start` または `Initialize` がエラー) |
| 6 | apikey (Bearer 欠落) | get_user_me | 同上で 401 |
| 7 | idproxy (memory store) | get_user_me | 401 (cookie 未提示) — idproxy が未認証リクエストを reject する基本動作を確認 |
| 8 | idproxy (memory store) | get_liked_tweets | 同上 — 認証 OK パスは Google OIDC 実通信が必要なため CI ではここで線引き |

**設計判断**: idproxy の認証成功パスは Google OIDC との実通信が必要なため、E2E テストでは「未認証時に reject される」までを契約として pin する。これにより 3 モード切替の middleware 注入が正しく機能していることを担保する。

### 共通ヘルパ

```go
// freePort: M15 流用
// startMockXAPI: httptest.Server with /2/users/me + /2/users/:id/liked_tweets
// startMCPServer(t, mw, mockXAPIURL) -> (serverURL, cleanup func())
```

### 失敗系のリスク

- mark3labs/mcp-go client の `Initialize` が 401 を非エラーで返すケースがあれば、HTTP raw レイヤで POST して `resp.StatusCode == 401` を直接検証する。
- idproxy + memory store の `idproxy.New` は内部で OIDC discovery (`/.well-known/openid-configuration`) を叩く。これは `github.com/youyo/idproxy/testutil` の `NewMockIdP(t)` で httptest ベースの mock IdP を立てれば完全に解決できる (M20 で利用実績あり、idproxy_test.go:244 参照)。本マイルストーンの mcp_e2e_test.go でも同じ `testutil.NewMockIdP(t)` を利用する。これにより idproxy E2E は CI で完全閉じた状態で動作する。

## 検証手順

### 自動テスト

```bash
go test -race -count=1 ./...
golangci-lint run ./...
go vet ./...
go build -o /tmp/x ./cmd/x
```

すべて 0 issues / 全 pass を必須とする。

### 手動確認

```bash
# auth=none で起動 → curl で initialize
X_API_KEY=dummy X_API_SECRET=dummy \
X_ACCESS_TOKEN=dummy X_ACCESS_TOKEN_SECRET=dummy \
  /tmp/x mcp --auth none --port 18080 &
MCP_PID=$!

# initialize リクエスト送信
curl -s -X POST http://127.0.0.1:18080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"clientInfo":{"name":"manual","version":"1.0.0"}}}'

# /healthz 確認
curl -s http://127.0.0.1:18080/healthz

kill $MCP_PID
```

### 環境変数 defaulting 確認

```bash
X_MCP_HOST=0.0.0.0 X_MCP_PORT=19090 X_MCP_AUTH=apikey X_MCP_API_KEY=test-key \
X_API_KEY=dummy X_API_SECRET=dummy \
X_ACCESS_TOKEN=dummy X_ACCESS_TOKEN_SECRET=dummy \
  /tmp/x mcp --help  # default 値が env を反映していることを確認
```

## リスクと対策

| リスク | 対策 |
|---|---|
| `signal.NotifyContext` の SIGTERM テストが Go の制約で書きづらい | ctx cancel 経路で graceful shutdown を pin する (M15 既存テストで証明済み)。本マイルストーンは Cobra command 構築 + flag parsing + helper 関数の unit テストに集中 |
| ~~idproxy memory store の OIDC discovery が CI 実行時に外部依存になる~~ | 解決済み: `github.com/youyo/idproxy/testutil.NewMockIdP(t)` で httptest mock IdP を立てる (M20 利用実績、idproxy_test.go:244)。CI 完全閉じ |
| ~~`config.DataDir()` の返り値仕様~~ | 解決済み: `DataDir()` は `<XDG_DATA_HOME>/x` を返す (xdg.go:36-38)。`filepath.Join(dataDir, "idproxy.db")` で十分 |
| cobra flag default を env で決める実装が pflag の挙動と不整合 | flag default 値を `newMcpCmd()` 内のローカル変数で計算 (env を読む) するシンプル実装に統一。実行時の RunE 内で再度 env を読まない |
| MCP モード固有の `loadMCPCredentials` が CLI モードの `LoadCredentialsFromEnvOrFile` と紛らわしい | doc コメントで「MCP は env-only」を明示。関数名 prefix `loadMCP*` で識別性を確保 |
| host:port の組み立てが `%s:%d` で IPv6 を壊す可能性 | spec §6 の例 (`127.0.0.1` / `0.0.0.0`) が IPv4 のみで、IPv6 サポートは v0.3.0 以降の課題。本 M24 では IPv4 前提 + コメントで明示 |

## 決定事項

- **D-1**: `--apikey-env` のデフォルト値は **リテラル文字列 `X_MCP_API_KEY`** (env 連動なし)。`X_MCP_API_KEY` は共有シークレットの **値** を保持する env であり、flag デフォルトを env 上書きする用途ではない (spec §6 と整合、advisor 指摘 #1)。
- **D-2**: MCP モードでは `credentials.toml` を一切読まない。spec §11 の不変条件を `loadMCPCredentials` で pin する (テストで file 存在下の env 欠落 → `ErrCredentialsMissing` を検証)。
- **D-3**: `STORE_BACKEND` 値の正規化は小文字化 + trim。spec §11 の `memory` / `sqlite` / `redis` / `dynamodb` のみ受理。空文字は default `memory`。
- **D-4**: `SQLITE_PATH` のデフォルトは `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db`。`config.DataDir()` の仕様に従い、必要なら `/x` サブディレクトリを join する。
- **D-5**: idproxy E2E テストは memory store + 未認証 reject までを契約とし、OAuth callback 実通信は CI で叩かない (Google OIDC は手動確認)。
- **D-6**: signal context は `signal.NotifyContext(ctx, SIGINT, SIGTERM)` 1 行で十分。defer stop() を必ず呼ぶ。実 SIGTERM 発火テストは省略 (M15 の ctx cancel テストで証明済み)。
- **D-7**: `runMcp` 内のステータス出力は stderr (`cmd.ErrOrStderr()`) に書く。stdout は MCP の HTTP レスポンスと混ざらない (HTTP server なので別 channel) が、ログ用途は stderr が慣例。
- **D-8**: cobra flag の env 反映は `newMcpCmd()` 内のローカル変数で計算する。RunE 内で os.Getenv を直接読まないことで、テスト時の env 注入 → flag default 反映が直線的に動く。
- **D-9**: `runMcp` のエラー写像は cmd/x/main.go の既存 switch に乗せる:
  - `ErrInvalidArgument` (`--auth` / `STORE_BACKEND` 値不正) → exit 2
  - `xapi.ErrAuthentication` 包む `ErrCredentialsMissing` → exit 3
  - その他 → exit 1
- **D-10**: コミットメッセージは `feat(cli): x mcp サブコマンドと 3 モード E2E テストを追加 (v0.2.0 機能完成)` + フッターに `Plan: plans/x-m24-cli-mcp-e2e.md`。

## ハンドオフ (M25 への引き継ぎ予定情報)

- v0.2.0 リリース時の CHANGELOG 追記内容:
  - Added: `x mcp` サブコマンド (3 モード認証: none / apikey / idproxy)
  - Added: MCP tools (get_user_me / get_liked_tweets) を Streamable HTTP で提供
  - Added: idproxy 4 store backend (memory / sqlite / redis / dynamodb) 全サポート
- README MCP セクションは spec §11 の起動例 3 つ (none / apikey / idproxy+dynamodb) をベースに整形
- v0.2.0 タグは M14 (release.yml) の workflow が走ることで GitHub Releases へ自動配布される

## TDD チェックリスト

- [ ] Red: mcp_test.go (12 unit) + mcp_e2e_test.go (8 シナリオ) を先に書いて failing 確認
- [ ] Green: mcp.go + mcp_auth.go の最小実装で全 pass
- [ ] Refactor: 共通ヘルパ抽出 / 命名整理 / doc コメント整備
- [ ] go test -race -count=1 ./... 全 pass
- [ ] golangci-lint run ./... 0 issues
- [ ] go vet ./... clean
- [ ] go build -o /tmp/x ./cmd/x 成功
- [ ] 手動: `/tmp/x mcp --auth none --port 18080` 起動 + `curl` で initialize 200
- [ ] 手動: `/healthz` 200 + `ok\n`
- [ ] commit: 単一コミット + Plan フッター
