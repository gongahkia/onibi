# Web Push

Onibi web push sends approval notifications to the installed phone web app. Browser tabs are not enough on iOS.

## Prerequisites

- HTTPS must be valid end-to-end for the app, `/manifest.webmanifest`, and `/sw.js`. Do not depend on an HTTP to HTTPS redirect for the service worker path.
- The manifest must use `display: "standalone"`.
- The user must add Onibi to the Home Screen and launch it from the installed icon.
- `Notification.requestPermission()` must run from a user tap. In Onibi, tap `PUSH` in the cockpit toolbar.
- `PushManager` must exist in the installed app. Safari tabs on iOS do not expose it.

## Flow

1. Pair the browser as owner.
2. Add the app to the Home Screen.
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
| VAPID private key | encrypted KV key `push_vapid_priv_enc` |
| VAPID public key | KV key `push_vapid_pub` |
| push endpoint | encrypted `push_subscriptions.endpoint_enc` |
| push `p256dh` key | encrypted `push_subscriptions.p256dh_enc` |
| push `auth` key | encrypted `push_subscriptions.auth_enc` |

On each approval request, Onibi sends a Web Push payload with TTL `30`, urgency `high`, and a deep link to `/s/<session_id>?approval=<id>`. `410 Gone` responses delete the subscription.

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
