# Security

Onibi controls local terminal processes from a Telegram bot. Treat the bot token, paired Telegram account, state directory, and intake socket as sensitive.

If you find a vulnerability, please avoid posting exploit details or tokens in a public issue. Use GitHub's private vulnerability reporting for this repository if available, or contact the maintainer privately through the GitHub profile associated with it. If neither channel is available, open a public issue requesting a private contact channel without including the vulnerability details. Include the affected version or commit, reproduction steps, impact, and whether a token may have been exposed. If a token is exposed, rotate it with BotFather immediately and run `onibi telegram disable` locally.

Security fixes are provided on a best-effort basis. There is no guaranteed response or support window. See [THREAT-MODEL.md](THREAT-MODEL.md) for the intended trust boundary and residual risks.
