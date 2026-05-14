# M37: GitHub Actions を Node.js 24 対応版にバンプ

## Overview

| 項目 | 値 |
|------|---|
| ステータス | 未着手 |
| 対象 v リリース | v0.4.1 相当 (chore) |
| Phase | II: メンテナンス・改善 |
| 依存 | M14 (release.yml 作成) |
| 主要対象ファイル | `.github/workflows/release.yml` |

## Goal

v0.4.0 リリース後の CI で Node.js 20 Actions が 2026-06-02 に強制 Node.js 24 化されるという警告が発生。4 つの Action を Node.js 24 対応版にバンプし、警告を解消する。

## 対象 Action と更新内容

| Action | 現在 | 更新後 | 変更点 |
|--------|------|--------|--------|
| `docker/setup-qemu-action` | v3.6.0 (Node.js 20) | v4.0.0 (Node.js 24) | SHA のみ更新 |
| `docker/setup-buildx-action` | v3.11.1 (Node.js 20) | v4.0.0 (Node.js 24) | SHA のみ更新 |
| `docker/login-action` | v3.5.0 (Node.js 20) | v4.1.0 (Node.js 24) | SHA のみ更新 |
| `tibdex/github-app-token` | v2.1.0 (Node.js 20) | `actions/create-github-app-token` v2 | Action 差し替え + `app_id` → `app-id`、`private_key` → `private-key` |

### tibdex 差し替えの理由

`tibdex/github-app-token` の最新版 v2.1.0 は Node.js 20 ベースのままで、Node.js 24 対応版が存在しない。GitHub 公式の `actions/create-github-app-token` は Node.js 24 対応済みで、入力フォーマットが若干異なる (アンダースコア → ハイフン)。

## 変更後の該当ステップ

```yaml
      - name: Set up QEMU (multi-arch docker)
        uses: docker/setup-qemu-action@ce360397dd3f832beb865e1373c09c0e9f86d70a # v4.0.0

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@4d04d5d9486b7bd6fa91e7baf45bbb4f8b9deedd # v4.0.0

      - name: Login to ghcr.io
        uses: docker/login-action@4907a6ddec9925e35a0a9e82d7399ccc52663121 # v4.1.0
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Generate GitHub App token
        id: app-token
        uses: actions/create-github-app-token@fee1f7d63c2ff003460e3d139729b119787bc349 # v2
        with:
          app-id: ${{ secrets.APP_ID }}
          private-key: ${{ secrets.APP_PRIVATE_KEY }}
```

## Tasks

- [ ] `.github/workflows/release.yml` の 4 Action を更新 (SHA pin)
- [ ] `actionlint .github/workflows/release.yml` で syntax エラーなし確認
- [ ] コミット: `chore(ci): GitHub Actions を Node.js 24 対応版にバンプ`

## Completion Criteria

- `actionlint` で 0 エラー
- 次の `v*` タグ push で Node.js 20 deprecation 警告が消える
- `HOMEBREW_TAP_GITHUB_TOKEN` の生成が引き続き動作する
