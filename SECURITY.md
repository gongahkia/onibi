# Security

Onibi controls existing Ghostty terminals, and the daemon's persistent multiplexer sessions when `onibi start` is running, through a paired private web interface. Treat browser pairing links and sessions, the state directory, and the intake socket as sensitive. Use `onibi web devices revoke-all` locally while the web server is running to revoke browser grants and any unused pairing link. Stop the web process or `onibi web service stop` to cut off control while it is stopped. Restart alone preserves explicitly remembered devices.

If you find a vulnerability, please avoid posting exploit details or tokens in a public issue. Use GitHub's private vulnerability reporting for this repository if available, or contact the maintainer privately through the GitHub profile associated with it. If neither channel is available, open a public issue requesting a private contact channel without including the vulnerability details. Include the affected version or commit, reproduction steps, impact, and whether a pairing link or browser credential may have been exposed. If one is exposed, run `onibi web devices revoke-all` locally.

Security fixes are provided on a best-effort basis. There is no guaranteed response or support window. See [THREAT-MODEL.md](THREAT-MODEL.md) for the intended trust boundary and residual risks.
