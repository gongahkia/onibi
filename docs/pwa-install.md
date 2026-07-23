# PWA Install

Onibi's phone cockpit can be installed as a Home Screen web app. This is required
for iOS Web Push; Safari tabs are not enough.

## Requirements

- Open Onibi over trusted HTTPS.
- Pair the browser as owner before installing.
- Keep `/manifest.webmanifest`, `/sw.js`, and app assets on the same origin.
- Manifest fields must include `name`, `short_name`, `start_url`, `display`,
  `theme_color`, `background_color`, 192px and 512px PNG icons, and maskable icons.

Current manifest source: `frontend/public/manifest.webmanifest`.

The service worker caches only same-origin, query-free `GET /assets/` responses. It does not cache pair URLs, HTML, API responses, WebSocket traffic, or session/push payloads.

For a separately run Onibi host, open that host directly and add its own browser bookmark or Home Screen entry. The cockpit does not keep, import, or transfer a host directory; existing browser-side host registry data is ignored.

## iPhone And iPad

1. Run `onibi start`.
2. Transfer and fully trust the printed `onibi-local-ca.mobileconfig` through
   a channel you control.
3. Pair from Safari or a browser that offers Add to Home Screen.
4. Use Share, then Add to Home Screen.
5. Confirm the name `Onibi` and the launcher icon.
6. Launch Onibi from the Home Screen icon.

Expected:

- The app opens without Safari browser chrome.
- `window.matchMedia("(display-mode: standalone)").matches` is `true`.
- The toolbar `PUSH` button can request notification permission from a user tap.

If Add to Home Screen is missing, confirm the page was loaded over trusted
HTTPS and use Safari. If the installed app opens as a normal tab, delete the
icon, reopen the paired Safari page, and add it again.

## Android

1. Run `onibi start`.
2. Transfer and trust the printed `onibi-local-ca.crt` through Android system
   Security settings.
3. Pair from Chrome.
4. Open the Chrome menu.
5. Tap Add to Home screen or Install app.
6. Confirm the name `Onibi` and the launcher icon.
7. Launch Onibi from the installed icon.

Expected:

- The app opens in standalone display mode.
- The icon uses the 512px Onibi PNG or maskable variant.
- The service worker is active and `/sw.js` is served with JavaScript content type.

## Audit

Local lint:

```bash
cd frontend
npm test -- --run src/__tests__/manifest.test.ts
npm run build
```

Manual device checks:

```js
window.matchMedia("(display-mode: standalone)").matches
```

Lighthouse target: PWA score 90 or higher after the app is paired and launched
over trusted HTTPS.

## Troubleshooting

Install prompt missing:

- Confirm `/manifest.webmanifest` returns JSON from the paired origin.
- Confirm the manifest has `display: "standalone"` and 192px/512px PNG icons.
- Confirm HTTPS is trusted before adding to Home Screen.

Push unavailable:

- Confirm the app was launched from the installed icon.
- Confirm `PushManager` exists in the installed app context.
- Reopen the app after iOS reboot before expecting push notifications.
