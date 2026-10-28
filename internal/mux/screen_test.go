package mux

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestScreenTextPreservesLiteralBytesAndBoundsCommands(t *testing.T) {
	for _, text := range []string{"$HOME ${SHELL} $:ku: ^C \\n \\033 \"quoted\" 'single' 日本語\x00", strings.Repeat("a", 127) + strings.Repeat("日本語\\$^\"", 1500)} {
		runner := &scriptedRunner{}
		ctrl := newScreenWithRunner("screen", "/tmp/screenrc", runner)
		if err := ctrl.SendText(t.Context(), Target{Session: "work"}, text, true); err != nil {
			t.Fatal(err)
		}
		var delivered strings.Builder
		for _, call := range runner.calls[:len(runner.calls)-1] {
			if !reflect.DeepEqual(call.args[:8], []string{"-c", "/tmp/screenrc", "-S", "work", "-p", "0", "-X", "stuff"}) {
				t.Fatal(call.args)
			}
			encoded := call.args[8]
			if len(encoded) > 512 || len(encoded)%4 != 0 {
				t.Fatalf("unsafe command length: %d", len(encoded))
			}
			var chunk strings.Builder
			for i := 0; i < len(encoded); i += 4 {
				if encoded[i] != '\\' {
					t.Fatalf("unescaped data: %q", encoded)
				}
				b, err := strconv.ParseUint(encoded[i+1:i+4], 8, 8)
				if err != nil {
					t.Fatal(err)
				}
				chunk.WriteByte(byte(b))
			}
			if !utf8.ValidString(chunk.String()) {
				t.Fatal("split UTF-8 rune")
			}
			delivered.WriteString(chunk.String())
		}
		if delivered.String() != text {
			t.Fatal("literal bytes changed")
		}
		last := runner.calls[len(runner.calls)-1].args
		if last[len(last)-1] != "\r" {
			t.Fatal("missing final Enter")
		}
	}
}

func TestScreenPartialFailureStopsWithoutEnter(t *testing.T) {
	runner := &scriptedRunner{errors: []error{nil, errors.New("write failed")}}
	ctrl := newScreenWithRunner("screen", "", runner)
	if err := ctrl.SendText(t.Context(), Target{Session: "work"}, strings.Repeat("a", 400), true); err == nil || len(runner.calls) != 2 {
		t.Fatalf("err=%v calls=%d", err, len(runner.calls))
	}
}

func TestScreenKeyAlsoUsesLiteralEncoding(t *testing.T) {
	runner := &scriptedRunner{}
	ctrl := newScreenWithRunner("screen", "", runner)
	if err := ctrl.SendKey(t.Context(), Target{Session: "work"}, "up"); err != nil {
		t.Fatal(err)
	}
	args := runner.calls[0].args
	if args[len(args)-1] != `\033\133\101` {
		t.Fatal(args)
	}
}
