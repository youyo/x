package authgate_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/youyo/idproxy"
	"github.com/youyo/x/internal/authgate"
)

// TestNewSQLiteStore_EmptyPath は空文字列を渡すと ErrSQLitePathRequired が返ることを
// 確認する。XDG パス解決等の defaulting は CLI 層 (M24) の責務で、authgate 層は
// 空文字を弾く責務に徹する。
func TestNewSQLiteStore_EmptyPath(t *testing.T) {
	t.Parallel()

	s, err := authgate.NewSQLiteStore("")
	if err == nil {
		if s != nil {
			_ = s.Close()
		}
		t.Fatal("NewSQLiteStore(\"\") expected error, got nil")
	}
	if !errors.Is(err, authgate.ErrSQLitePathRequired) {
		t.Fatalf("err = %v, want errors.Is ErrSQLitePathRequired", err)
	}
	if s != nil {
		t.Fatalf("NewSQLiteStore(\"\") returned non-nil store: %v", s)
	}
}

// TestNewSQLiteStore_ImplementsStore は NewSQLiteStore の返り値が idproxy.Store を
// 満たし、non-nil であることを確認する。コンパイル時の型適合性と runtime の non-nil
// 性の両方を担保する。
func TestNewSQLiteStore_ImplementsStore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "store.db")
	s, err := authgate.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore(%q) failed: %v", path, err)
	}
	if s == nil {
		t.Fatal("NewSQLiteStore returned nil store")
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	// コンパイル時に idproxy.Store interface を満たすことを検証する補助。
	_ = idproxyStoreCompileCheck(s)
}

// TestNewSQLiteStore_CreatesParentDir は親ディレクトリが存在しない場合に
// 自動作成されることを確認する。spec §11 のデフォルトパス
// `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db` は初回起動時にディレクトリが
// 未作成の可能性が高いため、ライブラリ側でカバーする。
func TestNewSQLiteStore_CreatesParentDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	nested := filepath.Join(dir, "nested", "sub")
	path := filepath.Join(nested, "store.db")

	s, err := authgate.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore(%q) failed: %v", path, err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	// 親ディレクトリが作成されているか確認。
	if info, statErr := os.Stat(nested); statErr != nil {
		t.Fatalf("parent dir %q not created: %v", nested, statErr)
	} else if !info.IsDir() {
		t.Fatalf("parent path %q is not a directory", nested)
	}

	// DB ファイル自体が作成されているか確認。
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("db file %q not created: %v", path, statErr)
	}
}

// TestNewSQLiteStore_SetsPerm0600 はメイン DB と WAL サイドカー (-wal / -shm) の
// 各ファイルが 0600 で作成・締め直しされることを確認する。
//
// WAL サイドカーは初回書き込み (schema 実行) 後に lazy 生成されるため、ファイルが
// 存在する場合のみ assert する。Windows では os.Chmod の挙動が限定的なため
// runtime.GOOS で skip する。
func TestNewSQLiteStore_SetsPerm0600(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX only: Windows では os.Chmod が限定的")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "store.db")
	s, err := authgate.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore(%q) failed: %v", path, err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	// メイン DB ファイルは必ず 0600 でなければならない。
	if info, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("stat main db %q failed: %v", path, statErr)
	} else if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("main db perm = %o, want 0600", got)
	}

	// WAL サイドカーは存在する場合のみ 0600 を assert (lazy 生成のため未生成もありうる)。
	for _, suffix := range []string{"-wal", "-shm"} {
		sidecar := path + suffix
		info, statErr := os.Stat(sidecar)
		if statErr != nil {
			if errors.Is(statErr, fs.ErrNotExist) {
				continue
			}
			t.Errorf("stat %q failed: %v", sidecar, statErr)
			continue
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("sidecar %q perm = %o, want 0600", sidecar, got)
		}
	}
}

// TestNewSQLiteStore_OverridesLoosePerm は事前に緩い権限 (0644) で空ファイルを
// 配置した場合に、起動時に 0600 へ締め直されることを確認する。spec §10 の
// 「起動時にパーミッションを検査」要件に対する強化動作。
func TestNewSQLiteStore_OverridesLoosePerm(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX only: Windows では os.Chmod が限定的")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "store.db")

	// 事前に空ファイルを 0o644 で作成。
	if err := os.WriteFile(path, nil, 0o644); err != nil { //nolint:gosec // テスト前提条件として意図的に緩い perm
		t.Fatalf("seed db file failed: %v", err)
	}

	s, err := authgate.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore(%q) failed: %v", path, err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("stat %q failed: %v", path, statErr)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("loose perm not tightened: got %o, want 0600", got)
	}
}

// TestNewSQLiteStore_PersistsAcrossReopen は 1 回目の Set → Close 後に再オープン
// したストアから同じ session を Get できることを確認する。永続性 (ファイル DB の
// 本質的価値) の sanity check。
func TestNewSQLiteStore_PersistsAcrossReopen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "store.db")

	const sessionID = "persist-test-id"
	want := &idproxy.Session{
		ID:             sessionID,
		ProviderIssuer: "https://accounts.example.com",
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(1 * time.Hour),
	}

	// 1 回目: 書き込み → Close
	s1, err := authgate.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("first NewSQLiteStore failed: %v", err)
	}
	ctx := context.Background()
	if setErr := s1.SetSession(ctx, sessionID, want, time.Hour); setErr != nil {
		_ = s1.Close()
		t.Fatalf("SetSession failed: %v", setErr)
	}
	if closeErr := s1.Close(); closeErr != nil {
		t.Fatalf("first Close failed: %v", closeErr)
	}

	// 2 回目: 同じパスで再オープン → Get
	s2, err := authgate.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("second NewSQLiteStore failed: %v", err)
	}
	t.Cleanup(func() {
		_ = s2.Close()
	})

	got, err := s2.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got == nil {
		t.Fatal("GetSession returned nil session after reopen")
	}
	if got.ID != want.ID {
		t.Errorf("got.ID = %q, want %q", got.ID, want.ID)
	}
	if got.ProviderIssuer != want.ProviderIssuer {
		t.Errorf("got.ProviderIssuer = %q, want %q", got.ProviderIssuer, want.ProviderIssuer)
	}
}

// TestNewSQLiteStore_BasicSetGet は薄ラッパーが正しい関数を呼んでいるかの
// sanity check (memory store の対応テストと同等の検証)。
func TestNewSQLiteStore_BasicSetGet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "store.db")
	store, err := authgate.NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore(%q) failed: %v", path, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	ctx := context.Background()
	sess := &idproxy.Session{
		ID:             "basic-set-get-id",
		ProviderIssuer: "https://accounts.example.com",
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(1 * time.Hour),
	}
	if setErr := store.SetSession(ctx, sess.ID, sess, time.Hour); setErr != nil {
		t.Fatalf("SetSession failed: %v", setErr)
	}

	got, err := store.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got == nil {
		t.Fatal("GetSession returned nil session")
	}
	if got.ID != sess.ID {
		t.Errorf("got.ID = %q, want %q", got.ID, sess.ID)
	}
}

// TestNewSQLiteStore_MemoryPath は ":memory:" 特殊パスでもエラーにならず、
// 親ディレクトリ作成と chmod を skip しつつ Set/Get が動作することを確認する。
func TestNewSQLiteStore_MemoryPath(t *testing.T) {
	t.Parallel()

	store, err := authgate.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore(:memory:) failed: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	ctx := context.Background()
	sess := &idproxy.Session{
		ID:             "memory-test-id",
		ProviderIssuer: "https://accounts.example.com",
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(1 * time.Hour),
	}
	if setErr := store.SetSession(ctx, sess.ID, sess, time.Hour); setErr != nil {
		t.Fatalf("SetSession failed: %v", setErr)
	}

	got, err := store.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got == nil || got.ID != sess.ID {
		t.Fatalf("GetSession got %+v, want id=%q", got, sess.ID)
	}
}
