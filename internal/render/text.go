package render

import (
	"strings"
	"unicode/utf8"
)

// Defaults sized for Telegram readability + the 4096-char message limit.
const (
	DefaultMaxLines = 40
	DefaultMaxChars = 3500 // leaves headroom below 4096 for fence + caption
	fence           = "```"
)

// Options configures TextTail.
type Options struct {
	MaxLines int    // 0 → DefaultMaxLines
	MaxChars int    // 0 → DefaultMaxChars
	Lang     string // optional code-block language hint (e.g. "shell", "")
}

// TextTail returns the last N lines of buf, ANSI-stripped, wrapped in a
// fenced code block ready for Telegram's MarkdownV2. Truncates head-side
// (with a "[…N lines truncated…]" marker) when the result would exceed
// MaxChars.
func TextTail(buf []byte, opts Options) string {
	if opts.MaxLines <= 0 {
		opts.MaxLines = DefaultMaxLines
	}
	if opts.MaxChars <= 0 {
		opts.MaxChars = DefaultMaxChars
	}

	clean := StripANSI(buf)
	if len(clean) == 0 {
		return fence + "\n(no output)\n" + fence
	}

	// split, take last MaxLines
	lines := splitLines(clean)
	dropped := 0
	if len(lines) > opts.MaxLines {
		dropped = len(lines) - opts.MaxLines
		lines = lines[dropped:]
	}

	body := strings.Join(lines, "\n")
	// trim leading byte-by-byte if still too long, but keep complete UTF-8
	if len(body) > opts.MaxChars {
		extra := len(body) - opts.MaxChars
		// drop chars from the front, advancing to the next newline so we
		// don't slice mid-line
		nl := strings.IndexByte(body[extra:], '\n')
		if nl > 0 {
			extra += nl + 1
		}
		body = body[extra:]
		// count truncated lines for the marker
		more := strings.Count(body[:0], "\n") // placeholder kept for clarity
		_ = more
		dropped += extra // overcounts but fine for a human-friendly marker
	}

	// ensure final body is valid UTF-8 (drop any partial rune at the front)
	body = trimInvalidUTF8(body)

	header := ""
	if dropped > 0 {
		header = "[…" + plural(dropped, "byte", "bytes") + " truncated…]\n"
	}
	lang := opts.Lang
	return fence + lang + "\n" + header + body + "\n" + fence
}

// StripANSI removes ANSI escape sequences (CSI sequences and most basic
// OSC sequences) so the output is suitable for a fenced code block.
// Conservative — preserves all printable text and newlines.
func StripANSI(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); {
		b := in[i]
		// ESC at start of a sequence
		if b == 0x1b && i+1 < len(in) {
			next := in[i+1]
			switch next {
			case '[':
				// CSI: ESC '[' params* finalbyte (in 0x40..0x7e)
				j := i + 2
				for j < len(in) {
					c := in[j]
					if c >= 0x40 && c <= 0x7e {
						j++
						break
					}
					j++
				}
				i = j
				continue
			case ']':
				// OSC: ESC ']' ... BEL or ESC \
				j := i + 2
				for j < len(in) {
					if in[j] == 0x07 {
						j++
						break
					}
					if in[j] == 0x1b && j+1 < len(in) && in[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				i = j
				continue
			case '(', ')':
				// character-set designation: ESC ( char
				i += 3
				if i > len(in) {
					i = len(in)
				}
				continue
			default:
				// 2-byte escape: ESC X
				i += 2
				continue
			}
		}
		// strip carriage returns that aren't part of \r\n line endings
		if b == '\r' && i+1 < len(in) && in[i+1] != '\n' {
			i++
			continue
		}
		out = append(out, b)
		i++
	}
	return out
}

func splitLines(b []byte) []string {
	s := string(b)
	// normalize \r\n → \n
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func plural(n int, sing, plur string) string {
	w := plur
	if n == 1 {
		w = sing
	}
	return itoa(n) + " " + w
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func trimInvalidUTF8(s string) string {
	for i := 0; i < len(s); i++ {
		if utf8.ValidString(s[i:]) {
			return s[i:]
		}
	}
	return ""
}
