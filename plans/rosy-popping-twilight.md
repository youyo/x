# プラン: Node.js 24 対応 + x CLI スキル更新 + M37/M38 プラン作成

## Context

v0.4.0 リリース後の CI で Node.js 20 Actions が 2026-06-02 に強制 Node.js 24 化されるという警告が出た。同時に M29-M36 で追加した9コマンド群 (tweet/timeline/user/list/space/trends/dm) が `skills/x/SKILL.md` に未反映。これら2件を処理し、それぞれの計画ファイルも plans/ に追加する。

---

## タスク 1: Node.js 24 対応 — `.github/workflows/release.yml`

### 変更対象 (4 Actions)

| 現在 | 新バージョン | 新 SHA |
|------|------------|--------|
| `docker/setup-qemu-action@29109295...` (v3.6.0) | **v4.0.0** | `ce360397dd3f832beb865e1373c09c0e9f86d70a` |
| `docker/setup-buildx-action@e468171a...` (v3.11.1) | **v4.0.0** | `4d04d5d9486b7bd6fa91e7baf45bbb4f8b9deedd` |
| `docker/login-action@184bdaa0...` (v3.5.0) | **v4.1.0** | `4907a6ddec9925e35a0a9e82d7399ccc52663121` |
| `tibdex/github-app-token@3beb63f4...` (v2.1.0) | **actions/create-github-app-token v2** | `fee1f7d63c2ff003460e3d139729b119787bc349` |

`tibdex/github-app-token` は最新 v2.1.0 でも Node.js 20 ベースのため、GitHub 公式の `actions/create-github-app-token` に差し替える。

### API の差分 (tibdex → actions/create-github-app-token)

```yaml
# 変更前
uses: tibdex/github-app-token@3beb63f4bd073e61482598c45c71c1019b59b73a # v2.1.0
with:
  app_id: ${{ secrets.APP_ID }}
  private_key: ${{ secrets.APP_PRIVATE_KEY }}

# 変更後
uses: actions/create-github-app-token@fee1f7d63c2ff003460e3d139729b119787bc349 # v2
with:
  app-id: ${{ secrets.APP_ID }}         # アンダースコア → ハイフン
  private-key: ${{ secrets.APP_PRIVATE_KEY }}  # アンダースコア → ハイフン
```

`steps.app-token.outputs.token` は変わらず同じ参照で使用可能。

---

## タスク 2: `skills/x/SKILL.md` 更新

既存ファイル: `skills/x/SKILL.md` — M28 以前の5コマンドのみ記載。M29-M36 で追加した以下9コマンド群を追記する。

### Subcommands テーブル追記内容

| 追加行 | 説明 |
|--------|------|
| `tweet get / liking-users / retweeted-by / quote-tweets` | Tweet lookup + social signals (URL→ID 自動変換) |
| `tweet search` | 過去 7 日のキーワード検索 (Basic tier 以上) |
| `tweet thread` | conversation_id からスレッド全取得 |
| `timeline tweets / mentions / home` | ユーザータイムライン3種 |
| `user get / search / following / followers / blocking / muting` | ユーザー lookup・グラフ |
| `list get / tweets / members / owned / followed / memberships / pinned` | リスト操作 |
| `space get / by-ids / search / by-creator / tweets` | アクティブ Space (live のみ) |
| `trends get / personal` | WOEID / パーソナライズトレンド |
| `dm list / get / conversation / with` | DM Read (Pro 推奨) |

---

## タスク 3: plans/ への M37/M38 プランファイル追加

### 作成ファイル

1. `plans/x-m37-nodejs24-actions.md` — Node.js 24 対応計画
2. `plans/x-m38-skill-update.md` — skills/x/SKILL.md 更新計画

### `plans/x-roadmap.md` への追記

- マイルストーン総数: 36 → 38
- Phase II セクション (M37 / M38) を Progress 末尾に追加
- Changelog に追記

---

## 変更対象ファイル一覧

| ファイル | 変更種別 |
|---------|---------|
| `.github/workflows/release.yml` | 4 Action のバージョン更新 |
| `skills/x/SKILL.md` | Subcommands テーブル + Quick Reference 追記 |
| `plans/x-m37-nodejs24-actions.md` | 新規作成 |
| `plans/x-m38-skill-update.md` | 新規作成 |
| `plans/x-roadmap.md` | Meta / Progress / Changelog 更新 |

---

## 実装順序

1. plans/x-m37-*.md + plans/x-m38-*.md + plans/x-roadmap.md を更新
2. `.github/workflows/release.yml` を更新してコミット
3. `skills/x/SKILL.md` を更新してコミット
4. `git push origin main` (main ブランチにマージ後)

## Verification

- `actionlint .github/workflows/release.yml` で syntax エラーなし
- 次の `v*` タグ push で Node.js 20 deprecation 警告が消える
- `skills/x/SKILL.md` に M29-M36 のコマンドが全て網羅されている
