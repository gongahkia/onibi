package daemon

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

const (
	TelegramSecretBotToken = "TELEGRAM_BOT_TOKEN"
	TelegramKVOwnerChatID  = "telegram.owner_chat_id"
	TelegramKVOwnerUserID  = "telegram.owner_user_id"
	TelegramKVPairCode     = "telegram.pair_code"
)

func NewTelegramPairCode() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	n := binary.BigEndian.Uint32(b[:]) % 1000000
	return fmt.Sprintf("%06d", n), nil
}
