# PHASE-05 — Transports (Tailscale Funnel + Cloudflare Tunnel + LAN)

> Dated 27 May 2026. Depends on `PHASE-03-PLAN-27-MAY.md`. Parallel to `PHASE-04-PLAN-27-MAY.md`.

## Context

PHASE-04 demos over LAN. Real-world Onibi means phone-on-LTE → daemon-on-home-mac. That requires the daemon to be reachable from the public internet without the user opening firewall ports.

The user explicitly chose to implement **all three transports as equal first-class options**:
1. **Tailscale Funnel** — best for users already on Tailscale (or willing to install).
2. **Cloudflare Tunnel** — free quick-tunnel, no account required, but ephemeral URL.
3. **LAN + self-signed TLS + cert-pinned PWA** — works on home wifi without any third party.

Settings UI lets the user enable any combination. Pairing payload includes all reachable URLs so the PWA can try them in order.

## Dependencies

- PHASE-03 merged. Server binds 127.0.0.1 by default; transports rebind or publish externally.
- Working Tailscale CLI on dev machine (`tailscale` in `$PATH`).
- `cloudflared` available (bundled or installed).

## Deliverables

### D1 — Transport trait

**File**: `app/src-tauri/src/transport/mod.rs`

```rust
#[async_trait::async_trait]
pub trait Transport: Send + Sync {
    fn name(&self) -> &'static str;
    fn requires_external_dep(&self) -> Option<&'static str>;  // e.g., "tailscale CLI"
    async fn start(&self, local_port: u16) -> Result<TransportHandle>;
    async fn status(&self) -> TransportStatus;
}

pub struct TransportHandle {
    pub public_url: Option<String>,    // None for LAN
    pub fingerprint: Option<String>,   // SHA256 of TLS cert, for LAN pinning
    pub shutdown: tokio::sync::oneshot::Sender<()>,
}

pub enum TransportStatus {
    Stopped, Starting, Running { url: Option<String> }, Failed(String),
}
```

Implementations:
- `transport::tailscale::TailscaleFunnel`
- `transport::cloudflared::CloudflareTunnel`
- `transport::lan::LanTransport`

### D2 — Tailscale Funnel

**File**: `app/src-tauri/src/transport/tailscale.rs`

Strategy: shell out to `tailscale` CLI.

```rust
pub struct TailscaleFunnel { /* state */ }

#[async_trait]
impl Transport for TailscaleFunnel {
    fn name(&self) -> &'static str { "tailscale-funnel" }
    fn requires_external_dep(&self) -> Option<&'static str> { Some("tailscale") }

    async fn start(&self, port: u16) -> Result<TransportHandle> {
        // Check tailscale is logged in:
        let status = run("tailscale", &["status", "--json"]).await?;
        // Get our DNS name:
        let name = parse_dns_name(&status)?;  // e.g., "host.tailnet-abcd.ts.net"
        // Start funnel:
        let _ = run("tailscale", &["funnel", "--bg", &port.to_string()]).await?;
        let url = format!("https://{}/", name);
        Ok(TransportHandle { public_url: Some(url), fingerprint: None, shutdown: /* spawn watcher that runs `tailscale funnel reset` on shutdown */ })
    }
    // status: parse `tailscale funnel status --json`
}
```

Edge cases:
- Tailscale not installed → return error with install hint.
- Tailscale not logged in → error with `tailscale up` hint.
- Funnel quota exhausted → surface error to UI; suggest LAN fallback.
- Restart safety: on daemon start, run `tailscale funnel status` and reuse if already configured.

### D3 — Cloudflare Tunnel

**File**: `app/src-tauri/src/transport/cloudflared.rs`

Strategy: spawn `cloudflared tunnel --url http://127.0.0.1:<port>` as a child process. Parse stderr for the `https://<random>.trycloudflare.com` URL.

```rust
pub struct CloudflareTunnel { binary_path: PathBuf }

impl CloudflareTunnel {
    pub fn locate() -> Result<Self> {
        which::which("cloudflared")
            .map(|p| Self { binary_path: p })
            .map_err(|_| anyhow!("cloudflared not on PATH"))
    }
}

#[async_trait]
impl Transport for CloudflareTunnel {
    async fn start(&self, port: u16) -> Result<TransportHandle> {
        let mut child = Command::new(&self.binary_path)
            .args(["tunnel", "--url", &format!("http://127.0.0.1:{}", port), "--no-autoupdate"])
            .stderr(Stdio::piped())
            .spawn()?;
        let stderr = child.stderr.take().unwrap();
        let url = parse_cf_url_from_stderr(stderr).await?;
        let (tx, rx) = oneshot::channel();
        tokio::spawn(async move {
            rx.await.ok();
            let _ = child.kill().await;
        });
        Ok(TransportHandle { public_url: Some(url), fingerprint: None, shutdown: tx })
    }
}
```

Parse logic for cloudflared output: regex match `https://[a-z0-9-]+\.trycloudflare\.com`. First match wins.

Edge cases:
- `cloudflared` not on PATH → return error with download URL hint.
- Quick-tunnel URL changes on each cloudflared restart. Settings UI displays current URL; pairing QR regenerates.

### D4 — LAN with self-signed TLS

**File**: `app/src-tauri/src/transport/lan.rs`

Generate self-signed cert on first run (`rcgen` crate):
```toml
rcgen = "0.13"
rustls = "0.23"
rustls-pemfile = "2"
axum-server = { version = "0.7", features = ["tls-rustls"] }
```

Cert stored at `~/.config/onibi/lan.{crt,key}`. SAN: localhost, 127.0.0.1, and discovered LAN IPs (use `if_addrs` crate).

mDNS broadcast via `mdns-sd` crate: advertise `_onibi._tcp.local.` on port 17893.

Server binds two listeners in LAN mode: 127.0.0.1 (cleartext HTTP for local apps) **and** 0.0.0.0:17893 (TLS).

Fingerprint: SHA256 of DER-encoded cert, hex-encoded. Returned in `TransportHandle.fingerprint` and embedded in pairing payload.

Mobile side (PHASE-04 D7's `crypto.ts`):
- Standard browser fetch can't easily pin self-signed cert. PWA must trust the cert at the OS level.
- For LAN-only flow, instruct user to install the cert on phone (export, AirDrop or QR-as-base64) and trust it in Settings.
- Alternative: bundle mkcert-style root CA generation, install on dev machine; phone trusts via OS profile.
- v1.0 documents the cert install step as a one-time chore for LAN-only path. Most users will use Tailscale or Cloudflare instead. Per the user's explicit choice, LAN must be fully shipping, so document it crisply rather than skip.

### D5 — Transport manager

**File**: `app/src-tauri/src/transport/manager.rs`

```rust
pub struct TransportManager {
    transports: HashMap<&'static str, Box<dyn Transport>>,
    handles: RwLock<HashMap<&'static str, TransportHandle>>,
}

impl TransportManager {
    pub async fn enable(&self, name: &str) -> Result<()>;
    pub async fn disable(&self, name: &str) -> Result<()>;
    pub async fn pairing_payload(&self) -> PairingPayload;  // aggregates all running URLs + fingerprints
    pub async fn status_snapshot(&self) -> Vec<(String, TransportStatus)>;
}
```

Pairing payload (consumed by `GET /v1/qr` and `POST /v1/pair`):
```json
{
  "machine_id": "...",
  "token": "...",
  "vapid_public_key": "...",
  "transports": [
    { "name": "tailscale-funnel", "url": "https://host.tailnet-abcd.ts.net/" },
    { "name": "cloudflared",      "url": "https://random-xyz.trycloudflare.com/" },
    { "name": "lan",              "url": "https://192.168.1.42:17893/", "fingerprint": "sha256:abc..." }
  ]
}
```

PWA tries them in this order; falls back if one returns an error.

### D6 — Settings UI

**File**: `app/src/components/TransportSettings.tsx`

Three toggle switches:
- ☐ Tailscale Funnel  [status: ✓ running at https://...]
- ☐ Cloudflare Tunnel  [status: ⚠ cloudflared not installed — Install]
- ☐ LAN (self-signed)  [status: ✓ running at https://192.168.1.42:17893 (cert fingerprint: sha256:abc...)] [Show install QR]

Multiple can run. Toggling fires Tauri `transport_enable` / `transport_disable`.

Live status polled every 5s (or pushed via Tauri events from the manager).

### D7 — CLI

```
onibi transport list
onibi transport enable <name>
onibi transport disable <name>
onibi transport status
```

For headless servers (PHASE-06), these are the only way to configure.

### D8 — Pairing QR refresh

When transports change, regenerate `GET /v1/qr` payload. Mobile re-pairs on QR change (existing pairing flow handles this — POST `/v1/pair` updates record).

### D9 — Tests

- `tailscale::tests::parse_dns_name` (unit).
- `cloudflared::tests::parse_url_from_stderr` (unit).
- `lan::tests::cert_fingerprint_stability` (cert + key roundtrip).
- `manager::tests::aggregate_pairing` (mock transports return URLs; assert payload contains all).
- Integration: e2e shell script tests enabling each transport on a dev machine that has the deps installed (gated by env vars `ONIBI_TEST_TAILSCALE=1` etc.).

### D10 — Docs

**File**: `docs/transports.md` (new)

Covers:
- Setting up Tailscale (with screenshots).
- Setting up Cloudflare Tunnel (cloudflared install).
- LAN setup + cert-trust per platform (iOS profile install, Android cert install).
- Comparison table: ease, latency, persistence, account-required.
- Troubleshooting: common errors.

## Exit criteria

1. All three transports implement the `Transport` trait and pass unit tests.
2. Manual test matrix:
   - **Tailscale**: phone on LTE, mac on home wifi → reachable via Funnel URL.
   - **Cloudflare**: phone on LTE, mac on home wifi → reachable via trycloudflare.com URL.
   - **LAN**: phone on same wifi as mac → reachable via 192.168.x.y:17893 with cert trusted.
3. Settings UI shows live status for each.
4. Pairing payload aggregates all enabled transport URLs.
5. PWA failover: with two transports up, kill one mid-session; PWA reconnects via the other within 10s.
6. CI green; tests for `parse_dns_name`, `parse_url_from_stderr`, cert fingerprint roundtrip pass.
7. `docs/transports.md` published.

## Out of scope

- Self-hosted relay (v2.0 commercial path).
- Ngrok / other tunnel vendors.
- IPv6-only configurations (handled implicitly by `0.0.0.0` bind; not explicitly tested).
- Mesh VPNs other than Tailscale (Headscale should work; document but don't test).
- Cert auto-renewal (self-signed lifetime: 10 years).
- TURN/STUN for true peer-to-peer.

## Verification commands

```sh
cd /Users/gongahkia/Desktop/coding/projects/onibi
git checkout v1.5/phase-05-transports
cd app/src-tauri
cargo test --workspace
# Manual matrix:
# 1. tailscale funnel
ONIBI_TEST_TAILSCALE=1 ./scripts/test-transports.sh tailscale
# 2. cloudflared
ONIBI_TEST_CLOUDFLARED=1 ./scripts/test-transports.sh cloudflared
# 3. LAN
ONIBI_TEST_LAN=1 ./scripts/test-transports.sh lan
```

## Reference reading

- `PHASE-03-PLAN-27-MAY.md` (server / pairing logic you're extending).
- `PHASE-04-PLAN-27-MAY.md` (PWA consumes the transports[] array).
- [Tailscale Funnel docs](https://tailscale.com/kb/1223/funnel)
- [Cloudflared quick tunnels](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/)
- [rcgen](https://docs.rs/rcgen)
- [axum-server TLS](https://docs.rs/axum-server)
- [mdns-sd](https://docs.rs/mdns-sd)

## PR template

Title: `phase-05: transports (tailscale + cloudflared + LAN)`

Body:
```
## Deliverables
- [x] D1 Transport trait
- [x] D2 TailscaleFunnel
- [x] D3 CloudflareTunnel
- [x] D4 LAN + self-signed TLS + mDNS
- [x] D5 TransportManager (aggregate pairing payload)
- [x] D6 Settings UI with live status
- [x] D7 CLI subcommands
- [x] D8 Pairing QR refresh on transport change
- [x] D9 tests
- [x] D10 docs/transports.md

## Verification
- [x] cargo test green
- [x] Manual matrix all three transports (screencast)
- [x] PWA failover within 10s when one transport dies

Next: PHASE-06 (headless mode + packaging).
```
