# Jot

A dead-simple, self-hosted, private static-hosting service. Push a file or a folder, get a URL, share it inside your trusted audience. Backed by any S3-compatible object store, gated by OIDC (Google Workspace by default), driven by a single Go binary and a matching Go CLI.

The whole system is shaped around one workflow: an agent (or a human) generates an HTML artifact, pushes it, and gets a URL. That artifact is private to whoever is in the configured trust ring. Mostly push-and-forget, occasionally iterate.

> **Note on macOS:** the BSD `jot` utility (a number-sequence generator) ships pre-installed on macOS. The `jot` CLI documented here shadows it when installed via Homebrew or any `$PATH`-earlier location. If you need the BSD utility, invoke it as `/usr/bin/jot`. The README ships with this note prominently displayed so users who hit the surprise have an immediate answer.

---

## Goals

- One command, one URL: `jot push ./report.html` → `https://jot.example.com/a7b9c2d4/`.
- Every served page is gated by the same auth boundary as the deploy API. No public content.
- Atomic pushes — a request mid-push never sees a half-uploaded site.
- Content-addressed blobs with cross-deploy deduplication. Re-pushes upload only the diff.
- Push metadata (title, summary, tags) supplied at the CLI so future-self and other agents can find things.
- Local context: a `.jot/` folder in the working directory tracks what was pushed from there.
- Multi-tenant of pushes, not of organizations: one server install serves one trust ring.
- Self-hostable on a small VM, Cloud Run, or any container runtime. No external runtime dependencies beyond object storage and an OIDC IdP.

## Non-goals (v1)

- No SSR, serverless functions, or incremental static regeneration. Jot serves bytes.
- No build pipeline. Bring built artifacts.
- No git integration. Agents (or humans) invoke the CLI.
- No previews, promote, aliases, or named projects. Each push is its own deploy with its own URL.
- No redirects, URL rewrites, or `_redirects` files. Configure in your bundler if needed.
- No built-in CDN, edge functions, or image optimization.
- No web UI. The CLI is the contract.
- No machine tokens, PATs, or service-account auth. Everything is OIDC.

---

## Architecture

```
┌─────────────┐         ┌──────────────┐         ┌─────────────────┐
│   jot CLI   │────────▶│  jot-server  │────────▶│  S3-compatible  │
│   (Go)      │  HTTPS  │   (Go)       │  S3 API │  object store   │
└─────────────┘         └──────────────┘         └─────────────────┘
       │                       ▲
       │                       │ verifies bearer / cookie
       │                       │ via OIDC JWKS
       ▼                       │
┌─────────────┐                │
│    IdP      │────────────────┘
│  (Google,   │   OIDC discovery + JWKS
│  Okta, ...) │
└─────────────┘
```

Browser requests for served content: `Browser → (TLS terminator) → jot-server → object store`.
Jot does not terminate TLS. Run it behind Cloudflare, Cloud Run, Caddy, or any load balancer.

Jot is stateless on local disk: state lives entirely in the bucket. Restart-anywhere, no volumes.

---

## Storage layout

Everything in one bucket:

```
<bucket>/
├── blobs/
│   └── sha256/<hex>                # immutable, content-addressed file contents
├── manifests/
│   └── <slug>/<deploy-id>.json     # one manifest per deploy
├── slugs/
│   └── <slug>/current              # tiny pointer: {"manifest_id": "01HX..."}
├── _meta/
│   └── ...                         # reserved
└── _trash/                          # soft-deleted blobs awaiting 7d GC
```

Key properties:

- Every blob is named by `sha256(content)`. Uploading the same file twice is a no-op.
- Deploy IDs are ULIDs (sortable, URL-safe, 26 chars).
- Slug names match `^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`. Reserved: any path starting with `_`.
- `slugs/<slug>/current` is the only mutable object per slug. Updating it via S3 conditional `If-Match` is the atomic commit.
- Bounded history: last 10 manifests per slug retained by default.

---

## Manifest

```json
{
  "schema_version": 1,
  "id": "01HXABCDEFGHJKMNPQRSTVWXYZ",
  "slug": "a7b9c2d4",
  "created_at": "2026-05-19T12:00:00Z",
  "created_by": "alice@example.com",
  "actor": "claude-code",
  "title": "Q2 Sales Report",
  "summary": "Quarterly revenue breakdown by region, generated from BI export on May 19.",
  "tags": ["report", "q2", "finance"],
  "spa_fallback": "/index.html",
  "headers": {
    "/_next/static/**": { "cache-control": "public, max-age=31536000, immutable" }
  },
  "files": {
    "/index.html": { "sha256": "a1b2...", "size": 4321, "content_type": "text/html; charset=utf-8" },
    "/app.js":     { "sha256": "c3d4...", "size": 8421, "content_type": "application/javascript" }
  }
}
```

All metadata fields except `id`, `slug`, `created_at`, `created_by`, and `files` are optional. Manifests are immutable once written.

---

## Atomic deploy

1. CLI walks the source path and hashes every file.
2. CLI calls `POST /_api/deploys:check` with the list of hashes → server returns the subset missing in the bucket.
3. For each missing hash, server returns a signed PUT URL; CLI uploads directly to object storage.
4. CLI submits the manifest via `PUT /_api/deploys/<id>`. Server writes `manifests/<slug>/<id>.json`.
5. Server updates `slugs/<slug>/current` using S3 `If-Match` as a compare-and-swap.

Each step is independently safe. A crash before step 5 leaves orphan blobs and an unreferenced manifest, never a broken URL. GC reclaims them after 7 days.

**Concurrent pushes to the same slug:** the second push's `If-Match` fails with 412. CLI retries once, then errors out with a clear message naming the conflicting deploy ID and pusher.

---

## Auth

**One concept: OIDC.** Two surfaces: bearer tokens (CLI) and signed session cookies (browsers). Both share the same authorize rule and the same IdP configuration.

### CLI flow (loopback PKCE)

1. `jot login` reads the server URL.
2. CLI fetches `GET /_api/auth/config`:
   ```json
   { "issuer": "https://accounts.google.com",
     "client_id": "1234567890-abc.apps.googleusercontent.com",
     "scopes": ["openid", "email", "profile"] }
   ```
3. CLI does OIDC discovery on `issuer` for token/auth endpoints.
4. CLI spins up `http://127.0.0.1:50573/callback` by default, opens the browser to the authorize URL with PKCE. The port can be changed with `--callback-port`.
5. Operator signs in at the IdP; browser redirects to localhost with a code.
6. CLI exchanges code → ID token + refresh token. Stores refresh token in the OS keychain via `zalando/go-keyring` (fallback: `~/.config/jot/credentials.json` at 0600).
7. Per request: refresh → ID token → `Authorization: Bearer <id-token>`.

Headless variant: `jot login --no-browser` triggers OAuth 2.0 device-authorization flow. Useful for SSH sessions and remote dev containers.

### Browser flow (signed cookie session)

```
GET /<slug>/index.html              # no cookie
  → 302 /_auth/login?return_to=...

GET /_auth/login                    # generates state+PKCE, stores in short cookie
  → 302 <issuer>/authorize?...

GET /_auth/callback?code=...&state=...
  → exchange code, verify ID token, check authorize rules
  → Set-Cookie: jot_session=<signed-jwt>; HttpOnly; Secure; SameSite=Lax; Path=/
  → 302 /<slug>/index.html

GET /<slug>/index.html              # cookie present
  → verify signature, check expiry, re-check authorize rules
  → serve blob
```

Session cookie is a JWT signed with `cookie_secret` (HMAC-SHA256):

```json
{ "email": "alice@example.com", "hd": "example.com", "exp": 1747710000 }
```

Stateless: no server-side session table. Authorize rules are re-checked against the cookie's claims on every request, so tightening `authorize` takes effect immediately. Fixed-window TTL, default 8h. Expired cookies trigger a re-auth dance through the IdP — usually silent.

### Both surfaces, same handler

Every authenticated route accepts **either** `Authorization: Bearer <id-token>` **or** a valid `jot_session` cookie. One middleware, two strategies, same identity.

### Authorize rule

Configurable claim matching against the verified token/cookie:

```yaml
auth:
  authorize:
    required_claims:
      hd: example.com          # Google Workspace domain
    # or:
    # email_domains: [example.com, partner.com]
    # required_claims: { groups: ["engineering"] }
```

The `Authorize.Check` function is the only IdP-specific code path. Same OIDC verifier (`coreos/go-oidc/v3`) works for Google, Okta, Auth0, Keycloak, Authentik, Dex, anything OIDC-compliant.

### OAuth client setup at the IdP

Use separate OAuth clients for Google:

1. Web application client for browser sessions:
   - `https://jot.example.com/_auth/callback`
   - Store its client ID as `auth.client_id` and its secret as `auth.client_secret`.
2. Desktop app client for CLI login:
   - Store its client ID as `auth.cli_client_id`.
   - CLI login uses loopback PKCE at `http://127.0.0.1:50573/callback` by default; the port can be changed with `--callback-port`.

Google Web OAuth clients require a client secret during token exchange, so they cannot be used directly by the CLI. Other OIDC providers may allow one public PKCE client for both surfaces; in that case `auth.cli_client_id` can be omitted and the CLI discovers `auth.client_id`.

### Actor attribution

Optional `--actor <name>` flag (also `$JOT_ACTOR` env var) on `jot push`. Free-form string recorded on the manifest. Used for audit only — never trusted for authz. Conventional values: `claude-code`, `cursor`, `aider`.

---

## Serving behavior

### URL scheme

```
https://jot.example.com/
├── /<slug>/<path>             # serves the current deploy of <slug>
├── /_api/...                   # management API
├── /_auth/...                  # OAuth handlers
├── /_health                    # health check
└── /                           # 404 by default
```

No subdomains. No previews. One slug per URL. Each slug is its own page set.

### Path resolution

For `GET /<slug>/<rest>`:

1. Try `<rest>` as a literal manifest key.
2. If `<rest>` has no extension, try `<rest>.html`.
3. Try `<rest>/index.html` → if found, **301** to `/<slug>/<rest>/`.
4. If nothing matched and `spa_fallback` is set on the manifest **and** the request has `Accept: text/html`, serve the fallback with status 200.
5. If the deploy contains `/404.html`, serve it with status 404.
6. Otherwise, jot's built-in plain 404 page.

`GET /<slug>` (no trailing slash) → **301** to `/<slug>/`.

Asset requests (no `Accept: text/html`) on unauthenticated visits return **401** with a small JSON body, not a redirect. HTML requests get the 302 to login.

### Caching

- Every response carries `ETag: "<sha256>"`. Jot responds 304 on `If-None-Match` match.
- Default `Cache-Control`:
  - `text/html` → `public, max-age=0, must-revalidate`
  - everything else → `public, max-age=3600`
- Per-push overrides via `--header` at push time. Glob syntax: gitignore-style, `**` across segments, `*` within. First match wins.
- **Defaults are conservative on purpose.** Only fingerprinted asset paths (`main.Bx7t2KQa.css`) are safe to mark immutable. Jot can't auto-detect those — operators (or agents) opt in per glob.

---

## CLI

```
jot login   [--server URL] [--no-browser]
jot logout

jot push <path>
  [--as <slug>]              # use/create this slug; default: auto (8-char base32)
  [--title "..."]            # short human-readable label
  [--summary "..."]          # 1-2 sentences on what this is — used by `jot ls --search`
  [--tag <tag>]              # repeatable; for categorization
  [--index <file>]           # if directory contains multiple HTMLs and no index.html
  [--spa <path>]             # enable SPA fallback (e.g. /index.html)
  [--header "<glob>=<key>: <value>"]  # repeatable; per-path response headers
  [--actor <name>]           # free-form: claude-code, cursor, ...
  [--server URL]             # override default server
  [--json]                   # machine-readable output for agents

jot ls
  [--mine]                   # only the current user's pushes
  [--local]                  # only those tracked in ./.jot/
  [--tag <tag>]              # repeatable filter
  [--search <query>]         # substring match on title + summary
  [--limit N]                # default 50
  [--json]

jot inspect <slug|id>        # full metadata + file list for one deploy
jot history <slug>           # the bounded N versions of this slug
jot rollback <slug> [<id>]   # restore previous (or specific) manifest
jot rm <slug>                # hard-delete the slug and all its manifests
jot whoami

jot init server > jot.yaml   # admin: scaffold a server config
```

### Inline help is the spec

`jot push --help` and `jot ls --help` are deliberately verbose. Each flag documents:

- when to use it
- an example value
- for metadata flags, guidance on what makes a useful value

Example:

```
--summary string
    A 1-2 sentence description of what this deploy contains. This is the
    primary field used by `jot ls --search`, so write it as if you are
    leaving a note for your future self or another agent. Good summaries
    describe the content's purpose, scope, and time context.

    Example: "Q2 2026 revenue breakdown by region, generated from BI export
    on May 19. Includes interactive charts."
```

Agents read `--help` to know what to fill in. The CLI is self-documenting; there is no separate "API for agents."

### Multi-server

Per-server credentials in the keychain. Default server in `~/.config/jot/config.toml`. Override with `--server URL` or `$JOT_SERVER`. One CLI binary works against multiple jot installs (e.g. personal + work).

### Output

Human-readable by default:

```
$ jot push ./report.html --title "Q2 Sales" --summary "..." --tag report
Uploading 3/3 files (12 KB)
Pushed → https://jot.example.com/a7b9c2d4/
  Title:   Q2 Sales
  Summary: ...
  Tags:    report
```

`--json` streams one event per line during upload (`{"type":"upload","file":"...","done":3,"total":12}`) and emits a final result object — designed for agent consumption.

---

## `.jot/` local context

Created on first push from a directory. Contains exactly one file:

```
./.jot/pushes.json
```

```json
[
  {
    "slug": "a7b9c2d4",
    "url": "https://jot.example.com/a7b9c2d4/",
    "title": "Q2 Sales Report",
    "summary": "Quarterly revenue breakdown by region",
    "tags": ["report", "q2", "finance"],
    "pushed_at": "2026-05-19T12:34:56Z",
    "pushed_by": "alice@example.com"
  }
]
```

Auto-appended to `.gitignore` on creation (if a `.gitignore` is present) with a leading comment. Read by `jot ls --local`. Pure local state — never sent to the server.

This is the "context when we come back" mechanism. An agent dropped into a working directory runs `jot ls --local` and sees what was pushed from there, with enough metadata to know what's what.

---

## Operational concerns

### Limits (defaults; configurable)

| Limit | Default |
|---|---|
| Files per push | 100 |
| Bytes per file | 10 MB |
| Bytes per push (total) | 50 MB |
| Manifests retained per slug | 10 |
| Session cookie TTL | 8h |
| Soft-delete TTL for unreferenced blobs | 7 days |

Hitting a limit returns a clear error naming the offending file/total and the configured limit.

### GC

Server runs a daily background sweep (configurable cron):

1. List all manifests across all slugs.
2. Union the referenced blob hashes.
3. Move any blob in `blobs/sha256/` not in the union to `_trash/<hash>` with a TTL marker.
4. Hard-delete `_trash/` items older than 7 days.

The 7-day window is the concurrent-push safety margin: an in-flight push whose manifest hasn't been committed is safe from GC during that window.

No `jot gc` admin command. It just runs.

### Logging

Single stream of JSON to stdout. Every line includes:

```json
{ "ts": "...", "level": "info", "event_type": "push|access|auth|rollback|rm|error", ... }
```

Operators run any log shipper they like.

### Health

`GET /_health` → 200 if the server can issue a HEAD against a known prefix in the bucket; 503 otherwise. Does **not** ping the IdP's JWKS endpoint (it's flaky and stale-cache-tolerant).

### Versioning

- Manifest schema includes `schema_version`. Server refuses manifests with an unknown major version. Additive changes within a major are safe.
- `GET /_api/version` → `{"server":"1.4.2","manifest_schema":1,"min_cli":"1.0.0"}`. CLI checks once per session (cached); below `min_cli` → loud warning but proceeds.

### Content type & encoding

- Content-Type detected at push time by file extension (Go's `mime` package). Stored on the manifest. No content sniffing on serve.
- Jot does not compress. Cloudflare/Caddy in front does brotli/gzip negotiation.

### Cookie secret

Jot refuses to start without a valid `cookie_secret` (32+ random bytes, hex-encoded). It will not auto-generate one. Generate with `openssl rand -hex 32` and store in config or a secret manager.

This is intentional: silently generating a secret bites operators when they later spin up a second instance and wonder why all sessions break across both.

---

## Configuration

```yaml
# jot.yaml
server:
  addr: ":8080"
  base_url: https://jot.example.com    # used to build redirect URIs and shared URLs
  history_size: 10                      # deploys retained per slug
  insecure_http: false                  # dev only; disables Secure flag on cookies

storage:
  endpoint: https://s3.amazonaws.com    # or R2, Garage, GCS-in-interop, etc.
  region: auto                          # required by some clients
  bucket: jot-prod
  access_key_id: ...
  secret_access_key: ...
  force_path_style: false               # set true for Garage / MinIO-like backends

auth:
  issuer: https://accounts.google.com
  audience: 1234567890-abc.apps.googleusercontent.com
  client_id: 1234567890-abc.apps.googleusercontent.com
  cli_client_id: 1234567890-cli.apps.googleusercontent.com
  client_secret: GOCSPX-xxxxxxxxxxxxxxxxxxxx
  cookie_secret: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
  session_ttl: 8h
  authorize:
    required_claims:
      hd: example.com

limits:
  files_per_push: 100
  bytes_per_file: 10485760
  bytes_per_push: 52428800
```

Server reads config from `--config <path>`, `$JOT_CONFIG`, or `./jot.yaml` (in that order).

`jot init server` prints a documented scaffold with placeholders.

---

## Repository layout

```
jot/
├── cmd/
│   ├── jot-server/main.go
│   └── jot/main.go
├── internal/
│   ├── manifest/        # shared types + (de)serialization
│   ├── storage/         # S3 driver behind an interface
│   ├── auth/            # OIDC verifier, browser flow, authorize rules
│   ├── server/          # HTTP handlers
│   └── cli/             # CLI command implementations
├── deploy/
│   ├── docker/Dockerfile
│   └── local/                        # Garage + jot demo (below)
│       ├── docker-compose.yml
│       ├── garage.toml
│       └── jot.yaml
├── go.mod
└── README.md
```

The `storage` interface is the only place that knows about S3 specifics. Native GCS, Azure Blob, or filesystem drivers could slot in as additional packages later.

---

## Distribution

Three install channels at v1.

### GitHub Releases

Signed binaries published on every `v*` tag. Platforms:

- `linux/amd64`, `linux/arm64`
- `darwin/amd64`, `darwin/arm64`
- `windows/amd64` — CLI only; nobody runs `jot-server` on Windows

Each release includes both `jot` and `jot-server` binaries, SHA-256 checksums, and Cosign signatures.

### Homebrew

The primary install path for end users on macOS and Linux. Published via a custom tap at `<org>/homebrew-jot`:

```bash
brew install <org>/jot/jot           # CLI
brew install <org>/jot/jot-server    # server, for operators who want it
```

The formula pulls the matching binary from GitHub Releases and verifies the checksum.

**Note on the macOS shadow:** installing via Homebrew is precisely how the `jot` CLI ends up shadowing the BSD `jot` utility. Homebrew's `/opt/homebrew/bin` (Apple Silicon) or `/usr/local/bin` (Intel) is earlier in `$PATH` than `/usr/bin`, so `jot` resolves to our binary after install. Users who need the BSD utility invoke `/usr/bin/jot` directly. This is documented prominently in the formula's `caveats:` block so `brew install` itself prints the heads-up:

```
==> Caveats
This formula installs `jot`, which on macOS shadows the BSD `jot` utility
(a number-sequence generator). To use the BSD jot, invoke it as
`/usr/bin/jot`.
```

A `homebrew-core` submission is a v2 consideration. Core has a "notable, stable, maintained" bar that v1 won't clear, and the custom tap is one extra command for users (`brew tap <org>/jot` if implicit tap fetch isn't enabled).

### Docker image

For operators running the server. Published to `ghcr.io/<org>/jot:<version>` and `:latest`. Built `FROM scratch`, around 15 MB compressed. Multi-arch (amd64 + arm64). The CLI is not distributed via Docker — it's a static binary best installed locally.

### Release pipeline

Tagged releases use semver. CI on every `v*` tag: builds binaries, generates checksums, signs with Cosign, publishes to GitHub Releases, pushes the Docker image, and opens a follow-up PR to the Homebrew tap repo bumping the formula's version and SHA. The tap PR is auto-merged once CI on the tap repo passes.

---

## Local demo (Garage + jot)

A self-contained Docker Compose stack for trying jot on your laptop. Uses [Garage](https://garagehq.deuxfleurs.fr/) as the S3-compatible store — a small Rust binary by the Deuxfleurs cooperative, designed for self-hosting. We're avoiding MinIO because it went AGPL and archived its community edition in early 2026; Garage is the current OSS-friendly small-deployment option.

Two simplifications for dev mode:

- `insecure_http: true` — drops the `Secure` flag on cookies so `http://localhost` works.
- `auth.mode: dev` — bypasses OIDC and treats every request as `dev@local`. Loud startup warning. Never enable in production.

### `deploy/local/docker-compose.yml`

```yaml
services:
  garage:
    image: dxflrs/garage:v1.0.1
    ports:
      - "3900:3900"   # S3 API
      - "3902:3902"   # Admin API
    volumes:
      - ./garage.toml:/etc/garage.toml:ro
      - garage-data:/var/lib/garage

  garage-init:
    image: dxflrs/garage:v1.0.1
    depends_on: [garage]
    volumes:
      - ./garage.toml:/etc/garage.toml:ro
    entrypoint: ["/bin/sh", "-c"]
    command:
      - |
        sleep 3
        NODE_ID=$$(garage status | awk 'NR==3 {print $$1}')
        garage layout assign "$$NODE_ID" -z dc1 -c 1G -t local
        garage layout apply --version 1
        garage bucket create jot || true
        garage key create jot-key || true
        garage bucket allow --read --write --owner jot --key jot-key
        garage key info jot-key  # prints the access + secret keys

  jot:
    image: ghcr.io/<org>/jot:latest
    depends_on: [garage-init]
    ports: ["8080:8080"]
    volumes:
      - ./jot.yaml:/etc/jot.yaml:ro
    environment:
      JOT_CONFIG: /etc/jot.yaml

volumes:
  garage-data:
```

### `deploy/local/jot.yaml`

```yaml
server:
  addr: ":8080"
  base_url: http://localhost:8080
  insecure_http: true

storage:
  endpoint: http://garage:3900
  region: garage
  bucket: jot
  access_key_id: <from-garage-init-output>
  secret_access_key: <from-garage-init-output>
  force_path_style: true

auth:
  mode: dev
  cookie_secret: 0000000000000000000000000000000000000000000000000000000000000000
  session_ttl: 1h
```

### Try it

```bash
cd deploy/local
docker compose up
# Copy the Garage access key + secret from the garage-init log into jot.yaml.
# Restart jot.

# In another terminal:
jot login --server http://localhost:8080      # dev mode signs you in as dev@local
echo '<h1>hello jot</h1>' > index.html
jot push index.html --title "Hello"
# → Pushed → http://localhost:8080/a7b9c2d4/
```

Open the URL in a browser, see your HTML. Run `jot ls`, `jot inspect <slug>`, etc.

---

## Production setup on GCP — agent prompt

Copy-paste the block below into an agent (Claude Code, Cursor, etc.) that has `gcloud` configured for the target project. The agent will run the provisioning, pausing at the single step that gcloud cannot automate (OAuth client creation).

```text
You are deploying jot, a self-hosted private static-hosting service, to GCP.
The final running service should be reachable at a custom domain, gated by
Google Workspace OIDC (only members of one Workspace domain can sign in).

Jot is stateless and deploys best as a Cloud Run service backed by a GCS
bucket (used via the S3 XML interop API with HMAC keys). Secrets go in
Secret Manager.

Inputs (ask the user for each, then proceed):
  - GCP_PROJECT:        the GCP project ID to deploy into
  - REGION:             a Cloud Run region (default: europe-west1)
  - JOT_DOMAIN:         the public hostname (e.g. jot.example.com)
  - WORKSPACE_DOMAIN:   the Google Workspace domain to gate on (e.g. example.com)
  - JOT_IMAGE:          the jot container image to deploy
                        (default: ghcr.io/<org>/jot:latest)

Execute these steps. After each, verify success before moving on. If anything
fails, stop and explain.

1. Confirm gcloud auth and set the project:
     gcloud config set project $GCP_PROJECT
     gcloud auth list

2. Enable required APIs:
     gcloud services enable run.googleapis.com storage.googleapis.com \
       iamcredentials.googleapis.com secretmanager.googleapis.com

3. Create the bucket jot will use for storage:
     gcloud storage buckets create gs://jot-$GCP_PROJECT \
       --location=$REGION --uniform-bucket-level-access

4. Create a service account for jot and grant it bucket access:
     gcloud iam service-accounts create jot-server \
       --display-name="jot server"
     gcloud storage buckets add-iam-policy-binding gs://jot-$GCP_PROJECT \
       --member="serviceAccount:jot-server@$GCP_PROJECT.iam.gserviceaccount.com" \
       --role="roles/storage.objectAdmin"

5. Create HMAC credentials for the service account (S3 interop):
     gcloud storage hmac create \
       jot-server@$GCP_PROJECT.iam.gserviceaccount.com
   Capture the accessId and secret from the output. Store both in Secret Manager:
     echo -n "<accessId>" | gcloud secrets create jot-s3-access --data-file=-
     echo -n "<secret>"   | gcloud secrets create jot-s3-secret --data-file=-

6. Generate and store a cookie secret:
     openssl rand -hex 32 | gcloud secrets create jot-cookie-secret --data-file=-

7. STOP — manual step required. OAuth client creation is not supported by
   gcloud. Walk the user through the Console:

     a. Visit https://console.cloud.google.com/apis/credentials?project=$GCP_PROJECT
     b. Create Credentials → OAuth client ID → Application type: Web application
     c. Name: "jot-web"
     d. Authorized redirect URIs:
          - https://$JOT_DOMAIN/_auth/callback
     e. Click Create. Have the user copy the web Client ID and Client secret.
     f. Create Credentials → OAuth client ID → Application type: Desktop app
     g. Name: "jot-cli"
     h. Click Create. Have the user copy the desktop Client ID.

   Store them in Secret Manager:
     echo -n "<web-client-id>"     | gcloud secrets create jot-oauth-client-id --data-file=-
     echo -n "<web-client-secret>" | gcloud secrets create jot-oauth-client-secret --data-file=-
     echo -n "<cli-client-id>"     | gcloud secrets create jot-oauth-cli-client-id --data-file=-

8. Grant the jot service account access to read its secrets:
     for s in jot-s3-access jot-s3-secret jot-cookie-secret \
              jot-oauth-client-id jot-oauth-client-secret \
              jot-oauth-cli-client-id; do
       gcloud secrets add-iam-policy-binding $s \
         --member="serviceAccount:jot-server@$GCP_PROJECT.iam.gserviceaccount.com" \
         --role="roles/secretmanager.secretAccessor"
     done

9. Deploy jot to Cloud Run:
     gcloud run deploy jot \
       --image=$JOT_IMAGE \
       --region=$REGION \
       --service-account=jot-server@$GCP_PROJECT.iam.gserviceaccount.com \
       --allow-unauthenticated \
       --port=8080 \
       --min-instances=1 \
       --max-instances=10 \
       --set-env-vars="JOT_SERVER_BASE_URL=https://$JOT_DOMAIN,\
JOT_STORAGE_ENDPOINT=https://storage.googleapis.com,\
JOT_STORAGE_BUCKET=jot-$GCP_PROJECT,\
JOT_STORAGE_REGION=auto,\
JOT_AUTH_ISSUER=https://accounts.google.com,\
JOT_AUTH_AUTHORIZE_HD=$WORKSPACE_DOMAIN,\
JOT_AUTH_SESSION_TTL=8h" \
       --set-secrets="JOT_STORAGE_ACCESS_KEY_ID=jot-s3-access:latest,\
JOT_STORAGE_SECRET_ACCESS_KEY=jot-s3-secret:latest,\
JOT_AUTH_AUDIENCE=jot-oauth-client-id:latest,\
JOT_AUTH_CLIENT_ID=jot-oauth-client-id:latest,\
JOT_AUTH_CLI_CLIENT_ID=jot-oauth-cli-client-id:latest,\
JOT_AUTH_CLIENT_SECRET=jot-oauth-client-secret:latest,\
JOT_AUTH_COOKIE_SECRET=jot-cookie-secret:latest"

   Note: --allow-unauthenticated tells Cloud Run not to gate the service with
   its own IAM — jot does its own OIDC gating. This is correct.

10. Map the custom domain:
      gcloud run domain-mappings create --service=jot \
        --domain=$JOT_DOMAIN --region=$REGION
    Cloud Run prints DNS records the user adds at their registrar.
    Wait for the user to confirm DNS has propagated (use `dig $JOT_DOMAIN`).

11. Verify:
      curl -I https://$JOT_DOMAIN/_health
    Should return 200. If 503, check the bucket is reachable and HMAC keys
    are correct.

12. Print these commands for the user to run on their laptop:
      jot login --server https://$JOT_DOMAIN
      echo '<h1>hello</h1>' > index.html
      jot push index.html --title "First push"

Stop. Report back the JOT_DOMAIN, the Cloud Run URL, the bucket name, and
the names of the secrets created.
```

---

## Out of scope for v1 (candidates for v2)

- Web UI / dashboard.
- Per-slug ACLs (anyone in the trust ring sees and can mutate any slug today).
- Multiple OIDC issuers on one server (CI getting native short-lived tokens from GitHub Actions OIDC).
- ACME / Let's Encrypt inside jot (run a TLS terminator in front).
- Native GCS, Azure Blob, or filesystem storage drivers.
- Redirect rules / `_redirects` files.
- Bandwidth / pushes-per-minute rate limiting.
- Compression (brotli/gzip) on the jot hot path. Use a CDN.
- Image optimization.
- Push from a URL or git ref (currently CLI uploads from local disk only).
- TTL / auto-expiry on deploys.
- Soft-delete of slugs (`rm` is permanent today).
