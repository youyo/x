---
triggers:
  - x cli
  - x command
  - X API CLI
  - x コマンド
  - X CLI
description: Hub skill for the x CLI — X (formerly Twitter) API v2 CLI + Remote MCP server in a single binary.
---

# x CLI — Hub

`x` is a single Go binary that provides both a CLI and a Remote MCP server for the X (formerly Twitter) API v2. It uses OAuth 1.0a User Context authentication.

## Installation

```bash
# Go install
go install github.com/youyo/x/cmd/x@latest

# Homebrew
brew install youyo/tap/x
```

## Subcommands

| Subcommand | Description |
|------------|-------------|
| `configure` | Interactive OAuth 1.0a credential setup and validation |
| `me` | Fetch the authenticated user profile |
| `liked list` | Fetch liked tweets (JST date helpers, full pagination, NDJSON) |
| `tweet get` | Look up a tweet by ID or URL; `--ids` for batch (up to 100) |
| `tweet liking-users` | List users who liked a tweet |
| `tweet retweeted-by` | List users who retweeted a tweet |
| `tweet quote-tweets` | List quote tweets of a tweet |
| `tweet search` | Search recent tweets — past 7 days (**Basic tier required**) |
| `tweet thread` | Fetch a full thread via conversation_id + search/recent |
| `timeline tweets` | Fetch a user's tweet timeline (default: self) |
| `timeline mentions` | Fetch mentions of a user (default: self) |
| `timeline home` | Fetch the home timeline of the authenticated user |
| `user get` | Look up users by ID, @username, URL, or batch |
| `user search` | Search users by keyword |
| `user following` | List users that a user follows |
| `user followers` | List followers of a user |
| `user blocking` | List users blocked by the authenticated user (self only) |
| `user muting` | List users muted by the authenticated user (self only) |
| `list get` | Look up a List by numeric ID or X List URL |
| `list tweets` | Fetch tweets from a List |
| `list members` | List members of a List |
| `list owned` | List Lists owned by a user (default: self) |
| `list followed` | List Lists followed by a user (default: self) |
| `list memberships` | List Lists that a user is a member of (default: self) |
| `list pinned` | List pinned Lists (self only) |
| `space get` | Look up a Space by ID or URL (active/live only) |
| `space by-ids` | Look up multiple Spaces by IDs (batch, up to 100) |
| `space search` | Search active Spaces by keyword |
| `space by-creator` | Look up Spaces by creator user IDs |
| `space tweets` | Fetch tweets associated with a Space |
| `trends get` | Fetch trends by WOEID (Tokyo=1118370 / Japan=23424856 / Worldwide=1) |
| `trends personal` | Fetch personalized trends for the authenticated user |
| `dm list` | List recent DM events (**Pro tier recommended**, Basic ~1/24h) |
| `dm get` | Look up a single DM event by ID |
| `dm conversation` | Fetch DM events for a specific conversation |
| `dm with` | Fetch 1-on-1 DM events with a specific user |
| `mcp` | Start the Remote MCP server (Streamable HTTP) |
| `version` | Show version, commit, and build date |

## Exit Codes

All subcommands share the same exit code convention:

| Code | Meaning          |
|------|------------------|
| 0    | Success          |
| 1    | Generic error    |
| 2    | Argument error   |
| 3    | Auth error       |
| 4    | Permission error |
| 5    | Not found        |

## CLI vs MCP Mode — Credential Responsibility

| Mode       | Credential source             | Notes                                     |
|------------|-------------------------------|-------------------------------------------|
| CLI mode   | env vars → `credentials.toml` | env takes priority; file used as fallback |
| MCP mode   | env vars **only**             | `credentials.toml` is **never** read      |

**Required env vars (all modes):**

```
X_API_KEY
X_API_SECRET
X_ACCESS_TOKEN
X_ACCESS_TOKEN_SECRET
```

**File (CLI mode only):** `${XDG_DATA_HOME:-~/.local/share}/x/credentials.toml` (permissions `0600` enforced)

This separation is a spec invariant (§11). MCP mode is designed for immutable infrastructure (Lambda, containers) where secrets are injected via SSM Parameter Store → env.

## Skill Index

Use the dedicated skills for detailed workflows:

| Skill                               | Covers                                                          |
|-------------------------------------|-----------------------------------------------------------------|
| [`x-setup`](../x-setup/SKILL.md)   | `x configure`, credential files, env-only mode, troubleshooting |
| [`x-liked`](../x-liked/SKILL.md)   | `x liked list` flags, JST helpers, pagination, jq pipelines    |
| [`x-mcp`](../x-mcp/SKILL.md)       | `x mcp` server start, auth modes, env vars, Lambda/Docker       |

## Quick Reference

```bash
# Verify auth is working
x me

# Fetch yesterday's likes (JST) as NDJSON — most common pipeline command
x liked list --yesterday-jst --all --ndjson

# Look up a tweet by ID or URL (note_tweet full text included by default)
x tweet get 2054681397256962438
x tweet get https://x.com/USER/status/2054681397256962438

# Search recent tweets (past 7 days, Basic tier required)
x tweet search "NGINX CVE" --max-results 20 --no-json
x tweet search "from:youyo" --yesterday-jst --all --ndjson

# Fetch a full conversation thread
x tweet thread 2054681397256962438 --author-only --no-json

# Home timeline as NDJSON stream
x timeline home --yesterday-jst --all --ndjson

# Look up a user by @username
x user get @youyo --no-json

# List followers (defaults to self)
x user followers --no-json

# Fetch tweets from a List
x list tweets <LIST_ID> --all --ndjson

# Search active Spaces
x space search "AI engineering" --no-json

# Tokyo trends (WOEID 1118370)
x trends get 1118370 --no-json

# Start MCP server locally (dev, no auth)
X_API_KEY=... X_API_SECRET=... X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
x mcp --auth none --host 127.0.0.1 --port 8080

# Show version
x version --no-json
```

> **Tier notes:**
> - `tweet search` / `tweet thread` require **Basic tier** ($200/month) or higher.
> - `dm *` commands require **Pro tier** ($5,000/month) for practical use; Basic has ~1 call/24h limit.
> - All other commands work with any tier that includes OAuth 1.0a User Context access.
