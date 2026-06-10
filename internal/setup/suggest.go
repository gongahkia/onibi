package setup

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// suggestUsername returns a hard-to-guess Telegram bot username suggestion
// of the form "onibi_<8hex>_bot". Mitigates threat T6 (username squat /
// discovery race).
func suggestUsername() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "onibi_xxxx_bot"
	}
	return fmt.Sprintf("onibi_%s_bot", hex.EncodeToString(b))
}
