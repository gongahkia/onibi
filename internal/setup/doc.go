// Package setup is the interactive wizard: prompts for the BotFather token,
// generates a single-use deeplink pairing payload (32 random bytes, 5-min
// TTL), prints the t.me/<bot>?start=pair_<token> URL plus QR, awaits the
// pair callback, records owner_id permanently. Ends with mandatory Telegram
// 2FA acknowledgment gate.
package setup
