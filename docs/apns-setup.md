# APNs setup

Onibi APNs sends notify-only approval alerts through Apple Push Notification service using token-based provider authentication.

Sources:

- <https://developer.apple.com/help/account/keys/create-a-private-key/>
- <https://developer.apple.com/help/account/identifiers/enable-app-capabilities/>
- <https://developer.apple.com/support/compare-memberships/>
- <https://developer.apple.com/library/archive/documentation/NetworkingInternet/Conceptual/RemoteNotificationsPG/CommunicatingwithAPNs.html>
- <https://developer.apple.com/library/archive/documentation/NetworkingInternet/Conceptual/RemoteNotificationsPG/CreatingtheNotificationPayload.html>
- <https://developer.apple.com/news/?id=wy4tb0uo>
- <https://github.com/sideshow/apns2>

## Requirements

- Apple Developer Program access to Certificates, Identifiers & Profiles.
- Account Holder or Admin role for key creation.
- A native iOS/macOS app bundle with Push Notifications enabled.
- The app's APNs device token from a development, TestFlight, ad hoc, enterprise, or App Store install.

PWA-only Onibi cannot obtain a native APNs device token. Use web push unless you have a companion/native app that registers with APNs and hands you the token.

Apple Developer Program enrollment is annual paid membership unless your organization has a fee waiver. Apple lists the annual Apple Developer Program fee as 99 USD in its enrollment docs as of July 8, 2026; verify regional pricing during enrollment.

## Create Key

In Apple Developer:

1. Open Certificates, Identifiers & Profiles.
2. Open Keys and create a private key.
3. Select Apple Push Notification service.
4. Choose the environment configuration and key type.
5. For topic-specific keys, select the bundle ID topic.
6. Download the `.p8` file once and store it securely.

Apple gives you:

- Key ID, for example `ABC123DEFG`.
- Team ID, from account membership.
- `.p8` private key file.

## Enable App

In Identifiers:

1. Open the App ID for the native companion app.
2. Enable Push Notifications.
3. Make sure the bundle ID exactly matches `ONIBI_APNS_TOPIC`.
4. Rebuild/reinstall the app with a provisioning profile that includes Push Notifications.
5. Register for remote notifications in the app and copy the APNs device token.

APNs rejects pushes when the token, environment, and topic do not match.

## Configure Onibi

Production or TestFlight/App Store install:

```bash
export ONIBI_APNS_KEY_PATH=/secure/path/AuthKey_ABC123DEFG.p8
export ONIBI_APNS_KEY_ID=ABC123DEFG
export ONIBI_APNS_TEAM_ID=TEAM123456
export ONIBI_APNS_TOPIC=com.example.OnibiCompanion
export ONIBI_APNS_DEVICE_TOKEN=<hex-device-token>
export ONIBI_APNS_ENV=production
```

Development build from Xcode:

```bash
export ONIBI_APNS_ENV=development
```

Run:

```bash
onibi up --transport=apns
```

If APNs env is absent or incomplete in non-APNs modes, Onibi falls back to the existing web-push notifier.

## Verify

Mock shape/race test:

```bash
go test -race ./internal/apns -run TestPushShape
```

Live APNs smoke:

```bash
ONIBI_LIVE_APNS=1 go test ./internal/apns -run LiveAPNs
```

Doctor:

```bash
onibi doctor --transport=apns --offline
ONIBI_DOCTOR_LIVE=1 onibi doctor --transport=apns
```

APNs accepts payloads up to 4096 bytes for regular remote notifications. Onibi rejects larger APNs payloads before sending.

## Security

- Treat the `.p8` key like a deploy key for every configured APNs topic it can access.
- Store the key outside the repo with owner-only permissions.
- Prefer topic-specific keys when available.
- Revoke and replace the key in Apple Developer if the file leaks.
- Do not log full device tokens in shared artifacts.
