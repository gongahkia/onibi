# Web Push

Onibi web push sends approval notifications to the installed phone web app. Browser tabs are not enough on iOS.
Install steps live in [`pwa-install.md`](./pwa-install.md).

## Prerequisites

- HTTPS must be valid end-to-end for the app, `/manifest.webmanifest`, and `/sw.js`. Do not depend on an HTTP to HTTPS redirect for the service worker path.
- The manifest must use `display: "standalone"`.
- The user must add Onibi to the Home Screen and launch it from the installed icon.
- `Notification.requestPermission()` must run from a user tap. In Onibi, tap `PUSH` in the cockpit toolbar.
- `PushManager` must exist in the installed app. Safari tabs on iOS do not expose it.

## Flow

1. Pair the browser as owner.
2. Add the app to the Home Screen; see [`pwa-install.md`](./pwa-install.md).
3. Open Onibi from the Home Screen icon.
4. Tap `PUSH`.

The client fetches `GET /push/vapid-public-key`, requests notification permission, calls:

```js
registration.pushManager.subscribe({
  userVisibleOnly: true,
  applicationServerKey
})
```

Then it posts the subscription JSON to `POST /push/subscribe`.

The daemon stores:

| item | storage |
|---|---|
| VAPID keypair | OS secret item `onibi.push.vapid.v1` |
| VAPID public key | KV key `push_vapid_pub` |
| push endpoint | encrypted `push_subscriptions.endpoint_enc` |
| push `p256dh` key | encrypted `push_subscriptions.p256dh_enc` |
| push `auth` key | encrypted `push_subscriptions.auth_enc` |

On each approval request, Onibi sends a Web Push payload with TTL `30`, urgency `high`, and a deep link to `/s/<session_id>?approval=<id>`. `410 Gone` responses delete the subscription.

## VAPID Rotation

Rotate VAPID keys annually, when a machine is decommissioned, or immediately after any suspected compromise of the local OS account, keychain, fallback secret file, or Onibi state directory. The VAPID key authorizes Onibi to send to browser push endpoints; a stolen private key can send notifications to devices that subscribed under that public key.

Onibi stores the current VAPID keypair in the OS secret backend as `onibi.push.vapid.v1`, with dotenv fallback on hosts without a usable keychain or Secret Service. SQLite keeps the public key in `push_vapid_pub` so the installed web app can subscribe, plus encrypted subscription endpoints and browser keys.

Run:

```sh
onibi push rotate
```

Rotation generates a fresh VAPID keypair, updates `push_vapid_pub`, and deletes all existing `push_subscriptions` rows. Existing browser subscriptions were created for the old public key, so reopen the installed Home Screen app and tap `PUSH` to subscribe again. The app also compares the browser subscription key against `/push/vapid-public-key` on refresh and replaces stale subscriptions after rotation.

Check state with:

```sh
onibi doctor --push
```

The push doctor verifies the keyring VAPID item, the SQLite public key, legacy DB-stored private key migration state, and subscription count.

## iOS Constraints

- No silent push: every service-worker `push` event must call `showNotification()`.
- No background sync: do not rely on deferred sync to repair subscriptions.
- Service workers can be unreliable after reboot. Onibi refreshes the subscription on app open when the user previously enabled push.
- EU DMA caveat: if the installed standalone web app path is unavailable or `PushManager` is absent, browser push is unsupported on that device.

## Troubleshooting

No notification:

- Confirm the app was launched from the Home Screen, not a Safari tab.
- Confirm notification permission is granted in iOS Settings.
- Open the app once after reboot to refresh the subscription.
- Check `GET /push/vapid-public-key` returns `{"key":"..."}`.
- Check `POST /push/subscribe` returns `204`.
- Check daemon logs for Web Push send errors.
- If logs show `410 Gone`, the subscription was deleted; reopen the installed app and tap `PUSH`.

Permission prompt never appears:

- Confirm the `PUSH` button was tapped in the installed app.
- Confirm `window.matchMedia("(display-mode: standalone)").matches` is true.
- Confirm `PushManager` exists in the app context.

Service worker missing:

- Confirm `/sw.js` is served over HTTPS with no redirect.
- Confirm the trusted local CA profile is installed and fully trusted on iPhone.
