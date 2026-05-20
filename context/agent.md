# Agent Context

This file is the fast orientation layer for future agents working on Jot. Read this before editing code, then use `context/progress.md`, `context/project-map.html`, `README.md`, and `concept.md` for detail.

## Vision

Jot is a small, private, self-hosted static-hosting tool for people and agents who generate HTML artifacts and need to share them inside a trusted workspace.

The core experience should stay simple:

```bash
jot push ./report.html --title "Q2 Sales" --summary "Revenue breakdown by region"
```

The result is a private URL. The content is not public, not indexed, and not mixed with marketing pages or a web UI. The CLI is the primary product surface.

## Product Principles

- One command should produce one useful URL.
- Default output should be human-readable and immediately shareable.
- JSON output should be stable enough for agents and scripts.
- Authentication should feel like normal workspace login, not custom token management.
- Production storage should use cloud-native identity instead of long-lived bucket credentials.
- The server should remain stateless on local disk.
- Documentation must avoid private deployment details.
- Keep the implementation boring, inspectable, and easy to operate.

## Current Shape

- Language: Go.
- CLI entrypoint: `cmd/jot`.
- Server entrypoint: `cmd/jot-server`.
- Current version: `0.1.8`.
- Primary install path: Homebrew tap `skorfmann/homebrew-jot`.
- Primary production architecture: Cloud Run plus GCS plus OIDC.
- Local demo architecture: Docker Compose plus Garage plus dev auth mode.

## Architecture In One Pass

The CLI authenticates with OIDC, hashes local files, asks the server which blobs are missing, uploads missing blobs directly to object storage through signed URLs, then asks the server to commit a manifest.

The server validates the manifest, writes immutable deploy metadata, and atomically updates the slug's current pointer. Browser requests resolve the slug's current manifest, verify auth through a signed session cookie or bearer token, and serve the requested blob.

State lives in the bucket:

- `blobs/sha256/<hash>`: immutable file bytes.
- `manifests/<slug>/<deploy-id>.json`: immutable deploy metadata.
- `slugs/<slug>/current`: mutable pointer to current deploy.
- `_trash/<hash>`: soft-deleted unreferenced blobs awaiting GC.

## Key Decisions To Preserve

- Use Go CDK blob storage, not a direct provider lock-in throughout the app.
- Use GCS Application Default Credentials and IAM `signBlob` for GCS signed URLs.
- Do not require GCS HMAC/S3 interoperability credentials in production.
- Use separate Google OAuth clients for web sessions and CLI loopback login.
- Keep browser session cookies stateless.
- Keep slug deploys atomic through conditional pointer writes.
- Keep release binaries as the source for Homebrew formula installs.
- Keep the macOS BSD `jot` shadowing caveat visible.

## Current CLI Surface

- `jot login`
- `jot logout`
- `jot push <path>`
- `jot ls`
- `jot inspect <slug|id>`
- `jot history <slug>`
- `jot rollback <slug> [id]`
- `jot rm <slug>`
- `jot whoami`
- `jot init server`

`jot ls` should show full private URLs in human output. `--json` is intended for agents.

## Current Limits

- 100 files per push.
- 1 GiB per file.
- 3 GiB per push.
- 15 minute signed upload URL expiry.
- 10 manifests retained per slug.
- 7 day trash retention.

The upload size limits are config-driven. If multi-GB uploads are slow in practice, revisit signed URL expiry.

## Repository Hygiene

Do not commit:

- Private production domain.
- Workspace domains.
- OAuth client IDs.
- OAuth client secrets.
- Cloud project IDs.
- Generated local `.jot/` state.
- User-specific deployment config.

Use examples like `jot.example.com`, `example.com`, and `example-project`.

The current untracked `.github/social-preview.png` is intentionally not part of routine code/docs commits unless explicitly requested.

## Common Verification

Run:

```bash
go test ./...
```

For release/install work, also verify:

```bash
brew test skorfmann/jot/jot
jot --help
jot ls --limit 3
```

For server deployment work, verify:

```bash
curl -fsS https://jot.example.com/_health
curl -fsS https://jot.example.com/_api/version
```

Use the real deployment URL only in local commands or secret/config operations, never in committed files.

## Good Next Improvements

- Return deploy URLs directly from `GET /_api/deploys`.
- Add CLI compatibility warnings based on `/_api/version`.
- Make GC cadence configurable.
- Consider configurable signed upload URL expiry.
- Add stronger release automation around Homebrew tap updates when Docker publish is slow.
- Improve large-file smoke tests without committing large fixtures.
