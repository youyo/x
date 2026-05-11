package authgate_test

import (
	"context"
	"testing"
	"time"

	"github.com/youyo/idproxy"
	"github.com/youyo/x/internal/authgate"
)

// TestNewMemoryStore_ImplementsStore は NewMemoryStore が idproxy.Store を満たし、
// non-nil の値を返すことを確認する。
//
// 戻り値型自体はシグネチャ上 idproxy.Store だが、本テストでは追加で動的型アサーション
// を行わず、non-nil 性と Close 可能性のみを検証することで、ラッパー差し替えに対する
// 安定性を担保する。
func TestNewMemoryStore_ImplementsStore(t *testing.T) {
	t.Parallel()

	s := authgate.NewMemoryStore()
	if s == nil {
		t.Fatal("NewMemoryStore() returned nil")
	}
	// idproxy.Store interface 適合性のコンパイル時保証。
	_ = idproxyStoreCompileCheck(s)
	t.Cleanup(func() {
		_ = s.Close()
	})
}

// idproxyStoreCompileCheck は引数の型が idproxy.Store を満たすことをコンパイル時に
// 検査する補助関数。テストランタイムでは呼び出されるだけで何もしない。
func idproxyStoreCompileCheck(_ idproxy.Store) struct{} { return struct{}{} }

// TestNewMemoryStore_BasicSetGet は memory store を経由したセッションの保存・取得が
// 正常に動作することを確認する (薄ラッパーが正しい関数を呼んでいるかの sanity check)。
func TestNewMemoryStore_BasicSetGet(t *testing.T) {
	t.Parallel()

	store := authgate.NewMemoryStore()
	t.Cleanup(func() {
		_ = store.Close()
	})

	ctx := context.Background()
	sess := &idproxy.Session{
		ID:             "test-session-id",
		ProviderIssuer: "https://accounts.example.com",
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(1 * time.Hour),
	}
	if err := store.SetSession(ctx, sess.ID, sess, time.Hour); err != nil {
		t.Fatalf("SetSession failed: %v", err)
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
