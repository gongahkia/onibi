package termui

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func TestShadowBannerAlignmentFallbackAndSingleRendering(t *testing.T) {
	for _, width := range []int{24, 40, 80, 120} {
		for _, ascii := range []bool{false, true} {
			var output bytes.Buffer
			ui := &UI{Out: &output, Interactive: true, ASCII: ascii, width: width}
			ui.Banner("Control Ghostty wherever text renders")
			first := output.String()
			ui.Banner("duplicate")
			if output.String() != first {
				t.Fatal("banner repeated")
			}
			for _, line := range strings.Split(ansiPattern.ReplaceAllString(first, ""), "\n") {
				if uniseg.StringWidth(line) >= width {
					t.Fatalf("width %d overflow: %q", width, line)
				}
			}
			if ascii && strings.ContainsAny(first, "█░") {
				t.Fatal("non-ASCII glyph in fallback")
			}
			if width >= 39 && !strings.Contains(first, "\x1b[2m") {
				t.Fatal("missing shadow styling")
			}
		}
	}
	for _, double := range []bool{false, true} {
		rows := logoRows(double, false)
		for _, row := range rows {
			if uniseg.StringWidth(row) != uniseg.StringWidth(rows[0]) {
				t.Fatal("logo rows differ in width")
			}
		}
	}
}
func TestNoColorAndPlainPresentation(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	ui := New(&out)
	ui.Interactive = true
	ui.Banner("Header")
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("NO_COLOR did not suppress banner styling")
	}
	out.Reset()
	ui = New(&out)
	ui.Banner("Header")
	live := NewLive(ui)
	called := false
	live.Start(func(time.Duration) string { called = true; return "activity" })
	live.Write([]byte("event\n"))
	live.Close()
	if called || strings.Contains(out.String(), "\x1b") || !strings.HasSuffix(out.String(), "event\n") {
		t.Fatal("plain output animated")
	}
}
func TestFieldWrappingPreservesWideTextAndHangingIndent(t *testing.T) {
	var out bytes.Buffer
	ui := &UI{Out: &out, Interactive: true, width: 64}
	ui.Field("Logs", "/a/very/long/path/with/no/spaces/"+strings.Repeat("界", 30))
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatal("expected wrapped field")
	}
	for i, line := range lines {
		if uniseg.StringWidth(line) >= 64 {
			t.Fatalf("overflow %q", line)
		}
		if i > 0 && !strings.HasPrefix(line, strings.Repeat(" ", 15)) {
			t.Fatal("lost continuation indent")
		}
	}
}
func TestLiveSerializesRecordsBelowBannerAndStopsCleanly(t *testing.T) {
	var out bytes.Buffer
	ui := &UI{Out: &out, Interactive: true, width: 80}
	ui.Banner("Logs")
	live := NewLive(ui)
	seen := make(chan struct{})
	var once sync.Once
	live.Start(func(time.Duration) string { once.Do(func() { close(seen) }); return "Following logs / Ctrl-C exits" })
	<-seen
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for record := range 10 {
				if _, err := fmt.Fprintf(live, "12:00:00  INFO   record-%d-%d\n", worker, record); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	time.Sleep(120 * time.Millisecond)
	ui.mu.Lock()
	during := out.String()
	ui.mu.Unlock()
	screen := terminalRows(during)
	if !strings.Contains(screen[len(screen)-1], "Following logs") {
		t.Fatal("activity is not beneath log records")
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	after := out.String()
	time.Sleep(120 * time.Millisecond)
	if out.String() != after {
		t.Fatal("animation survived Close")
	}
	if live.Records() != 40 {
		t.Fatalf("records=%d", live.Records())
	}
	for worker := range 4 {
		for record := range 10 {
			want := fmt.Sprintf("record-%d-%d", worker, record)
			if strings.Count(strings.Join(terminalRows(after), "\n"), want) != 1 {
				t.Fatalf("lost/duplicated %s", want)
			}
		}
	}
	if _, err := live.Write([]byte("late\n")); err != os.ErrClosed {
		t.Fatal("accepted write after close")
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
}

// Model the narrow set of terminal operations emitted by Live. Screen-level
// assertions catch erased/duplicated records that byte-stream checks miss.
func terminalRows(raw string) []string {
	rows := [][]rune{{}}
	row, col := 0, 0
	for len(raw) > 0 {
		if raw[0] == '\x1b' {
			match := ansiPattern.FindString(raw)
			if match != "" && strings.HasPrefix(raw, match) {
				switch match {
				case "\x1b[2K":
					rows[row] = nil
				case "\x1b[1A":
					row = max(0, row-1)
				}
				raw = raw[len(match):]
				continue
			}
		}
		ch, size := utf8.DecodeRuneInString(raw)
		raw = raw[size:]
		switch ch {
		case '\r':
			col = 0
		case '\n':
			row++
			col = 0
			if row == len(rows) {
				rows = append(rows, nil)
			}
		default:
			for len(rows[row]) <= col {
				rows[row] = append(rows[row], ' ')
			}
			rows[row][col] = ch
			col++
		}
	}
	result := make([]string, len(rows))
	for i, line := range rows {
		result[i] = string(line)
	}
	return result
}

func TestActivityPrioritizesExitHintOnNarrowTerminals(t *testing.T) {
	for _, width := range []int{28, 40, 60, 96} {
		live := NewLive(&UI{Out: &bytes.Buffer{}, Interactive: true, width: width})
		text := live.StatusLine("HTTP live", time.Second, "12 requests / 3 active", "Ctrl-C stops")
		if !strings.Contains(text, "Ctrl-C stops") || uniseg.StringWidth(text) > width-11 {
			t.Fatalf("width=%d status=%q", width, text)
		}
		live.Close()
	}
}
