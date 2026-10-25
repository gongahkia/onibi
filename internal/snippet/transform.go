package snippet

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dlclark/regexp2/v2"
)

type regex = *regexp2.Regexp

func (t *transform) compile() error {
	if len(t.Pattern) > 4096 {
		return fmt.Errorf("snippet transform regex exceeds 4 KiB")
	}
	if err := regexBudget(t.Pattern); err != nil {
		return err
	}
	options := regexp2.ECMAScript
	seen := map[rune]bool{}
	for _, flag := range t.Flags {
		if seen[flag] {
			return fmt.Errorf("duplicate snippet regex flag %c", flag)
		}
		seen[flag] = true
		switch flag {
		case 'g', 'y', 'd':
		case 'i':
			options |= regexp2.IgnoreCase
		case 'm':
			options |= regexp2.Multiline
		case 's':
			options |= regexp2.Singleline
		case 'u':
			options |= regexp2.Unicode
		default:
			return fmt.Errorf("unsupported snippet regex flag %c", flag)
		}
	}
	re, err := regexp2.Compile(t.Pattern, options)
	if err != nil {
		return fmt.Errorf("snippet transform: %w", err)
	}
	re.MatchTimeout = 25 * time.Millisecond
	t.re = re
	return nil
}

// Bound numeric-repeat expansion before invoking the regex compiler, whose
// optimizations can otherwise allocate a very large fixed prefix. This is a
// conservative resource bound; it is separate from the matching timeout.
func regexBudget(pattern string) error {
	depth, repeats, class := 0, 1, false
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		if c == '\\' {
			i++
			continue
		}
		if c == '[' {
			class = true
		}
		if c == ']' {
			class = false
		}
		if class {
			continue
		}
		if c == '(' {
			depth++
			if depth > 64 {
				return fmt.Errorf("snippet regex nesting exceeds 64")
			}
		}
		if c == ')' {
			depth--
		}
		if c != '{' {
			continue
		}
		j, count := i+1, 1
		for j < len(pattern) && (digit(pattern[j]) || pattern[j] == ',') {
			if pattern[j] == ',' {
				j++
				continue
			}
			start := j
			for j < len(pattern) && digit(pattern[j]) {
				j++
			}
			n, err := strconv.Atoi(pattern[start:j])
			if err != nil || n > MaxExpansion {
				return fmt.Errorf("snippet regex repeat exceeds work limit")
			}
			count = max(count, n)
		}
		if j < len(pattern) && pattern[j] == '}' {
			if count > MaxExpansion/max(1, len(pattern))/repeats {
				return fmt.Errorf("snippet regex repeat expansion exceeds work limit")
			}
			repeats *= count
		}
	}
	return nil
}
func (t *transform) apply(ctx context.Context, input string) (string, error) {
	if len(input) > MaxExpansion {
		return "", fmt.Errorf("snippet transform input exceeds 128 KiB")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	m, err := t.re.FindStringMatch(input)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	end, expected, count := 0, 0, 0
	global, sticky := strings.Contains(t.Flags, "g"), strings.Contains(t.Flags, "y")
	for m != nil {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		start, length := m.ByteRange()
		if sticky && start != expected {
			break
		}
		out.WriteString(input[end:start])
		replacement, err := renderFormat(t.Format, m)
		if err != nil {
			return "", err
		}
		out.WriteString(replacement)
		if out.Len() > MaxExpansion {
			return "", fmt.Errorf("snippet transform output exceeds 128 KiB")
		}
		end = start + length
		expected = end
		count++
		if !global {
			break
		}
		if count >= 4096 {
			return "", fmt.Errorf("snippet transform exceeds 4096 matches")
		}
		if length == 0 && end < len(input) {
			_, n := utf8.DecodeRuneInString(input[end:])
			expected += n
		}
		m, err = t.re.FindNextMatch(m)
		if err != nil {
			return "", err
		}
	}
	out.WriteString(input[end:])
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if out.Len() > MaxExpansion {
		return "", fmt.Errorf("snippet expansion exceeds 128 KiB")
	}
	return out.String(), nil
}
func renderFormat(parts []format, match *regexp2.Match) (string, error) {
	var out strings.Builder
	for _, part := range parts {
		value := ""
		if group := match.GroupByNumber(part.Group); group != nil {
			value = group.String()
		}
		switch part.Kind {
		case "":
			out.WriteString(part.Text)
		case "capture":
			out.WriteString(value)
		case "if", "else", "conditional":
			branch := part.No
			if value != "" {
				branch = part.Yes
			}
			if part.Kind == "else" && value != "" {
				out.WriteString(value)
			} else {
				result, err := renderFormat(branch, match)
				if err != nil {
					return "", err
				}
				out.WriteString(result)
			}
		default:
			out.WriteString(changeCase(value, part.Kind))
		}
		if out.Len() > MaxExpansion {
			return "", fmt.Errorf("snippet format expansion exceeds 128 KiB")
		}
	}
	return out.String(), nil
}
func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
func changeCase(value, kind string) string {
	switch kind {
	case "upcase":
		return strings.ToUpper(value)
	case "downcase":
		return strings.ToLower(value)
	case "capitalize":
		return upperFirst(value)
	}
	words := []string{}
	runes := []rune(value)
	start := -1
	flush := func(end int) {
		if start >= 0 {
			words = append(words, strings.ToLower(string(runes[start:end])))
			start = -1
		}
	}
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush(i)
			continue
		}
		if start < 0 {
			start = i
			continue
		}
		if unicode.IsUpper(r) && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]) && unicode.IsUpper(runes[i-1]))) {
			flush(i)
			start = i
		}
	}
	flush(len(runes))
	if kind == "snakecase" {
		return strings.Join(words, "_")
	}
	if kind == "kebabcase" {
		return strings.Join(words, "-")
	}
	for i := range words {
		if i > 0 || kind == "pascalcase" {
			words[i] = upperFirst(words[i])
		}
	}
	return strings.Join(words, "")
}
