# Jot Implementation Notes

This file tracks implementation decisions, tradeoffs, and spec interpretations that were not fully pinned down in `concept.md`.

## 2026-05-19

- Started from an empty directory containing only `concept.md`, so this implementation scaffolds the full Go project rather than adapting an existing codebase.
- Used `github.com/skorfmann/jot` as the module path because the requested GitHub owner is `skorfmann`.
- The spec describes a single OAuth client whose browser flow uses a client secret, while the CLI discovers only `issuer` and `client_id`. The CLI implementation therefore treats CLI login as a public PKCE client flow. If the IdP requires a client secret for loopback token exchange, operators should create/use an OIDC client type that permits public PKCE loopback and register `http://localhost`.
- For a single-file push, the CLI maps the file to `/index.html` so `jot push ./report.html` produces a useful root URL, matching the headline workflow.
- Header overrides are represented as a JSON object in the manifest, as specified. Since JSON object ordering is not meaningful, matching is deterministic by sorted glob key instead of user insertion order.
- Garbage collection is implemented as a server background loop that starts one minute after boot and then runs every 24 hours. The spec calls it a configurable cron; the first implementation keeps the cadence fixed to avoid introducing another config surface before it is needed.
- `jot rm <slug>` deletes the current pointer and manifest history for the slug. Blob objects are reclaimed by the normal unreferenced-blob GC rather than synchronously deleting blobs during the API request, so large slugs do not make `rm` slow or partially fail midway through shared blob accounting.
- The Docker image is still `FROM scratch`, but it includes CA certificates copied from the build stage because OIDC discovery and JWKS retrieval require HTTPS trust roots.
