package authgate

import (
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/youyo/idproxy"
	redisstore "github.com/youyo/idproxy/store/redis"
)

// redisKeyPrefix は idproxy/store/redis に渡す KeyPrefix の固定値である。
//
// idproxy/store/redis.Store の内部キー生成ルールは `prefix + ns + ":" + id` で
// あるため、末尾コロン付きの値を指定することで最終キーは `idproxy:session:<id>` /
// `idproxy:authcode:<code>` 等の形式となり、共有 Redis 上での他用途キーとの
// 名前空間衝突を防ぐ。
const redisKeyPrefix = "idproxy:"

// ErrRedisURLRequired は [NewRedisStore] に空文字を渡した場合に返るエラーである。
// errors.Is で判定すること。
//
// spec §11 の `STORE_BACKEND=redis` モードでは `REDIS_URL` の解決結果として
// 必ず非空 URL が渡される想定。defaulting は CLI 層 (M24) の責務で、authgate 層は
// 空文字を errors.Is で識別可能な sentinel で弾く責務に徹する。
var ErrRedisURLRequired = errors.New("authgate: redis url is required")

// NewRedisStore は spec §11 の `REDIS_URL` を解析し、
// [github.com/youyo/idproxy/store/redis.New] を呼んで idproxy.Store の Redis 実装を
// 返す薄いラッパーである。
//
// 用途: spec §11 `STORE_BACKEND=redis` モード。複数インスタンス / 軽量サーバー
// 構成での共有状態管理を想定する。
//
// 引数 rawURL (例):
//
//   - `redis://localhost:6379/0`
//   - `redis://:password@host:6379/1`
//   - `rediss://host:6380/2` (TLS 自動有効化)
//
// URL の解析は [github.com/redis/go-redis/v9.ParseURL] に委譲する。スキームに
// 応じた TLS 有効化、user:password@host:port、/N 形式の DB 番号など、redis URL
// に関する仕様は全て go-redis の挙動に従う。`rediss://` スキームの場合は
// TLS が自動有効化される (redisstore.Options.TLS = true)。
//
// 動作:
//
//   - 空文字 → [ErrRedisURLRequired]
//   - URL 解析失敗 → fmt.Errorf で wrap (ErrRedisURLRequired ではない)
//   - 接続 / PING 失敗 → fmt.Errorf で wrap
//
// 戻り値:
//
//   - 成功時: idproxy.Store interface を満たす値。利用者は不要になった時点で
//     [idproxy.Store.Close] を呼び出し、内部の redis.Client を解放する責務を負う。
//   - 失敗時: 上記いずれかのエラー。
//
// TTL は idproxy/store/redis 側が SET EX で自動付与するため、本関数では扱わない。
func NewRedisStore(rawURL string) (idproxy.Store, error) {
	if rawURL == "" {
		return nil, ErrRedisURLRequired
	}

	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("authgate: parse redis url: %w", err)
	}

	store, err := redisstore.New(redisstore.Options{
		Addr:      opts.Addr,
		Password:  opts.Password,
		DB:        opts.DB,
		TLS:       opts.TLSConfig != nil,
		KeyPrefix: redisKeyPrefix,
	})
	if err != nil {
		return nil, fmt.Errorf("authgate: open redis store: %w", err)
	}
	return store, nil
}
