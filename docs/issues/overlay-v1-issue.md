# GH Issue Draft: Always-On Jot Overlay Injection (v1)

## Summary
Implement an always-on overlay (“Jot menu thingy”) injected into eligible Jot-served HTML pages, providing non-destructive navigation/history/context workflows.

This should behave like a support/chat-style launcher + panel, but focused on deploy navigation and context.

## Goals
- Always-on injection for eligible HTML pages.
- Strong visual/interaction isolation from host content (Shadow DOM + event isolation).
- Fast context-first UX with progressive data hydration.
- Non-destructive interactions only in v1.

## Non-Goals (v1)
- Rollback/delete/destructive actions.
- Telemetry/analytics.
- Settings UI.
- Pagination/"view more" flows.
- Kill switch.

## Product Decisions Locked
- Injection mode: server-side by `jot-server` for Jot-hosted HTML.
- Injection policy: always-on for eligible HTML responses.
- Data model: per-user + global scopes.
- Default user flow: page relevance first (slug history), but reopen to last-open tab from localStorage.
- Launcher position: bottom-right only.
- Panel sizing: fixed.
- Keyboard shortcut: enabled by default (`Cmd/Ctrl+J`).
- Destructive actions: excluded from v1.

## Eligibility Rules (Injection Gate)
Inject only when all are true:
1. Request method is `GET`.
2. Response content type is `text/html`.
3. Route is not system/auth/api/health endpoints.

Exclude all non-HTML content (JSON/CSS/JS/images/fonts/binaries).

## Server Work
- [ ] Add reserved static asset serving under `/_jot/*` for overlay bundle assets.
- [ ] Add HTML response transformation in content-serving path to inject:
  - [ ] mount node (`#jot-overlay-root`)
  - [ ] bootstrap JSON script (`#jot-overlay-bootstrap`)
  - [ ] hashed JS/CSS asset references
- [ ] Ensure no injection for API/auth/health/system endpoints.
- [ ] Add immutable cache headers for hashed assets.

### Bootstrap Payload (authoritative for current page)
```json
{
  "slug": "string|null",
  "deployId": "string|null",
  "title": "string|null",
  "url": "string",
  "createdBy": "string|null",
  "createdAt": "ISO8601|null",
  "tags": ["string"],
  "apiBase": "/_api"
}
```

## Frontend Subproject Work (`web/overlay`)
- [ ] Scaffold TypeScript + React + Vite project.
- [ ] Build hashed production assets for server delivery.
- [ ] Mount overlay in Shadow DOM.
- [ ] Implement launcher + fixed panel layout:
  - [ ] Desktop: right-side panel (~380px width).
  - [ ] Mobile: full-screen/sheet behavior.
- [ ] Implement tabs:
  - [ ] Current slug history (default fallback)
  - [ ] My activity
  - [ ] Global discoverable deploys
  - [ ] Context

## Data Fetching & State
- [ ] On open, fetch in parallel:
  - [ ] `/_api/slugs/{slug}/history` (if slug exists)
  - [ ] `/_api/deploys?mine=1&limit=50`
  - [ ] `/_api/deploys?limit=50`
- [ ] Cache in memory with TTL:
  - [ ] Slug history: 20s
  - [ ] My/global: 45s
- [ ] Abort in-flight requests on panel close.
- [ ] Persist last-open tab in localStorage.

## Context Quick Actions
- [ ] Copy slug
- [ ] Copy deploy ID
- [ ] Copy current URL
- [ ] Jump to slug history tab
- [ ] Ephemeral toast feedback (~1.5s)

## Accessibility Requirements (v1 blocker)
- [ ] Keyboard-focusable launcher with visible focus style.
- [ ] Proper dialog semantics for panel.
- [ ] Focus trap while open.
- [ ] `Esc` closes panel.
- [ ] Focus returns to launcher on close.
- [ ] Labeled controls / ARIA coverage.
- [ ] WCAG AA color contrast.
- [ ] Respect `prefers-reduced-motion`.

## Error/Empty State UX
- [ ] Inline section-level error states (no global crash state).
- [ ] Retry affordance on failed sections.
- [ ] Keep partial data visible if one section fails.
- [ ] Opinionated empty-state copy for each section.

## Testing Tasks
### Go server tests
- [ ] Injects only eligible HTML responses.
- [ ] Never injects into API/auth/health endpoints.
- [ ] Non-HTML passthrough is unchanged.
- [ ] Bootstrap + asset refs are inserted correctly.

### Frontend tests
- [ ] Open/close behavior.
- [ ] Keyboard shortcut behavior (`Cmd/Ctrl+J`, ignore typing contexts).
- [ ] Focus trap + Esc + focus return.
- [ ] LocalStorage tab persistence.
- [ ] Request abort on close.
- [ ] Cache TTL behavior.
- [ ] Copy action toast behavior.

### Integration checks
- [ ] Overlay appears on Jot-served HTML.
- [ ] Does not appear on JSON/static binaries.
- [ ] Works on mobile viewport.

## Acceptance Criteria
- Eligible HTML pages consistently show overlay launcher.
- Panel opens quickly and reliably without host-style collisions.
- Context tab renders from bootstrap without extra initial roundtrip.
- History/activity/global tabs load with resilient partial-failure behavior.
- Keyboard/a11y requirements pass.
- No destructive operations exposed in v1.

## Labels (suggested)
- `feature`
- `frontend`
- `server`
- `ux`
- `a11y`

## Implementation Notes
- Keep route handling aligned with existing server route boundaries (`/_api`, `/_auth`, `_health`, content catch-all).
- Prefer small curated headless primitives over heavy UI kits.
