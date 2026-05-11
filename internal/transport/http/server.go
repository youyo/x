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

	mux := stdhttp.NewServeMux()
	mux.Handle(s.path, h)

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
