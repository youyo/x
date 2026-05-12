---
triggers:
  - x mcp
  - x MCP server
  - MCP X API
  - Remote MCP X
  - X MCP サーバー
  - MCP サーバー起動
description: Start and configure the x Remote MCP server — auth modes, env vars, Lambda/Docker deployment.
---

# x-mcp — MCP Server

`x mcp` starts a Streamable HTTP MCP server that exposes X API v2 operations as MCP tools. It runs as a long-lived process and is designed for deployment on Lambda, containers, or any HTTP-reachable host.

## Critical Invariant

**MCP mode reads credentials from env vars only.** `credentials.toml` is never read, regardless of whether the file exists. This is a spec invariant (§11) designed for immutable infrastructure where secrets come from SSM Parameter Store → Lambda env or equivalent.

Required env vars:

```
X_API_KEY
X_API_SECRET
X_ACCESS_TOKEN
X_ACCESS_TOKEN_SECRET
```

---

## Start Command

```bash
x mcp [flags]
```

### Flags

| Flag              | Env var        | Default     | Description                                                     |
|-------------------|----------------|-------------|-----------------------------------------------------------------|
| `--auth`          | `X_MCP_AUTH`   | `idproxy`   | Auth mode: `none` \| `apikey` \| `idproxy`                     |
| `--host`          | `X_MCP_HOST`   | `127.0.0.1` | Bind host                                                       |
| `--port`          | `X_MCP_PORT`   | `8080`      | Bind port                                                       |
| `--path`          | `X_MCP_PATH`   | `/mcp`      | MCP endpoint path                                               |
| `--apikey-env`    | —              | `X_MCP_API_KEY` | Env var name holding the shared secret (apikey mode only)   |

---

## Auth Modes

### `none` — No authentication (local dev only)

```bash
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
x mcp --auth none --host 127.0.0.1 --port 8080
```

Do not expose `--auth none` to the network. Use only for local development and testing.

### `apikey` — Bearer token

```bash
X_MCP_API_KEY=my-shared-secret \
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
x mcp --auth apikey --host 0.0.0.0 --port 8080
```

Clients must send `Authorization: Bearer my-shared-secret` on every request. The secret is read from `X_MCP_API_KEY` at startup (constant-time comparison to prevent timing attacks).

To use a different env var name:

```bash
x mcp --auth apikey --apikey-env MY_CUSTOM_SECRET_VAR
```

### `idproxy` — OIDC cookie session

```bash
STORE_BACKEND=memory \
X_API_KEY=... X_API_SECRET=... \
X_ACCESS_TOKEN=... X_ACCESS_TOKEN_SECRET=... \
x mcp --auth idproxy --host 0.0.0.0 --port 8080
```

Requires an OIDC provider in front. Session state is persisted in the store backend:

| `STORE_BACKEND` | Use case                                        |
|-----------------|-------------------------------------------------|
| `memory`        | Single-instance, dev/test (state lost on restart)|
| `sqlite`        | Single-instance, persistent                     |
| `redis`         | Multi-instance, low latency                     |
| `dynamodb`      | Serverless / Lambda, managed persistence        |

---

## Provided MCP Tools

| Tool name          | Equivalent CLI command        | Description                       |
|--------------------|-------------------------------|-----------------------------------|
| `get_user_me`      | `x me`                        | Fetch the authenticated user      |
| `get_liked_tweets` | `x liked list`                | Fetch liked tweets with filtering |

---

## Exit Codes

| Code | Meaning        |
|------|----------------|
| 0    | Success        |
| 1    | Generic error  |
| 2    | Argument error |
| 3    | Auth error     |

---

## Lambda Deployment

1. Build the binary with GoReleaser (or use the prebuilt GitHub Release artifact).
2. Package as a Lambda function (use the provided Dockerfile or Lambda zip handler).
3. Set all required env vars via SSM Parameter Store → Lambda environment:

```
X_API_KEY        → ssm:/x/api_key
X_API_SECRET     → ssm:/x/api_secret
X_ACCESS_TOKEN   → ssm:/x/access_token
X_ACCESS_TOKEN_SECRET → ssm:/x/access_token_secret
X_MCP_AUTH       → apikey
X_MCP_API_KEY    → ssm:/x/mcp_api_key
STORE_BACKEND    → dynamodb
```

4. Set `--host 0.0.0.0` (or `X_MCP_HOST=0.0.0.0`) to bind on all interfaces.
5. Place an ALB or API Gateway in front for HTTPS termination.

Do not mount `credentials.toml` in Lambda — MCP mode ignores it and the attempt will silently have no effect.

---

## Docker Deployment

```dockerfile
# Dockerfile ships the prebuilt binary from GoReleaser
FROM gcr.io/distroless/static-debian12:nonroot
COPY x /usr/local/bin/x
ENTRYPOINT ["/usr/local/bin/x"]
CMD ["mcp"]
```

```bash
docker run --rm \
  -e X_API_KEY=... \
  -e X_API_SECRET=... \
  -e X_ACCESS_TOKEN=... \
  -e X_ACCESS_TOKEN_SECRET=... \
  -e X_MCP_AUTH=apikey \
  -e X_MCP_API_KEY=... \
  -e X_MCP_HOST=0.0.0.0 \
  -p 8080:8080 \
  ghcr.io/youyo/x:latest mcp
```

---

## Notes

- The server handles graceful shutdown on SIGTERM/SIGINT.
- `--auth none` is not safe for production. Always use `apikey` or `idproxy` in network-accessible deployments.
- `credentials.toml` is never consulted in MCP mode — this is by design, not a bug.
