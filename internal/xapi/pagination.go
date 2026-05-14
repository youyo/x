package xapi

import "time"

// pagination.go は rate-limit aware なページ間待機ロジックを集約する (M29 T7 / D-4)。
//
// 抽出元: M8 の likes.go (computeLikesInterPageWait) — RateLimitInfo + Client.now() のみに
// 依存し、ページ本体の型 (LikedTweetsResponse 等) には依存しない単純な計算なので、
// 将来 M30 の SearchRecent (EachSearchPage) でそのまま再利用できるようにここに切り出す。
//
// EachLikedPage 本体 (型 *LikedTweetsResponse 固定) は likes.go に残す。汎用化 (Each[T])
// は generics 等の段階的検討に委ね、本マイルストーンではスコープ外 (D-4)。

// defaultInterPageDelay は EachXxxPage がページ間に最低限挟む待機時間である
// (spec §10 「ページ間の最小待機: 200ms (バースト抑止)」)。
const defaultInterPageDelay = 200 * time.Millisecond

// computeInterPageWait は rate-limit 情報に基づきページ間の待機時間を計算する。
//
// 規則:
//   - rateLimit が未取得 (Raw=false) → defaultInterPageDelay (200ms)
//   - Remaining が threshold より多い → defaultInterPageDelay
//   - Remaining が threshold 以下かつ Reset が未来 → reset までの差分 (最大 rateLimitMaxWait)
//     ただし 200ms より短ければ 200ms にフォールバック
//   - Reset が過去 (clock skew or stale ヘッダ) → 200ms にフォールバック
//
// threshold は呼び出し側 (likes.go では likesRateLimitThreshold = 2) が指定する。
func (c *Client) computeInterPageWait(rl RateLimitInfo, threshold int) time.Duration {
	wait := defaultInterPageDelay
	if !rl.Raw || rl.Remaining < 0 || rl.Remaining > threshold || rl.Reset.IsZero() {
		return wait
	}
	until := rl.Reset.Sub(c.now())
	if until > rateLimitMaxWait {
		until = rateLimitMaxWait
	}
	if until > wait {
		wait = until
	}
	return wait
}
