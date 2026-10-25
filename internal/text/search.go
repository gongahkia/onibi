package text

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

const MaxMatches = 10000

// Search is bounded and cancellable between matches. Go's regexp engine has
// linear-time matching; results are version-bound by the caller.
func Search(ctx context.Context, source, query string, regex bool) ([]Edit, error) {
	if query == "" {
		return nil, nil
	}
	var re *regexp.Regexp
	var err error
	if regex {
		re, err = regexp.Compile(query)
		if err != nil {
			return nil, err
		}
	}
	if regex {
		locations := re.FindAllStringIndex(source, MaxMatches+1)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(locations) > MaxMatches {
			return nil, errors.New("more than 10,000 matches; narrow your search")
		}
		out := make([]Edit, 0, len(locations))
		for _, loc := range locations {
			if loc[0] == loc[1] {
				return nil, errors.New("zero-width patterns are not supported; match text to replace")
			}
			out = append(out, Edit{Start: loc[0], End: loc[1]})
		}
		return out, nil
	}
	out := []Edit{}
	offset := 0
	for offset <= len(source) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		start, end := -1, -1
		i := strings.Index(source[offset:], query)
		if i >= 0 {
			start = offset + i
			end = start + len(query)
		}
		if start < 0 {
			break
		}
		if start == end {
			return nil, errors.New("zero-width patterns are not supported; match text to replace")
		}
		out = append(out, Edit{Start: start, End: end})
		if len(out) > MaxMatches {
			return nil, errors.New("more than 10,000 matches; narrow your search")
		}
		offset = end
	}
	return out, nil
}
func Replacements(ctx context.Context, source, query, replacement string, regex bool) ([]Edit, error) {
	edits, err := Search(ctx, source, query, regex)
	if err != nil {
		return nil, err
	}
	var re *regexp.Regexp
	var groups [][]int
	if regex {
		re = regexp.MustCompile(query)
		groups = re.FindAllStringSubmatchIndex(source, MaxMatches+1)
	}
	for i := range edits {
		e := &edits[i]
		e.Text = replacement
		if regex {
			e.Text = string(re.ExpandString(nil, replacement, source, groups[i]))
		}
	}
	return edits, nil
}
