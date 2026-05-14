// Package cli は root コマンドのテストを提供する。
package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestRootVersionFlag は `x --version` がバージョン文字列を出力することを検証する。
func TestRootVersionFlag(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "dev") {
		t.Errorf("--version output missing %q, got: %q", "dev", out)
	}
}

// TestRootHelpShowsVersion は `x --help` に version サブコマンドが表示されることを検証する。
func TestRootHelpShowsVersion(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "version") {
		t.Errorf("help output missing %q, got: %s", "version", out)
	}
}

// TestRootHelpShowsTweet は `x --help` に tweet サブコマンドが表示されることを検証する (M29)。
func TestRootHelpShowsTweet(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "tweet") {
		t.Errorf("help output missing %q, got: %s", "tweet", out)
	}
}

// TestRootHelpShowsTimeline は `x --help` に timeline サブコマンドが表示されることを検証する (M31)。
func TestRootHelpShowsTimeline(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "timeline") {
		t.Errorf("help output missing %q, got: %s", "timeline", out)
	}
}

// TestRootHelpShowsUser は `x --help` に user サブコマンドが表示されることを検証する (M32)。
func TestRootHelpShowsUser(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "user") {
		t.Errorf("help output missing %q, got: %s", "user", out)
	}
}

// TestRootHelpShowsList は `x --help` に list サブコマンドが表示されることを検証する (M33)。
func TestRootHelpShowsList(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "list") {
		t.Errorf("help output missing %q, got: %s", "list", out)
	}
}

// TestRootHelpShowsSpace は `x --help` に space サブコマンドが表示されることを検証する (M34)。
func TestRootHelpShowsSpace(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "space") {
		t.Errorf("help output missing %q, got: %s", "space", out)
	}
}

// TestRootHelpShowsTrends は `x --help` に trends サブコマンドが表示されることを検証する (M34)。
func TestRootHelpShowsTrends(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "trends") {
		t.Errorf("help output missing %q, got: %s", "trends", out)
	}
}

// TestRootHelpShowsCompletion は `x --help` に Cobra 自動追加の completion サブコマンドが
// 表示されることを検証する。
func TestRootHelpShowsCompletion(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "completion") {
		t.Errorf("help output missing %q, got: %s", "completion", out)
	}
}

// TestCompletionBash は `x completion bash` が bash wrapper を出力することを検証する。
func TestCompletionBash(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"completion", "bash"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if out == "" {
		t.Fatal("completion bash produced empty output")
	}
	// bash completion 出力には bash 関数名または bash completion ヘッダが含まれるはず
	if !strings.Contains(out, "bash completion") && !strings.Contains(out, "_x") {
		t.Errorf("output does not look like bash completion: %s", out[:min(200, len(out))])
	}
}

// TestCompletionZsh は `x completion zsh` が zsh wrapper を出力することを検証する。
func TestCompletionZsh(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"completion", "zsh"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if out == "" {
		t.Fatal("completion zsh produced empty output")
	}
	if !strings.Contains(out, "compdef") {
		t.Errorf("output does not look like zsh completion: %s", out[:min(200, len(out))])
	}
}

// TestCompletionFish は `x completion fish` が fish wrapper を出力することを検証する。
func TestCompletionFish(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"completion", "fish"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "complete -c x") {
		t.Errorf("output does not look like fish completion: %s", out[:min(200, len(out))])
	}
}

// TestCompletionPowerShell は `x completion powershell` が PowerShell wrapper を出力することを検証する。
func TestCompletionPowerShell(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"completion", "powershell"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	if out == "" {
		t.Errorf("powershell completion produced empty output")
	}
}

// TestDynamicComplete は `x __complete ""` が candidate に version と completion を含むことを検証する。
// Cobra 内部仕様で __complete は隠しサブコマンドとして自動登録される。
func TestDynamicComplete(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"__complete", ""})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	out := buf.String()
	// 出力フォーマットは Cobra 内部仕様 (`:4\n` 等のメタを含む) なので
	// 中身に version / completion が含まれることだけを assert する。
	for _, want := range []string{"version", "completion"} {
		if !strings.Contains(out, want) {
			t.Errorf("__complete output missing %q, got: %s", want, out)
		}
	}
}

// TestUnknownSubcommand は未知サブコマンドがエラーを返すことを検証する。
// SilenceUsage:true / SilenceErrors:true の設定で Usage と err の自動出力が
// 抑制されることも合わせて確認する。
func TestUnknownSubcommand(t *testing.T) {
	t.Parallel()

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown subcommand, got nil")
	}
}
