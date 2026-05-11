package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/youyo/x/internal/config"
)

// errConfigureCancelled は対話モードでユーザーが上書き確認を拒否した際に返される。
// xapi 番兵エラーを内包しないため fallthrough で exit 1 (ExitGenericError) に写像される。
//
// 番兵エラー化 (var) して errors.Is で識別可能にしている: テストから「キャンセル経路を
// 通った」ことを精密に検証するため (plans/x-m12-cli-configure.md D-2)。
var errConfigureCancelled = errors.New("configure cancelled by user")

// configurePathsJSON は --print-paths の JSON 出力スキーマ。
//
// 仕様 (plans/x-m12-cli-configure.md D-4):
//   - config:      ${XDG_CONFIG_HOME}/x/config.toml
//   - credentials: ${XDG_DATA_HOME}/x/credentials.toml
//   - data_dir:    ${XDG_DATA_HOME}/x (idproxy sqlite 等の配置先)
//
// 環境変数未設定時は HOME 配下のデフォルトに解決される (config.Dir / config.DataDir 参照)。
type configurePathsJSON struct {
	Config      string `json:"config"`
	Credentials string `json:"credentials"`
	DataDir     string `json:"data_dir"`
}

// configureCheckJSON は --check の JSON 出力スキーマ。
//
// 仕様 (plans/x-m12-cli-configure.md D-3):
//   - ok=true:  credentials.toml が perm 0600 + 4 必須フィールド完備 + config.toml に secret 混入なし
//   - ok=false: いずれかが満たされない (理由は Issues に英語 1 行ずつ)
//
// exit code は ok にかかわらず 0 とする (情報出力に専念、jq '.ok' 等での判定を意図)。
type configureCheckJSON struct {
	OK     bool     `json:"ok"`
	Issues []string `json:"issues"`
}

// newConfigureCmd は `x configure` サブコマンドを生成する factory。
//
// モード分岐 (排他ではなく優先順位):
//  1. --print-paths → 設定ファイルのパス情報を JSON / human で出力 (副作用なし、return)
//  2. --check       → 既存ファイルの構成検証を JSON / human で出力 (副作用なし、return)
//  3. デフォルト    → 対話モード (4 つの X API トークンを stdin から読み credentials.toml に保存)
//
// 設計判断:
//   - --print-paths と --check が同時指定された場合、--print-paths が優先 (D-5)
//   - 対話モードでは TTY (os.Stdin) のときのみ term.ReadPassword で echo オフ。
//     非 TTY (テスト / パイプ) では bufio.Reader で通常 read (D-13)
//   - 既存 credentials.toml がある場合は上書き確認プロンプトで保護 (D-2)
//
// エラーは wrap せず呼び出し側 (cmd/x/main.go run()) に伝搬する。番兵エラー写像:
//   - cli.ErrInvalidArgument → exit 2 (対話モードの空フィールド)
//   - errConfigureCancelled  → 内包しないので fallthrough で exit 1
func newConfigureCmd() *cobra.Command {
	var (
		printPaths bool
		check      bool
		noJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "configure",
		Short: "set up X API credentials and verify configuration",
		Long: "Interactive setup of X API OAuth 1.0a credentials.\n" +
			"With no flags, prompts for 4 fields and writes them to credentials.toml (perm 0600).\n" +
			"--print-paths: print resolved paths for config.toml / credentials.toml / data dir.\n" +
			"--check: verify credentials.toml perm + required fields + no secret leak in config.toml.\n" +
			"--no-json: emit human-readable output instead of JSON (for --print-paths / --check).\n" +
			"Exit codes: 0 success, 1 generic / cancelled, 2 invalid argument.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if printPaths {
				return runConfigurePrintPaths(cmd, noJSON)
			}
			if check {
				return runConfigureCheck(cmd, noJSON)
			}
			return runConfigureInteractive(cmd)
		},
	}
	cmd.Flags().BoolVar(&printPaths, "print-paths", false, "print config.toml / credentials.toml / data dir paths")
	cmd.Flags().BoolVar(&check, "check", false, "verify existing configuration (perm, required fields, secret leaks)")
	cmd.Flags().BoolVar(&noJSON, "no-json", false, "emit human-readable output instead of JSON")
	return cmd
}

// runConfigurePrintPaths は --print-paths の出力処理を行う (D-4)。
//
// XDG 環境変数 (XDG_CONFIG_HOME / XDG_DATA_HOME) を解決し、各ファイルの完全パスを
// JSON または human 形式で stdout に出力する。ファイル存在のチェックは行わない
// (パス情報の出力に責務を絞る、ファイル状態は --check で別途検証する)。
func runConfigurePrintPaths(cmd *cobra.Command, noJSON bool) error {
	cfgPath, err := config.DefaultCLIConfigPath()
	if err != nil {
		return fmt.Errorf("resolve config.toml path: %w", err)
	}
	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		return fmt.Errorf("resolve credentials.toml path: %w", err)
	}
	dataDir, err := config.DataDir()
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	out := configurePathsJSON{
		Config:      cfgPath,
		Credentials: credPath,
		DataDir:     dataDir,
	}
	if noJSON {
		_, werr := fmt.Fprintf(cmd.OutOrStdout(),
			"config=%s\ncredentials=%s\ndata_dir=%s\n",
			out.Config, out.Credentials, out.DataDir)
		return werr
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(out)
}

// runConfigureCheck は --check の検証処理を行う (D-3 / D-9)。
//
// 検証項目 (順序固定、各失敗を独立した issue として append):
//  1. credentials.toml 存在チェック (存在しない → issue 追加して以降のパーミッション / フィールドチェックはスキップ)
//  2. credentials.toml パーミッション (config.CheckPermissions が POSIX で group/other 0 を要求)
//  3. credentials.toml の 4 必須フィールド (api_key / api_secret / access_token / access_token_secret) 非空
//  4. config.toml シークレット混入 (config.CheckConfigNoSecrets)
//
// ファイル不在は credentials.toml では問題視するが config.toml ではスキップする
// (config.toml はオプショナル、CheckConfigNoSecrets が ErrNotExist を nil で握り潰す)。
//
// X API 認証情報の "有効性" は X API を叩いて確認する (`x me`) のが本筋なので、
// 本関数では network I/O を行わない (D-9)。
func runConfigureCheck(cmd *cobra.Command, noJSON bool) error {
	issues := collectCheckIssues()
	result := configureCheckJSON{
		OK:     len(issues) == 0,
		Issues: issues,
	}
	if noJSON {
		return writeCheckHuman(cmd.OutOrStdout(), &result)
	}
	// Issues が空の場合でも JSON は []  を出力したい (nil だと "null" になる)。
	if result.Issues == nil {
		result.Issues = []string{}
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
}

// collectCheckIssues は --check で検査する各項目を順次評価し、issue 文字列スライスを返す。
//
// issue メッセージ仕様:
//   - 英語 1 行 / 値は含めず (シークレット漏洩防止)
//   - キー名は出して良い (api_key, api_secret, ...)
//   - パスは絶対パスを含めて良い (ユーザー診断に必須)
func collectCheckIssues() []string {
	var issues []string

	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		issues = append(issues, fmt.Sprintf("cannot resolve credentials.toml path: %v", err))
	} else {
		issues = append(issues, checkCredentialsFile(credPath)...)
	}

	cfgPath, err := config.DefaultCLIConfigPath()
	if err != nil {
		issues = append(issues, fmt.Sprintf("cannot resolve config.toml path: %v", err))
	} else if err := config.CheckConfigNoSecrets(cfgPath); err != nil {
		issues = append(issues, fmt.Sprintf("config.toml: %v", err))
	}
	return issues
}

// checkCredentialsFile は credentials.toml のパーミッション + 必須フィールドを検証する。
//
// 注意: パーミッションエラー時も LoadCredentials は値を返却するので、フィールドチェックも継続する。
// 存在しない場合は ErrCredentialsNotFound を 1 つの issue に集約し、以降のチェックは省略する。
func checkCredentialsFile(path string) []string {
	var issues []string

	if perr := config.CheckPermissions(path); perr != nil {
		if errors.Is(perr, config.ErrCredentialsNotFound) {
			return []string{fmt.Sprintf("credentials.toml not found at %s", path)}
		}
		// それ以外 (ErrPermissionsTooOpen 等) は issue に追加して継続。
		issues = append(issues, fmt.Sprintf("credentials.toml: %v", perr))
	}

	creds, lerr := config.LoadCredentials(path)
	if lerr != nil {
		issues = append(issues, fmt.Sprintf("credentials.toml: cannot load: %v", lerr))
		return issues
	}
	for _, fc := range credsFieldChecks(creds) {
		if fc.empty {
			issues = append(issues, fmt.Sprintf("credentials.toml: missing field: %s", fc.name))
		}
	}
	return issues
}

// credsFieldChecks は credentials.toml に必須な 4 フィールドの非空チェック表を返す。
func credsFieldChecks(c *config.Credentials) []struct {
	name  string
	empty bool
} {
	return []struct {
		name  string
		empty bool
	}{
		{"api_key", c.APIKey == ""},
		{"api_secret", c.APISecret == ""},
		{"access_token", c.AccessToken == ""},
		{"access_token_secret", c.AccessTokenSecret == ""},
	}
}

// writeCheckHuman は configureCheckJSON を human-readable で w に書き出す。
//
// フォーマット (D-3):
//
//	ok: true
//	  - <issue1>
//	  - <issue2>
func writeCheckHuman(w io.Writer, r *configureCheckJSON) error {
	if _, err := fmt.Fprintf(w, "ok: %t\n", r.OK); err != nil {
		return err
	}
	for _, issue := range r.Issues {
		if _, err := fmt.Fprintf(w, "  - %s\n", issue); err != nil {
			return err
		}
	}
	return nil
}

// runConfigureInteractive は対話モードで 4 つの X API トークンを stdin から読み、
// credentials.toml に保存する処理を行う (D-1 / D-2 / D-6 / D-13)。
//
// フロー:
//  1. credentials.toml 存在チェック → あれば上書き確認プロンプト ("[y/N]")
//     - "y" / "Y" のみ続行、それ以外は errConfigureCancelled で exit 1
//  2. 4 フィールドを順に prompt (TTY なら term.ReadPassword で echo オフ、非 TTY なら bufio)
//     - 各値は strings.TrimSpace、空ならば ErrInvalidArgument (exit 2)
//  3. config.SaveCredentials で perm 0600 / dir 0700 で保存
//  4. 完了メッセージを stdout に出力
func runConfigureInteractive(cmd *cobra.Command) error {
	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		return fmt.Errorf("resolve credentials.toml path: %w", err)
	}

	reader := bufio.NewReader(cmd.InOrStdin())

	// 既存ファイルの保護プロンプト。
	if _, statErr := os.Stat(credPath); statErr == nil {
		ok, perr := confirmOverwrite(cmd, reader, credPath)
		if perr != nil {
			return perr
		}
		if !ok {
			return errConfigureCancelled
		}
	}

	// 4 フィールドの読み込み。
	fields := []struct {
		name string
		dst  *string
	}{
		{"api_key", new(string)},
		{"api_secret", new(string)},
		{"access_token", new(string)},
		{"access_token_secret", new(string)},
	}
	for _, f := range fields {
		v, err := promptSecret(cmd, reader, fmt.Sprintf("X API %s: ", f.name))
		if err != nil {
			return err
		}
		v = strings.TrimSpace(v)
		if v == "" {
			return fmt.Errorf("%w: %s cannot be empty", ErrInvalidArgument, f.name)
		}
		*f.dst = v
	}

	creds := &config.Credentials{
		APIKey:            *fields[0].dst,
		APISecret:         *fields[1].dst,
		AccessToken:       *fields[2].dst,
		AccessTokenSecret: *fields[3].dst,
	}
	if err := config.SaveCredentials(credPath, creds); err != nil {
		return fmt.Errorf("save credentials.toml: %w", err)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "saved credentials to %s\n", credPath); err != nil {
		return err
	}
	return nil
}

// confirmOverwrite は credentials.toml が既存の場合の上書き確認プロンプトを表示し、
// ユーザー応答 (y / Y のみ true) を返す。
//
// 入力ソースは reader (cmd.InOrStdin() ベース)。プロンプト出力先は stderr
// (TTY の echo オフ状態でも見えるように)。EOF は 'n' 扱い (= 上書きしない)。
func confirmOverwrite(cmd *cobra.Command, reader *bufio.Reader, path string) (bool, error) {
	if _, err := fmt.Fprintf(cmd.ErrOrStderr(),
		"credentials.toml already exists at %s. Overwrite? [y/N]: ", path); err != nil {
		return false, err
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read overwrite confirmation: %w", err)
	}
	ans := strings.TrimSpace(line)
	return ans == "y" || ans == "Y", nil
}

// promptSecret は label を stderr に出してから 1 行入力を受け取る。
//
// TTY 判定 (D-13):
//   - cmd.InOrStdin() が *os.File で、かつ term.IsTerminal が真 → term.ReadPassword で echo オフ
//   - それ以外 (bytes.Buffer / strings.Reader / 非 TTY *os.File) → bufio.Reader.ReadString('\n')
//
// 返却値は改行を含む / 含まない両方ありうるため、呼び出し側で TrimSpace を必須とする。
//
// TTY パスでは reader は使わない (term.ReadPassword が fd を直接読むため)。
// 非 TTY パスでは reader を介して読む。テスト時は後者の経路を通る。
func promptSecret(cmd *cobra.Command, reader *bufio.Reader, label string) (string, error) {
	if _, err := fmt.Fprint(cmd.ErrOrStderr(), label); err != nil {
		return "", err
	}
	if f, ok := cmd.InOrStdin().(*os.File); ok {
		fd := int(f.Fd())
		if term.IsTerminal(fd) {
			b, err := term.ReadPassword(fd)
			if err != nil {
				return "", fmt.Errorf("read password: %w", err)
			}
			// term.ReadPassword は改行を消費するが echo されないため、UX のため改行を出す。
			if _, werr := fmt.Fprintln(cmd.ErrOrStderr()); werr != nil {
				return "", werr
			}
			return string(b), nil
		}
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read input: %w", err)
	}
	return line, nil
}
