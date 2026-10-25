package snippet

import (
	"context"
	"strings"
	"testing"
	"time"
)

func expansion(t *testing.T, source string, variables map[string]string, values map[int]string) Expansion {
	t.Helper()
	p, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	p, err = p.Resolve(variables)
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Expand(context.Background(), values, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestGrammar(t *testing.T) {
	tests := []struct{ source, want string }{
		{`fn(${1:name}, ${2|one,two|}, $1)$0`, `fn(name, one, name)`},
		{`$1 ${1:hello ${2:world}} $2`, `hello world hello world world`},
		{`${KNOWN} ${MISSING:default ${1:child}} ${UNKNOWN}`, `value default child UNKNOWN`},
		{`${1|a\,b,c\|d,\\path|}`, `a,b`},
		{`C:\Users\file \$1 \} \\ $0`, `C:\Users\file $1 } \ `},
		{`${FILE/(.*)\..+$/$1/}`, `foo-bar`},
		{`${FILE/(.*)/${1:/upcase}/}`, `FOO-BAR.TXT`},
		{`${FILE/[^a-z]//gi}`, `foobartxt`},
		{`${MISSING/(.*)/${1:+yes}${1:-no}/}`, `no`},
		{`${KNOWN/(v)(.*)/${1:?yes:no}-${2}/}`, `yes-alue`},
		{`${1:hello-world} ${1/(.*)/${1:/pascalcase}/}`, `hello-world HelloWorld`},
		{`${1:HTTPServer} ${1/(.*)/${1:/snakecase}/}`, `HTTPServer http_server`},
		{`${1:你好世界} ${1/(你好)(世界)/$2$1/}`, `你好世界 世界你好`},
		{`${1:abc} ${1/(?<=a)b/B/}`, `abc aBc`},
		{`${1:aabb} ${1/(.)\1/$1/g}`, `aabb ab`},
		{`${1} ${2} $1`, `  `},
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			r := expansion(t, tt.source, map[string]string{"KNOWN": "value", "FILE": "foo-bar.txt", "MISSING": ""}, nil)
			if r.Text != tt.want {
				t.Fatalf("got %q want %q", r.Text, tt.want)
			}
		})
	}
}
func TestNestedOverridesMirrorsAndTransformRefresh(t *testing.T) {
	p, err := Compile(`${1:hello ${2:world}} / $1 / $2 / ${1/(.*)/${1:/upcase}/}`)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = p.Resolve(nil)
	first, err := p.Expand(context.Background(), nil, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Stops) != 3 || len(first.Stops[1].Ranges) != 3 {
		t.Fatal(first.Stops)
	}
	typed, err := p.Expand(context.Background(), map[int]string{2: "you"}, first.Cache, false)
	if err != nil {
		t.Fatal(err)
	}
	if typed.Text != "hello you / hello you / you / HELLO WORLD" {
		t.Fatal(typed.Text)
	}
	next, err := p.Expand(context.Background(), map[int]string{2: "you"}, typed.Cache, true)
	if err != nil {
		t.Fatal(err)
	}
	if next.Text != "hello you / hello you / you / HELLO YOU" {
		t.Fatal(next.Text)
	}
	replaced, err := p.Expand(context.Background(), map[int]string{1: "x", 2: "you"}, next.Cache, true)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Text != "x / x / you / X" || len(replaced.Stops[1].Ranges) != 1 {
		t.Fatal(replaced.Text, replaced.Stops)
	}
}
func TestBoundsCyclesAndTimeout(t *testing.T) {
	for _, source := range []string{`${1`, `${1|a,b}`, `${1/a/b/z}`, `${1/(abcdef){999999999}/x/}`, `${1/a/${1:/unknown}/}`, strings.Repeat("${1:", 40) + strings.Repeat("}", 40), strings.Repeat("x", MaxSource+1)} {
		if _, err := Compile(source); err == nil {
			t.Fatal("accepted invalid/unbounded snippet", short(source))
		}
	}
	for _, source := range []string{`${1:$1}`, `${1:$2}${2:$1}`} {
		p, err := Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		p, _ = p.Resolve(nil)
		if _, err = p.Expand(context.Background(), nil, nil, true); err == nil {
			t.Fatal("cyclic placeholder accepted")
		}
	}
	p, err := Compile(`${VALUE/(a+)+$/x/}`)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = p.Resolve(map[string]string{"VALUE": strings.Repeat("a", 10000) + "!"})
	start := time.Now()
	_, err = p.Expand(context.Background(), nil, nil, true)
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatal("backtracking was not bounded", err, time.Since(start))
	}
}
func short(s string) string {
	if len(s) > 80 {
		return s[:80]
	}
	return s
}
func FuzzCompileExpand(f *testing.F) {
	for _, s := range []string{"hello", `${1:name} $1`, `${1:${2:inner}}`, `$VAR`, `${1/(.*)/${1:/upcase}/}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, err := Compile(s)
		if err != nil {
			return
		}
		p, err = p.Resolve(nil)
		if err != nil {
			return
		}
		_, _ = p.Expand(context.Background(), nil, nil, false)
	})
}
