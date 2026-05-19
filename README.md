# Jot

Jot is a dead-simple, self-hosted, private static-hosting service. Push a file or folder, get a URL, and share it inside one OIDC-protected trust ring.

> macOS already ships a BSD utility named `jot`. Installing this CLI earlier in `$PATH` shadows it. Use `/usr/bin/jot` when you need the BSD number-sequence generator.

## Install From Source

```bash
go install github.com/skorfmann/jot/cmd/jot@latest
go install github.com/skorfmann/jot/cmd/jot-server@latest
```

## Homebrew

```bash
brew install skorfmann/jot/jot
brew install skorfmann/jot/jot-server
```

## Quick Local Demo

```bash
cd deploy/local
docker compose up
```

Copy the Garage access key and secret printed by `garage-init` into `deploy/local/jot.yaml`, then restart the `jot` service.

```bash
jot login --server http://localhost:8080
echo '<h1>hello jot</h1>' > index.html
jot push index.html --title "Hello"
```

## Server

Generate a starter config:

```bash
jot init server > jot.yaml
jot-server --config jot.yaml
```

Jot expects S3-compatible object storage and OIDC configuration. It stores immutable blobs under `blobs/sha256/`, immutable manifests under `manifests/<slug>/<id>.json`, and an atomic current pointer under `slugs/<slug>/current`.

## CLI

```bash
jot login --server https://jot.example.com
jot push ./report.html --title "Q2 Sales" --summary "Q2 2026 revenue breakdown by region" --tag report
jot ls --mine
jot inspect <slug|id>
jot history <slug>
jot rollback <slug> [id]
jot rm <slug>
```

Use `--json` on `push`, `ls`, `inspect`, and `history` for agent-friendly output.

## Auth

Production mode uses OIDC ID tokens for the CLI and signed `jot_session` cookies for browsers. Both are checked by the same authorization rule.

Local demo mode uses:

```yaml
auth:
  mode: dev
```

Dev mode treats every request as `dev@local` and logs a startup warning. Do not enable it in production.

## Configuration

`jot-server` reads config from `--config`, `$JOT_CONFIG`, or `./jot.yaml`. Important environment overrides include:

- `JOT_SERVER_BASE_URL`
- `JOT_STORAGE_ENDPOINT`
- `JOT_STORAGE_BUCKET`
- `JOT_STORAGE_ACCESS_KEY_ID`
- `JOT_STORAGE_SECRET_ACCESS_KEY`
- `JOT_AUTH_ISSUER`
- `JOT_AUTH_AUDIENCE`
- `JOT_AUTH_CLIENT_SECRET`
- `JOT_AUTH_COOKIE_SECRET`
- `JOT_AUTH_AUTHORIZE_HD`

Generate `auth.cookie_secret` with:

```bash
openssl rand -hex 32
```
