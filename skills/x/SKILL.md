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

| Subcommand      | Description                                                     |
|-----------------|-----------------------------------------------------------------|
| `configure`     | Interactive OAuth 1.0a credential setup and validation          |
| `me`            | Fetch the authenticated user profile                            |
| `liked list`    | Fetch liked tweets (JST date helpers, full pagination, NDJSON)  |
| `mcp`           | Start the Remote MCP server (Streamable HTTP)                   |
| `version`       | Show version, commit, and build date                            |

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

# Start MCP server locally (dev, no auth)
X_API_KEY=... X_API_SECRET=... X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
x mcp --auth none --host 127.0.0.1 --port 8080

# Show version
x version --no-json
```
