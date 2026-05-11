// Package version はバージョン情報のテストを提供する。
package version

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDefaultVersion は ldflags 未指定時のデフォルト値を検証する。
func TestDefaultVersion(t *testing.T) {
	t.Parallel()
	if Version != "dev" {
		t.Errorf("Version = %q, want %q", Version, "dev")
	}
}

// TestDefaultCommit は Commit 変数のデフォルト値を検証する。
func TestDefaultCommit(t *testing.T) {
	t.Parallel()
	if Commit != "none" {
		t.Errorf("Commit = %q, want %q", Commit, "none")
	}
}

// TestDefaultDate は Date 変数のデフォルト値を検証する。
func TestDefaultDate(t *testing.T) {
	t.Parallel()
	if Date != "unknown" {
		t.Errorf("Date = %q, want %q", Date, "unknown")
	}
}

// TestNewInfo は NewInfo() が現在のパッケージ変数を構造体に複製することを検証する。
func TestNewInfo(t *testing.T) {
	t.Parallel()
	info := NewInfo()
	if info.Version != Version {
		t.Errorf("info.Version = %q, want %q", info.Version, Version)
	}
	if info.Commit != Commit {
		t.Errorf("info.Commit = %q, want %q", info.Commit, Commit)
	}
	if info.Date != Date {
		t.Errorf("info.Date = %q, want %q", info.Date, Date)
	}
}

// TestString は String() が空でなく Version を含むことを検証する。
func TestString(t *testing.T) {
	t.Parallel()
	s := String()
	if s == "" {
		t.Fatal("String() returned empty")
	}
	if !strings.Contains(s, "dev") {
		t.Errorf("String() = %q, want to contain %q", s, "dev")
	}
}

// TestInfoJSON は Info 構造体が JSON シリアライズ可能で必須キーを含むことを検証する。
func TestInfoJSON(t *testing.T) {
	t.Parallel()
	info := NewInfo()
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	for _, key := range []string{"version", "commit", "date"} {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON missing key %q, got: %s", key, b)
		}
	}
}
