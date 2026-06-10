// Package logging configures log/slog with a redaction handler middleware
// that scans every record for the loaded bot-token string and replaces with
// [REDACTED]. Unit-tested across all log levels. Token also redacted from
// error messages via wrapping at the telegram client boundary. Phase 1.
package logging
