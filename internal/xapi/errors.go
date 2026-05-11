package xapi

import (
	"errors"
	"fmt"
	"net/http"
)

// 番兵エラー群: X API のエラー応答カテゴリを表現する。
//
// errors.Is で照合し、具体的な HTTP レスポンス本体・ヘッダが必要な場合は
// APIError を errors.As で取り出す。
//
// マッピング根拠は docs/specs/x-spec.md §6 エラーハンドリングポリシー。
var (
	// ErrAuthentication は X API が 401 を返したことを示す。
	// CLI 層では internal/app.ExitAuthError (3) に写像される。
	ErrAuthentication = errors.New("xapi: authentication failed (401)")
	// ErrPermission は X API が 403 を返したことを示す。
	// CLI 層では internal/app.ExitPermissionError (4) に写像される。
	ErrPermission = errors.New("xapi: permission denied (403)")
	// ErrNotFound は X API が 404 を返したことを示す。
	// CLI 層では internal/app.ExitNotFoundError (5) に写像される。
	ErrNotFound = errors.New("xapi: not found (404)")
	// ErrRateLimit は 429 によるリトライが最大回数に達して枯渇したことを示す。
	// CLI 層では internal/app.ExitGenericError (1) に写像される
	// (§6 の exit code 表に 429 は明示されないため generic 扱い)。
	ErrRateLimit = errors.New("xapi: rate limit exhausted after retries")
)

// APIError は X API から返却された HTTP エラーレスポンスを構造化したエラーである。
//
// 用法:
//   - errors.Is(err, ErrAuthentication) などで番兵カテゴリを判定する
//   - errors.As(err, &apiErr) でレスポンス本体 (Body / Header / StatusCode) を取得する
type APIError struct {
	// StatusCode は X API から返された HTTP ステータスコードである。
	StatusCode int
	// Body は X API から返されたレスポンス本体の生バイト列である (truncate なし)。
	Body []byte
	// Header は X API から返されたレスポンスヘッダのコピーである。
	Header http.Header

	// sentinel は 401/403/404/429 のいずれかに該当した場合に対応する番兵エラーを保持し、
	// errors.Is でのカテゴリ判定に用いる。それ以外の 4xx/5xx では nil。
	sentinel error
}

// Error は人間可読なエラー文字列を返す (Body は 200 バイトまでで打ち切る)。
func (e *APIError) Error() string {
	if e == nil {
		return "<nil APIError>"
	}
	return fmt.Sprintf("xapi: HTTP %d: %s", e.StatusCode, truncate(e.Body, 200))
}

// Unwrap は errors.Is が番兵照合できるように内包する番兵エラーを返す。
// 番兵が無い (sentinel == nil) 場合は nil を返す。
func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.sentinel
}

// ExitCodeFor は err を CLI の終了コード (internal/app の定数値) に写像する。
//
// マッピング (internal/app/exit.go の定数と同期させること):
//
//	nil               → 0 (ExitSuccess)
//	ErrAuthentication → 3 (ExitAuthError)
//	ErrPermission     → 4 (ExitPermissionError)
//	ErrNotFound       → 5 (ExitNotFoundError)
//	その他            → 1 (ExitGenericError)
//
// 循環依存回避のため internal/app は import せず、数値リテラルで定義する。
// internal/app/exit.go の値を変更する場合は本関数も同時に更新すること。
func ExitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	switch {
	case errors.Is(err, ErrAuthentication):
		return 3
	case errors.Is(err, ErrPermission):
		return 4
	case errors.Is(err, ErrNotFound):
		return 5
	default:
		return 1
	}
}

// truncate は b を最大 maxLen バイトに切り詰めて文字列化する。
// 超過時は末尾に "...(truncated)" を付与する。
func truncate(b []byte, maxLen int) string {
	if len(b) <= maxLen {
		return string(b)
	}
	return string(b[:maxLen]) + "...(truncated)"
}
