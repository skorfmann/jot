# Jot Implementation Notes

This file tracks implementation decisions, tradeoffs, and spec interpretations that were not fully pinned down in `concept.md`.

## 2026-05-19

- Started from an empty directory containing only `concept.md`, so this implementation scaffolds the full Go project rather than adapting an existing codebase.
- Used `github.com/skorfmann/jot` as the module path because the requested GitHub owner is `skorfmann`.
- The spec describes a single OAuth client whose browser flow uses a client secret, while the CLI discovers only `issuer` and `client_id`. The CLI implementation therefore treats CLI login as a public PKCE client flow. Google Web OAuth clients require a client secret during token exchange, so Google deployments use separate clients: a Web application client for browser sessions and a Desktop app client exposed as `auth.cli_client_id` for CLI loopback PKCE. The browser login flow uses the stable default callback `http://127.0.0.1:50573/callback` instead of a random port.
- For a single-file push, the CLI maps the file to `/index.html` so `jot push ./report.html` produces a useful root URL, matching the headline workflow.
- Header overrides are represented as a JSON object in the manifest, as specified. Since JSON object ordering is not meaningful, matching is deterministic by sorted glob key instead of user insertion order.
- Garbage collection is implemented as a server background loop that starts one minute after boot and then runs every 24 hours. The spec calls it a configurable cron; the first implementation keeps the cadence fixed to avoid introducing another config surface before it is needed.
- `jot rm <slug>` deletes the current pointer and manifest history for the slug. Blob objects are reclaimed by the normal unreferenced-blob GC rather than synchronously deleting blobs during the API request, so large slugs do not make `rm` slow or partially fail midway through shared blob accounting.
- The Docker image is still `FROM scratch`, but it includes CA certificates copied from the build stage because OIDC discovery and JWKS retrieval require HTTPS trust roots.
- The initial Homebrew tap formulas build from a pinned source archive because there is not yet a tagged binary release with checksums. The main repo still includes a release workflow that publishes binary artifacts; after the first release, the tap formulas should be switched to the binary URLs/checksums described in the spec.
- Cut `v0.1.0` and replaced the bootstrap Homebrew formulas with binary-release formulas. The local tap install tests confirmed the formulas now install the published binaries directly and no longer require Go as a build dependency.

## 2026-05-20

- Implemented the overlay as `web/overlay`, a React + TypeScript + Vite subproject, and embedded the built `dist` output through a small Go package in that same directory. This keeps the source project where the issue asked for it while avoiding `go:embed` path limitations.
- Treated the authenticated root deploy index (`/`) as eligible HTML and inject it with `slug`, `deployId`, and deploy metadata set to `null`. The issue excludes system/auth/api/health endpoints but does not explicitly exclude the root HTML page.
- The server buffers only eligible HTML responses so it can inject the mount node, bootstrap JSON, and hashed asset references, then computes an ETag for the transformed HTML. Non-HTML responses keep the existing streaming passthrough path.
- The CSS asset is injected as a hashed preload reference in host HTML, then the overlay script attaches that stylesheet inside the Shadow DOM. This satisfies the hashed CSS reference requirement without applying overlay styles to the host page.
- Mobile sheet styles use both viewport-width and device-width media queries because injected pages may not include their own viewport meta tag.
- The overlay relies on the existing authenticated `/_api/slugs/{slug}/history` and `/_api/deploys` endpoints. No new read APIs were added for v1.
- The overlay does not rewrite user-provided Content-Security-Policy headers. Very restrictive CSP on a deployed page may block the injected module script even though the asset is same-origin.
