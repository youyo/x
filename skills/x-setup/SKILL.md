---
triggers:
  - x setup
  - x configure
  - x credentials
  - OAuth 1.0a setup
  - X API credentials
  - X認証設定
  - x 認証設定
  - credentials.toml
description: Set up and validate OAuth 1.0a credentials for the x CLI.
---

# x-setup — Credentials & Configure

This skill covers credential setup, path inspection, and validation for the `x` CLI.

## Prerequisites

Obtain four values from the [X Developer Portal](https://developer.twitter.com/):

- API Key (`X_API_KEY`)
- API Secret (`X_API_SECRET`)
- Access Token (`X_ACCESS_TOKEN`)
- Access Token Secret (`X_ACCESS_TOKEN_SECRET`)

Your app must have **Read** permission and **OAuth 1.0a User Context** enabled.

---

## Interactive Setup

```bash
x configure
```

Prompts for all four fields and writes them to `credentials.toml` with permissions `0600`.

---

## Inspect Paths

```bash
x configure --print-paths           # JSON output
x configure --print-paths --no-json # Human-readable output
```

Displays the resolved paths for `config.toml`, `credentials.toml`, and the data directory.

Default locations (XDG-compliant):

| File               | Default path                                    |
|--------------------|-------------------------------------------------|
| `credentials.toml` | `~/.local/share/x/credentials.toml` (`0600`)   |
| `config.toml`      | `~/.config/x/config.toml` (`0644`)             |

---

## Validate Configuration

```bash
x configure --check
x configure --check --no-json  # Human-readable
```

Checks:
- `credentials.toml` permissions are exactly `0600`
- All four required fields are present
- `config.toml` contains no secret keys (guards against accidental leaks)

Exit codes on failure: `1` (generic) or `2` (argument error).

---

## Env-Only Operation (CI / Lambda)

Skip `credentials.toml` entirely and export env vars directly:

```bash
export X_API_KEY=your_api_key
export X_API_SECRET=your_api_secret
export X_ACCESS_TOKEN=your_access_token
export X_ACCESS_TOKEN_SECRET=your_access_token_secret
```

Env vars always take priority over `credentials.toml` in CLI mode.

For CI, inject secrets via GitHub Actions Secrets, AWS SSM Parameter Store, or equivalent:

```yaml
# GitHub Actions example
env:
  X_API_KEY: ${{ secrets.X_API_KEY }}
  X_API_SECRET: ${{ secrets.X_API_SECRET }}
  X_ACCESS_TOKEN: ${{ secrets.X_ACCESS_TOKEN }}
  X_ACCESS_TOKEN_SECRET: ${{ secrets.X_ACCESS_TOKEN_SECRET }}
```

---

## Security Recommendations

Add `credentials.toml` to `.gitignore`:

```gitignore
# .gitignore
~/.local/share/x/credentials.toml
# or if you keep a local copy in the project dir:
credentials.toml
```

Do not put API keys in `config.toml` — `x configure --check` will reject them.

---

## Verify Auth After Setup

```bash
x me           # JSON: {"id": "...", "username": "...", "name": "..."}
x me --no-json # Human-readable
```

---

## Troubleshooting

| Exit | Symptom                            | Action                                                       |
|------|------------------------------------|--------------------------------------------------------------|
| 3    | Auth error (`x me` or `x liked`)  | Re-run `x configure` or update env vars; check token expiry  |
| 4    | Permission error                   | Enable Read permission and OAuth 1.0a User Context in Developer Portal |
| 1    | `credentials.toml` perm != `0600` | Run `chmod 0600 ~/.local/share/x/credentials.toml`           |
| 2    | Missing required fields            | Re-run `x configure` and supply all four values              |
