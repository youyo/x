package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

// credentialsFileName は CLI シークレット (X API トークン) を保存するファイル名。
// spec §11 「`${XDG_DATA_HOME:-~/.local/share}/x/credentials.toml`」に対応する。
const credentialsFileName = "credentials.toml"

// ErrCredentialsNotFound は credentials.toml が存在しない場合に返される番兵エラー。
// errors.Is で判別可能。呼び出し側 (M9 の CLI 統合) が exit code 3 (AuthError) にマップする想定。
var ErrCredentialsNotFound = errors.New("credentials.toml が見つかりません")

// ErrPermissionsTooOpen は credentials.toml のパーミッションが他者にも読める状態のときに返される。
// 「他者にも読める」= group / other のいずれかのビットが立っている状態
// (= mode & 0o077 != 0)。spec §10「0600 でなければ警告」のうち、
// より厳しい 0400 / 0500 等は許容する解釈で実装している。
var ErrPermissionsTooOpen = errors.New("credentials.toml のパーミッションが緩すぎます (0600 推奨)")

// Credentials は credentials.toml の [xapi] セクションを表す。
//
// CLI モード専用。MCP モード (Lambda 等) では環境変数のみから取得し、
// 本構造体は spec §11 設計原則「MCP モードはファイル不使用」によりロードしない。
type Credentials struct {
	// APIKey は X API OAuth 1.0a の Consumer Key (`api_key`)。
	APIKey string `toml:"api_key"`
	// APISecret は X API OAuth 1.0a の Consumer Secret (`api_secret`)。
	APISecret string `toml:"api_secret"`
	// AccessToken は X API OAuth 1.0a の Access Token (`access_token`)。
	AccessToken string `toml:"access_token"`
	// AccessTokenSecret は X API OAuth 1.0a の Access Token Secret (`access_token_secret`)。
	AccessTokenSecret string `toml:"access_token_secret"`
}

// credentialsFile は credentials.toml のトップレベル構造ラッパ ([xapi] テーブル)。
// spec §11 の TOML レイアウトに合わせるためのもので外部公開しない。
type credentialsFile struct {
	XAPI Credentials `toml:"xapi"`
}

// DefaultCredentialsPath は DataDir()/credentials.toml を返す。
//
// M9/M12 等で credentials.toml のフルパスを得るためのヘルパ。
// パス算出のみで I/O は行わない。
func DefaultCredentialsPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialsFileName), nil
}

// LoadCredentials は path から Credentials を読み込んで返す。
//
// 挙動:
//   - path のファイルが存在しない → ErrCredentialsNotFound を返却
//   - パーミッションが 0600 より緩い (POSIX) → log.Printf("warning: ...") で警告し、値は返す
//   - TOML 構文エラー → wrap してエラー返却
//   - 読み込み成功 → デコード結果 (Credentials の値コピー) を返却
//
// 必須フィールドのバリデーション (空文字チェック) は本関数では行わない。
// 呼び出し側 (M9 で実装する `x me` 等の CLI 統合) で env override を加味した上で実施する。
func LoadCredentials(path string) (*Credentials, error) {
	if err := CheckPermissions(path); err != nil {
		switch {
		case errors.Is(err, ErrCredentialsNotFound):
			return nil, err
		case errors.Is(err, ErrPermissionsTooOpen):
			log.Printf("warning: %v: %s", err, path)
			// 続行して値は返す (spec §10 「警告」方針)
		default:
			return nil, err
		}
	}

	var file credentialsFile
	if _, err := toml.DecodeFile(path, &file); err != nil {
		return nil, fmt.Errorf("credentials.toml のデコードに失敗 (%s): %w", path, err)
	}
	creds := file.XAPI
	return &creds, nil
}

// SaveCredentials は c を path に書き込む。
//
// 設計:
//   - 親ディレクトリが無ければ 0700 で作成。既存ディレクトリでも明示的に 0700 へ
//     更新する (spec §11「ディレクトリを 0700 で作成」の意図を既存パスでも徹底)。
//   - tmp ファイル (path + .tmp-<pid>) に書き込んでから os.Rename で原子的に置換し、
//     書き込み途中での読み取りや TOC-TOU を最小化する。
//   - 書き込み後に明示的に os.Chmod(path, 0o600) を実行し、umask による緩和を打ち消す。
//
// perm 引数は意図的に取らない (常に 0600 を強制) — spec §10/§11 のセキュリティ原則を
// 呼び出し側の誤用 (例: 0644 渡し) から守るための設計判断。
func SaveCredentials(path string, c *Credentials) error {
	if c == nil {
		return errors.New("credentials が nil です")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("ディレクトリ作成に失敗 (%s): %w", dir, err)
	}
	// MkdirAll は既存ディレクトリの mode を変えないため、明示 Chmod で 0700 を強制する。
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("ディレクトリのパーミッション設定に失敗 (%s): %w", dir, err)
	}

	tmp := fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("一時ファイル作成に失敗 (%s): %w", tmp, err)
	}

	enc := toml.NewEncoder(f)
	if encErr := enc.Encode(credentialsFile{XAPI: *c}); encErr != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("credentials.toml のエンコードに失敗: %w", encErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("一時ファイルのクローズに失敗: %w", closeErr)
	}
	if renameErr := os.Rename(tmp, path); renameErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("credentials.toml のリネームに失敗 (%s → %s): %w", tmp, path, renameErr)
	}
	if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
		return fmt.Errorf("credentials.toml のパーミッション設定に失敗 (%s): %w", path, chmodErr)
	}
	return nil
}

// CheckPermissions は path のパーミッションが他者から読めない状態 (mode & 0o077 == 0) であることを検証する。
//
// 挙動:
//   - Windows (runtime.GOOS == "windows") → POSIX 流のビット検査は意味を成さないため常に nil
//   - ファイル不在 → ErrCredentialsNotFound (path 情報付き)
//   - mode & 0o077 != 0 (group / other に何か立っている) → ErrPermissionsTooOpen
//   - それ以外 (0400/0500/0600/0700 など) → nil
//
// spec §10「0600 でなければ警告」を、より厳しい mode (例: 0400) は許容するように
// 読み替えて実装している。doc コメントとして明示する意図。
func CheckPermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrCredentialsNotFound, path)
		}
		return fmt.Errorf("credentials.toml の状態取得に失敗 (%s): %w", path, err)
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: mode=%o path=%s", ErrPermissionsTooOpen, info.Mode().Perm(), path)
	}
	return nil
}
