# Transport Smoke

Default CI is hermetic. Run this checklist on the actual Mac and paired phone before claiming transport readiness.

## LAN and hotspot

1. Run `onibi up --transport=lan`.
2. Install and fully trust the printed local CA profile on the phone.
3. Scan the single-use pair QR and confirm the terminal loads.
4. Send text, `Esc`, arrows, `Ctrl-C`, and paste; confirm the managed terminal receives each input.
5. Trigger one Claude Code approval, approve it, then trigger another and deny it.
6. Stop Onibi and confirm the pair URL no longer works.

Repeat with an iPhone hotspot when the normal Wi-Fi blocks device-to-device traffic.

## Tailscale and other web paths

1. Confirm the phone can reach the selected network endpoint.
2. Start Onibi with the selected `--transport` mode and scan the newly printed pair URL.
3. Repeat the terminal and approval checks above.
4. Confirm a user without the pair URL and owner session cannot operate the cockpit.

Network reachability is not authorization. Onibi does not provide remote-host management, fleet enrollment, chat bridges, notification providers, or team control through these paths.
