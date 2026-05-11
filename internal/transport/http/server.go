package http

import (
	"context"
	"errors"
	"fmt"
	stdhttp "net/http"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

// 既定値定数群。NewServer の Option で上書きされない場合に使われる。
const (
	// DefaultAddr は HTTP listen アドレスの既定値である。
	// ローカル開発向けのループバックバインドを採用する。
	DefaultAddr = "127.0.0.1:8080"
	// DefaultPath は MCP エンドポイントの既定パスである。
	DefaultPath = "/mcp"
	// DefaultShutdownTimeout は graceful shutdown の既定タイムアウトである。
	// in-flight リクエストを完了させるための猶予として 30s を採用する。
	DefaultShutdownTimeout = 30 * time.Second
	// readHeaderTimeout は Slowloris 攻撃対策 (gosec G114) として設定する
	// http.Server.ReadHeaderTimeout の既定値である。
	readHeaderTimeout = 10 * time.Second
)

// Server は MCP サーバーを HTTP 経由で提供する薄いラッパーである。
//
// ライフサイクル:
//  1. NewServer(mcp, opts...) で構築
//  2. Run(ctx) で listen 開始
//  3. ctx.Done() (cancel / 親シグナル) で graceful shutdown
//
// シグナル監視は呼び出し側で signal.NotifyContext を使って ctx に統合する。
type Server struct {
	mcp             *mcpserver.MCPServer
	addr            string
	path            string
	shutdownTimeout time.Duration
	handlerMW       func(stdhttp.Handler) stdhttp.Handler
}

// Option は Server の任意設定を表す関数オプションである。
type Option func(*Server)

// WithAddr は HTTP listen アドレスを上書きする。
//
// 例: WithAddr(":8080") や WithAddr("0.0.0.0:8080")。
// 既定値は DefaultAddr ("127.0.0.1:8080")。
func WithAddr(addr string) Option {
	return func(s *Server) {
		s.addr = addr
	}
}

// WithPath は MCP エンドポイントパスを上書きする。
//
// 既定値は DefaultPath ("/mcp")。
func WithPath(path string) Option {
	return func(s *Server) {
		s.path = path
	}
}

// WithHandlerMiddleware は MCP handler を任意の middleware でラップする Option である。
//
// 主な用途は authgate (none / apikey / idproxy) の挿し込みである。引数 mw が nil の
// 場合は passthrough (本 Option を渡さなかった場合と等価) として扱う。
//
// /healthz エンドポイントはこの middleware の影響を受けず、常に認証なしで応答する
// (LWA / Lambda 健全性確認用)。これは設計不変条件であり、テスト
// TestRun_Healthz_BypassesMiddleware で pin している。
//
// 引数のシグネチャは authgate.Middleware インターフェースの Wrap メソッドと整合する。
// 呼び出し側で `WithHandlerMiddleware(mw.Wrap)` のように渡すことを想定する。
// 本パッケージは authgate に依存しない (循環依存防止)。
func WithHandlerMiddleware(mw func(stdhttp.Handler) stdhttp.Handler) Option {
	return func(s *Server) {
		s.handlerMW = mw
	}
}

// WithShutdownTimeout は graceful shutdown の最大待機時間を上書きする。
//
// ctx.Done() 受信後、この時間内に in-flight リクエストが完了しなければ
// 強制的に http.Server.Shutdown が打ち切られる。
// 既定値は DefaultShutdownTimeout (30s)。
func WithShutdownTimeout(d time.Duration) Option {
	return func(s *Server) {
		s.shutdownTimeout = d
	}
}

// NewServer は mcp サーバーを HTTP transport でラップする Server を返す。
//
// Option 未指定時のデフォルトは DefaultAddr / DefaultPath / DefaultShutdownTimeout。
func NewServer(mcp *mcpserver.MCPServer, opts ...Option) *Server {
	s := &Server{
		mcp:             mcp,
		addr:            DefaultAddr,
		path:            DefaultPath,
		shutdownTimeout: DefaultShutdownTimeout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Addr は構成済みの listen アドレスを返す (テスト・観測用)。
func (s *Server) Addr() string { return s.addr }

// Path は構成済みの MCP エンドポイントパスを返す (テスト・観測用)。
func (s *Server) Path() string { return s.path }

// ShutdownTimeout は構成済みの graceful shutdown タイムアウトを返す (テスト・観測用)。
func (s *Server) ShutdownTimeout() time.Duration { return s.shutdownTimeout }

// Run は HTTP サーバーを起動し、ctx.Done() で graceful shutdown する。
//
// 戻り値:
//   - ctx 終了による正常停止: nil
//   - listen 失敗 (ポート競合等): 非 nil error
//   - shutdown 失敗: 非 nil error (timeout / handler のリーク等)
//
// 内部では mark3labs/mcp-go の Streamable HTTP handler を構築し、
// 指定された path にマウントする。
func (s *Server) Run(ctx context.Context) error {
	h := mcpserver.NewStreamableHTTPServer(s.mcp, mcpserver.WithEndpointPath(s.path))

	var handler stdhttp.Handler = h
	if s.handlerMW != nil {
		handler = s.handlerMW(handler)
	}

	mux := stdhttp.NewServeMux()
	mux.Handle(s.path, handler)
	// /healthz は middleware の外側に常時公開する (LWA 健全性確認用)。
	// 認証 middleware がどんな挙動でも /healthz は影響を受けない設計不変条件である。
	mux.HandleFunc("/healthz", healthzHandler)

	srv := &stdhttp.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			return fmt.Errorf("http server listen: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http server shutdown: %w", err)
		}
		// goroutine 側の ListenAndServe は Shutdown 後に http.ErrServerClosed で返るので drain
		<-errCh
		return nil
	}
}

// healthzHandler は LWA / Lambda の死活確認用エンドポイントである。
// 認証 middleware の外側に露出させ、常に 200 OK と body "ok\n" を返す。
func healthzHandler(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
	w.WriteHeader(stdhttp.StatusOK)
	_, _ = fmt.Fprintln(w, "ok")
}
