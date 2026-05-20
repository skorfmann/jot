# Jot Progress Context

Purpose: compact, LLM-friendly project memory for future implementation sessions. Keep this file factual, public, and free of private deployment domains, OAuth IDs, secrets, and user-specific identifiers.

Last updated: 2026-05-20
Current code version: 0.1.9
Repository: github.com/skorfmann/jot

## Current State

- Jot is a self-hosted private static-hosting service with one Go server binary and one Go CLI binary.
- Users publish a file or directory with `jot push`, receive a private URL, and share it inside an OIDC-authenticated trust ring.
- Storage is object-store backed through Go CDK blob storage. GCS is the production-first path; S3-compatible stores remain supported for Garage/R2/local deployments.
- Browser content and API management routes are protected by the same authorization model: OIDC bearer tokens for CLI and signed cookies for browsers.
- Distribution is via GitHub Releases, GHCR Docker image, and the custom Homebrew tap `skorfmann/homebrew-jot`.

## Decisions Made

- Use module path `github.com/skorfmann/jot`.
- Use a single bucket as the durable state store. Server local disk is stateless.
- Use content-addressed blob keys: `blobs/sha256/<hash>`.
- Use immutable manifests: `manifests/<slug>/<deploy-id>.json`.
- Use one mutable current pointer per slug: `slugs/<slug>/current`.
- Use provider conditional writes for atomic slug updates.
- Use Go CDK blob storage as the abstraction boundary.
- For GCS on Cloud Run, use Application Default Credentials plus IAM `signBlob` for signed upload URLs.
- Do not create or require GCS HMAC/S3 interoperability credentials for production GCS.
- Use separate Google OAuth clients:
  - Web application client for browser sessions.
  - Desktop app client for CLI loopback PKCE.
- Store CLI credentials per server in OS keychain, with file fallback under the jot config dir.
- Single-file pushes map to `/index.html` so a pushed HTML file is useful at the slug root.
- Human CLI help is part of the product surface. Commands include examples.
- Homebrew install intentionally shadows macOS `/usr/bin/jot`; caveats document how to call the BSD utility.
- Public repo files must not contain the private production domain, private workspace domains, OAuth client IDs, OAuth secrets, or cloud project IDs.

## Built Features

### CLI

- `jot login` and `jot logout`
- `jot push <path>`
- `jot ls`
- `jot inspect <slug|id>`
- `jot history <slug>`
- `jot rollback <slug> [id]`
- `jot rm <slug>`
- `jot whoami`
- `jot init server`
- Global `--server` resolution through flag, `JOT_SERVER`, or config file.
- `--json` output on push/list/inspect/history for agent consumption.
- `jot ls` human output includes full deploy URLs.
- Local push context is recorded under `./.jot/pushes.json`.

### Server

- Health endpoint: `GET /_health`.
- Version endpoint: `GET /_api/version`.
- Public auth config discovery: `GET /_api/auth/config`.
- Browser auth flow:
  - `GET /_auth/login`
  - `GET /_auth/callback`
- Authenticated deploy API:
  - `POST /_api/deploys:check`
  - `PUT /_api/deploys/<id>`
  - `GET /_api/deploys`
  - `GET /_api/deploys/<slug|id>`
  - `GET /_api/slugs/<slug>/history`
  - `POST /_api/slugs/<slug>/rollback`
  - `DELETE /_api/slugs/<slug>`
  - `GET /_api/whoami`
- Static content serving under `/<slug>/...`.
- Authenticated root dashboard at `/` lists current deploys with full URLs, metadata, search, and a "mine" filter.
- HTML path resolution supports exact paths, extensionless `.html`, directory `index.html`, SPA fallback, and `/404.html`.
- ETags and default cache-control headers are emitted on served blobs.
- Background GC starts about one minute after boot and then runs every 24 hours.

### Storage

- Go CDK blob backend supports:
  - GCS URLs such as `gs://bucket?access_id=service-account@example-project.iam.gserviceaccount.com`.
  - S3-compatible explicit config fields for Garage/R2.
- Signed PUT URLs are issued for missing blobs.
- Current pointer compare-and-swap uses provider metadata:
  - GCS generation when available.
  - ETag/conditional write where supported.
- GCS signed URLs use IAM Credentials API `signBlob` when running without private key JSON.
- Soft deletion moves unreferenced blobs to `_trash/`; expired trash is later deleted.

### Deployment And Packaging

- Docker image is built from `deploy/docker/Dockerfile`.
- Local demo uses Garage via `deploy/local/docker-compose.yml`.
- GCP deployment prompt lives in `docs/gcp-agent-prompt.md`.
- Release workflow builds binaries, checksum file, Cosign bundle, GitHub Release assets, direct Homebrew tap update, and Docker image.
- Homebrew tap formulas install release binaries directly.
- The release workflow requires `HOMEBREW_TAP_TOKEN` with write access to `skorfmann/homebrew-jot`; missing tap credentials should fail the release instead of silently skipping publication.

## Important Defaults

- Version constant: `0.1.9`.
- Manifest schema version: `1`.
- Minimum CLI advertised by server: `0.1.0`.
- History retained per slug: `10`.
- Files per push: `100`.
- Bytes per file: `1073741824` bytes, 1 GiB.
- Bytes per push: `3221225472` bytes, 3 GiB.
- Auth session TTL: `8h`.
- Signed upload URL expiry: `15m`.
- GC trash TTL: `7d`.

## Config Surface

Server config is read from `--config`, `JOT_CONFIG`, or `./jot.yaml`.

Important environment overrides:

- `JOT_SERVER_ADDR`
- `JOT_SERVER_BASE_URL`
- `JOT_SERVER_HISTORY_SIZE`
- `JOT_SERVER_INSECURE_HTTP`
- `JOT_STORAGE_URL`
- `JOT_STORAGE_GOOGLE_ACCESS_ID`
- `JOT_STORAGE_ENDPOINT`
- `JOT_STORAGE_REGION`
- `JOT_STORAGE_BUCKET`
- `JOT_STORAGE_ACCESS_KEY_ID`
- `JOT_STORAGE_SECRET_ACCESS_KEY`
- `JOT_STORAGE_FORCE_PATH_STYLE`
- `JOT_AUTH_MODE`
- `JOT_AUTH_ISSUER`
- `JOT_AUTH_AUDIENCE`
- `JOT_AUTH_CLIENT_ID`
- `JOT_AUTH_CLI_CLIENT_ID`
- `JOT_AUTH_CLI_CLIENT_SECRET`
- `JOT_AUTH_CLIENT_SECRET`
- `JOT_AUTH_COOKIE_SECRET`
- `JOT_AUTH_SESSION_TTL`
- `JOT_AUTH_AUTHORIZE_HD`
- `JOT_LIMITS_FILES_PER_PUSH`
- `JOT_LIMITS_BYTES_PER_FILE`
- `JOT_LIMITS_BYTES_PER_PUSH`

## Operational Notes

- Production GCS bucket access should use the Cloud Run service account.
- The Cloud Run service account needs object admin access on the bucket.
- The same service account needs token-creator permission on itself for `signBlob`.
- The bucket should be in the same region as the Cloud Run service.
- Cloud Run domain mapping and managed certificate provisioning can take time after DNS records are set.
- Public repo docs should use placeholder domains such as `jot.example.com`.
- Private production config lives outside committed repo files.

## Tests And Verification

- Main verification command: `go test ./...`.
- CLI help tests assert examples are present.
- CLI list tests assert full URL output for remote and local list paths.
- Server root tests assert auth redirect behavior, current deploy rendering, search/mine filtering, and no raw blob keys in HTML.
- Storage tests cover GCS endpoint detection and human limit formatting.
- Server config tests cover duration parsing, CLI OAuth client envs, and Go CDK storage URL config.

## Known Follow-Ups

- Consider exposing deploy URLs directly from the list API instead of deriving them in the CLI.
- Consider increasing signed upload URL expiry if real users push very large artifacts over slow connections.
- Consider configurable GC cadence.
- Consider CLI compatibility enforcement against `/ _api/version` data. The endpoint exists, but enforcement is not implemented.
- Consider a real web UI only after CLI workflows are stable.
- Keep `context/progress.md` and `context/project-map.html` updated after architectural or operational changes.
