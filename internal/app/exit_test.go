// Package app は exit code 定数の table-driven テストを提供する。
package app

import "testing"

// TestExitCodeValues は 6 個の exit code 定数が仕様通りの値を持つことを検証する。
// 仕様 (docs/specs/x-spec.md §6 エラーハンドリングポリシー):
//   - 0 = success
//   - 1 = generic error
//   - 2 = argument / validation error
//   - 3 = auth error (X API 401 / idproxy 401)
//   - 4 = permission error (X API 403)
//   - 5 = not found (X API 404)
func TestExitCodeValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  int
		want int
	}{
		{"ExitSuccess", ExitSuccess, 0},
		{"ExitGenericError", ExitGenericError, 1},
		{"ExitArgumentError", ExitArgumentError, 2},
		{"ExitAuthError", ExitAuthError, 3},
		{"ExitPermissionError", ExitPermissionError, 4},
		{"ExitNotFoundError", ExitNotFoundError, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.want)
			}
		})
	}
}
