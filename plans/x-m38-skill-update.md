# M38: skills/x/SKILL.md を v0.4.0 (M29-M36) に更新

## Overview

| 項目 | 値 |
|------|---|
| ステータス | 未着手 |
| 対象 v リリース | — (docs のみ) |
| Phase | II: メンテナンス・改善 |
| 依存 | M29-M36 (CLI コマンド群の実装完了) |
| 主要対象ファイル | `skills/x/SKILL.md` |

## Goal

M29-M36 で追加した9コマンド群 (tweet/timeline/user/list/space/trends/dm) を `skills/x/SKILL.md` の Subcommands テーブルと Quick Reference に追記し、Claude が x CLI の全機能を把握できるようにする。

## 追加するコマンド一覧

| コマンド | サブコマンド | 説明 |
|---------|------------|------|
| `tweet` | `get`, `liking-users`, `retweeted-by`, `quote-tweets` | Tweet lookup + social signals (URL→ID 自動変換, M29) |
| `tweet` | `search` | 過去 7 日キーワード検索 (Basic tier 以上, M30) |
| `tweet` | `thread` | スレッド全取得 conversation_id 経由 (M30) |
| `timeline` | `tweets`, `mentions`, `home` | ユーザータイムライン3種 (M31) |
| `user` | `get`, `search`, `following`, `followers`, `blocking`, `muting` | ユーザー lookup・グラフ (M32) |
| `list` | `get`, `tweets`, `members`, `owned`, `followed`, `memberships`, `pinned` | リスト操作 (M33) |
| `space` | `get`, `by-ids`, `search`, `by-creator`, `tweets` | アクティブ Space (M34) |
| `trends` | `get`, `personal` | WOEID / パーソナライズトレンド (M34) |
| `dm` | `list`, `get`, `conversation`, `with` | DM Read (Pro 推奨, M35) |

## Tasks

- [ ] `skills/x/SKILL.md` の Subcommands テーブルに全コマンドを追記
- [ ] Quick Reference に代表的な使用例を追加
- [ ] Tier 制約 (search は Basic 以上、dm は Pro 推奨) を明記
- [ ] コミット: `docs(skills): x CLI サブコマンド一覧を v0.4.0 (M29-M36) に更新`

## Completion Criteria

- Subcommands テーブルに全14コマンドが記載されている
- Quick Reference に M29-M36 の代表的なコマンド例が含まれている
- Tier 制約 (tweet search: Basic+, dm: Pro推奨) が明記されている
