# M28 詳細計画: docs/x-api.md + docs/routine-prompt.md + v0.3.0 リリース準備

> Layer 2: マイルストーン詳細計画。Roadmap は [./x-roadmap.md](./x-roadmap.md) を参照。

## Meta

| 項目 | 値 |
|---|---|
| マイルストーン | M28 (最終) |
| 親 Roadmap | `plans/x-roadmap.md` |
| 前マイルストーン | M27 (examples/lambroll/README.md, commit 1599f7f) |
| スコープ | docs/ 配下 2 ファイル新規 + CHANGELOG + README 英日 + roadmap 完了マーク |
| 制約 | Go コード変更なし (Markdown のみ)、タグ push は行わない (CI 起動回避) |
| ステータス | Planned |
| 作成日 | 2026-05-12 |

## ゴール

v0.3.0 リリース直前の **文書セット** を完成させる。

1. `docs/x-api.md` — X API v2 を扱う上での認証 / レート制限 / 料金 / エンドポイント / エラーの開発者向け参考メモ
2. `docs/routine-prompt.md` — Claude Code Routines に貼り付ける推奨プロンプト雛形 (本リポジトリの責務外だが Routines ユーザー向けに提供)
3. `CHANGELOG.md` — `[0.3.0] - 2026-05-12` セクション追加
4. `README.md` / `README.ja.md` — Status 表で v0.3.0 をリリース済みに昇格、Documentation セクション追加 (docs/ 配下リンク)、Roadmap セクションは "全 28 マイルストーン完了" にトーンダウン
5. `plans/x-roadmap.md` — 全マイルストーン完了マーク + Current Focus を「完了」に更新

タグ `v0.3.0` の push は本マイルストーンでは **行わない** (ユーザー手動実施、本計画末尾に手順記載)。

## 非ゴール (スコープ外)

- Go コードの変更 (テスト含む)
- 外部 API への実呼び出し (docs 内のコード例は静的、検証不要)
- Routine プロンプトの動作確認 (Routines は research preview、ユーザー手元で検証する責務)
- `v0.3.0` タグ push と GitHub Release 公開

## 成果物

### F1. `docs/x-api.md` (新規)

X API v2 を `x` CLI / MCP から扱う上で必要な技術メモを集約する。

#### 章立て

1. **概要** — X API v2 の位置付け、本リポジトリで使うサブセット
2. **アカウント取得と App 設定** — X Developer Portal でのアカウント取得 → Project / App 作成までの導線 (プラン比較表は書かない: pay-per-usage で階層は公式 docs 非公開)
3. **OAuth 1.0a User Context — 取得手順**
   - X Developer Portal → Project / App 作成
   - User authentication settings → OAuth 1.0a を ON、`Read` 権限のみで充分
   - Keys and tokens タブ:
     - **Consumer Keys** (`API Key` / `API Secret`) を Regenerate
     - **Authentication Tokens** → **Access Token and Secret** を Generate (同じ User context で)
   - 4 シークレットを `x configure` または環境変数で渡す
4. **エンドポイント (本リポで使用するもの)**
   - `GET /2/users/me` — 自分の `user_id` / `username` / `name` 取得
     - 主要パラメータ: `user.fields=username,name`
     - 戻り値: `data.id`, `data.username`, `data.name`
   - `GET /2/users/:id/liked_tweets` — 任意ユーザーの Liked Posts 取得
     - 主要パラメータ: `max_results` (5-100), `pagination_token`, `start_time` / `end_time` (RFC 3339 UTC), `tweet.fields`, `expansions`, `user.fields`
     - 戻り値: `data[]`, `includes`, `meta.next_token`, `meta.result_count`
5. **Rate Limit** (公式: [docs.x.com/x-api/fundamentals/rate-limits](https://docs.x.com/x-api/fundamentals/rate-limits) 執筆時点)
   - `GET /2/users/me`: **75 req / 15 min / Per User** (Per App context は提供なし、User context のみ)
   - `GET /2/users/:id/liked_tweets`: **75 req / 15 min / Per User** および **75 req / 15 min / Per App**
   - レスポンスヘッダ: `x-rate-limit-limit`, `x-rate-limit-remaining`, `x-rate-limit-reset` (Unix epoch sec)
   - `x` の挙動: `internal/xapi/client.go` で `remaining ≤ 2` のとき `reset` まで context-aware sleep (最大 15 分)
6. **料金 (Owned Reads)** (公式: [docs.x.com/x-api/fundamentals/rate-limits](https://docs.x.com/x-api/fundamentals/rate-limits) 執筆時点)
   - X API は **pay-per-usage (従量課金)**。「Free / Basic / Pro」のような明示的なプラン階層は公式 docs に記載されておらず、最新の単価は [Developer Console](https://developer.x.com/) で確認することが推奨されている
   - 「Owned Reads」: 自分が所有する Like / Tweet 等の取得は **1 リソース (Tweet) あたり $0.001** (1,000 件で $1)
   - 本リポは `--max-pages` (default 50, max_results=100/page) でハードリミット → 1 回の `--all` で最大 5,000 Posts ≈ $5
7. **エラーレスポンス**
   - `401` 認証失敗 (`xapi.ErrAuthentication` → exit code 3)
   - `403` 権限不足 (`xapi.ErrPermission` → exit code 4)
   - `404` 該当なし (`xapi.ErrNotFound` → exit code 5)
   - `429` レート超過 (`xapi.ErrRateLimit` → 自動 retry、最大 3 回、exp backoff)
   - `5xx` サーバーエラー (`xapi.APIError`、retry あり)
   - JSON 構造: `{ "errors": [{ "title": "...", "detail": "...", "type": "..." }], "title": "...", "status": 4xx }`
8. **参考リンク**
   - [X API v2 公式ドキュメント](https://docs.x.com/x-api)
   - [OAuth 1.0a User Context overview](https://docs.x.com/resources/fundamentals/authentication/oauth-1-0a/overview)
   - [GET /2/users/me](https://docs.x.com/x-api/users/users-lookup-me)
   - [GET /2/users/:id/liked_tweets](https://docs.x.com/x-api/posts/likes-lookup/users-id-liked-tweets)
   - [Rate Limits](https://docs.x.com/fundamentals/rate-limits)
   - [API Plans / Pricing](https://docs.x.com/x-api/getting-started/about-x-api)

### F2. `docs/routine-prompt.md` (新規)

Claude Code Routines 用の推奨プロンプト雛形。本リポジトリは MCP サーバー提供までで責務終わりだが、ユーザーの立ち上げ負荷を下げるために雛形を提供する。

#### 章立て

1. **このドキュメントの位置付け**
   - Claude Code Routines (research preview) で `x` MCP を呼び出すサンプルプロンプト
   - 想定スケジュール: 毎朝 JST 8:00 (cron `0 8 * * *` JST)
   - 「前日に Like した Post → 技術観点で要約 → logvalet (Backlog) HEP_ISSUES プロジェクトに課題化」
   - Routines は仕様変更があり得るので、コピペ後に動作確認すること
   - **MCP ツール名表記の注意**: 本ドキュメントは `logvalet_issue_list` / `logvalet_issue_create` / `x_get_user_me` 等の **正規ツール名** で記述する。Routines / MCP クライアントによってはコネクター名を prefix する場合 (例: `mcp__logvalet__logvalet_issue_list`) があるため、自分の環境で実際に `tools/list` の出力を確認し、必要なら prefix を付与すること
2. **前提条件 (Routines 側の設定)**
   - x MCP サーバーが Function URL でデプロイ済み (examples/lambroll/ 参照)
   - Routines のコネクター MCP に `x` (URL) と `logvalet` (URL) を登録済み
   - Routines の Environment Variables に以下を設定 (現状は Routines 仕様で UI 設定):
     - 無し (シークレットは MCP 側の Lambda 環境変数で完結)
3. **推奨プロンプト雛形 (コピペ用、コードブロック)**
   - System / 命令プロンプト本体 (Markdown)
   - ステップ 1: `get_user_me` で `user_id` 取得 (x MCP)
   - ステップ 2: `get_liked_tweets(user_id=..., yesterday_jst=true, all=true, max_pages=20)` で前日分一括取得 (x MCP)
   - ステップ 3: Claude 自身が以下の観点で判定 + グルーピング
     - 技術的に検証/実装/読了の価値があるか (純情報共有・宣伝・ミーム等は除外)
     - 同じトピックは 1 件に集約
   - ステップ 4: `logvalet_issue_list(projectIdOrKey="HEP_ISSUES", keyword=<タイトル候補>)` で重複チェック (logvalet MCP)
     - ヒット時はスキップ + 既存課題 URL のリストを最後に出力
   - ステップ 5: `logvalet_issue_create` で新規作成 (logvalet MCP)
     - `projectIdOrKey`: `HEP_ISSUES`
     - `summary`: `[X Like] <トピック>` (50 文字以内)
     - `description`: 元 Post URL / 投稿者 / 本文要約 / 検証アクション案 (Markdown)
     - `priorityId`: 通常 (3)
     - `issueTypeId`: タスク
4. **課題テンプレ (description 部分)**
   - Markdown 雛形:
     ```markdown
     ## 元 Post
     - URL: https://x.com/<screen_name>/status/<id>
     - 投稿者: @<screen_name> (<name>)
     - 投稿日時 (JST): YYYY-MM-DD HH:MM
     - Like 日時 (JST): YYYY-MM-DD HH:MM

     ## 要点 (3 行以内)
     - ...

     ## 検証アクション案
     - [ ] <試すべきコマンド・読むべき記事>
     - [ ] <必要なら社内で共有する範囲>

     ## 元 Post 本文
     > <quoted text>
     ```
5. **失敗モードと対処**
   - X API rate limit 429: Routines 側でリトライしない (`x` MCP 側で 1 回までは自動 retry 済み)。翌日に持ち越す
   - logvalet 重複作成: `find_issues` を必ず先に呼ぶ
   - Routines 仕様変更: 公式 [Claude Code Routines docs](https://docs.claude.com/) を参照して構文を更新
6. **注意事項**
   - Routines は research preview のため API / プロンプト構文が変わる可能性
   - X API トークンは Lambda 環境変数経由 (SSM Parameter Store) であり、Routines 側に保管しない
   - HEP_ISSUES プロジェクトは Heptagon 社内固有。他組織で使う場合は `projectIdOrKey` を読み替える

### F3. `CHANGELOG.md` 更新

**意図**: `[Unreleased]` セクションは現状空で、その下に **新規 `[0.3.0]` セクションを挿入** する (既存内容の繰り上げではない)。最下部のコンペアリンクも v0.3.0 を先頭に置き換える。

```markdown
## [Unreleased]

## [0.3.0] - 2026-05-12

AWS Lambda Function URL + Lambda Web Adapter での Remote MCP デプロイサンプルと、Claude Code Routines 連携のための文書セットを追加。本バージョンで「CLI → MCP → 公開配布」の 3 フェーズ計画 (28 マイルストーン) が完了する。

### Added

#### `examples/lambroll/` — AWS Lambda デプロイサンプル
- `function.json` (provided.al2023, arm64, LWA Layer, SSM SecureString 注入) (M26)
- `function_url.json` (AuthType=NONE、認証は idproxy 側に集約) (M26)
- `bootstrap` シェル (`exec ./x mcp --host 0.0.0.0 --port "${PORT:-8080}"`) (M26)
- `.env.example` (lambroll が参照する env と SSM 経由 env の全リファレンス) (M26)
- `README.md` (Step 1-6 デプロイ手順 + Mermaid 図 + IAM/DynamoDB/SSM/OIDC セットアップ + トラブルシュート FAQ + コスト見積もり + クリーンアップ) (M27)

#### `docs/` — 開発者向け補足ドキュメント
- `docs/x-api.md` — X API v2 OAuth 1.0a 認証手順 / エンドポイント / Rate Limit / 課金 / エラーレスポンスを集約 (M28)
- `docs/routine-prompt.md` — Claude Code Routines に貼り付ける推奨プロンプト雛形と Backlog 課題テンプレ (M28)

### Compatibility
- v0.1.0 / v0.2.0 の機能は完全後方互換
- 本バージョンは **追加のみ**、CLI/MCP の挙動変更なし
- Go コード変更なし (docs と examples のみ)

[Unreleased]: https://github.com/youyo/x/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/youyo/x/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/youyo/x/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/youyo/x/releases/tag/v0.1.0
```

(既存の [Unreleased] と [0.2.0] の間に新セクション挿入、最下部のリンクも書き換え)

### F4. `README.md` / `README.ja.md` 更新

#### 4.1. Status セクション

- 概要文の `v0.3.0 で対応予定` → `v0.3.0 でリリース` に変更
- 表で `v0.3.0 (planned)` → `v0.3.0 (this release)` に格上げ、`v0.2.0` は `released` に降格 (表記揃え)

#### 4.2. Documentation セクション (新規追加)

`## Roadmap` セクションを丸ごと削除し、**削除した位置に `## Documentation` セクションを置く** (F4.3 と一体作業):

```markdown
## Documentation

| Document | Purpose |
|---|---|
| [`docs/specs/x-spec.md`](docs/specs/x-spec.md) | Full product specification |
| [`docs/x-api.md`](docs/x-api.md) | X API v2 OAuth 1.0a + rate limit + pricing reference |
| [`docs/routine-prompt.md`](docs/routine-prompt.md) | Claude Code Routines prompt template (yesterday's Likes → Backlog issues) |
| [`examples/lambroll/README.md`](examples/lambroll/README.md) | AWS Lambda Function URL deployment guide |
| [`CHANGELOG.md`](CHANGELOG.md) | Release history |
| [`plans/x-roadmap.md`](plans/x-roadmap.md) | Milestone breakdown |
```

(日本語版は表ヘッダと説明文を日本語化)

#### 4.3. Roadmap セクション

**削除**: 全 28 マイルストーン完了状態のため `## Roadmap` セクションは不要。削除した跡地に F4.2 の Documentation セクションを置く。

#### 4.4. Release procedure セクション

末尾の `For v0.2.0, the tag has not been pushed yet ...` を以下に置換:

> No tags (v0.1.0 / v0.2.0 / v0.3.0) have been pushed yet from this repository; the procedure above documents the intent.

(日本語版も同様: 「v0.1.0 / v0.2.0 / v0.3.0 のいずれのタグも本リポジトリからはまだ push していない。上記手順はその意図を文書化したもの。」)

### F5a. `examples/lambroll/README.md` 微修正

末尾「関連ドキュメント」セクションの

```markdown
- Claude Code Routines 用プロンプト雛形 (M28 で追加予定): `../../docs/routine-prompt.md`
```

を

```markdown
- Claude Code Routines 用プロンプト雛形: [`../../docs/routine-prompt.md`](../../docs/routine-prompt.md)
```

に変更。同セクションの仕様書リンクの直下に X API メモへのリンクも追加:

```markdown
- X API v2 リファレンス: [`../../docs/x-api.md`](../../docs/x-api.md)
```

### F5. `plans/x-roadmap.md` 更新

- Meta: `ステータス` を `完了 (全 28 マイルストーン)`、`最終更新` を `2026-05-12 (M28 完了)` に
- Current Focus セクション全体を以下に置換:
  ```markdown
  ## Current Focus

  全 28 マイルストーン完了。v0.3.0 の文書セット (docs/x-api.md + docs/routine-prompt.md + CHANGELOG) を整備済み。
  残タスクは **ユーザー手動による `git push origin main` + `git tag v0.3.0` + `git push origin v0.3.0`** のみ。
  ```
- M16〜M28 の `[ ]` を `[x]` に書き換え、各マイルストーンに `✅ 完了 (commit: ...)` を付与
- commit hash は `git log --oneline -30` の結果から確定済み (実装時に再取得不要):

  | マイルストーン | commit hash | 件名 |
  |---|---|---|
  | M16 (authgate 基盤 + none) | `12efddb` | feat(authgate): authgate 基盤と none モード、transport の middleware フック・/healthz を追加 |
  | M17 (MCP get_user_me) | `a3a3934` | feat(mcp): get_user_me ツールを追加 (user_id リネーム対応) |
  | M18 (MCP get_liked_tweets) | `1620ef5` | feat(mcp): get_liked_tweets ツールを追加 (全パラメータ + ページネーション + JST 優先順位) |
  | M19 (authgate apikey) | `d42719c` | feat(authgate): API キー Bearer 認証ミドルウェアを追加 |
  | M20 (idproxy + memory) | `35be1f1` | feat(authgate): idproxy 基盤と memory store backend を追加 |
  | M21 (idproxy sqlite) | `7b50dd8` | feat(authgate): idproxy sqlite store backend を追加 |
  | M22 (idproxy redis) | `491bb30` | feat(authgate): idproxy redis store backend を追加 |
  | M23 (idproxy dynamodb) | `2ffa62c` | feat(authgate): idproxy dynamodb store backend を追加 (4 store backend 完成) |
  | M24 (CLI x mcp + E2E) | `01f2a97` | feat(cli): x mcp サブコマンドと 3 モード E2E テストを追加 (v0.2.0 機能完成) |
  | M25 (v0.2.0 release prep) | `939f132` (+ `837e2eb` LOG_LEVEL 追記) | chore(release): v0.2.0 リリース準備 (README MCP セクション + CHANGELOG) |
  | M26 (lambroll files) | `82edb21` | feat(examples): lambroll でのデプロイサンプルを追加 |
  | M27 (lambroll README) | `1599f7f` | docs(examples): lambroll デプロイ手順の README を追加 |
  | M28 (docs + v0.3.0) | 本コミットで埋める | chore(release): v0.3.0 リリース準備 |

- M14 の `commit: TBD` は **`fbb2df8` (chore(ci): v0.1.0 リリース用 GitHub Actions workflow を追加)** に確定 (workflow 配置完了部分)
- M28 セクションの bullet 表記は M14 (v0.1.0 タグ) と整合させて以下に分割:
  ```markdown
  - [x] `docs/x-api.md` (OAuth 1.0a 認証メモ / rate limit / 課金)
  - [x] `docs/routine-prompt.md` (Claude Code Routines 用プロンプト雛形)
  - [x] `CHANGELOG.md` 更新
  - [ ] `v0.3.0` タグ ← **GitHub remote 設定後にユーザー手動で実施 (本マイルストーンでは扱わない)**
  ```

## 実装手順

1. **F1**: `/Users/youyo/src/github.com/youyo/x/docs/x-api.md` を新規作成
2. **F2**: `/Users/youyo/src/github.com/youyo/x/docs/routine-prompt.md` を新規作成
3. **F3**: `CHANGELOG.md` を Edit (Unreleased の下に [0.3.0] 追加、最下部リンク更新)
4. **F4a**: `README.md` (英) を Edit (Status / Documentation / Roadmap / Release procedure)
5. **F4b**: `README.ja.md` (日) を同様に Edit
6. **F5a**: `examples/lambroll/README.md` 末尾「関連ドキュメント」を Edit (M28 で追加予定→確定リンクに昇格、docs/x-api.md も追記)
7. **F5**: `plans/x-roadmap.md` を Edit (Meta / Current Focus / M16-M28 完了マーク)
8. **検証**:
   - `go test -race -count=1 ./...` 全 pass (Go コード変更なしだが念のため)
   - `goreleaser check` pass
   - `git grep` で docs/ 配下リンクの整合性確認 (相対パス)
8. **コミット**:
   - 単一コミット: `chore(release): v0.3.0 リリース準備 (docs/x-api.md + docs/routine-prompt.md + CHANGELOG)`
   - フッター: `Plan: plans/x-m28-docs-v030.md`

## TDD 観点

- Markdown のみ変更、テストは不要
- 動作確認は: `go test ./...` 全 pass を担保 (regression なし) + `goreleaser check` の YAML validity

## 検証チェックリスト

- [ ] `docs/x-api.md` が存在し、X Developer Portal の手順が正確
- [ ] `docs/routine-prompt.md` のプロンプト雛形がコピペで使える状態 (構文エラーなし)
- [ ] `CHANGELOG.md` に `[0.3.0] - 2026-05-12` セクションあり、コンペアリンク 3 本が正しい
- [ ] `README.md` / `README.ja.md` の Status 表が v0.3.0 リリース済みに更新済み
- [ ] `README.md` / `README.ja.md` に Documentation セクションがあり、docs/ 配下 2 ファイルへのリンクあり
- [ ] `plans/x-roadmap.md` の M16-M28 が全て `[x] 完了` マーク
- [ ] `examples/lambroll/README.md` の「関連ドキュメント」が `(M28 で追加予定)` 表記を含まず、`docs/routine-prompt.md` と `docs/x-api.md` 両方を相対リンクで参照
- [ ] `go test -race -count=1 ./...` 全 pass
- [ ] `goreleaser check` pass
- [ ] git commit が `chore(release): v0.3.0 ...` 形式、Plan フッター付与

## タグ push 手順 (ユーザー手動、本マイルストーン外)

```bash
# 0. GitHub remote 設定済み + secrets 登録済み前提
# 1. 最新 main を push
git checkout main
git push origin main

# 2. v0.3.0 タグ作成
git tag -a v0.3.0 -m "v0.3.0: lambroll サンプル + docs (Routines 連携準備)"
git push origin v0.3.0

# 3. release ワークフローを監視
gh run watch

# 4. 完了確認
gh release view v0.3.0
brew upgrade x  # Homebrew tap が自動更新されているはず
```

## Risks

| リスク | 影響 | 対策 |
|---|---|---|
| docs 内の X API URL リンク切れ | 低 | `docs.x.com` が現行公式ドメイン (developer.x.com から移行済み)。固定リンクのみ採用 |
| Routine プロンプトの仕様変更 | 中 | プロンプト冒頭に「research preview のため要動作確認」を明記 |
| CHANGELOG の compare リンク URL ミスマッチ | 低 | v0.1.0 / v0.2.0 と同じ書式で生成、git tag push 後に GitHub 上で自動解決 |

## 完了の定義

1. 7 ファイル (新規 2 + 編集 5) の編集完了
   - 新規: `docs/x-api.md`, `docs/routine-prompt.md`
   - 編集: `CHANGELOG.md`, `README.md`, `README.ja.md`, `examples/lambroll/README.md`, `plans/x-roadmap.md`, (`plans/x-m28-docs-v030.md` 本ファイル)
2. `go test -race -count=1 ./...` pass
3. `goreleaser check` pass
4. 単一コミット完了 (`Plan: plans/x-m28-docs-v030.md` フッター)
5. **タグ push は行わない** (ユーザー手動)

## 参考

- スペック: `docs/specs/x-spec.md` §14 (Surrounding Files)
- 前マイルストーン: `plans/x-m27-lambroll-readme.md` (commit 1599f7f)
- v0.2.0 リリース: `plans/x-m25-release-v020.md` (commit 939f132)
