# M27: examples/lambroll/README.md

## 目的

`examples/lambroll/` 配下の M26 ファイル (bootstrap / function.json / function_url.json / .env.example)
を使って、第三者が **README に書いてあるコマンドだけで Lambda Function URL に `x mcp` をデプロイ完走できる**
粒度の Step-by-step ドキュメントを作成する。

## スコープ

- ドキュメント (Markdown) のみ。Go コードや既存 JSON / shell に変更なし。
- 実デプロイは行わない。コマンドの dry-run / 構文確認のみで完了とする。

## 成果物

| パス | 用途 | 権限 |
|---|---|---|
| `examples/lambroll/README.md` | デプロイ手順書 (英日混在で運用想定。本体は日本語) | `0644` |

## 構成案

1. **タイトル + 概要**
   - `x mcp` を AWS Lambda Function URL + Lambda Web Adapter (LWA) で公開する手順。
   - 想定ユースケース: Claude Code Routines のコネクター MCP として毎朝呼び出される。
2. **アーキテクチャ図 (Mermaid)**
   - Routines → Function URL → Lambda (LWA + `x mcp`) → X API
   - idproxy セッション/トークンは DynamoDB (`x-mcp-idproxy`) に保存
   - シークレットは SSM Parameter Store (`/x-mcp/*`)
3. **前提条件**
   - AWS アカウント (権限あり) / AWS CLI (認証済)
   - lambroll (`brew install fujiwara/tap/lambroll`)
   - mise (任意 — `mise.toml` は本リポでは未配置のため `lambroll` を直接叩く)
   - X API v2 の OAuth 1.0a トークン 4 つ
   - OIDC プロバイダ (Google or Entra ID) で OAuth クライアント作成可能
4. **Step 1: IAM ロール作成**
   - 信頼ポリシー: `lambda.amazonaws.com`
   - 必要権限: `AWSLambdaBasicExecutionRole` + SSM `GetParameter` + DynamoDB CRUD (`x-mcp-idproxy`)
   - `aws iam create-role` + `aws iam attach-role-policy` + `aws iam put-role-policy` のコマンド一式
   - 生成された ARN を `.env` の `ROLE_ARN` に控える
5. **Step 2: DynamoDB テーブル作成**
   - テーブル名: `x-mcp-idproxy`
   - PartitionKey: `pk` (String)
   - TTL: `ttl` (idproxy 側でセッション期限を入れる)
   - BillingMode: PAY_PER_REQUEST (オンデマンド)
   - `aws dynamodb create-table` コマンド
6. **Step 3: SSM Parameter Store 投入**
   - 6 キー全部 (`apikey`/`none` 運用でもダミー必須) を `aws ssm put-parameter --type SecureString` で投入
   - `cookie_secret` は `openssl rand -hex 32` で生成
   - キー一覧テーブル (キー名 / 値の意味 / 例)
7. **Step 4: OIDC プロバイダ設定**
   - 4.1 Google Cloud Console: OAuth 2.0 Web Application Client 作成手順
   - 4.2 Microsoft Entra ID: App Registration 手順
   - Redirect URI: `<EXTERNAL_URL>/callback` (idproxy@v0.1.6 規約: prefix なし時はルート直下)
   - Issuer URL: Google `https://accounts.google.com` / Entra `https://login.microsoftonline.com/<tenant-id>/v2.0`
   - 複数 Issuer を併用する場合は `OIDC_ISSUER` / `OIDC_CLIENT_ID` をカンマ区切りで対応順に並べる
8. **Step 5: lambroll deploy**
   - `cp .env.example .env` → `.env` を編集
   - `set -a; . ./.env; set +a`
   - `lambroll deploy --function function.json --function-url function_url.json`
   - 出力された Function URL をメモ → `EXTERNAL_URL` を確定値に更新 → 再 deploy
   - OIDC プロバイダの Redirect URI も確定 URL に更新
9. **Step 6: 動作確認**
   - `curl https://<func-url>/healthz` → `ok`
   - ブラウザで Function URL → OIDC ログイン後に Streamable HTTP エンドポイント `/mcp` が応答することを確認
   - mcp-cli / claude.ai でツール呼び出しテスト (`get_user_me`)
10. **トラブルシュート FAQ**
    - 401 Unauthorized: OIDC 設定ミス / Cookie シークレット未投入 / Redirect URI 不一致
    - DynamoDB Throttle: オンデマンド推奨。`x-mcp-idproxy` の存在確認
    - Cookie ループ: `EXTERNAL_URL` が Function URL と一致していない場合
    - Cold start: arm64 + 256MB で p95 ~500ms。Provisioned Concurrency は推奨しない (コスト > 効用)
    - SSM 解決失敗: 6 キーすべて存在するか、Lambda 実行ロールに `ssm:GetParameter` があるか
11. **コスト見積もり (1 日 1 回 Routines 起動の想定)**
    - Lambda: ≒ $0.01 / 月 (256MB × 30 秒 × 30 回)
    - DynamoDB: ≒ $0.001 / 月 (PAY_PER_REQUEST、書き込みごく少量)
    - SSM: 標準パラメータ無料 / SecureString も無料 (使用 6 件)
    - X API: 公式 Free プランで賄える想定 (件数依存)
12. **クリーンアップ**
    - `lambroll delete --function function.json`
    - `aws dynamodb delete-table --table-name x-mcp-idproxy`
    - SSM パラメータ削除
    - IAM ロールデタッチ + 削除
13. **環境変数リファレンス**
    - 表形式で全環境変数 (必須 / 任意 / SSM 経由 / env 経由 / デフォルト値) を整理
    - logvalet README の形式を踏襲

## 設計判断

- **言語**: 本文は日本語。コマンド / コード片は英語。logvalet README に合わせる。
- **長さ**: 中量級 (200-300 行程度)。Step ごとに `bash` フェンスで実コマンドを示し、コピペで完走可能にする。
- **アーキ図**: Mermaid を採用 (GitHub レンダリング前提)。ASCII フォールバックは不要。
- **OIDC 例**: Google / Entra ID 両方を併記 (spec Open Questions 解決済み)。
- **mise**: M26 で `mise.toml` を `examples/lambroll/` 配下に置いていないため、deploy コマンドは `lambroll` を直接叩く構成にする。
- **Redirect URI**: idproxy@v0.1.6 のデフォルトプレフィックス (空) では `/callback` に着地する
  (本リポ §11 にも `EXTERNAL_URL` の説明あり)。
- **DynamoDB スキーマ**: idproxy の dynamodb store 実装 (M23) で実際に使用するスキーマを反映:
  - PartitionKey: `pk` (String) — 値は `session:<id>` / `authcode:<code>` 形式
  - 属性 `data` (String JSON), `ttl` (Number, Unix epoch 秒)
  - DynamoDB ネイティブ TTL 属性として `ttl` を有効化する。
- **コスト見積もり**: 概算で十分。厳密ではないことを注記。

## 検証

- 既存ファイルへの変更なし — Go テスト不要。
- Markdown lint (任意) で大きな構文崩れがないこと。
- 内部リンク (relative ファイル参照) が存在するファイル名を指していること。

## TDD ガイド

- README はコードでないため Red-Green-Refactor は適用しない。
- 代わりに「他者が README に従えばデプロイ完走できるか」を内部検証セルフレビューでチェック:
  - [ ] 全ての必須 SSM キーが手順に登場するか
  - [ ] IAM 信頼ポリシー / 権限が SSM + DynamoDB で必要なものを網羅しているか
  - [ ] Function URL 二段階デプロイ (仮 URL → 確定 URL 再デプロイ) の流れが明示されているか
  - [ ] Redirect URI の確定値更新フローが書かれているか
  - [ ] OIDC プロバイダ 2 種 (Google / Entra ID) の手順が両方記載されているか
  - [ ] FAQ が想定エラー (401 / DynamoDB / Cookie / cold start / SSM) を網羅しているか

## ハンドオフ (M28 へ)

- v0.3.0 タグ前の最後の Doc マイルストーン。M28 で `docs/x-api.md` + `docs/routine-prompt.md` を仕上げて
  `v0.3.0` をリリースする。
- README 末尾に「Claude Code Routines への組み込みは `docs/routine-prompt.md` を参照」と
  ポインタを残す (M28 で追記する想定)。
