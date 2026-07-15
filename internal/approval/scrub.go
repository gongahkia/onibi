package approval

import (
	"regexp"
)

// Redaction policy: conservative — false positives are user-confusing, so
// only patterns that are almost certainly secrets get scrubbed. The user
// can always reveal originals via the audit log on the local machine.
//
// What we redact:
//   - PEM blocks (BEGIN .* KEY)
//   - AWS access keys (AKIA / ASIA + 16 alnum)
//   - GitHub tokens (ghp_, gho_, ghu_, ghs_, ghr_ + 36 chars)
//   - Slack tokens (xox[a-z]-...)
//   - Stripe keys (sk_live_, rk_live_)
//   - Bearer tokens in inline strings ("Bearer <40+ alnum>")
//   - Common password/secret assignment forms (PASSWORD="..", PASS=.., SECRET=..)
//
// What we DON'T touch:
//   - file contents in general (we'd never catch everything)
//   - generic-looking long strings (false positives kill the UX)
const placeholder = "[REDACTED]"

var redactRules = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]+-----[\s\S]*?-----END [A-Z ]+-----`),
	regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]{20,}`),
	// password-like assignments — quoted value
	regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key)\s*=\s*"[^"]{4,}"`),
	// password-like JSON fields (with optional whitespace), keep the key intact
	regexp.MustCompile(`(?i)"(password|passwd|secret|token|api_key|apikey)"\s*:\s*"[^"]{4,}"`),
}

var redactCLISecret = regexp.MustCompile(`(?i)(--(?:password|passwd|secret|token|api[_-]?key)(?:=|\s+))(?:"[^"]+"|'[^']+'|\S+)`)

// Scrub returns a copy of s with matches of any redactRules replaced by
// the placeholder. The semantic of the replacement depends on the rule —
// for assignment-style matches we keep the key half intact.
func Scrub(s string) string {
	out := s
	// PEM, AWS, GH, Slack, Stripe, Bearer: full match → placeholder
	for _, re := range redactRules[:6] {
		out = re.ReplaceAllString(out, placeholder)
	}
	// `key = "value"` → `key = "[REDACTED]"`
	out = redactRules[6].ReplaceAllStringFunc(out, func(m string) string {
		// preserve key + spacing, replace quoted value
		return assignKey(m) + `="` + placeholder + `"`
	})
	// `"key": "value"` → `"key": "[REDACTED]"`
	out = redactRules[7].ReplaceAllStringFunc(out, func(m string) string {
		return jsonKey(m) + `: "` + placeholder + `"`
	})
	out = redactCLISecret.ReplaceAllString(out, "${1}"+placeholder)
	return out
}

func assignKey(m string) string {
	// captures the substring up to and including the key, dropping the value
	// fast scan: find '=' then strip back to key
	for i := 0; i < len(m); i++ {
		if m[i] == '=' {
			// keep everything before '=' but drop trailing whitespace
			j := i
			for j > 0 && (m[j-1] == ' ' || m[j-1] == '\t') {
				j--
			}
			return m[:j]
		}
	}
	return m
}

func jsonKey(m string) string {
	// find the colon between key and value
	for i := 0; i < len(m); i++ {
		if m[i] == ':' {
			j := i
			for j > 0 && (m[j-1] == ' ' || m[j-1] == '\t') {
				j--
			}
			return m[:j]
		}
	}
	return m
}
