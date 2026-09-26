package telegram

import "time"

const (
	ReconnectBackoffInitial = time.Second
	ReconnectBackoffCap     = 60 * time.Second
)

func ReconnectBackoff(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	delay := ReconnectBackoffInitial
	for i := 1; i < failures; i++ {
		if delay >= ReconnectBackoffCap/2 {
			return ReconnectBackoffCap
		}
		delay *= 2
	}
	if delay > ReconnectBackoffCap {
		return ReconnectBackoffCap
	}
	return delay
}
