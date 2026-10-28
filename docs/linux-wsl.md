# Linux and Windows through WSL

On Linux, including Fedora under WSL2, `onibi web` serves the phone cockpit for persistent tmux, Zellij and GNU Screen sessions. You can create sessions, read output, type literal text and keys, interrupt, rename and confirm session termination. Pairing, durable action receipts, attention signals and browser controls use the same implementation as macOS. No coding-agent wrappers or hooks are needed.

Native Ghostty window/tab/pane control remains **macOS-only**. On Linux those controls are hidden, diagnostics mark Ghostty **not applicable**, and native actions are rejected before dispatch. The cockpit does not control unrelated Windows Terminal or PowerShell windows. Native Windows execution is not supported; run the Linux binary inside WSL.

## Start locally

Install at least one supported multiplexer in the Linux distro. For example, `sudo dnf install tmux` on Fedora, or `sudo apt-get install tmux` on Debian/Ubuntu. The default is `auto`, preferring tmux, then Zellij, then Screen; `multiplexer.default` in Onibi's config can select one explicitly.

From this checkout, with the Go toolchain declared in `go.mod`:

```sh
make build
./bin/onibi web doctor
./bin/onibi web
```

Open the printed one-use pairing link, then choose **New session**. Use a program installed inside Linux (`bash`, a coding agent, a build command) and a Linux folder such as `/home/you/project`. The web server starts the sessions daemon automatically unless one is already running. Keep the foreground process open; Ctrl-C stops browser control but leaves multiplexer sessions running. Restart Onibi to reconnect to them.

Microsoft documents [Windows-to-WSL localhost access](https://learn.microsoft.com/en-us/windows/wsl/networking): a Windows browser can normally open the Linux server's printed loopback link. Firewall, forwarding and WSL configuration can affect this. Use the exact printed origin consistently. Do not switch Onibi to a public interface or disable authentication to work around connection problems.

For phone access, follow [private HTTPS and pairing](web.md#private-connection). The Tailscale Serve proxy must actually reach Onibi's Linux loopback listener. Running both in the same distro avoids a Windows/WSL proxy boundary; a Windows-hosted proxy needs its own connectivity check. Local browser tests do not establish that phone/Tailscale routing works.

State lives in `$XDG_DATA_HOME/onibi`, defaulting to `~/.local/share/onibi`. This contains `config.yaml`, the database, the private web administration socket and `logs/web.log`. The daemon intake socket uses `$XDG_RUNTIME_DIR/onibi.sock` when set, otherwise the state directory. Run the CLI and server as the same user with the same XDG settings; do not use `sudo onibi`.

## Linux user service

A systemd **user manager** is required only for background service management, not foreground use. Check it with `systemctl --user show --property=Version --value`. WSL installations without a user manager can continue using `onibi web` in a terminal.

Use a stable installed binary:

```sh
make install
onibi web service install                  # local HTTP, port 8787
# For private HTTPS instead, supply --origin https://your-host.your-tailnet.ts.net
onibi web service start
onibi web pair
onibi web service status
onibi web service restart
onibi web service stop
onibi web service remove
```

Installation writes a private `onibi-web.service` under `$XDG_CONFIG_HOME/systemd/user` (default `~/.config/systemd/user`), reloads the user manager and enables it for future user-manager starts. It does **not** start the process immediately. Start is explicit. Stop leaves it enabled; remove stops and disables it, then removes the unit, preserving state and sessions. A failed stop leaves the unit available for recovery. A fixed loopback port is required. Stop a foreground server using the same state directory before starting the service.

The unit captures `PATH`, `SHELL`, `XDG_DATA_HOME`, `XDG_RUNTIME_DIR` and `XDG_CONFIG_HOME` at installation. To change these or the listener/origin, remove and reinstall it from the intended environment. `onibi web status` checks the actual HTTP process; service status checks systemd. Use `onibi web logs` for application events and `journalctl --user -u onibi-web.service` for startup failures. Background mode does not print pairing secrets; get a fresh link with `onibi web pair`.

The unit deliberately stops only the Onibi process, so multiplexer sessions survive web-service stop/restart. This does **not** make sessions survive host reboot, WSL shutdown or termination of the Linux user session. Onibi does not enable lingering, keep WSL alive, prevent laptop sleep or restart programs after the distro is shut down. The foreground and service paths both need an awake, reachable host.

If systemd is not enabled in WSL, follow [Microsoft's systemd instructions](https://learn.microsoft.com/en-us/windows/wsl/systemd). With a supported WSL version, merge `[boot]` / `systemd=true` into `/etc/wsl.conf` without replacing existing settings. Applying it requires restarting WSL. **Save all work first:** `wsl.exe --shutdown` from Windows terminates running WSL instances and their processes. Onibi never performs this restart for you. A functioning system manager alone does not prove that `systemctl --user` works; check the user manager after reopening the distro.

## Repeatable verification

The normal Go suite is hermetic. Live suites are explicit opt-ins, with short-lived private state directories and multiplexer namespaces; they never target your existing Onibi service or sessions.

```sh
make test                         # Go tests with race detection
go vet ./...
make test-live                    # production CLI + HTTP + installed backends
ONIBI_LIVE_MUX=tmux,zellij,screen ONIBI_LIVE_RACE=1 make test-live
```

The live suite requires Node 22.12+, Go, `/bin/bash`, and at least one backend. By default missing backends are reported as skipped. `ONIBI_LIVE_MUX` makes the named backends mandatory; CI requires all three. It builds and starts the production executable, pairs through real HTTP, and checks Unicode/literal argument and input fidelity, keys, prompts, tmux bells, authentication/CSRF, receipt deduplication, graceful and crash recovery, scrollback, renaming, confirmed kill, adoption and natural exits. It cleans up only its own sessions. Set `ONIBI_LIVE_RACE=1` to instrument the production executable (requires CGO and a C compiler).

For a real browser driving a real multiplexer:

```sh
npm ci --prefix web-tests
(cd web-tests && npx playwright install chromium)
ONIBI_LIVE_MUX=tmux ONIBI_LIVE_RACE=1 make test-live-browser
```

Chromium pairs with the production server, creates a real session, sends Unicode, reads output, interrupts and confirms termination. This is separate from `make test-browser`, which exercises broader browser behavior against simulated terminal backends. CI runs that fixture suite in Chromium and WebKit, plus the live Linux suite in Chromium with all three multiplexers installed. Fedora may lack the shared libraries needed by Playwright's Ubuntu-targeted WebKit build; that is not a passing WebKit result.

To test the generated unit against a running systemd user manager:

```sh
make test-systemd
```

This requires tmux. It links a uniquely named runtime-only test unit, starts the production server, creates a real session, stops/restarts the unit and verifies that the same session survives. It removes its test unit and private tmux server afterward; it does not install or modify `onibi-web.service`. Regular Go tests also verify unit generation, systemd syntax when the parser is installed, mocked lifecycle failures, and source-uninstall ownership checks. The real-user-manager test is opt-in and is not assumed to run on GitHub-hosted CI.

On WSL with Windows interop and `curl.exe` available, `ONIBI_LIVE_WINDOWS=1 make test-live` additionally checks that Windows can reach the production server's authentication boundary over localhost. It does not run a Windows Onibi binary or automate a Windows browser.

Local validation on Fedora 44 / WSL2 covered the production Chromium workflow, tmux 3.7c, Zellij 0.45.1, GNU Screen 5.0.1 and the live systemd user-service lifecycle. It does not establish physical-phone delivery, Tailscale routing, macOS permission behavior or native Windows support.

## Native Windows is a separate port

WSL support uses Linux processes, paths, Unix sockets and multiplexers. A Windows executable needs platform-specific terminal sizing and process handling; authenticated local IPC with Windows access controls; Windows paths and service lifecycle; a supported terminal backend with verified input/capture semantics; and Windows build, integration and browser tests. The current release configuration intentionally publishes macOS and Linux builds only. A Windows browser connecting to WSL is a client of the Linux server, not evidence that this native port exists.
