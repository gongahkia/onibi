package daemon

import (
	"os"
	"strings"

	"github.com/gongahkia/onibi/internal/approval"
)

const ChatUnredactedEnv = "ONIBI_CHAT_UNREDACTED"

func redactChatText(s string) string {
	if chatUnredacted() {
		return s
	}
	return approval.Scrub(s)
}

func chatUnredacted() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(ChatUnredactedEnv))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
