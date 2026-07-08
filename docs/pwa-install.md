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

## iPhone And iPad

1. Run `onibi up`.
2. Install and fully trust `onibi-local-ca.mobileconfig` if Safari warns about HTTPS.
3. Pair from Safari.
4. Tap Share.
5. Tap Add to Home Screen.
6. Confirm the name `Onibi` and the launcher icon.
7. Launch Onibi from the Home Screen icon.

Expected:

- The app opens without Safari browser chrome.
- `window.matchMedia("(display-mode: standalone)").matches` is `true`.
- The toolbar `PUSH` button can request notification permission from a user tap.

If Add to Home Screen is missing, confirm the page was loaded in Safari over
trusted HTTPS. If the installed app opens as a normal tab, delete the icon,
reopen the paired Safari page, and add it again.

## Android

1. Run `onibi up`.
2. Pair from Chrome.
3. Open the Chrome menu.
4. Tap Add to Home screen or Install app.
5. Confirm the name `Onibi` and the launcher icon.
6. Launch Onibi from the installed icon.

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
