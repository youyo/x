# x — X (Twitter) API CLI & Remote MCP

Read this in: English | [日本語](README.ja.md)

[![CI](https://github.com/youyo/x/actions/workflows/ci.yml/badge.svg)](https://github.com/youyo/x/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.26-blue.svg)](go.mod)
[![Latest Release](https://img.shields.io/github/v/release/youyo/x?include_prereleases)](https://github.com/youyo/x/releases)

A single-binary Go CLI for working with the X (formerly Twitter) API v2 — designed as a building block for automating "yesterday's Liked posts → Backlog tickets" workflows via Claude Code Routines.

The design principle is **"CLI is the core, MCP is a thin wrapper"**. From `v0.3.0`, the AWS Lambda Function URL deployment sample (`examples/lambroll/`) and the Claude Code Routines prompt template (`docs/routine-prompt.md`) are also provided.

## Status

`v0.3.0` completes the 3-phase plan (CLI → MCP → public distribution). Release history:

| Version | Scope |
|---------|-------|
| `v0.1.0` | CLI: `x version` / `x me` / `x liked list` / `x configure` / `x completion` |
| `v0.2.0` | Remote MCP server (`x mcp --auth idproxy\|apikey\|none`) with `get_user_me` and `get_liked_tweets` tools, plus four `idproxy` store backends (memory / sqlite / redis / dynamodb) |
| `v0.3.0` | `examples/lambroll/` AWS Lambda Function URL deployment sample + Claude Code Routines prompt template (`docs/routine-prompt.md`) + X API v2 reference (`docs/x-api.md`) |
| `v0.4.0` | `x tweet get` / `liking-users` / `retweeted-by` / `quote-tweets` (M29) + `note_tweet` (long-form) default fetch + `--max-results 1..4` auto-correction for `liked list` |
| `v0.5.0` (this release, draft) | `x tweet search <query>` + `x tweet thread <ID\|URL>` (M30, **Basic tier required**) + `x timeline tweets` / `mentions` / `home` (M31) |

See [`docs/specs/x-spec.md`](docs/specs/x-spec.md) for the full product specification.

## Features

- **`x me`** — Resolve your own `user_id` / `username` via `GET /2/users/me`
- **`x liked list`** — Fetch Liked posts via `GET /2/users/:id/liked_tweets`
  - JST date helpers (`--since-jst`, `--yesterday-jst`) auto-convert to UTC
  - `--all` mode auto-follows `next_token` with rate-limit aware sleep (respects `x-rate-limit-remaining` / `x-rate-limit-reset`)
  - NDJSON streaming (`--ndjson`) for piping into other tools
  - Customizable `tweet.fields` / `expansions` / `user.fields`
  - `note_tweet` (long-form tweet body) fetched by default; `--no-json` prefers it over the truncated text
  - `--max-results 1..4` is normalised to the X API minimum (5) and the response is sliced (v0.4.0)
- **`x tweet get [ID|URL]` / `x tweet get --ids ID1,ID2,...`** — Look up a tweet by ID or X URL, or batch up to 100 IDs (v0.4.0)
- **`x tweet liking-users` / `x tweet retweeted-by` / `x tweet quote-tweets`** — Social signals for a tweet (v0.4.0)
- **`x tweet search <query>`** — Search recent tweets (past 7 days, **X API Basic tier required**) via `GET /2/tweets/search/recent`. Supports X search operators (`from:` / `lang:` / `conversation_id:` etc.), JST date helpers, `--all` pagination, and NDJSON streaming (v0.5.0)
- **`x tweet thread <ID|URL>`** — Fetch a tweet's full conversation thread (2 API calls: `GetTweet` + `search/recent` with `conversation_id`). `--author-only` filters to the root author's posts (v0.5.0)
- **`x timeline tweets [<ID>]`** — Fetch a user's tweet timeline via `GET /2/users/:id/tweets` (defaults to self). `--exclude retweets,replies` supported (v0.5.0)
- **`x timeline mentions [<ID>]`** — Fetch tweets mentioning a user via `GET /2/users/:id/mentions` (defaults to self). Note: X API does not support `exclude` on this endpoint, so the flag is intentionally not registered (v0.5.0)
- **`x timeline home`** — Fetch the authenticated user's home timeline via `GET /2/users/:id/timelines/reverse_chronological`. The target user is always self (X API spec); `--user-id` is intentionally not exposed (v0.5.0)
- **`x configure`** — Interactive setup of XDG-compliant config + credentials files
- **`x mcp`** — Start a Streamable HTTP MCP server (Claude Code Routines / MCP client connectivity)
  - Three auth modes: `none` (local dev only), `apikey` (Bearer token), `idproxy` (OIDC + cookie session)
  - Four `idproxy` store backends: `memory` / `sqlite` / `redis` / `dynamodb`
  - MCP tools: `get_user_me`, `get_liked_tweets`
  - `GET /healthz` for Lambda Web Adapter / k8s liveness probes
  - Graceful shutdown on SIGINT/SIGTERM
- **`x version`** — Build information (version / commit / build date)
- **`x completion`** — Shell completion for bash / zsh / fish / powershell (Cobra built-in)
- **OAuth 1.0a** static token authentication (user context)
- **XDG Base Directory Specification** compliant — non-secret config and secrets live in separate files
- **Stable exit codes** (`0` / `1` / `2` / `3` / `4` / `5`) for scripting

## Installation

### Homebrew

```bash
brew install youyo/tap/x
```

### `go install`

```bash
go install github.com/youyo/x/cmd/x@latest
```

### Docker

```bash
docker pull ghcr.io/youyo/x:latest
docker run --rm ghcr.io/youyo/x:latest version
```

### GitHub Releases

Download a pre-built tarball for your OS / arch from the [Releases page](https://github.com/youyo/x/releases) and extract the `x` binary into your `$PATH`.

### From source

```bash
git clone https://github.com/youyo/x.git
cd x
go build -o x ./cmd/x
./x version
```

## Quick Start

### 1. Obtain X API credentials

You need an X (Twitter) Developer App with **OAuth 1.0a User Context** enabled. See [Obtaining X API credentials](#obtaining-x-api-credentials) below.

You will end up with four secrets:

- `X_API_KEY` (consumer key)
- `X_API_SECRET` (consumer secret)
- `X_ACCESS_TOKEN`
- `X_ACCESS_TOKEN_SECRET`

### 2. Configure

Interactive mode (saves to XDG-compliant paths, `credentials.toml` is `chmod 0600`):

```bash
x configure
```

Or set environment variables directly:

```bash
export X_API_KEY=...
export X_API_SECRET=...
export X_ACCESS_TOKEN=...
export X_ACCESS_TOKEN_SECRET=...
```

### 3. Verify

```bash
x me
# {"id":"12345","username":"yourname","name":"Your Name"}
```

### 4. Fetch yesterday's Liked posts

```bash
x liked list --yesterday-jst --all
```

Or as NDJSON for piping:

```bash
x liked list --yesterday-jst --all --ndjson | jq -r '.id + " " + .text'
```

## CLI Recipes

### Common `x me` patterns

```bash
# JSON (default) — pipe into jq
x me | jq -r '.username'

# Human-readable single line for shell scripts
x me --no-json
# → id=12345 username=yourname name=Your Name
```

### Fetching Liked posts

```bash
# Single page (up to 100 posts)
x liked list --max-results 100

# Specific date range (UTC RFC3339)
x liked list \
  --start-time 2026-05-10T00:00:00Z \
  --end-time   2026-05-10T23:59:59Z

# Yesterday in JST (auto-converts to UTC range, fetches all pages)
x liked list --yesterday-jst --all

# Custom JST date
x liked list --since-jst 2026-05-10 --all

# Limit pages when piping into LLM context (avoid runaway costs)
x liked list --yesterday-jst --all --max-pages 5

# Custom fields (e.g. include retweet/like counts and entities)
x liked list --yesterday-jst --all \
  --tweet-fields "id,text,author_id,created_at,public_metrics,entities" \
  --expansions   "author_id" \
  --user-fields  "username,name,verified"

# Stream as NDJSON (one tweet per line) for piping into jq / xargs / LLMs
x liked list --yesterday-jst --all --ndjson \
  | jq -r '"- [\(.text | gsub("\n"; " ") | .[0:80])](https://x.com/i/web/status/\(.id))"'
```

### Inspecting / verifying your configuration

```bash
x configure --print-paths
# {
#   "config":      "/home/you/.config/x/config.toml",
#   "credentials": "/home/you/.local/share/x/credentials.toml",
#   "data_dir":    "/home/you/.local/share/x"
# }

x configure --check
# Validates credentials.toml permissions (0600) and the absence of secrets in config.toml.
```

### Use with Claude Code

```bash
# Add CLI to a local Claude Code session for one-off use
echo '{"x":{"command":"x","args":["mcp","--auth","none","--host","127.0.0.1","--port","18080"]}}' \
  > ~/.config/claude/mcp.json
```

For a persistent remote setup, deploy via `examples/lambroll/` and add the Function URL as a Claude Code Routines connector — see [`docs/routine-prompt.md`](docs/routine-prompt.md).

## Quick Start (MCP server)

The `x mcp` subcommand starts a [Streamable HTTP](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports) MCP server. Three auth modes are available. In MCP mode, **all secrets must come from environment variables** — `credentials.toml` is never read.

Endpoints (regardless of auth mode):

- `POST /mcp` — MCP Streamable HTTP entry point (auth applies)
- `GET /healthz` — liveness probe (always returns `200 ok`, bypasses auth)

### Local development (`--auth none`)

```bash
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
  x mcp --auth none --host 127.0.0.1 --port 8080
```

### Shared API Key (`--auth apikey`)

```bash
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
X_MCP_API_KEY=$(openssl rand -hex 32) \
  x mcp --auth apikey --host 0.0.0.0 --port 8080
```

Clients must send `Authorization: Bearer ${X_MCP_API_KEY}`. The comparison is constant-time (`subtle.ConstantTimeCompare`). The `--apikey-env` flag selects **which environment variable name** holds the shared secret (default: `X_MCP_API_KEY`).

### OIDC + cookie session (`--auth idproxy`, default)

Uses [`github.com/youyo/idproxy`](https://github.com/youyo/idproxy). Choose a persistent store backend via `STORE_BACKEND`:

```bash
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
OIDC_ISSUER=https://accounts.google.com,https://login.microsoftonline.com/<tenant>/v2.0 \
OIDC_CLIENT_ID=<google-client-id>,<entra-client-id> \
OIDC_CLIENT_SECRET=<google-client-secret> \
COOKIE_SECRET=$(openssl rand -hex 32) \
EXTERNAL_URL=https://x-mcp.example.com \
STORE_BACKEND=dynamodb \
DYNAMODB_TABLE_NAME=x-mcp-idproxy \
AWS_REGION=ap-northeast-1 \
  x mcp --auth idproxy --host 0.0.0.0 --port 8080
```

Store backends:

| `STORE_BACKEND` | Required env vars | Use case |
|---|---|---|
| `memory` (default) | — | unit tests, ephemeral local dev |
| `sqlite` | `SQLITE_PATH` (default `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db`) | single-process local dev (`modernc.org/sqlite`, pure Go) |
| `redis` | `REDIS_URL` (e.g. `redis://localhost:6379/0`) | lightweight servers, native TTL (`go-redis/v9`) |
| `dynamodb` | `DYNAMODB_TABLE_NAME`, `AWS_REGION` | Lambda multi-container, `ConsistentRead` (`aws-sdk-go-v2`) |

### Available MCP tools

| Tool | Description |
|---|---|
| `get_user_me` | Returns `{ user_id, username, name }` for the OAuth 1.0a user. |
| `get_liked_tweets` | Returns Liked posts with full pagination (`all=true`, `max_pages`, rate-limit aware). Accepts `user_id`, `start_time` / `end_time`, `since_jst`, `yesterday_jst`, `max_results`, `tweet_fields`, `expansions`, `user_fields`. JST helpers take precedence: `yesterday_jst > since_jst > start_time/end_time` (matches the CLI). |

### MCP client recipes

#### From `curl` (raw Streamable HTTP / JSON-RPC 2.0)

```bash
# 1) initialize handshake
curl -sS -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
    "protocolVersion":"2025-03-26",
    "capabilities":{},
    "clientInfo":{"name":"curl","version":"1.0"}}}'

# 2) list available tools
curl -sS -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'

# 3) call get_user_me
curl -sS -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call",
       "params":{"name":"get_user_me","arguments":{}}}'

# 4) call get_liked_tweets (yesterday JST, all pages)
curl -sS -X POST http://127.0.0.1:8080/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call",
       "params":{"name":"get_liked_tweets",
         "arguments":{"yesterday_jst":true,"all":true,"max_pages":5}}}'
```

With `--auth apikey`, add `-H "Authorization: Bearer <token>"` to every request.

#### From Claude Code Routines

1. Deploy via `examples/lambroll/` (see [`examples/lambroll/README.md`](examples/lambroll/README.md))
2. In Claude Code Routines, add the deployed Function URL as a connector
3. Use the prompt template in [`docs/routine-prompt.md`](docs/routine-prompt.md) — it covers fetching yesterday's Liked posts, judging technical relevance, and creating Backlog issues with duplicate detection

#### From mark3labs/mcp-go (Go client)

```go
import "github.com/mark3labs/mcp-go/client"

c, _ := client.NewStreamableHttpClient("http://127.0.0.1:8080/mcp")
c.Start(ctx)
defer c.Close()

_, _ = c.Initialize(ctx, mcp.InitializeRequest{...})
result, _ := c.CallTool(ctx, mcp.CallToolRequest{
    Params: mcp.CallToolParams{
        Name:      "get_liked_tweets",
        Arguments: map[string]any{"yesterday_jst": true, "all": true},
    },
})
```

## Configuration

### File layout (XDG Base Directory Specification)

| Kind | Path | Permissions |
|------|------|-------------|
| Non-secret config | `${XDG_CONFIG_HOME:-~/.config}/x/config.toml` | `0644` |
| Secrets (CLI only) | `${XDG_DATA_HOME:-~/.local/share}/x/credentials.toml` | `0600` |

Writing secrets into `config.toml` is **explicitly rejected** at load time (`ErrSecretInConfig`). The two files are kept on different paths to make `.gitignore` of the secrets file trivial and to avoid accidental commits.

### Load priority

CLI mode (`x me`, `x liked list`, `x configure`):

1. CLI flag
2. Environment variable
3. `credentials.toml` (secrets only)
4. `config.toml` (non-secret only)
5. Built-in default

MCP mode (`x mcp`):

1. CLI flag (`--host` / `--port` / `--path` / `--auth` / `--apikey-env`)
2. Environment variable (including secrets)
3. Built-in default

**MCP mode never reads `config.toml` or `credentials.toml`** — all secrets must come from environment variables. This makes Lambda / container deployments deterministic.

### `config.toml` example

```toml
# CLI defaults. Do NOT put secrets here.
[cli]
output = "json"           # json | ndjson | table
log_level = "info"

[liked]
default_max_results = 100
default_max_pages = 50
default_tweet_fields = "id,text,author_id,created_at,entities,public_metrics"
default_expansions   = "author_id"
default_user_fields  = "username,name"
```

### `credentials.toml` example (`chmod 0600`)

```toml
[xapi]
api_key             = "..."
api_secret          = "..."
access_token        = "..."
access_token_secret = "..."
```

### Environment variables (X API, CLI / MCP common)

| Name | Purpose | Required |
|------|---------|----------|
| `X_API_KEY` | OAuth 1.0a consumer key | Yes |
| `X_API_SECRET` | OAuth 1.0a consumer secret | Yes |
| `X_ACCESS_TOKEN` | OAuth 1.0a access token | Yes |
| `X_ACCESS_TOKEN_SECRET` | OAuth 1.0a access token secret | Yes |
| `XDG_CONFIG_HOME` | Override config dir (CLI only) | No |
| `XDG_DATA_HOME` | Override data dir (CLI only) | No |

When set, environment variables take precedence over file-based credentials.

### MCP server environment variables (v0.2.0+, MCP mode only)

| Name | Purpose | Default / Required |
|------|---------|--------------------|
| `X_MCP_HOST` | bind host | `127.0.0.1` |
| `X_MCP_PORT` | bind port | `8080` |
| `X_MCP_PATH` | MCP endpoint prefix | `/mcp` |
| `X_MCP_AUTH` | `idproxy` / `apikey` / `none` | `idproxy` |
| `X_MCP_API_KEY` | apikey-mode shared secret **value** (compared with `Authorization: Bearer ...`) | required when `--auth apikey` |
| `OIDC_ISSUER` | idproxy OIDC issuer (comma-separated, multiple OK) | required when `--auth idproxy` |
| `OIDC_CLIENT_ID` | idproxy OIDC client ID (comma-separated, aligned with `OIDC_ISSUER`) | required when `--auth idproxy` |
| `OIDC_CLIENT_SECRET` | idproxy OIDC client secret | issuer-dependent |
| `COOKIE_SECRET` | idproxy session encryption key (hex, 32B+) | required when `--auth idproxy` |
| `EXTERNAL_URL` | idproxy external URL | required when `--auth idproxy` |
| `STORE_BACKEND` | `memory` / `sqlite` / `redis` / `dynamodb` | `memory` |
| `SQLITE_PATH` | sqlite DB file path | `${XDG_DATA_HOME:-~/.local/share}/x/idproxy.db` |
| `REDIS_URL` | Redis connection URL | required when `STORE_BACKEND=redis` |
| `DYNAMODB_TABLE_NAME` | DynamoDB table name | required when `STORE_BACKEND=dynamodb` |
| `AWS_REGION` | AWS region | required on Lambda / when `STORE_BACKEND=dynamodb` |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` | `info` |

## Obtaining X API credentials

1. Sign in to the [X Developer Portal](https://developer.x.com/).
2. Create a Project + App (free tier is sufficient for personal Liked Posts use).
3. In the App settings, enable **OAuth 1.0a User Context** with `Read` permission.
4. Generate **Consumer Keys** (`X_API_KEY`, `X_API_SECRET`).
5. Generate **Access Token and Secret** for the same user (`X_ACCESS_TOKEN`, `X_ACCESS_TOKEN_SECRET`).
6. Run `x configure` and paste them in, or export them as environment variables.

> Rate limit: `GET /2/users/me` and `GET /2/users/:id/liked_tweets` are both limited to **75 requests per 15 minutes** in the free tier. `x` automatically respects `x-rate-limit-remaining` / `x-rate-limit-reset` headers during `--all` mode to avoid 429s. Owned Reads cost approximately `$0.001` per Tweet at the time of writing — set `--max-pages` to bound cost.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Generic error |
| `2` | Argument / validation error (invalid flag combination, malformed value) |
| `3` | Authentication error (X API `401`, missing credentials) |
| `4` | Permission error (X API `403`) |
| `5` | Not found (X API `404`) |

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `x me` exits with code `3` ("credentials are required") | `X_API_*` env vars not set and `credentials.toml` not found | `x configure` to create `credentials.toml`, or `export X_API_KEY=...` etc. Run `x configure --check` to verify. |
| `x me` exits with code `3` ("401 Unauthorized") | OAuth 1.0a tokens invalid / regenerated on X Developer Portal | Re-issue tokens from the Developer Portal and re-run `x configure`. |
| `x liked list` exits with code `4` (Permission) | `403`: account suspended or missing read scope | Check the Developer App permissions include **Read** at minimum. |
| `x liked list --all` takes very long | Rate-limit hit, `x` is sleeping until `x-rate-limit-reset` | Expected. Cap with `--max-pages 5` if needed. |
| `x liked list` returns `meta.result_count = 0` | `start_time` / `end_time` window does not match any Likes, or your account has no Likes in that range | Try `x liked list` without time filters to verify connectivity. |
| `x mcp` exits with code `3` immediately ("X_MCP_API_KEY is required") | `--auth apikey` selected but `X_MCP_API_KEY` env var not set | Set `X_MCP_API_KEY=$(openssl rand -hex 32)` and re-launch. |
| MCP client gets `401` from `/mcp` | Wrong `Authorization` header or `idproxy` cookie expired | apikey: verify `Bearer <token>`. idproxy: browser-authenticate via `EXTERNAL_URL` first. |
| MCP client gets `404` on a path | Server mounts MCP on `/mcp` only by default | Either use the default path or set `--path` / `X_MCP_PATH`. |
| Where are config / credential files? | XDG-resolved at runtime | `x configure --print-paths` |
| Routines connector refuses connection | Function URL not public or `EXTERNAL_URL` mismatched the OIDC callback registration | See [`examples/lambroll/README.md`](examples/lambroll/README.md) trouble-shooting section. |

## Documentation

| Document | Purpose |
|---|---|
| [`docs/specs/x-spec.md`](docs/specs/x-spec.md) | Full product specification (Approved v1.0.0) |
| [`docs/x-api.md`](docs/x-api.md) | X API v2 OAuth 1.0a + rate limit + Owned Reads pricing reference |
| [`docs/routine-prompt.md`](docs/routine-prompt.md) | Claude Code Routines prompt template (yesterday's Likes → Backlog issues) |
| [`examples/lambroll/README.md`](examples/lambroll/README.md) | AWS Lambda Function URL deployment guide (lambroll + LWA) |
| [`CHANGELOG.md`](CHANGELOG.md) | Release history |
| [`plans/x-roadmap.md`](plans/x-roadmap.md) | Milestone breakdown (all 28 milestones complete) |

## Development

This repo uses [`mise`](https://mise.jdx.dev/) for toolchain management.

```bash
mise install                # installs Go 1.26.x

# Run tests (race + coverage)
go test -race ./...

# Lint (golangci-lint v2)
golangci-lint run

# Build a local binary
go build -o x ./cmd/x

# Snapshot release (no upload, no Docker daemon required)
goreleaser release --snapshot --clean --skip docker,docker_manifest
```

Continuous integration runs lint + test + build + docker on every push and PR to `main`. See [`.github/workflows/ci.yml`](.github/workflows/ci.yml).

### Release procedure (maintainer)

Releases are tag-driven. Once a `vX.Y.Z` tag is pushed, [`.github/workflows/release.yml`](.github/workflows/release.yml) runs GoReleaser and publishes the Homebrew formula + Docker images automatically.

```bash
git checkout main && git pull
git status                                # working tree must be clean
grep "^## \[X.Y.Z\]" CHANGELOG.md         # CHANGELOG must already have the new section
git tag vX.Y.Z
git push origin vX.Y.Z
```

The release workflow then:

- runs `goreleaser` (darwin / linux × amd64 / arm64 tarballs + checksums)
- publishes GitHub Releases assets
- updates the `youyo/homebrew-tap` formula
- pushes Docker images to `ghcr.io/youyo/x:X.Y.Z` and `:latest`

No tags (`v0.1.0` / `v0.2.0` / `v0.3.0`) have been pushed yet from this repository; the procedure above documents the intent.

## Contributing

Issues and pull requests are welcome. The project follows:

- **Conventional Commits** (Japanese commit messages are accepted, since the primary maintainer is Japanese-speaking)
- **Test-Driven Development** (Red → Green → Refactor) — see [`CLAUDE.md`](CLAUDE.md) for the agent-driven workflow used in this repo
- **No secrets in `config.toml`** — the loader will reject them

## License

[MIT](LICENSE) — Copyright (c) 2026 Naoto Ishizawa / Heptagon
