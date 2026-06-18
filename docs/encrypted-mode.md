# Encrypted Mode

Encrypted mode is for owners who do not want Telegram bot chat storage to hold approval inputs, session output, prompts, or Mini App decisions in plaintext. Typical cases include regulated work, journalism, incident response, sensitive code review, and private client repos.

It does not make Telegram disappear from the path. Telegram still sees bot identity, owner account, timing, message size, button metadata, and the hosted Mini App page. The local OS user and the owner Telegram account remain trusted.

## Modes

`telegram.encrypted_mode` accepts:

| value | behavior |
|---|---|
| `off` | Plain Telegram messages and callbacks. |
| `ask` | Encrypted approval path. Current daemon code handles this like `on`. |
| `on` | Encrypted approvals, output, prompt acknowledgements, logs/status/session lists, and Mini App actions. |

Enable it with:

```bash
onibi setup --enable-encrypted-mode --encrypted-mode on
```

After setup creates and pairs the seed, switch the mode explicitly with:

```bash
onibi config --set telegram.encrypted_mode on
```

## Envelope

The daemon and Mini App share a 32-byte seed. Setup stores the daemon copy in the active Onibi secret backend: macOS Keychain, Linux Secret Service, or the `0600` `.env` fallback. The Mini App stores the device copy in Telegram WebApp SecureStorage.

Each encrypted item is an envelope:

| field | meaning |
|---|---|
| `v` | envelope version, currently `1` |
| `kind` | plaintext type, such as `approval`, `image`, `secure`, or `action` |
| `exp` | Unix expiry timestamp |
| `nonce` | base64url AES-GCM nonce |
| `ct` | base64url ciphertext |

Crypto path:

1. Decode the 32-byte seed from base64url.
2. Derive a 32-byte AES key with HKDF-SHA256.
3. HKDF salt is `onibi-envelope-v1`; info is `telegram-mini-app`.
4. Encrypt JSON plaintext with AES-256-GCM and a random 12-byte nonce.
5. Authenticate AAD as `v=1;kind=<kind>;exp=<unix>`.
6. Base64url-encode the whole wire JSON.

Daemon-to-Mini-App envelopes currently use a 24-hour TTL. Mini App action envelopes sent back to the daemon use a 5-minute TTL.

## Mini App

The Mini App is the static file at [`docs/miniapp/index.html`](./miniapp/index.html). It decrypts incoming envelopes in the Telegram client, renders approval/output/prompt controls, encrypts decisions back as `kind=action`, and sends them with `tg.sendData`.

Use the default hosted URL or self-host the static file from an HTTPS origin you control:

```bash
onibi setup --enable-encrypted-mode --encrypted-mode on --mini-app-url https://example.com/onibi/miniapp/
```

The Mini App URL must use HTTPS. Setup prints a seed URL and QR code. Open or scan that inside Telegram so SecureStorage can save the seed on the device.

If the encrypted item URL would be too long, the daemon sends a `.enc` document and an Open Mini App button. Paste or import the `.enc` content in the Mini App.

## UX Changes

Plaintext command paths are refused while encrypted mode is enabled:

| plaintext path | encrypted replacement |
|---|---|
| `/prompt <text>` | `/secure`, then send from the Mini App |
| `/send <text>` or `//<text>` | `/secure`, then send from the Mini App |
| `/editprompt <id> <text>` | `/secure` flow |
| `/rename <id> <name>` | `/secure` flow |
| reply-to-approval edit text | Mini App Edit action |

`/secure` opens encrypted controls for prompt entry, target selection, interrupt, kill, and approval decisions.

## Rotation

`onibi setup --enable-encrypted-mode` creates a seed when none exists and reuses the existing seed when one is already stored. To rotate, remove `envelope_seed_b64` from the active Onibi secret backend first, then run setup again and re-pair each Telegram device through the printed seed URL or QR code.

For the `.env` fallback backend, remove the `envelope_seed_b64` entry from the Onibi env file. For Keychain or Secret Service, remove the same key from service `sh.onibi.daemon` with the OS secret manager.

Old encrypted Telegram messages require the old seed. Keep a backup if you need to decrypt old items after rotation.

## Non-Defenses

- Same-user local compromise. The daemon-side seed is readable by the trusted OS user once the secret backend is unlocked.
- Owner Telegram account compromise. The owner can still open the Mini App on paired devices and control Onibi.
- Paired-device compromise. Anyone with access to a device that has the SecureStorage seed can decrypt current envelopes for that seed.
- Mini App host compromise. Served JavaScript can read plaintext after decrypt and before encrypt.
- Telegram metadata. Encrypted mode does not hide bot account, owner account, delivery timing, message size, or that an Onibi interaction happened.
- Plaintext already typed into Telegram before encrypted mode was enabled.
