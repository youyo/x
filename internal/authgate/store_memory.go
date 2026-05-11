package authgate

import (
	"github.com/youyo/idproxy"
	"github.com/youyo/idproxy/store"
)

// NewMemoryStore は idproxy のインメモリ Store を返す。
//
// シングルインスタンス起動 / テスト用途向けで、プロセス再起動でセッションは消失する。
// 内部実装は [github.com/youyo/idproxy/store.NewMemoryStore] を呼び出す薄いラッパー
// であり、追加機能は持たない。
//
// M21–M23 で sqlite / redis / dynamodb 用の対応関数 (NewSQLiteStore 等) を追加するまで、
// 本関数が [ModeIDProxy] のデフォルト Store となる。返り値はバックグラウンド
// クリーンアップ goroutine を内蔵するため、利用者は不要になった時点で
// [idproxy.Store.Close] を呼び出して解放すること。
func NewMemoryStore() idproxy.Store {
	return store.NewMemoryStore()
}
