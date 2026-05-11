package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/youyo/x/internal/config"
)

// TestConfigure_PrintPaths_JSON は `x configure --print-paths` が
// {config, credentials, data_dir} の JSON を stdout に書き出すことを検証する。
func TestConfigure_PrintPaths_JSON(t *testing.T) {
	isolateXDG(t)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"configure", "--print-paths"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var got struct {
		Config      string `json:"config"`
		Credentials string `json:"credentials"`
		DataDir     string `json:"data_dir"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", buf.String(), err)
	}
	if !strings.HasSuffix(got.Config, filepath.Join("x", "config.toml")) {
		t.Errorf("config = %q, want suffix x/config.toml", got.Config)
	}
	if !strings.HasSuffix(got.Credentials, filepath.Join("x", "credentials.toml")) {
		t.Errorf("credentials = %q, want suffix x/credentials.toml", got.Credentials)
	}
	if !strings.HasSuffix(got.DataDir, "x") {
		t.Errorf("data_dir = %q, want suffix x", got.DataDir)
	}
}

// TestConfigure_PrintPaths_Human は --no-json で human フォーマット出力されることを検証する。
func TestConfigure_PrintPaths_Human(t *testing.T) {
	isolateXDG(t)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"configure", "--print-paths", "--no-json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, key := range []string{"config=", "credentials=", "data_dir="} {
		if !strings.Contains(out, key) {
			t.Errorf("output missing key %q: %q", key, out)
		}
	}
}

// TestConfigure_Check_AllGood は credentials.toml が 0600 + 4 フィールド完備 +
// config.toml に secret 混入なし のときに ok=true が返ることを検証する。
func TestConfigure_Check_AllGood(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm 検査は POSIX 専用 (config.CheckPermissions が Windows では nil 返却)")
	}
	isolateXDG(t)

	// credentials.toml を 0600 で配置。
	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		t.Fatalf("DefaultCredentialsPath: %v", err)
	}
	if err := config.SaveCredentials(credPath, &config.Credentials{
		APIKey:            "k",
		APISecret:         "s",
		AccessToken:       "t",
		AccessTokenSecret: "ts",
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"configure", "--check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var got struct {
		OK     bool     `json:"ok"`
		Issues []string `json:"issues"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", buf.String(), err)
	}
	if !got.OK {
		t.Errorf("ok = false, want true (issues=%v)", got.Issues)
	}
	if len(got.Issues) != 0 {
		t.Errorf("issues = %v, want empty", got.Issues)
	}
}

// TestConfigure_Check_CredentialsMissing は credentials.toml 不在で ok=false が返ることを検証する。
func TestConfigure_Check_CredentialsMissing(t *testing.T) {
	isolateXDG(t)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"configure", "--check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var got struct {
		OK     bool     `json:"ok"`
		Issues []string `json:"issues"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", buf.String(), err)
	}
	if got.OK {
		t.Errorf("ok = true, want false")
	}
	if len(got.Issues) == 0 {
		t.Fatalf("issues empty, want at least one")
	}
	if !strings.Contains(got.Issues[0], "credentials.toml") {
		t.Errorf("issue[0] = %q, want mention of credentials.toml", got.Issues[0])
	}
}

// TestConfigure_Check_PermissionsTooOpen は 0644 の credentials.toml で ok=false が返ることを検証する。
func TestConfigure_Check_PermissionsTooOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm 検査は POSIX 専用")
	}
	isolateXDG(t)

	// credentials.toml を 0600 で配置してから 0644 に変更。
	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		t.Fatalf("DefaultCredentialsPath: %v", err)
	}
	if err := config.SaveCredentials(credPath, &config.Credentials{
		APIKey:            "k",
		APISecret:         "s",
		AccessToken:       "t",
		AccessTokenSecret: "ts",
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}
	if err := os.Chmod(credPath, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"configure", "--check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var got struct {
		OK     bool     `json:"ok"`
		Issues []string `json:"issues"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", buf.String(), err)
	}
	if got.OK {
		t.Errorf("ok = true, want false")
	}
	// 既存 ErrPermissionsTooOpen は日本語メッセージ + "mode=644" を含む。
	// 日本語 / 英語両方の表現にロバストになるよう "mode=" マーカーで検査。
	if !hasIssueContaining(got.Issues, "mode=") && !hasIssueContaining(got.Issues, "perm") {
		t.Errorf("issues = %v, want at least one mentioning mode= or perm", got.Issues)
	}
}

// TestConfigure_Check_MissingField は api_secret が空の credentials.toml で ok=false が返ることを検証する。
func TestConfigure_Check_MissingField(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm 検査は POSIX 専用")
	}
	isolateXDG(t)

	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		t.Fatalf("DefaultCredentialsPath: %v", err)
	}
	if err := config.SaveCredentials(credPath, &config.Credentials{
		APIKey: "k",
		// APISecret 欠落
		AccessToken:       "t",
		AccessTokenSecret: "ts",
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"configure", "--check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var got struct {
		OK     bool     `json:"ok"`
		Issues []string `json:"issues"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", buf.String(), err)
	}
	if got.OK {
		t.Errorf("ok = true, want false")
	}
	if !hasIssueContaining(got.Issues, "api_secret") {
		t.Errorf("issues = %v, want at least one mentioning api_secret", got.Issues)
	}
}

// TestConfigure_Check_SecretInConfig は config.toml に [xapi] api_key=... が書かれた場合
// ok=false が返ることを検証する。
func TestConfigure_Check_SecretInConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm 検査は POSIX 専用")
	}
	isolateXDG(t)

	// 健全な credentials.toml を置く (それ以外の項目を pass させる)。
	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		t.Fatalf("DefaultCredentialsPath: %v", err)
	}
	if err := config.SaveCredentials(credPath, &config.Credentials{
		APIKey: "k", APISecret: "s", AccessToken: "t", AccessTokenSecret: "ts",
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	// 不正な config.toml を書く。
	cfgPath, err := config.DefaultCLIConfigPath()
	if err != nil {
		t.Fatalf("DefaultCLIConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte("[xapi]\napi_key = \"oops\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"configure", "--check"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var got struct {
		OK     bool     `json:"ok"`
		Issues []string `json:"issues"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", buf.String(), err)
	}
	if got.OK {
		t.Errorf("ok = true, want false")
	}
	if !hasIssueContaining(got.Issues, "secret") && !hasIssueContaining(got.Issues, "xapi") {
		t.Errorf("issues = %v, want at least one mentioning secret/xapi", got.Issues)
	}
}

// TestConfigure_Check_Human は --no-json 出力が ok:true/false で始まることを検証する。
func TestConfigure_Check_Human(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm 検査は POSIX 専用")
	}
	isolateXDG(t)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"configure", "--check", "--no-json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "ok: ") {
		t.Errorf("human output should start with 'ok: ', got %q", out)
	}
}

// TestConfigure_Interactive_Success は対話モードに stdin から 4 行流し込み、
// credentials.toml が perm 0600 で作成されることを検証する。
func TestConfigure_Interactive_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm 検査は POSIX 専用")
	}
	isolateXDG(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	// stdin に 4 行 (api_key / api_secret / access_token / access_token_secret) を流す。
	cmd.SetIn(strings.NewReader("kk\nss\ntt\nts\n"))
	cmd.SetArgs([]string{"configure"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		t.Fatalf("DefaultCredentialsPath: %v", err)
	}
	info, err := os.Stat(credPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("credentials.toml perm = %o, want 0600 (group/other unset)", info.Mode().Perm())
	}

	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds.APIKey != "kk" || creds.APISecret != "ss" ||
		creds.AccessToken != "tt" || creds.AccessTokenSecret != "ts" {
		t.Errorf("creds = %+v, want {kk,ss,tt,ts}", creds)
	}
}

// TestConfigure_Interactive_EmptyField は空行を含む入力で ErrInvalidArgument
// (exit 2 写像) が返ることを検証する。
func TestConfigure_Interactive_EmptyField(t *testing.T) {
	isolateXDG(t)

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	// 2 行目を空にする。
	cmd.SetIn(strings.NewReader("kk\n\ntt\nts\n"))
	cmd.SetArgs([]string{"configure"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute err = nil, want ErrInvalidArgument")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want errors.Is(ErrInvalidArgument)", err)
	}
}

// TestConfigure_Interactive_Overwrite_Yes は既存ファイルがある状態で
// "y" → 4 行を流し込み、上書き保存が成功することを検証する。
func TestConfigure_Interactive_Overwrite_Yes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm 検査は POSIX 専用")
	}
	isolateXDG(t)

	// 既存 credentials.toml を配置。
	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		t.Fatalf("DefaultCredentialsPath: %v", err)
	}
	if err := config.SaveCredentials(credPath, &config.Credentials{
		APIKey: "old", APISecret: "old", AccessToken: "old", AccessTokenSecret: "old",
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	// "y" で上書き承諾 → 4 行入力。
	cmd.SetIn(strings.NewReader("y\nnew\nnew\nnew\nnew\n"))
	cmd.SetArgs([]string{"configure"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds.APIKey != "new" {
		t.Errorf("APIKey = %q, want new (overwritten)", creds.APIKey)
	}
}

// TestConfigure_Interactive_Overwrite_No は既存ファイルがある状態で
// "n" を返した場合に cancel エラー (上書きしない) になることを検証する。
func TestConfigure_Interactive_Overwrite_No(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm 検査は POSIX 専用")
	}
	isolateXDG(t)

	credPath, err := config.DefaultCredentialsPath()
	if err != nil {
		t.Fatalf("DefaultCredentialsPath: %v", err)
	}
	if err := config.SaveCredentials(credPath, &config.Credentials{
		APIKey: "old", APISecret: "old", AccessToken: "old", AccessTokenSecret: "old",
	}); err != nil {
		t.Fatalf("SaveCredentials: %v", err)
	}

	cmd := NewRootCmd()
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errBuf)
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetArgs([]string{"configure"})

	err = cmd.Execute()
	if err == nil {
		t.Fatal("Execute err = nil, want cancel error")
	}

	// 既存ファイルは変更されていないこと。
	creds, lerr := config.LoadCredentials(credPath)
	if lerr != nil {
		t.Fatalf("LoadCredentials: %v", lerr)
	}
	if creds.APIKey != "old" {
		t.Errorf("APIKey = %q, want old (not overwritten)", creds.APIKey)
	}
}

// hasIssueContaining は issues の中に substr を含む要素が存在するかを返す。
func hasIssueContaining(issues []string, substr string) bool {
	for _, s := range issues {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// TestLikedList_ConfigToml_Overrides は config.toml [liked] の値が
// `x liked list` のフラグデフォルトに反映されることを検証する (M12 連携)。
func TestLikedList_ConfigToml_Overrides(t *testing.T) {
	// XDG を隔離した上で config.toml を書き込む。
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("X_API_KEY", "env-key")
	t.Setenv("X_API_SECRET", "env-secret")
	t.Setenv("X_ACCESS_TOKEN", "env-token")
	t.Setenv("X_ACCESS_TOKEN_SECRET", "env-token-secret")

	cfgPath, err := config.DefaultCLIConfigPath()
	if err != nil {
		t.Fatalf("DefaultCLIConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	tomlBody := `[liked]
default_tweet_fields = "id,text"
default_expansions   = "author_id"
default_user_fields  = "username"
default_max_pages    = 7
`
	if err := os.WriteFile(cfgPath, []byte(tomlBody), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	srv, state := newLikedTestServer(t, nil)
	stubLikedClientFactory(t, srv.URL)

	cmd := NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"liked", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	_, rawQs := state.snapshot()
	// liked_tweets エンドポイントへのクエリを探す。
	found := ""
	for _, q := range rawQs {
		if strings.Contains(q, "tweet.fields") {
			found = q
			break
		}
	}
	if found == "" {
		t.Fatalf("no query contained tweet.fields, got: %v", rawQs)
	}
	if !strings.Contains(found, "tweet.fields=id%2Ctext") &&
		!strings.Contains(found, "tweet.fields=id,text") {
		t.Errorf("query %q does not contain tweet.fields=id,text (expected from config.toml)", found)
	}
	if !strings.Contains(found, "user.fields=username") {
		t.Errorf("query %q does not contain user.fields=username (expected from config.toml)", found)
	}
}
