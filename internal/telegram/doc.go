// Package telegram wraps github.com/go-telegram/bot with our enforcement
// layer: dedicated http.Client (TLS 1.2 floor, no proxy, no InsecureSkipVerify),
// owner-check middleware, defensive deleteWebhook on startup, getUpdates race
// detector, message-id↔session-id threading map for reply-to routing. Phase 1.
package telegram
