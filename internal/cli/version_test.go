// Package cli は version サブコマンドのテストを提供する。
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestVersionDefaultJSON は `x version` (default) が JSON 出力することを検証する。
func TestVersionDefaultJSON(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var m map[string]string
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %q", err, buf.String())
	}
	for _, key := range []string{"version", "commit", "date"} {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON missing key %q, output: %s", key, buf.String())
		}
	}
}

// TestVersionDefaultValues は JSON 出力にデフォルト値が含まれることを検証する。
func TestVersionDefaultValues(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var m map[string]string
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if m["version"] != "dev" {
		t.Errorf("version field = %q, want %q", m["version"], "dev")
	}
}

// TestVersionNoJSON は `x version --no-json` が human-readable 文字列を出すことを検証する。
func TestVersionNoJSON(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version", "--no-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	out := buf.String()
	// JSON ではないこと
	var any map[string]any
	if err := json.Unmarshal(buf.Bytes(), &any); err == nil {
		t.Errorf("expected non-JSON output, got JSON: %s", out)
	}
	// 期待する human-readable 要素
	for _, want := range []string{"dev", "commit:", "built:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q, got: %q", want, out)
		}
	}
}

// TestVersionHelpShowsNoJSON は `x version --help` に --no-json フラグが表示されることを検証する。
func TestVersionHelpShowsNoJSON(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "--no-json") {
		t.Errorf("help output missing --no-json flag, got: %s", out)
	}
}
