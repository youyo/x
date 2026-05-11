package authgate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/youyo/idproxy"
	sqlitestore "github.com/youyo/idproxy/store/sqlite"
)

// ErrSQLitePathRequired は [NewSQLiteStore] に空のパスを渡した場合に返るエラー。
//
// `STORE_BACKEND=sqlite` モードでは spec §11 の `SQLITE_PATH` を解決した結果として
// 必ず非空のパスが渡される想定。XDG 環境変数のデフォルト展開 (例:
// `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db`) は CLI 層 (M24) の責務で、
// authgate 層は空文字を errors.Is で識別可能な sentinel で弾く責務に徹する。
var ErrSQLitePathRequired = errors.New("authgate: sqlite path is required")

// NewSQLiteStore は [github.com/youyo/idproxy/store/sqlite.New] を呼び出して
// idproxy.Store の sqlite 実装を返す薄いラッパーである。
//
// 用途: spec §11 `STORE_BACKEND=sqlite` モード、もしくはローカル開発・テスト用途
// の永続化バックエンド。内部実装は pure Go の `modernc.org/sqlite` (CGO 不要)。
//
// 引数 path:
//
//   - 通常はファイルパス (絶対/相対どちらでも可)。spec §11 のデフォルトは
//     `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db` (CLI 層 M24 で解決)。
//   - 親ディレクトリが存在しない場合は `0o700` で自動作成する (credentials.toml の
//     ディレクトリ権限規約に整合)。既存ディレクトリの権限は変更しない。
//   - 特殊値 `":memory:"` を許容する (テスト用途、メモリ DB)。`:memory:` の場合は
//     親ディレクトリ作成および chmod を skip する。
//   - 空文字列は [ErrSQLitePathRequired] で reject される。
//
// セキュリティ (spec §10):
//
//   - 起動直後にメイン DB ファイルおよび WAL モードのサイドカー (`<path>-wal`,
//     `<path>-shm`) に対して `os.Chmod` で 0600 を設定する。
//   - サイドカーは初回書き込み後に lazy 生成されるため存在しないことがあるが、
//     その場合 ENOENT は無視する (best-effort)。
//   - POSIX 環境では確実に 0600 となるが、Windows 等の非 POSIX 環境では
//     `os.Chmod` の挙動が限定的なため、失敗を握りつぶして続行する。
//
// 戻り値:
//
//   - 成功時: idproxy.Store interface を満たす [*sqlitestore.Store]。利用者は
//     不要になった時点で [idproxy.Store.Close] を呼んで内部の cleanup goroutine を
//     停止する責務を負う。
//   - 失敗時: 空 path → [ErrSQLitePathRequired]、親ディレクトリ作成失敗・sqlite
//     open 失敗は `fmt.Errorf` でラップしたエラーを返す。
//
// スキーマ初期化は呼び出し先の `idproxy/store/sqlite.New` 内部で
// `CREATE TABLE IF NOT EXISTS` 経由で実行されるため、本関数では migrate を扱わない。
func NewSQLiteStore(path string) (idproxy.Store, error) {
	if path == "" {
		return nil, ErrSQLitePathRequired
	}

	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("authgate: create sqlite parent dir: %w", err)
		}
	}

	store, err := sqlitestore.New(path)
	if err != nil {
		return nil, fmt.Errorf("authgate: open sqlite store: %w", err)
	}

	if path != ":memory:" {
		// POSIX: best-effort で 0600 を強制する。WAL モードのサイドカー
		// (-wal / -shm) も対象。存在しないファイル (lazy 生成前) や Windows での
		// 失敗は致命的ではないので無視する。
		for _, p := range []string{path, path + "-wal", path + "-shm"} {
			_ = os.Chmod(p, 0o600) //nolint:errcheck,gosec // best-effort: 失敗時は doc.go の説明通り無視
		}
	}
	return store, nil
}
