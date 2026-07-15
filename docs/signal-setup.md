# Signal setup

Onibi's Signal client talks to a local `signal-cli` HTTP JSON-RPC daemon. `signal-cli` is unofficial Signal infrastructure; keep it current because old releases can stop working when Signal server behavior changes.

Sources:

- <https://github.com/AsamK/signal-cli>
- <https://github.com/AsamK/signal-cli/blob/master/man/signal-cli-jsonrpc.5.adoc>
- <https://formulae.brew.sh/formula/signal-cli>

## Install

macOS:

```bash
brew install signal-cli
signal-cli --version
```

Homebrew listed `signal-cli` stable `0.14.5` on July 8, 2026. Check the formula page before setup if you need the current version.

## Link a phone

Start the daemon without `-a` so the JSON-RPC API exposes account-link commands:

```bash
signal-cli daemon --http=127.0.0.1:6001
```

In another shell, request a link URI:

```bash
curl -s http://127.0.0.1:6001/api/v1/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"startLink","id":"link-1"}'
```

The response contains `result.deviceLinkUri`, for example `sgnl://linkdevice?...`. Encode that URI as a QR code and scan it from Signal mobile under linked devices:

```bash
brew install qrencode
qrencode -t ansiutf8 'sgnl://linkdevice?...'
```

Finish provisioning after the phone scans the QR:

```bash
curl -s http://127.0.0.1:6001/api/v1/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"finishLink","id":"link-2","params":{"deviceLinkUri":"sgnl://linkdevice?...","deviceName":"onibi-mac"}}'
```

`finishLink` can also be sent immediately after `startLink`; the official JSON-RPC docs say it waits for the primary device.

## Run daemon

Single account:

```bash
signal-cli -a +15551234567 daemon --http=127.0.0.1:6001
```

Multi-account:

```bash
signal-cli daemon --http=127.0.0.1:6001
```

With multi-account mode, Onibi sends the account in each JSON-RPC `params` object:

```bash
ONIBI_SIGNAL_RPC_URL=http://127.0.0.1:6001
ONIBI_SIGNAL_ACCOUNT=+15551234567
ONIBI_SIGNAL_RECIPIENT=+15557654321
```

Optional:

```bash
ONIBI_SIGNAL_RECIPIENTS=+15557654321,+15559876543
ONIBI_SIGNAL_GROUP_ID=<base64-group-id>
ONIBI_SIGNAL_OWNER=+15557654321
```

`ONIBI_SIGNAL_OWNER` restricts inbound text/reactions to one source. If it is unset and exactly one recipient is configured, Onibi only accepts events from that recipient. For group use, set `ONIBI_SIGNAL_OWNER` when you need owner-only control.

Start Onibi:

```bash
onibi config set experimental.providers true
onibi up --transport=signal
```

Onibi sends approval prompts to the configured recipient or group. React `👍` or `✅` to approve, react `👎` or `❌` to deny, and send plain text messages to write to the active PTY.

Check the daemon:

```bash
curl -i http://127.0.0.1:6001/api/v1/check
```

Send a smoke message:

```bash
curl -s http://127.0.0.1:6001/api/v1/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"send","id":"send-1","params":{"account":"+15551234567","recipient":["+15557654321"],"message":"onibi signal smoke"}}'
```

Tail incoming events:

```bash
curl -N http://127.0.0.1:6001/api/v1/events
```

If the daemon runs with `--receive-mode=manual`, call `subscribeReceive` first:

```bash
curl -s http://127.0.0.1:6001/api/v1/rpc \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"subscribeReceive","id":"sub-1","params":{"account":"+15551234567"}}'
```

## Onibi tests

Mock parity:

```bash
go test -race ./internal/signal -run TestParityAxes
```

Live smoke:

```bash
ONIBI_LIVE_SIGNAL=1 ONIBI_SIGNAL_RPC_URL=http://127.0.0.1:6001 ONIBI_SIGNAL_ACCOUNT=+15551234567 ONIBI_SIGNAL_RECIPIENT=+15557654321 go test ./internal/signal -run LiveSignal
```

Set `ONIBI_SIGNAL_STREAM=1` only when you can send an inbound Signal message during the test window.

## Security

Bind `--http` to `127.0.0.1`; do not expose the JSON-RPC daemon to a LAN or public tunnel. The API can send Signal messages and read incoming events for linked accounts.

`signal-cli` stores passwords and cryptographic keys under:

```text
$XDG_DATA_HOME/signal-cli/data/
$HOME/.local/share/signal-cli/data/
```

Protect the OS user account and filesystem permissions for those directories.
