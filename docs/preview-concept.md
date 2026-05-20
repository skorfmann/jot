# Server-Generated Previews Concept

## Purpose

Jot pages are private, authenticated static artifacts. That is the core product boundary.

For workflows such as GitHub issues, chat, tickets, and docs, users still need a lightweight visual reference they can paste into another system. A generated screenshot gives that system a useful preview while the actual Jot page remains private.

The preview feature should not make the Jot CLI a GitHub client. The CLI publishes to Jot and prints URLs/Markdown. Other systems consume normal links and images.

## Product Shape

After a deploy, Jot can provide:

```text
Page:
  https://jot.example.com/abc123/

Preview:
  https://jot.example.com/_preview/abc123/01HX...png

Markdown:
  [![Q2 Revenue Report](https://jot.example.com/_preview/abc123/01HX...png)](https://jot.example.com/abc123/)
```

The page URL remains private and requires normal Jot auth.

The preview image has a separate visibility policy. If it should render inline in GitHub or similar tools, GitHub must be able to fetch it without logging into Jot. That means the preview image is either public, signed, or otherwise intentionally shareable.

## Non-Goals

- No GitHub issue API integration in the CLI.
- No GitHub auth in Jot.
- No iframe or script embed inside GitHub issues.
- No automatic exposure of private page content without explicit preview policy.
- No screenshot rendering on the user's laptop as the primary design.

## Visibility Model

Recommended default:

```yaml
preview:
  enabled: false
  public: false
```

Recommended opt-in for shareable previews:

```yaml
preview:
  enabled: true
  public: true
  ttl: 168h
```

Visibility options:

| Mode | GitHub inline image works | Privacy posture | Notes |
| --- | --- | --- | --- |
| Private preview | No | Strongest | Requires Jot auth, useful only inside Jot UI. |
| Public immutable preview | Yes | Intentional content copy | Simple and cacheable, but screenshot is public. |
| Signed expiring preview | Usually yes until expiry | Bounded exposure | Best balance for paste workflows. |
| GitHub-uploaded screenshot | Yes | Copied into GitHub | Requires separate GitHub-aware automation, not Jot CLI. |

For v1, prefer signed expiring previews if expiration/revocation is cheap. Otherwise use explicit public immutable previews and make the tradeoff obvious in CLI output.

## Architecture

Preferred architecture:

```text
jot CLI
  -> uploads deploy

jot-server
  -> writes manifest
  -> updates current pointer
  -> creates preview render job
  -> serves preview PNGs

jot-renderer
  -> runs Chromium/Playwright
  -> opens an immutable render URL with a short-lived render token
  -> takes screenshot
  -> writes PNG to the bucket
```

Keep Chromium out of the main server process if this becomes a durable feature. It keeps the server image small, reduces cold-start cost, and isolates browser runtime risk.

A simpler prototype can render in-process, but it should be behind a feature flag and should not run in the request path.

## Render Target

Render an immutable deploy, not the mutable slug:

```text
/_render/abc123/01HX...
```

The render target resolves the exact manifest ID. This avoids a race where the slug changes while the screenshot is being created.

The rendered HTML can still use normal relative asset paths, resolved against that manifest.

## Render Auth

The renderer should not log in as a user and should not receive a normal browser session.

Use a short-lived internal render token:

```text
/_render/abc123/01HX...?token=<signed-token>
```

Token constraints:

- scoped to one slug and one deploy ID
- short TTL, for example 60 seconds
- valid only for render routes and deploy asset reads
- not accepted by management APIs
- not reusable for normal browser sessions

The render route should bypass the normal OIDC browser flow only when the render token validates.

## Storage Layout

Preview files live in the same bucket as other Jot state:

```text
previews/
  <slug>/
    <deploy-id>.png
    <deploy-id>.json
```

The optional JSON metadata can include:

```json
{
  "slug": "abc123",
  "deploy_id": "01HX...",
  "width": 1440,
  "height": 900,
  "format": "png",
  "created_at": "2026-05-20T12:00:00Z",
  "expires_at": "2026-05-27T12:00:00Z",
  "status": "ready"
}
```

## API Sketch

Deploy response:

```json
{
  "manifest": {},
  "url": "https://jot.example.com/abc123/",
  "preview": {
    "status": "pending",
    "url": "https://jot.example.com/_preview/abc123/01HX...png",
    "markdown": "[![Q2 Revenue Report](https://jot.example.com/_preview/abc123/01HX...png)](https://jot.example.com/abc123/)"
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
  "url": "https://jot.example.com/_preview/abc123/01HX...png",
  "markdown": "[![Q2 Revenue Report](https://jot.example.com/_preview/abc123/01HX...png)](https://jot.example.com/abc123/)"
}
```

Preview image:

```http
GET /_preview/<slug>/<deploy-id>.png
```

If previews are private, this route requires Jot auth. If previews are public/signed, it validates the preview policy instead.

## CLI Behavior

The CLI remains Jot-only:

```bash
jot push ./report.html --title "Q2 Revenue Report" --preview
```

Output:

```text
Published:
  https://jot.example.com/abc123/

Preview:
  https://jot.example.com/_preview/abc123/01HX...png

Markdown:
  [![Q2 Revenue Report](https://jot.example.com/_preview/abc123/01HX...png)](https://jot.example.com/abc123/)
```

If the preview is public or signed:

```text
Note: the preview image can be viewed by anyone with the preview URL.
The full Jot page still requires authentication.
```

## Renderer Details

Use Playwright/Chromium for predictable rendering of modern pages.

Default screenshot settings:

```text
viewport: 1440x900
device_scale_factor: 1
full_page: false
wait_until: networkidle
timeout: 30s
format: png
```

Renderer should block or constrain dangerous behavior:

- no access to metadata services
- no local filesystem reads
- no private network access unless explicitly allowed
- max render time
- max output size
- fixed viewport and no user-provided browser flags

## Job Model

V1 can use a simple background job:

```text
PUT /_api/deploys/<id>
  -> write manifest
  -> update current pointer
  -> enqueue preview render
  -> return response with preview pending
```

For Cloud Run, durable job options:

- Cloud Tasks calling a renderer endpoint
- Pub/Sub event consumed by a renderer service
- Cloud Run Jobs for isolated render execution

Avoid relying only on in-memory queues for production. A server restart should not permanently lose pending previews.

## Failure Behavior

Preview generation must not fail the deploy.

Possible statuses:

```text
disabled
pending
ready
failed
expired
```

If rendering fails, keep the deploy successful and expose the failure through preview status.

Common failure causes:

- page never reaches network idle
- browser crash
- render token expires
- page has external dependencies blocked by network policy
- output exceeds max size

## Open Questions

- Should previews be enabled globally, per deploy, or both?
- Should public previews be immutable forever or expiring by default?
- Should preview URLs include a signed token or rely on unguessable deploy IDs?
- Should a new deploy to the same slug invalidate older previews?
- Should the root dashboard show preview thumbnails?
- Should the overlay be included in screenshots or suppressed during render?

## Recommended V1

1. Add preview config, default off.
2. Add preview storage keys and status API.
3. Add `--preview` to `jot push`.
4. Add server-side render token and immutable render route.
5. Build a separate `jot-renderer` container using Playwright.
6. Store PNG previews in the bucket.
7. Print Markdown in CLI output when preview is ready or pending.
8. Keep GitHub completely out of the CLI and server.
