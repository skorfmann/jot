# Server-Generated Previews Concept

## Purpose

Jot pages are private, authenticated static artifacts. The preview feature adds a shareable screenshot for paste workflows such as GitHub issues, chat, tickets, and docs while keeping the actual Jot page behind normal Jot auth.

The CLI and server stay GitHub-agnostic. Jot publishes a private page, generates a screenshot, and returns regular URLs/Markdown. GitHub or any other tool only consumes normal Markdown image links.

## Locked V1 Decisions

- Preview generation is on by default when required preview config is present.
- Missing required preview config fails startup unless previews are explicitly disabled.
- Every successful deploy gets a preview job automatically.
- Preview rendering is asynchronous and never makes an otherwise valid deploy fail.
- Preview jobs retry up to 3 times, then status becomes `failed`.
- Preview images are shareable through signed URLs.
- Signed URLs expire after 30 days by default.
- Preview PNGs stay stored while the deploy exists; only URLs expire.
- Deleting a slug deletes that slug's preview records and PNGs.
- History pruning deletes previews for pruned deploys.
- Rollback does not regenerate previews.
- No manual preview regeneration API in V1.
- Preview state stays separate from immutable deploy manifests.
- Production V1 uses Cloud Tasks and a separate renderer service.
- Local validation uses the same production-shaped server/renderer split, but without Cloud Tasks.
- The renderer is a separate Node/TypeScript service using Playwright/Chromium.
- The renderer never receives bucket credentials and never writes preview storage directly.
- The renderer posts PNG bytes and metadata back to `jot-server`; `jot-server` writes storage.
- Production renderer requests from Cloud Tasks use OIDC service authentication.
- Production completion callbacks use renderer service OIDC plus a job-scoped completion token.
- Local validation may use signed job tokens without Google OIDC.
- Rendered pages use a dedicated render origin configured with `preview.render_base_url`.
- Render auth uses a short-lived render token plus a short-lived scoped render cookie.
- Render cookies are valid only on render routes/origin, not normal Jot content routes.
- Preview renders suppress the Jot overlay.
- V1 screenshots are HTML-only.
- V1 screenshots render only the deploy root entrypoint.
- Fallback 404 pages are not screenshotted.
- V1 screenshots are fixed first viewport only, default `1440x900`, not full page.
- Renderer waits for `load`, then a 1 second settle delay, with a 30 second hard timeout.
- Renderer may access public internet resources, but must block private/internal/metadata/file access.
- Root dashboard shows preview thumbnails when ready.
- Deploy list / `jot ls` includes preview status only.
- `jot inspect` includes signed preview URL and Markdown when available.

## Product Shape

After a deploy:

```text
Page:
  https://jot.example.com/abc123/

Preview:
  https://jot.example.com/_preview/abc123/01HX.../preview.png?exp=...&sig=...

Markdown:
  [![Q2 Revenue Report](https://jot.example.com/_preview/abc123/01HX.../preview.png?exp=...&sig=...)](https://jot.example.com/abc123/)
```

The page URL remains private and requires normal Jot auth.

The preview image is intentionally shareable. Anyone with a valid signed preview URL can view that screenshot until the URL expires. The full Jot page still requires authentication.

## Non-Goals

- No GitHub issue API integration in the CLI.
- No GitHub auth in Jot.
- No iframe/script embed support for GitHub issues.
- No screenshots generated on the user's laptop as the primary design.
- No manual preview regeneration API in V1.
- No preview generation for non-HTML roots in V1.
- No full-page screenshots in V1.

## Configuration

Preview support is on by default, but startup must fail if preview support is effectively enabled and required fields are missing. Local/demo configs can explicitly disable previews.

Example production config:

```yaml
preview:
  enabled: true
  render_base_url: https://render.jot.example.com
  renderer_url: https://jot-renderer.example.com/render
  url_signing_secret: replace-with-secret
  render_signing_secret: replace-with-secret
  completion_signing_secret: replace-with-secret
  url_ttl: 720h
  render_token_ttl: 60s
  render_cookie_ttl: 60s
  completion_token_ttl: 10m
  max_attempts: 3
  render_timeout: 30s
  settle_delay: 1s
  max_image_bytes: 5242880
  viewport_width: 1440
  viewport_height: 900
  queue:
    driver: cloudtasks
    project: example-project
    location: europe-west4
    name: jot-preview
    service_account: jot-server@example-project.iam.gserviceaccount.com
```

Local validation config can use a direct queue driver:

```yaml
preview:
  enabled: true
  render_base_url: http://render.localhost:8080
  renderer_url: http://127.0.0.1:9090/render
  url_signing_secret: dev-url-secret
  render_signing_secret: dev-render-secret
  completion_signing_secret: dev-completion-secret
  queue:
    driver: direct
```

## Architecture

Production:

```text
jot CLI
  -> uploads deploy

jot-server
  -> writes immutable manifest
  -> updates current pointer
  -> writes preview metadata: pending
  -> enqueues Cloud Task
  -> serves signed preview URLs
  -> receives renderer completion callback
  -> writes preview PNG and final metadata

Cloud Tasks
  -> calls jot-renderer /render with OIDC auth

jot-renderer
  -> runs Playwright/Chromium
  -> opens render URL with short-lived render token
  -> screenshots first viewport
  -> POSTs PNG + metadata to jot-server completion endpoint
```

Local validation:

```text
jot-server
  -> writes preview metadata: pending
  -> dispatches directly to local jot-renderer HTTP endpoint in background

jot-renderer
  -> renders the same way as production
  -> POSTs completion callback to local jot-server
```

This validates the server/renderer split, render auth, render origin, preview storage, and signed preview serving before adding Cloud Tasks-specific deployment work.

## Render Origin

Use an explicit render origin:

```yaml
preview:
  render_base_url: https://render.jot.example.com
```

The render origin maps to `jot-server`, but it serves only render-authenticated immutable deploy routes. It exists so root-absolute asset URLs work correctly:

```html
<link rel="stylesheet" href="/style.css">
<script src="/app.js"></script>
```

When the renderer opens:

```text
https://render.jot.example.com/abc123/01HX.../
```

root-absolute `/style.css` resolves on the render origin and is served from the same immutable deploy.

Requests to the render origin without a valid render token or render cookie return `404` or `403`.

## Render Auth

The renderer does not log in as a user and does not receive a normal Jot browser session.

Initial render request:

```text
https://render.jot.example.com/abc123/01HX.../?token=<render-token>
```

The render token is signed with `preview.render_signing_secret` and scoped to:

- action: `render`
- slug
- deploy ID
- expiry, default 60 seconds

After validating the render token, `jot-server` sets a short-lived render cookie:

```http
Set-Cookie: jot_render=<signed-render-cookie>; Max-Age=60; HttpOnly; Secure; SameSite=Lax
```

The render cookie is scoped to the same slug/deploy ID and accepted only on the render origin/routes. It is not accepted by normal Jot content routes or management APIs.

## Preview URL Signing

Preview URL:

```text
/_preview/<slug>/<deploy-id>/preview.png?exp=<unix>&sig=<hmac>
```

Storage:

```text
previews/<slug>/<deploy-id>/metadata.json
previews/<slug>/<deploy-id>/preview.png
```

The preview signature is exact-path scoped. The signed payload includes:

```text
GET
/_preview/<slug>/<deploy-id>/preview.png
<exp>
```

`GET` and `HEAD` are accepted. `HEAD` validates against the same canonical `GET` signature.

Expired or invalid signatures return `403 Forbidden`.

Signed URLs are generated dynamically by `jot-server`; they are not stored in preview metadata.

## Preview Metadata

Preview metadata is separate from the deploy manifest because manifests are immutable and preview state changes asynchronously.

Metadata path:

```text
previews/<slug>/<deploy-id>/metadata.json
```

Example:

```json
{
  "status": "ready",
  "slug": "abc123",
  "deploy_id": "01HX...",
  "path": "/",
  "image_key": "previews/abc123/01HX.../preview.png",
  "width": 1440,
  "height": 900,
  "format": "png",
  "attempts": 1,
  "created_at": "2026-05-20T12:00:00Z",
  "updated_at": "2026-05-20T12:00:08Z",
  "error_code": null,
  "message": null
}
```

Statuses:

```text
pending
ready
failed
unsupported
```

Internal logs and private metadata may include raw renderer errors. API/UI responses expose sanitized `error_code` and `message` only.

## API Shape

Deploy response while preview is pending:

```json
{
  "manifest": {},
  "url": "https://jot.example.com/abc123/",
  "preview": {
    "status": "pending"
  }
}
```

Preview status:

```http
GET /_api/deploys/<deploy-id>/preview
```

```json
{
  "status": "ready",
  "url": "https://jot.example.com/_preview/abc123/01HX.../preview.png?exp=...&sig=...",
  "markdown": "[![Q2 Revenue Report](https://jot.example.com/_preview/abc123/01HX.../preview.png?exp=...&sig=...)](https://jot.example.com/abc123/)"
}
```

Deploy list / `jot ls` includes preview status only:

```json
{
  "deploys": [
    {
      "id": "01HX...",
      "slug": "abc123",
      "preview": {
        "status": "ready"
      }
    }
  ]
}
```

`jot inspect` includes signed preview URL and Markdown when available.

No manual preview regeneration endpoint exists in V1.

## Completion Callback

The renderer completes a job by POSTing the screenshot to `jot-server`:

```http
POST /_internal/previews/<job-id>/complete
Authorization: Bearer <completion-token>
Content-Type: multipart/form-data
```

Parts:

```text
metadata: application/json
preview: image/png
```

The completion token is signed with `preview.completion_signing_secret` and scoped to:

- action: `preview_complete`
- job ID
- slug
- deploy ID
- expiry, default 10 minutes

Production additionally requires Cloud Run service-to-service OIDC from the configured renderer service account. Local validation can rely on the completion token only.

`jot-server` validates the token, enforces `max_image_bytes`, writes `preview.png`, and writes final `metadata.json`.

## Renderer Behavior

Renderer runtime:

- Node/TypeScript
- Playwright/Chromium
- separate service/package, not part of `jot-server`

Default screenshot settings:

```text
viewport_width: 1440
viewport_height: 900
device_scale_factor: 1
full_page: false
wait_until: load
settle_delay: 1s
timeout: 30s
format: png
```

The renderer opens the immutable deploy root on the render origin. It does not render arbitrary paths in V1.

Renderer network policy:

- allow render origin
- allow public internet resources
- block private RFC1918 ranges
- block localhost/link-local
- block cloud metadata endpoints
- block `file://`
- enforce total render timeout

Preview renders suppress the Jot overlay.

## Root Entrypoint Rules

V1 renders only the deploy root entrypoint.

Supported:

```text
GET /
  -> successful HTML resolution
```

Unsupported:

- non-HTML root response
- missing root entrypoint
- fallback `/404.html` response
- arbitrary secondary pages
- full-page screenshots

If unsupported, preview metadata becomes:

```json
{
  "status": "unsupported",
  "error_code": "unsupported_root",
  "message": "Preview generation supports HTML root pages only."
}
```

## Queue Model

Queue abstraction:

```text
disabled
direct
cloudtasks
```

`direct` is for local validation. It calls the renderer HTTP endpoint from a background worker but still uses the same render token and completion callback flow.

`cloudtasks` is production V1. It creates an HTTP task targeting `jot-renderer /render` with Cloud Tasks OIDC auth.

Cloud Tasks payload includes:

```json
{
  "job_id": "01HX...",
  "slug": "abc123",
  "deploy_id": "01HX...",
  "attempt": 1,
  "render_url": "https://render.jot.example.com/abc123/01HX.../?token=...",
  "completion_url": "https://jot.example.com/_internal/previews/01HX.../complete",
  "completion_token": "..."
}
```

Cloud Tasks handles durable retry scheduling. Jot preview metadata tracks attempts and final status.

## Storage Lifecycle

Preview objects live as long as their deploy manifest lives.

Delete slug:

```text
delete slugs/<slug>/current
delete manifests/<slug>/...
delete previews/<slug>/...
```

Prune history:

```text
delete manifests/<slug>/<old-id>.json
delete previews/<slug>/<old-id>/metadata.json
delete previews/<slug>/<old-id>/preview.png
```

Rollback does not regenerate previews because previews are tied to immutable deploy IDs.

## CLI Behavior

The CLI remains Jot-only.

Default `jot push` output:

```text
Published:
  https://jot.example.com/abc123/

Preview:
  pending
```

Optional wait mode:

```bash
jot push ./report.html --wait-preview
```

If the preview becomes ready before the wait timeout:

```text
Published:
  https://jot.example.com/abc123/

Preview:
  https://jot.example.com/_preview/abc123/01HX.../preview.png?exp=...&sig=...

Markdown:
  [![Q2 Revenue Report](https://jot.example.com/_preview/abc123/01HX.../preview.png?exp=...&sig=...)](https://jot.example.com/abc123/)

Note: the preview image can be viewed by anyone with the preview URL until it expires.
The full Jot page still requires authentication.
```

`jot ls` shows preview status only.

`jot inspect <slug|id>` shows preview status, signed URL, and Markdown when ready.

## Dashboard Behavior

The authenticated root dashboard shows preview thumbnails by default when status is `ready`.

Fallback states:

```text
pending     -> pending badge
failed      -> failed badge with sanitized message
unsupported -> no preview badge
```

The dashboard generates fresh signed URLs at render time, so dashboard thumbnails should not expire while viewing the page.

## Implementation Sequence

This is one V1 milestone, but implementation should proceed in dependency order:

1. Add preview config and validation.
2. Add preview metadata types and storage methods.
3. Add URL/render/completion token signing helpers.
4. Refactor content serving to support render options:
   - immutable deploy ID
   - no overlay
   - no fallback 404 screenshot
5. Add render-origin route handling in `jot-server`.
6. Add preview image route with signed URL validation.
7. Add completion callback endpoint.
8. Add queue abstraction and direct local driver.
9. Add Node/TypeScript `renderer/` service using Playwright.
10. Validate locally with two processes:
    - `jot-server`
    - `jot-renderer`
11. Add Cloud Tasks queue driver.
12. Add renderer container and GCP deployment docs.
13. Update CLI outputs:
    - push preview status
    - optional `--wait-preview`
    - inspect signed URL/Markdown
14. Update root dashboard thumbnails.
15. Deploy production Cloud Tasks + renderer service.

## Open Follow-Ups

- Support non-HTML root previews.
- Support explicit preview path selection.
- Support full-page screenshots.
- Support manual preview regeneration.
- Support WebP or thumbnail variants.
- Add key rotation with current/previous preview signing secrets.
- Add private/internal-only render networking once infrastructure warrants it.
