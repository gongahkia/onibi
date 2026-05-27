# PHASE-04 — Mobile PWA + Web Push (KILLER DEMO)

> Dated 27 May 2026. Depends on `PHASE-03-PLAN-27-MAY.md`. Parallel to `PHASE-05-PLAN-27-MAY.md`.

## Context

This is the moat. cmux has no mobile story. Onibi's pitch — *"pause, your phone buzzes, you approve from the train"* — collapses if the PWA isn't smooth. Phase 04 builds the **mobile PWA** that consumes the protocol defined in PHASE-03, mirroring the terminal output and presenting approvals.

The 90-second screencast plays here:
1. `claude code "delete node_modules"` runs on Mac.
2. Onibi pauses Claude at PreToolUse.
3. **Phone buzzes** (web push) **AND** desktop tab flashes — simultaneously.
4. Tap notification → ApprovalCard opens in PWA.
5. Tap Approve (or Edit to change to `echo skipped`).
6. Claude resumes; output mirror shows result.

iOS PWA push has a sharp edge: requires home-screen install. Onboard around it. Android PWA push works natively.

## Dependencies

- PHASE-03 merged. HTTP+WS server live on `127.0.0.1:17893` minimum; PHASE-05 will make it reachable from the public internet, but Phase 04 can use LAN for dev.
- VAPID keypair generation hook in PHASE-03's pairing logic.
- Tauri app's pairing endpoint emits QR + deep link.

## Deliverables

### D1 — Mobile project layout

**Directory**: `mobile/` (already scaffolded in PHASE-00).

Replace the Vite + React + TS skeleton with the structure below.

```
mobile/
├── public/
│   ├── manifest.webmanifest
│   ├── icon-192.png
│   ├── icon-512.png
│   ├── icon-maskable.png
│   └── apple-touch-icon.png
├── src/
│   ├── main.tsx
│   ├── App.tsx
│   ├── router.tsx                # tanstack-router or react-router
│   ├── lib/
│   │   ├── api.ts               # bearer-auth fetch wrapper
│   │   ├── ws.ts                # auto-reconnect WS client
│   │   ├── push.ts              # service-worker registration + subscribe
│   │   ├── store.ts             # zustand store
│   │   └── crypto.ts            # cert pinning helpers
│   ├── views/
│   │   ├── PairingView.tsx
│   │   ├── InboxView.tsx
│   │   ├── ApprovalCard.tsx
│   │   ├── EditCommandView.tsx
│   │   ├── TerminalMirrorView.tsx
│   │   ├── RunFeedView.tsx
│   │   └── SettingsView.tsx
│   ├── styles/
│   │   ├── global.css
│   │   └── tokens.css
│   └── sw.ts                    # service worker (workbox)
├── package.json
├── vite.config.ts
├── tsconfig.json
└── index.html
```

Dependencies (`mobile/package.json`):
```json
{
  "dependencies": {
    "react": "^19.0.0",
    "react-dom": "^19.0.0",
    "react-router-dom": "^6.28.0",
    "zustand": "^5.0.0",
    "@xterm/xterm": "^5.5.0",
    "@xterm/addon-fit": "^0.10.0",
    "workbox-window": "^7.3.0"
  },
  "devDependencies": {
    "vite-plugin-pwa": "^0.21.0",
    "vitest": "^2.0.0"
  }
}
```

### D2 — Manifest + service worker

**File**: `mobile/public/manifest.webmanifest`
```json
{
  "name": "Onibi",
  "short_name": "Onibi",
  "description": "Approve your AI agent from your phone",
  "start_url": "/",
  "display": "standalone",
  "theme_color": "#0b0e14",
  "background_color": "#0b0e14",
  "icons": [
    { "src": "/icon-192.png", "sizes": "192x192", "type": "image/png" },
    { "src": "/icon-512.png", "sizes": "512x512", "type": "image/png" },
    { "src": "/icon-maskable.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable" }
  ]
}
```

**File**: `mobile/src/sw.ts` (built by vite-plugin-pwa with `strategies: 'injectManifest'`)

Handles:
- `push` event → `self.registration.showNotification(title, { body, tag, data: {approval_id} })`
- `notificationclick` → open `/inbox/:approval_id` in client (focus existing client window if open)
- `message` event from main thread (e.g., to refresh subscription)

### D3 — Pairing flow

**File**: `mobile/src/views/PairingView.tsx`

Two paths:
1. **QR**: user opens onibi-desktop, "Show Pairing QR" → scans with phone. QR encodes a URL like `https://<host>/pair?token=...&fp=...` or a deep link `onibi://pair?host=...&port=...&token=...&fp=...`. On scan, PWA stores credentials.
2. **Deep link tap**: clicking the same link from any browser opens the PWA at `/pair` with the params filled in.

State after pairing:
- `host`, `port`, `bearerToken`, `certFingerprint`, `transports[]` saved to `localStorage` (eventually IndexedDB if persistence becomes an issue).
- Validates by calling `GET /v1/status`; success → navigate to inbox.
- Pin TLS cert fingerprint (when transport is LAN with self-signed). For Cloudflare/Tailscale, trust the public CA chain.

### D4 — Inbox + Run feed views

**File**: `mobile/src/views/InboxView.tsx`

Tabbed:
- **Pending** (default): list of approval-pending events. Each row shows agent icon, tool, command snippet, age, machine.
- **Recent runs**: list of run events (started/completed). Tappable → opens `RunFeedView` (read-only terminal mirror).

Real-time via WS subscription. On approval-pending → push notification fires (handled by service worker even if app backgrounded). On approval-resolved → remove from pending list (or mark "resolved on X").

### D5 — Approval card + Edit

**File**: `mobile/src/views/ApprovalCard.tsx`

Mirrors desktop modal (PHASE-03 D6):
- Agent + tool + cwd
- Command (monospace, wraps)
- Last 50 lines of run output (read from WS or fetch `GET /v1/run/event?session_id=...&limit=50`)
- Three buttons: Allow / Edit / Deny

**File**: `mobile/src/views/EditCommandView.tsx`

Full-screen `<textarea>` (monospace, large). Two buttons:
- Cancel → back to ApprovalCard
- Approve with edits → POST `/v1/approval/:id/decide` with `{decision: "allow", updatedInput: {...}}`

For diff inputs (e.g., Edit tool), show before/after. v1.0 keeps it as a single textarea with the raw input JSON; pretty diff view is a v1.1 polish item.

### D6 — Terminal mirror

**File**: `mobile/src/views/TerminalMirrorView.tsx`

xterm.js read-only:
- Subscribes to WS `pty-output` for the active session.
- Initial fetch: `GET /v1/run/event?session_id=...&include_output=true` returns the rolling buffer.
- No keyboard input wired (read-only).
- Pinch-zoom for font size.
- Disable scroll-to-bottom auto-follow when user has scrolled up; show "↓ jump to live" button.

### D7 — Web push

**File**: `mobile/src/lib/push.ts`

On first inbox visit (and on Settings "Enable notifications"):
1. Request notification permission via `Notification.requestPermission()`.
2. Subscribe to push: `registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: <vapidPublicKey> })`.
3. POST subscription to `/v1/pair` (re-pair with push subscription attached).

Server side (PHASE-03 D2 + D4):
- VAPID keypair generated on first daemon boot, stored in keychain.
- `POST /v1/pair` accepts `pushSubscription` JSON; persists to `devices` table.
- On approval-pending event, fan out: WS broadcast + `web-push` crate POST to all subscribed endpoints with payload `{title, body, approval_id}`.

Crate to add (PHASE-03 retroactively or in this phase):
```toml
web-push = "0.10"
```

If PHASE-03 hasn't added it, add a hand-off note and do it in PHASE-04.

iOS quirk: web push requires PWA installed to home screen (iOS 16.4+). Onboarding page covers this — see D9.

### D8 — Auto-reconnect WS

**File**: `mobile/src/lib/ws.ts`

Exponential backoff: 1s, 2s, 4s, 8s, max 30s. Reset on successful open. Heartbeat ping every 25s (server times out at 90s).

On connect, server replays "approval-pending" for any in-flight approvals so a client that just opened the app sees them.

### D9 — Onboarding

**File**: `mobile/src/views/OnboardingView.tsx`

Shown on first launch. Three steps:
1. Welcome + "what is Onibi" 1-paragraph.
2. iOS-detected: instruct "Add to Home Screen" with screenshot. Android: skip.
3. Pair via QR or deep link.

Persist `onboardingComplete=true` to localStorage.

### D10 — Styles

`mobile/src/styles/global.css` + `tokens.css`:
- Dark default (matches desktop).
- Big tap targets (min 44pt iOS, 48dp Android).
- Safe-area-aware (use `env(safe-area-inset-*)`).
- Toast notifications (custom thin lib or use `sonner`).

Visual goal: looks like a Linear or Cron mobile app. Polished, restrained.

### D11 — Mobile-side test

**File**: `mobile/src/views/ApprovalCard.test.tsx` and a few sibling tests:
- ApprovalCard renders pending event.
- Tapping Allow calls api with correct decision payload.
- WS reconnect after disconnect re-fetches pending list.

### D12 — End-to-end demo script

**File**: `scripts/e2e-mobile-demo.md`

Step-by-step manual run for screencast:
- Start `onibi --headless` on Mac. Note `host` + `token`.
- On Mac/desktop: launch Onibi app; install Claude Code adapter.
- On phone: install PWA from `http://<mac-lan-ip>:17893/m/` (mobile served from same Rust server under `/m/*`).
  - Rust server serves `mobile/dist/` static files under `/m/`. Add a static fileserver in `app/src-tauri/src/server/static.rs` using `tower-http::services::ServeDir`.
- Pair via QR.
- Run `claude code "delete node_modules"` on Mac.
- Phone buzz + desktop flash within 500ms of hook fire.
- Tap notification on phone → Approve → claude completes.

## Exit criteria

1. `mobile/` builds and serves: `pnpm --filter onibi-mobile build` produces `dist/`.
2. Daemon serves `mobile/dist/` at `/m/`.
3. PWA installable to home screen on iOS 16.4+ and Android.
4. Pairing via QR works.
5. **Killer demo runnable** per `scripts/e2e-mobile-demo.md`. Screencast recorded.
6. Web push fires within 500ms of approval-pending event when phone is locked (test on real iPhone + real Android).
7. WS auto-reconnect within 5s of network blip.
8. All mobile vitest cases green; CI builds mobile.

## Out of scope

- Transports (PHASE-05); LAN only for now.
- Native iOS app (v2.0).
- Real-time syntax highlighting of edit-buffer diff (v1.1).
- Multi-account / multi-tailnet UI (v1.2).
- SMS bridge (v1.1).

## Verification commands

```sh
cd /Users/gongahkia/Desktop/coding/projects/onibi
git checkout v1.5/phase-04-mobile
cd mobile
pnpm install --frozen-lockfile
pnpm typecheck
pnpm test
pnpm build
# In another terminal:
cd ../app && pnpm tauri dev
# Open http://<lan>:17893/m/ on phone, install PWA, follow scripts/e2e-mobile-demo.md
```

## Reference reading

- `PHASE-03-PLAN-27-MAY.md` — protocol you're consuming.
- `SPEC.md` v1.0 — endpoint contracts.
- [vite-plugin-pwa docs](https://vite-pwa-org.netlify.app)
- [Workbox push notifications](https://developer.chrome.com/docs/workbox/handling-service-worker-updates)
- [web-push crate](https://docs.rs/web-push)
- [iOS PWA notifications guide](https://webkit.org/blog/13878/web-push-for-web-apps-on-ios-and-ipados/)
- `docs/archive/OnibiWeb/` — reference for prior approach (not imported).

## PR template

Title: `phase-04: mobile pwa + web push (killer demo)`

Body:
```
## Deliverables
- [x] D1 mobile/ scaffold
- [x] D2 manifest + service worker
- [x] D3 PairingView (QR + deep link)
- [x] D4 InboxView (Pending + Recent runs tabs)
- [x] D5 ApprovalCard + EditCommandView
- [x] D6 TerminalMirrorView (read-only xterm)
- [x] D7 web push (VAPID, /v1/pair persistence, web-push fanout)
- [x] D8 auto-reconnect WS client
- [x] D9 OnboardingView (iOS install instructions)
- [x] D10 styles + tap targets
- [x] D11 vitest
- [x] D12 e2e-mobile-demo.md + screencast

## Verification
- [x] mobile build green
- [x] PWA installable on iOS + Android
- [x] Killer demo runs end-to-end (screencast attached)
- [x] Push fires within 500ms on locked phone

Next: PHASE-05 (transports — make the LAN demo work from anywhere).
```
