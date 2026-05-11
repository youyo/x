# M26: examples/lambroll/ ファイル一式

## 目的

`lambroll deploy` のみで AWS Lambda Function URL に `x mcp` を本番デプロイできる
サンプル一式を `examples/lambroll/` 配下に配置する。README.md は M27 で別途作成する。

## スコープ

- ファイル作成のみ。Go コード変更なし。
- 実 AWS リソース作成は別作業 (本マイルストーンは dry-run / 構文検証まで)。

## 成果物 (4 ファイル)

| パス | 用途 | 権限 |
|---|---|---|
| `examples/lambroll/bootstrap` | LWA が起動する shell entrypoint | `0755` |
| `examples/lambroll/function.json` | Lambda function 定義 (lambroll) | `0644` |
| `examples/lambroll/function_url.json` | Function URL 設定 (lambroll) | `0644` |
| `examples/lambroll/.env.example` | 環境変数テンプレ (SSM 参照含む) | `0644` |

## 1. bootstrap (shell script)

```sh
#!/bin/sh
set -eu
exec ./x mcp --host 0.0.0.0 --port "${PORT:-8080}"
```

- LWA Layer が PORT を渡してくる前提
- `0.0.0.0` バインドで LWA からの forward を受ける
- `set -eu` で失敗時即終了
- パーミッション `0755` (実行可)

## 2. function.json

lambroll の Go template (`{{ ssm '...' }}` / `{{ must_env '...' }}` / `{{ env '...' '...' }}`)
を活用し、シークレットは SSM Parameter Store 参照テンプレで埋める。

```json
{
  "FunctionName": "x-mcp",
  "Description": "x MCP server on Lambda Function URL",
  "Runtime": "provided.al2023",
  "Handler": "bootstrap",
  "Architectures": ["arm64"],
  "MemorySize": 256,
  "Timeout": 30,
  "Role": "{{ must_env `ROLE_ARN` }}",
  "Layers": [
    "arn:aws:lambda:{{ must_env `AWS_REGION` }}:753240598075:layer:LambdaAdapterLayerArm64:27"
  ],
  "Environment": {
    "Variables": {
      "X_API_KEY":              "{{ ssm `/x-mcp/x_api_key` }}",
      "X_API_SECRET":           "{{ ssm `/x-mcp/x_api_secret` }}",
      "X_ACCESS_TOKEN":         "{{ ssm `/x-mcp/x_access_token` }}",
      "X_ACCESS_TOKEN_SECRET":  "{{ ssm `/x-mcp/x_access_token_secret` }}",

      "X_MCP_AUTH":             "{{ env `X_MCP_AUTH` `idproxy` }}",
      "X_MCP_API_KEY":          "{{ env `X_MCP_API_KEY` `` }}",

      "OIDC_ISSUER":            "{{ env `OIDC_ISSUER` `` }}",
      "OIDC_CLIENT_ID":         "{{ env `OIDC_CLIENT_ID` `` }}",
      "OIDC_CLIENT_SECRET":     "{{ ssm `/x-mcp/oidc_client_secret` }}",
      "COOKIE_SECRET":          "{{ ssm `/x-mcp/cookie_secret` }}",
      "EXTERNAL_URL":           "{{ must_env `EXTERNAL_URL` }}",

      "STORE_BACKEND":          "{{ env `STORE_BACKEND` `dynamodb` }}",
      "DYNAMODB_TABLE_NAME":    "{{ env `DYNAMODB_TABLE_NAME` `x-mcp-idproxy` }}",
      "AWS_REGION":             "{{ must_env `AWS_REGION` }}",

      "LOG_LEVEL":              "{{ env `LOG_LEVEL` `info` }}"
    }
  }
}
```

### 設計判断

- **Runtime / Architecture**: `provided.al2023` + `arm64`。GoReleaser で同アーキバイナリを出力済み。
- **LWA Layer**: arm64 v27 (`arn:aws:lambda:<region>:753240598075:layer:LambdaAdapterLayerArm64:27`)。
  - region は `{{ must_env `AWS_REGION` }}` で展開。
- **MemorySize 256 / Timeout 30**:
  - X API 呼び出し + idproxy セッションチェックで CPU heavy ではないため 256MB で十分
  - MCP の Streamable HTTP は短時間 RPC なので 30 秒で打ち切る
  - logvalet (Timeout 900) より短いのは MCP の性質 (ストリーミング応答ではない)
- **Role**: `{{ must_env `ROLE_ARN` }}` で外部注入 (IAM 作成は README で案内)
- **シークレット (SSM 参照)**: `X_API_*` × 4 / `OIDC_CLIENT_SECRET` / `COOKIE_SECRET`
- **非シークレット可変値 (env 参照)**: `X_MCP_AUTH` / `OIDC_ISSUER` / `OIDC_CLIENT_ID` /
  `STORE_BACKEND` / `DYNAMODB_TABLE_NAME` / `LOG_LEVEL` (デフォルト付き)
- **必須 env**: `EXTERNAL_URL` / `AWS_REGION` / `ROLE_ARN` (must_env)
- **AuthGate デフォルト**: `idproxy` (lambroll 公開デプロイは認証必須が想定運用)
- **STORE_BACKEND デフォルト**: `dynamodb` (Lambda マルチコンテナ前提)

### バインド設定

- `X_MCP_HOST` / `X_MCP_PORT` / `X_MCP_PATH` は env に入れない。
  - bootstrap で `--host 0.0.0.0 --port "${PORT:-8080}"` を明示的に渡す方式が LWA 標準。
  - PATH (`/mcp`) はデフォルト値で運用。

## 3. function_url.json

```json
{
  "Config": {
    "AuthType": "NONE",
    "InvokeMode": "BUFFERED"
  }
}
```

- **AuthType NONE**: idproxy 層 (X 内部) が OIDC 認証を処理するため、Function URL では
  生のパブリック露出にする。
- **CORS なし**: Routines は同一 origin で叩かない前提 (spec §10 注記)。
- **InvokeMode BUFFERED**: Streamable HTTP は応答が短い RPC のため。
  RESPONSE_STREAM は不要 (mcp-go の SSE は idproxy gate 配下では使わない)。

## 4. .env.example

`lambroll deploy` 実行時に shell から export する変数のテンプレ。
**実値はコミットしない**。`.env.example` のみコミット可 (.gitignore で `!.env.example` 許可済)。

```sh
# ========================================
# lambroll deploy 時に必要な環境変数
# ========================================

# --- 必須 (must_env) ---

# Lambda 実行ロール ARN
# 信頼ポリシー: lambda.amazonaws.com
# 必要権限: AWSLambdaBasicExecutionRole + SSM GetParameter + DynamoDB CRUD (idproxy テーブル)
ROLE_ARN=arn:aws:iam::123456789012:role/x-mcp-lambda-role

# Lambda リージョン (LWA Layer 解決にも利用)
AWS_REGION=ap-northeast-1

# idproxy の外部 URL (Function URL or 独自ドメイン)
# 末尾スラッシュなし
EXTERNAL_URL=https://x-mcp.example.com

# --- 認証モード (省略時 idproxy) ---

# none|apikey|idproxy
# X_MCP_AUTH=idproxy

# apikey モード時のみ必要 (BUT lambroll 公開デプロイでは idproxy 推奨)
# X_MCP_API_KEY=

# --- idproxy 設定 (X_MCP_AUTH=idproxy 時) ---

# OIDC Issuer (カンマ区切りで複数可)
# 例 (Google):   https://accounts.google.com
# 例 (Entra ID): https://login.microsoftonline.com/<tenant-id>/v2.0
OIDC_ISSUER=https://accounts.google.com

# 上の Issuer に対応する Client ID (カンマ区切りで複数可)
OIDC_CLIENT_ID=

# --- ストレージバックエンド (省略時 dynamodb) ---

# memory|sqlite|redis|dynamodb (Lambda は dynamodb 推奨)
# STORE_BACKEND=dynamodb

# DynamoDB テーブル名 (idproxy セッション/トークン用)
# 事前作成が必要 (パーティションキー: pk, ソートキー: sk, 詳細は README)
# DYNAMODB_TABLE_NAME=x-mcp-idproxy

# --- ログ ---

# debug|info|warn|error
# LOG_LEVEL=info

# ========================================
# SSM Parameter Store に投入する値 (参考)
# ========================================
# 以下はコードから SSM 経由で参照されるため、aws ssm put-parameter で事前投入する
#
# /x-mcp/x_api_key              (SecureString)
# /x-mcp/x_api_secret           (SecureString)
# /x-mcp/x_access_token         (SecureString)
# /x-mcp/x_access_token_secret  (SecureString)
# /x-mcp/oidc_client_secret     (SecureString)
# /x-mcp/cookie_secret          (SecureString, openssl rand -hex 32 で生成)
```

## 検証手順

1. `jq . examples/lambroll/function.json` で JSON 構文 OK
2. `jq . examples/lambroll/function_url.json` で JSON 構文 OK
3. `ls -l examples/lambroll/bootstrap` で `-rwxr-xr-x` (0755) 確認
4. `sh -n examples/lambroll/bootstrap` で shell 構文 OK
5. `.env.example` は env 形式 (KEY=VALUE もしくは `# KEY=VALUE`) のみで構成

## 関連

- spec: docs/specs/x-spec.md §10 (シークレット) / §11 (環境変数) / §13 (リリース戦略)
- 参考: /Users/youyo/src/github.com/youyo/logvalet/examples/lambroll/
- 次マイルストーン: M27 (examples/lambroll/README.md)

## M27 への引き継ぎ

- ファイル一覧 (上記 4 ファイル)
- 必要な事前準備:
  - IAM ロール (Lambda 実行 + SSM 読み取り + DynamoDB CRUD)
  - SSM Parameter Store キー命名: `/x-mcp/<lowercase_env_name>` (SecureString)
    - **全 6 キーの投入が deploy 前提**: `function.json` は SSM 参照を無条件で展開するため、
      `X_MCP_AUTH=apikey` や `none` 運用時も `oidc_client_secret` / `cookie_secret` を
      ダミー値 (例: `unused`) で投入しないと `lambroll deploy` が失敗する。
  - DynamoDB テーブル `x-mcp-idproxy` の事前作成
  - OIDC プロバイダ (Google / Entra ID) の Client ID / Secret 発行
- デプロイ手順前提:
  1. `cp .env.example .env` → 実値編集
  2. `aws ssm put-parameter` で SecureString を投入
  3. `lambroll deploy --function function.json --function-url function_url.json`
