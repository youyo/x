package authgate_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/youyo/idproxy"
	"github.com/youyo/x/internal/authgate"
)

// startMiniredis は miniredis を起動し、テスト終了時に Close する補助関数である。
// miniredis の TTL は実時間と連動しないため、SET EX 後に背景で FastForward する
// ticker は本マイルストーンでは不要 (期限切れ検証は M22 のスコープ外)。
func startMiniredis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr := miniredis.RunT(t)
	return mr
}

// TestNewRedisStore_EmptyURL は空文字列を渡すと ErrRedisURLRequired が返ることを
// 確認する。CLI 層 (M24) で REDIS_URL の defaulting が行われる前提のため、authgate
// 層は空文字を sentinel で識別可能に弾く責務に徹する。
func TestNewRedisStore_EmptyURL(t *testing.T) {
	t.Parallel()

	s, err := authgate.NewRedisStore("")
	if err == nil {
		if s != nil {
			_ = s.Close()
		}
		t.Fatal("NewRedisStore(\"\") expected error, got nil")
	}
	if !errors.Is(err, authgate.ErrRedisURLRequired) {
		t.Fatalf("err = %v, want errors.Is ErrRedisURLRequired", err)
	}
	if s != nil {
		t.Fatalf("NewRedisStore(\"\") returned non-nil store: %v", s)
	}
}

// TestNewRedisStore_InvalidURL は URL として解析不能な文字列を渡した場合、
// ErrRedisURLRequired ではなく redis.ParseURL のエラーを wrap して返すことを
// 確認する (sentinel と区別可能であること)。
func TestNewRedisStore_InvalidURL(t *testing.T) {
	t.Parallel()

	cases := []string{
		"::not-a-url::",
		"redis://:invalid-port-format/abc",
	}
	for _, raw := range cases {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			s, err := authgate.NewRedisStore(raw)
			if err == nil {
				if s != nil {
					_ = s.Close()
				}
				t.Fatalf("NewRedisStore(%q) expected error, got nil", raw)
			}
			if errors.Is(err, authgate.ErrRedisURLRequired) {
				t.Errorf("err = %v, must NOT be ErrRedisURLRequired (空文字以外で sentinel に誤マッチ)", err)
			}
			if s != nil {
				t.Errorf("NewRedisStore(%q) returned non-nil store", raw)
			}
		})
	}
}

// TestNewRedisStore_InvalidScheme は redis / rediss / unix 以外のスキームを
// 拒否することを確認する。redis.ParseURL の "invalid URL scheme" エラーを
// wrap して返すこと。
func TestNewRedisStore_InvalidScheme(t *testing.T) {
	t.Parallel()

	s, err := authgate.NewRedisStore("http://example.com:6379")
	if err == nil {
		if s != nil {
			_ = s.Close()
		}
		t.Fatal("NewRedisStore(http://...) expected error, got nil")
	}
	if errors.Is(err, authgate.ErrRedisURLRequired) {
		t.Errorf("err = %v, must NOT be ErrRedisURLRequired", err)
	}
	if !strings.Contains(err.Error(), "scheme") {
		t.Errorf("err = %v, want message containing 'scheme'", err)
	}
}

// TestNewRedisStore_ImplementsStore は NewRedisStore の返り値が idproxy.Store を
// 満たし non-nil であることを確認する。コンパイル時の型適合性と runtime の
// non-nil 性の両方を担保する。
func TestNewRedisStore_ImplementsStore(t *testing.T) {
	t.Parallel()

	mr := startMiniredis(t)
	url := "redis://" + mr.Addr() + "/0"

	s, err := authgate.NewRedisStore(url)
	if err != nil {
		t.Fatalf("NewRedisStore(%q) failed: %v", url, err)
	}
	if s == nil {
		t.Fatal("NewRedisStore returned nil store")
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	_ = idproxyStoreCompileCheck(s)
}

// TestNewRedisStore_PingFailure は到達不能な host への接続が Ping エラーを
// wrap して返すことを確認する。
//
// 実装ノート (advisor 助言反映):
//   - redisstore.New は 5s timeout の Ping を実行する。closed-port 系の
//     アドレスを使うことで TCP refused を即座に検出させる (5s wait 回避)。
//   - 本テストでは bind + 即 Close で「直前まで生きていた、いま死んでる」
//     ポートを取得し、5s timeout 待ちを避ける。
func TestNewRedisStore_PingFailure(t *testing.T) {
	t.Parallel()

	// 空きポートを取得して即時 Close → 必ず "connection refused" になる。
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	addr := l.Addr().String()
	if closeErr := l.Close(); closeErr != nil {
		t.Fatalf("listener.Close failed: %v", closeErr)
	}

	url := "redis://" + addr + "/0"
	start := time.Now()
	s, err := authgate.NewRedisStore(url)
	elapsed := time.Since(start)

	if err == nil {
		if s != nil {
			_ = s.Close()
		}
		t.Fatalf("NewRedisStore(%q) expected error, got nil", url)
	}
	// connection refused は数ミリ秒以内で返る想定。CI ノイズを考慮して 4s 上限。
	if elapsed >= 4*time.Second {
		t.Errorf("Ping failure took too long: %v (want < 4s)", elapsed)
	}
	if errors.Is(err, authgate.ErrRedisURLRequired) {
		t.Errorf("err = %v, must NOT be ErrRedisURLRequired", err)
	}
}

// TestNewRedisStore_BasicSetGet は薄ラッパーが SetSession / GetSession の
// ラウンドトリップを正しく動かすことを sanity check する。memory/sqlite の
// 対応テストと同等の検証。
func TestNewRedisStore_BasicSetGet(t *testing.T) {
	t.Parallel()

	mr := startMiniredis(t)
	url := "redis://" + mr.Addr() + "/0"

	store, err := authgate.NewRedisStore(url)
	if err != nil {
		t.Fatalf("NewRedisStore(%q) failed: %v", url, err)
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

// TestNewRedisStore_RoundTripAcrossClients は同じ miniredis インスタンスに
// 対し 2 つの authgate.NewRedisStore を作り、1 つ目で Set → 2 つ目で Get
// できることを確認する。
//
// 注: sqlite のような "ファイル on-disk の永続性" ではなく "サーバ側 state を
// 別クライアントから観測できる" ことの sanity check (miniredis は mr.Close()
// で state が消える)。
func TestNewRedisStore_RoundTripAcrossClients(t *testing.T) {
	t.Parallel()

	mr := startMiniredis(t)
	url := "redis://" + mr.Addr() + "/0"

	const sessionID = "cross-client-id"
	want := &idproxy.Session{
		ID:             sessionID,
		ProviderIssuer: "https://accounts.example.com",
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(1 * time.Hour),
	}

	s1, err := authgate.NewRedisStore(url)
	if err != nil {
		t.Fatalf("first NewRedisStore failed: %v", err)
	}
	ctx := context.Background()
	if setErr := s1.SetSession(ctx, sessionID, want, time.Hour); setErr != nil {
		_ = s1.Close()
		t.Fatalf("SetSession failed: %v", setErr)
	}
	if closeErr := s1.Close(); closeErr != nil {
		t.Fatalf("first Close failed: %v", closeErr)
	}

	s2, err := authgate.NewRedisStore(url)
	if err != nil {
		t.Fatalf("second NewRedisStore failed: %v", err)
	}
	t.Cleanup(func() {
		_ = s2.Close()
	})

	got, err := s2.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got == nil {
		t.Fatal("GetSession returned nil session after reconnect")
	}
	if got.ID != want.ID {
		t.Errorf("got.ID = %q, want %q", got.ID, want.ID)
	}
}

// TestNewRedisStore_KeyPrefix は authgate.NewRedisStore が KeyPrefix="idproxy:"
// を渡すことを検証する。idproxy/store/redis.Store の k() は
// prefix + ns + ":" + id を生成するため、SetSession 後のキーは
// "idproxy:session:<id>" 形式となる。
//
// 共有 Redis 上での名前空間衝突を防ぐためにこの prefix を維持することが重要。
func TestNewRedisStore_KeyPrefix(t *testing.T) {
	t.Parallel()

	mr := startMiniredis(t)
	url := "redis://" + mr.Addr() + "/0"

	store, err := authgate.NewRedisStore(url)
	if err != nil {
		t.Fatalf("NewRedisStore(%q) failed: %v", url, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	const sessionID = "prefix-check-id"
	sess := &idproxy.Session{
		ID:             sessionID,
		ProviderIssuer: "https://accounts.example.com",
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(1 * time.Hour),
	}
	ctx := context.Background()
	if setErr := store.SetSession(ctx, sessionID, sess, time.Hour); setErr != nil {
		t.Fatalf("SetSession failed: %v", setErr)
	}

	wantKey := "idproxy:session:" + sessionID
	keys := mr.Keys()
	found := false
	for _, k := range keys {
		if k == wantKey {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected key %q not found in miniredis. all keys: %v", wantKey, keys)
	}
}

// TestNewRedisStore_ParsesDBNumber は URL の path /N が DB 番号として
// 解釈されることを確認する。miniredis v2.30+ は DB SELECT をサポート
// するため、DB 3 に書いたキーが DB 0 から見えないことで検証する。
func TestNewRedisStore_ParsesDBNumber(t *testing.T) {
	t.Parallel()

	mr := startMiniredis(t)
	url := "redis://" + mr.Addr() + "/3"

	store, err := authgate.NewRedisStore(url)
	if err != nil {
		t.Fatalf("NewRedisStore(%q) failed: %v", url, err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})

	const sessionID = "db-number-id"
	sess := &idproxy.Session{
		ID:             sessionID,
		ProviderIssuer: "https://accounts.example.com",
		CreatedAt:      time.Now(),
		ExpiresAt:      time.Now().Add(1 * time.Hour),
	}
	ctx := context.Background()
	if setErr := store.SetSession(ctx, sessionID, sess, time.Hour); setErr != nil {
		t.Fatalf("SetSession failed: %v", setErr)
	}

	wantKey := "idproxy:session:" + sessionID
	if !mr.DB(3).Exists(wantKey) {
		t.Errorf("key %q not found in DB 3 (expected); DB(3) keys: %v", wantKey, mr.DB(3).Keys())
	}
	if mr.DB(0).Exists(wantKey) {
		t.Errorf("key %q found in DB 0 (should be isolated to DB 3); DB(0) keys: %v", wantKey, mr.DB(0).Keys())
	}
}

// TestNewRedisStore_ParsesPassword は URL の userinfo (パスワード) が
// REQUIREPASS をかけた miniredis に正しく渡されることを確認する。
// 正しいパスワードで接続成功、間違ったパスワードで失敗を返す。
func TestNewRedisStore_ParsesPassword(t *testing.T) {
	t.Parallel()

	mr := startMiniredis(t)
	mr.RequireAuth("s3cret")

	t.Run("correct password", func(t *testing.T) {
		t.Parallel()
		url := "redis://:s3cret@" + mr.Addr() + "/0"
		store, err := authgate.NewRedisStore(url)
		if err != nil {
			t.Fatalf("NewRedisStore(correct) failed: %v", err)
		}
		t.Cleanup(func() {
			_ = store.Close()
		})
	})

	t.Run("wrong password", func(t *testing.T) {
		t.Parallel()
		url := "redis://:wrong@" + mr.Addr() + "/0"
		store, err := authgate.NewRedisStore(url)
		if err == nil {
			if store != nil {
				_ = store.Close()
			}
			t.Fatal("NewRedisStore(wrong) expected error, got nil")
		}
		if errors.Is(err, authgate.ErrRedisURLRequired) {
			t.Errorf("err = %v, must NOT be ErrRedisURLRequired", err)
		}
	})
}

// TestParseRedisURL_RedissTLSDetection は redis.ParseURL("rediss://...") が
// TLSConfig を non-nil に設定することを確認する。これは NewRedisStore が
// 依存する go-redis の挙動を「契約」として固定するスモークテスト
// (advisor 助言反映: 実 TLS handshake は miniredis では検証不可なので
// パース段階の契約検証に留める)。
func TestParseRedisURL_RedissTLSDetection(t *testing.T) {
	t.Parallel()

	opts, err := goredis.ParseURL("rediss://example.com:6380/0")
	if err != nil {
		t.Fatalf("goredis.ParseURL failed: %v", err)
	}
	if opts.TLSConfig == nil {
		t.Error("rediss:// scheme should set TLSConfig non-nil, got nil")
	}

	opts2, err := goredis.ParseURL("redis://example.com:6380/0")
	if err != nil {
		t.Fatalf("goredis.ParseURL (plain) failed: %v", err)
	}
	if opts2.TLSConfig != nil {
		t.Errorf("redis:// scheme should NOT set TLSConfig, got %v", opts2.TLSConfig)
	}
}
