package editor

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/text"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func parseKey(value string) (string, error) {
	parts := strings.Split(value, "+")
	var mod tcell.ModMask
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "ctrl", "control":
			mod |= tcell.ModCtrl
		case "alt", "option":
			mod |= tcell.ModAlt
		case "shift":
			mod |= tcell.ModShift
		default:
			return "", fmt.Errorf("unknown key modifier %q", p)
		}
	}
	base := strings.ToLower(parts[len(parts)-1])
	aliases := map[string]string{"esc": "escape", "return": "enter", "pageup": "pgup", "pagedown": "pgdn", "space": " "}
	if v, ok := aliases[base]; ok {
		base = v
	}
	if base == " " && mod&tcell.ModCtrl != 0 {
		return tcell.NewEventKey(tcell.KeyCtrlSpace, 0, mod).Name(), nil
	}
	if utf8.RuneCountInString(base) == 1 {
		r, _ := utf8.DecodeRuneInString(base)
		if mod&tcell.ModShift != 0 && mod&tcell.ModCtrl == 0 {
			r = unicode.ToUpper(r)
		}
		return tcell.NewEventKey(tcell.KeyRune, r, mod).Name(), nil
	}
	for key, name := range tcell.KeyNames {
		if strings.EqualFold(name, base) {
			return tcell.NewEventKey(key, 0, mod).Name(), nil
		}
	}
	return "", fmt.Errorf("unknown key %q", value)
}
func (a *App) configureKeys() error {
	a.bindings = map[string]func(){}
	keys := []string{}
	for key := range a.Config.Keybindings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		canonical, err := parseKey(key)
		if err != nil {
			return err
		}
		if _, ok := a.bindings[canonical]; ok {
			return fmt.Errorf("duplicate shortcut %q", key)
		}
		name := a.Config.Keybindings[key]
		if name == "" {
			a.bindings[canonical] = func() {}
			continue
		}
		found := false
		for i := range a.Commands {
			if strings.EqualFold(a.Commands[i].Name, name) {
				a.bindings[canonical] = a.Commands[i].Run
				a.Commands[i].Key = key
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("keybinding %s: unknown command %q", key, name)
		}
	}
	return nil
}
func (a *App) editingCommands() {
	a.Commands = append(a.Commands,
		command{"Save all", "", func() { a.saveAll(nil) }}, command{"Reload from disk", "", a.reload},
		command{"Delete lines", "", func() { a.lines("delete") }}, command{"Duplicate lines", "", func() { a.lines("duplicate") }},
		command{"Move lines up", "Alt+Up", func() { a.lines("up") }}, command{"Move lines down", "Alt+Down", func() { a.lines("down") }},
		command{"Toggle line comments", "", a.commentLines}, command{"Select line", "", func() {
			start, end := selectedLines(a.Current().Doc)
			a.Current().Doc.Selections = []text.Selection{{Anchor: start, Head: end}}
		}},
		command{"Move word left", "Ctrl+Left", func() { a.word(-1, false, false) }}, command{"Move word right", "Ctrl+Right", func() { a.word(1, false, false) }},
		command{"Delete word left", "Ctrl+Backspace", func() { a.word(-1, false, true) }}, command{"Delete word right", "Ctrl+Delete", func() { a.word(1, false, true) }})
}
func wordClass(r rune) int {
	if unicode.IsSpace(r) {
		return 0
	}
	if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_' {
		return 1
	}
	return 2
}
func wordOffset(r text.Rope, p, delta int) int {
	class := func(at int) int {
		v, _ := utf8.DecodeRuneInString(r.Slice(at, min(r.Len(), at+4)))
		return wordClass(v)
	}
	if delta > 0 {
		if p >= r.Len() {
			return p
		}
		c := class(p)
		for p < r.Len() && class(p) == c {
			p = next(r, p)
		}
		for p < r.Len() && class(p) == 0 {
			p = next(r, p)
		}
		return p
	}
	for p > 0 && class(previous(r, p)) == 0 {
		p = previous(r, p)
	}
	if p == 0 {
		return p
	}
	c := class(previous(r, p))
	for p > 0 && class(previous(r, p)) == c {
		p = previous(r, p)
	}
	return p
}
func (a *App) word(delta int, extend, remove bool) {
	d := a.Current().Doc
	for i, s := range d.Selections {
		if remove && s.Start() != s.End() {
			continue
		}
		p := wordOffset(d.Text, s.Head, delta)
		if !extend && !remove {
			s.Anchor = p
		}
		s.Head = p
		d.Selections[i] = s
	}
	a.normalizeSelections()
	if remove {
		a.insert("")
	}
	a.reveal()
}
func selectedLines(d *text.Document) (int, int) {
	start, end := d.Text.Len(), 0
	for _, s := range d.Selections {
		start = min(start, s.Start())
		end = max(end, s.End())
	}
	first, last := d.Text.LineAt(start), d.Text.LineAt(end)
	if end > start && end == d.Text.LineStart(last) {
		last--
	}
	return d.Text.LineStart(first), d.Text.LineStart(last + 1)
}
func (a *App) lines(operation string) {
	b := a.Current()
	d := b.Doc
	if len(d.Selections) != 1 {
		a.Status = "Line commands take one contiguous selection"
		return
	}
	start, end := selectedLines(d)
	block := d.Text.Slice(start, end)
	nl := b.Newline
	switch operation {
	case "delete":
		if end == d.Text.Len() && start > 0 && !strings.HasSuffix(block, nl) {
			start -= len(nl)
		}
		a.apply([]text.Edit{{Start: start, End: end}})
	case "duplicate":
		if strings.HasSuffix(block, nl) {
			a.apply([]text.Edit{{Start: end, End: end, Text: block}})
		} else {
			a.apply([]text.Edit{{Start: end, End: end, Text: nl + block}})
		}
	case "up", "down":
		first, last := start, end
		left, right := block, ""
		old := d.Selections[0]
		delta := 0
		if operation == "up" {
			if start == 0 {
				return
			}
			first = d.Text.LineStart(d.Text.LineAt(start) - 1)
			left = d.Text.Slice(first, start)
			right = block
			delta = first - start
		} else {
			if end == d.Text.Len() {
				return
			}
			last = d.Text.LineStart(d.Text.LineAt(end) + 1)
			right = d.Text.Slice(end, last)
			delta = len(right)
			if !strings.HasSuffix(right, nl) {
				delta += len(nl)
			}
		}
		tail := ""
		if strings.HasSuffix(right, nl) {
			tail = nl
		}
		replacement := strings.TrimSuffix(right, nl) + nl + strings.TrimSuffix(left, nl) + tail
		version := d.Version
		a.apply([]text.Edit{{Start: first, End: last, Text: replacement}})
		if d.Version != version {
			old.Anchor += delta
			old.Head += delta
			d.Selections = []text.Selection{old}
			a.clampSelections(b)
			a.reveal()
		}
	}
}
func (a *App) commentLines() {
	b := a.Current()
	ext := strings.ToLower(filepath.Ext(b.File.Path))
	prefix := ""
	switch ext {
	case ".go", ".c", ".h", ".cc", ".cpp", ".hpp", ".rs", ".js", ".jsx", ".ts", ".tsx", ".java", ".cs", ".swift", ".kt":
		prefix = "//"
	case ".py", ".sh", ".bash", ".zsh", ".rb", ".pl", ".yaml", ".yml", ".toml":
		prefix = "#"
	case ".sql", ".lua":
		prefix = "--"
	}
	if prefix == "" {
		a.Status = "No line-comment syntax for this file type"
		return
	}
	d := b.Doc
	start, end := selectedLines(d)
	first := d.Text.LineAt(start)
	last := d.Text.LineAt(end)
	if end == d.Text.Len() || end > d.Text.LineStart(last) {
		last++
	}
	type line struct {
		at   int
		body string
	}
	lines := []line{}
	all := true
	for i := first; i < last && i < d.Text.Lines(); i++ {
		s := d.Text.Line(i)
		body := strings.TrimLeft(s, " \t")
		if body == "" {
			continue
		}
		lines = append(lines, line{d.Text.LineStart(i) + len(s) - len(body), body})
		all = all && strings.HasPrefix(body, prefix)
	}
	edits := []text.Edit{}
	for _, l := range lines {
		if all {
			n := len(prefix)
			if strings.HasPrefix(l.body, prefix+" ") {
				n++
			}
			edits = append(edits, text.Edit{Start: l.at, End: l.at + n})
		} else {
			edits = append(edits, text.Edit{Start: l.at, End: l.at, Text: prefix + " "})
		}
	}
	a.apply(edits)
}
func (a *App) saveAll(done func()) {
	if a.Readonly {
		a.Status = "Read-only mode"
		return
	}
	for i, b := range a.Buffers {
		if b.Transient || !b.Doc.Dirty() {
			continue
		}
		if b.File.Path == "" {
			a.Active = i
			a.saveAsThen(func() { a.saveAll(done) })
			return
		}
		if err := b.File.Save(b.Doc); err != nil {
			a.Active = i
			a.Status = err.Error()
			return
		}
	}
	a.scheduleRecovery()
	a.Status = "Saved all buffers"
	if done != nil {
		done()
	}
}
func (a *App) reload() {
	b := a.Current()
	if b.File.Path == "" || b.Transient {
		a.Status = "No file to reload"
		return
	}
	run := func() {
		f, d, err := OpenFile(b.File.Path, int64(a.Config.MaxFileMiB)<<20)
		if err != nil {
			a.Status = err.Error()
			return
		}
		if !f.exists {
			a.Status = "File disappeared; buffer retained"
			return
		}
		pos := b.Doc.Selections[0].Head
		// Keep reload undoable, while updating the saved root and disk fingerprint.
		if err := b.Doc.Apply(b.Doc.Version, []text.Edit{{Start: 0, End: b.Doc.Text.Len(), Text: d.Text.String()}}); err != nil {
			a.Status = err.Error()
			return
		}
		b.Doc.MarkSaved()
		b.File = f
		b.DiskConflict = false
		b.Newline = detectNewline(b.Doc.Text)
		pos = min(pos, b.Doc.Text.Len())
		for !b.Doc.Text.Boundary(pos) {
			pos--
		}
		b.Doc.Selections = []text.Selection{{Anchor: pos, Head: pos}}
		a.changed()
		a.Status = "Reloaded from disk; Undo restores previous text"
	}
	if b.Doc.Dirty() {
		a.list("Reload discards unsaved text (undoable)", []choice{{"Keep editing", func() {}}, {"Reload from disk", run}})
	} else {
		run()
	}
}
