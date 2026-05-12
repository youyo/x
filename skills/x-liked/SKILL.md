---
triggers:
  - x liked
  - liked tweets
  - liked posts
  - yesterday likes
  - JST likes
  - 前日いいね
  - 昨日のいいね
  - liked list
description: Fetch liked tweets with the x CLI — JST date helpers, pagination, NDJSON output, and jq pipelines.
---

# x-liked — Liked List Workflows

All commands below use `x liked list`. For credential setup, see [`x-setup`](../x-setup/SKILL.md).

## Most Common Command

```bash
x liked list --yesterday-jst --all --ndjson
```

- `--yesterday-jst` — Expands to JST previous day 00:00–23:59 as `start_time`/`end_time`
- `--all` — Auto-follows `next_token` up to `--max-pages` (default: 50)
- `--ndjson` — One JSON object per line, HTML escape disabled — ideal for pipe processing

---

## All Flags

| Flag                         | Default             | Description                                                      |
|------------------------------|---------------------|------------------------------------------------------------------|
| `--yesterday-jst`            | —                   | Previous JST day (overrides `--since-jst`)                       |
| `--since-jst YYYY-MM-DD`     | —                   | JST date (overrides `--start-time`/`--end-time`)                 |
| `--start-time RFC3339`       | —                   | Earliest tweet time in UTC                                       |
| `--end-time RFC3339`         | —                   | Latest tweet time in UTC                                         |
| `--all`                      | false               | Auto-follow pagination                                           |
| `--max-pages N`              | 50                  | Max pages when `--all` is set                                    |
| `--max-results N`            | 100                 | Tweets per page (1–100)                                          |
| `--pagination-token TOKEN`   | —                   | Resume from a previous `next_token`                              |
| `--user-id ID`               | authenticated user  | Target a different user (requires app-level permission)          |
| `--ndjson`                   | false               | Line-delimited JSON output                                       |
| `--no-json`                  | false               | Human-readable tab-separated output                              |
| `--tweet-fields FIELDS`      | `id,text,author_id,created_at,entities,public_metrics` | Comma-separated tweet fields |
| `--user-fields FIELDS`       | `username,name`     | Comma-separated user fields                                      |
| `--expansions FIELDS`        | `author_id`         | Comma-separated expansions                                       |

---

## Date / Time Workflows

### Previous JST day (most common)

```bash
x liked list --yesterday-jst --all --ndjson
```

### Specific JST date

```bash
x liked list --since-jst 2026-05-12 --all --ndjson
```

### RFC3339 UTC time range

```bash
x liked list \
  --start-time 2026-05-11T15:00:00Z \
  --end-time   2026-05-12T14:59:59Z \
  --all --ndjson
```

Prefer `--since-jst` / `--yesterday-jst` over manual UTC conversion to avoid timezone mistakes.

---

## Output Modes

| Flag        | Use case                                           |
|-------------|----------------------------------------------------|
| (default)   | Full `{data, includes, meta}` JSON response        |
| `--ndjson`  | One tweet per line — use for pipes and streaming   |
| `--no-json` | Tab-separated human-readable — do not parse with scripts |

---

## jq Pipeline Examples

```bash
# Filter Japanese-language tweets
x liked list --yesterday-jst --all --ndjson \
  | jq -c 'select(.lang == "ja")'

# Extract text of tweets mentioning Go, Rust, or TypeScript
x liked list --yesterday-jst --all --ndjson \
  | jq -c 'select(.text | test("Go|Rust|TypeScript"))' \
  | jq -r '.text'

# Save to file for later processing
x liked list --yesterday-jst --all --ndjson \
  > /tmp/yesterday-liked.ndjson

# Count likes
x liked list --yesterday-jst --all --ndjson | wc -l
```

---

## Manual Pagination

When you do not use `--all`, retrieve pages manually:

```bash
# Page 1
x liked list --max-results 100 | tee /tmp/page1.json

# Extract next_token
NEXT=$(jq -r '.meta.next_token' /tmp/page1.json)

# Page 2
x liked list --max-results 100 --pagination-token "$NEXT"
```

`meta.next_token` is absent (or `null`) when there are no more pages.

---

## Field Expansion Example

```bash
x liked list --yesterday-jst --all --ndjson \
  --tweet-fields id,text,author_id,created_at,public_metrics,entities,lang \
  --user-fields  username,name,verified \
  --expansions   author_id
```

Field names must exactly match the X API v2 field names — a typo returns `exit 2`.

---

## Rate Limits

X API v2 has per-endpoint rate limits. When the limit is hit, `x liked list` exits with code `1` and prints the error details. Wait for the rate limit window to reset (typically 15 minutes) before retrying.

For bulk operations, consider adding delays between pages or using `--max-pages` to cap requests per run.

---

## Error Reference

| Exit | Common cause                          | Resolution                                              |
|------|---------------------------------------|---------------------------------------------------------|
| 2    | Invalid flag or `--since-jst` format  | Use strict `YYYY-MM-DD` format                          |
| 3    | Token expired or revoked              | Re-run `x configure` or refresh env vars                |
| 4    | App missing Read / User context scope | Fix permissions in X Developer Portal                   |
| 5    | `--user-id` does not exist            | Verify ID with `x me` or check the target user ID       |
| 1    | Rate limit or API error               | Wait and retry; check error message for details         |

---

## Notes

- `exit 0` with an empty `data` array is normal — it means no likes in that time range.
- Use `jq '.meta.result_count'` to distinguish empty from error programmatically.
- In automation scripts, always consume JSON/NDJSON output, not `--no-json`.
