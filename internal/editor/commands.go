package editor

import (
	"context"
	"fmt"
	"github.com/gongahkia/shack/internal/text"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (a *App) registerCommands() {
	a.Commands = []command{
		{"Save", "Ctrl+S", a.Save}, {"Save as", "", a.saveAs}, {"New buffer", "Ctrl+N", func() { a.NewBuffer(); a.schedule() }},
		{"Open file by path", "Ctrl+O", func() {
			a.prompt("Open file", "", func(p string) {
				if err := a.Open(p); err != nil {
					a.Status = err.Error()
				}
				a.schedule()
			})
		}},
		{"Find project file", "Ctrl+P", a.filePicker}, {"Next buffer", "Alt+N", func() { a.cycle(1) }}, {"Previous buffer", "Alt+P", func() { a.cycle(-1) }},
		{"Switch buffer", "", a.bufferPicker}, {"Close buffer", "Ctrl+W", a.closeBuffer}, {"Quit", "Ctrl+Q", a.requestQuit},
		{"Find text", "Ctrl+F", a.findPrompt}, {"Next match", "F3 / Alt+F", a.nextMatch}, {"Preview replace", "Ctrl+R", a.beginReplace},
		{"Toggle regular expression search", "", func() { a.Regex = !a.Regex; a.Status = fmt.Sprintf("Regex search: %t", a.Regex) }},
		{"Select all search matches", "Alt+A", a.selectMatches}, {"Add next occurrence", "Ctrl+D", a.addOccurrence},
		{"Undo", "Ctrl+Z", func() {
			if !a.Readonly && a.Current().Doc.Undo() {
				a.changed()
			}
		}}, {"Redo", "Ctrl+Y", func() {
			if !a.Readonly && a.Current().Doc.Redo() {
				a.changed()
			}
		}},
		{"Copy", "Ctrl+C", func() { a.copy(false) }}, {"Cut", "Ctrl+X", func() { a.copy(true) }}, {"Paste", "Ctrl+V", a.pasteClipboard},
		{"Paste Shack internal clipboard", "", func() { a.pasteText(a.clipboard) }},
		{"Go to line", "Ctrl+G", func() { a.prompt("Go to line", "", a.gotoLine) }},
		{"Preview uppercase selection", "", func() { a.transform("Uppercase", strings.ToUpper) }},
		{"Preview lowercase selection", "", func() { a.transform("Lowercase", strings.ToLower) }},
		{"Preview trim trailing whitespace", "", func() {
			a.transform("Trim trailing whitespace", func(s string) string {
				parts := strings.Split(s, "\n")
				for i, p := range parts {
					cr := strings.HasSuffix(p, "\r")
					parts[i] = strings.TrimRight(p, " \t\r")
					if cr {
						parts[i] += "\r"
					}
				}
				return strings.Join(parts, "\n")
			})
		}},
		{"Preview sort lines", "", func() { a.transform("Sort lines", func(s string) string { return sortLines(s, false) }) }},
		{"Preview sort and deduplicate lines", "", func() { a.transform("Sort and deduplicate", func(s string) string { return sortLines(s, true) }) }},
		{"Toggle mouse capture", "", func() {
			a.Config.Mouse = !a.Config.Mouse
			if a.Config.Mouse {
				a.Screen.EnableMouse()
			} else {
				a.Screen.DisableMouse()
			}
			a.Status = fmt.Sprintf("Mouse capture: %t", a.Config.Mouse)
		}},
		{"Toggle light / dark theme", "", func() {
			if a.Config.Theme == "dark" {
				a.Config.Theme = "light"
			} else {
				a.Config.Theme = "dark"
			}
		}},
		{"Help / keyboard shortcuts", "F1", a.help},
	}
}
func sortLines(s string, unique bool) string {
	newline := "\n"
	if strings.Contains(s, "\r\n") {
		newline = "\r\n"
	}
	final := strings.HasSuffix(s, newline)
	s = strings.TrimSuffix(s, newline)
	parts := strings.Split(s, newline)
	sort.Strings(parts)
	if unique {
		out := parts[:0]
		for _, p := range parts {
			if len(out) == 0 || out[len(out)-1] != p {
				out = append(out, p)
			}
		}
		parts = out
	}
	out := strings.Join(parts, newline)
	if final {
		out += newline
	}
	return out
}
func (a *App) palette() {
	items := []choice{}
	for _, c := range a.Commands {
		label := c.Name
		if c.Key != "" {
			label += "    " + c.Key
		}
		items = append(items, choice{label, c.Run})
	}
	a.list("Commands · type to filter", items)
}
func (a *App) help() {
	a.palette()
	a.Dialog.Title = "Help · arrows select, Enter runs, Escape closes"
}
func (a *App) bufferPicker() {
	items := []choice{}
	for i, b := range a.Buffers {
		index := i
		items = append(items, choice{bufferName(b), func() { a.Active = index; a.Matches = nil; a.schedule() }})
	}
	a.list("Open buffers", items)
}
func (a *App) gotoLine(value string) {
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		a.Status = "Enter a positive line number"
		return
	}
	d := a.Current().Doc
	p := d.Text.LineStart(min(n-1, d.Text.Lines()-1))
	d.Selections = []text.Selection{{Anchor: p, Head: p}}
	a.reveal()
}
func (a *App) indexFiles() {
	root := a.Root
	go func() {
		files := []string{}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if a.ctx.Err() != nil {
				return fs.SkipAll
			}
			if len(files) >= 10000 {
				return fs.SkipAll
			}
			if entry.IsDir() {
				if path != root {
					switch entry.Name() {
					case ".git", "node_modules", "vendor", ".cache", "_build", "_opam", ".venv", "target", "build", "dist":
						return fs.SkipDir
					}
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if entry.Type().IsRegular() {
				rel, e := filepath.Rel(root, path)
				if e == nil {
					files = append(files, rel)
				}
			}
			return nil
		})
		a.post(func() {
			a.Files = files
			if err != nil {
				a.Status = "File index: " + err.Error()
			}
		})
	}()
}
func (a *App) filePicker() {
	items := []choice{}
	for _, file := range a.Files {
		path := filepath.Join(a.Root, file)
		items = append(items, choice{file, func() {
			if err := a.Open(path); err != nil {
				a.Status = err.Error()
			}
			a.Matches = nil
			a.schedule()
		}})
	}
	a.list("Project files · Ctrl+O opens any path", items)
	if len(items) == 0 {
		a.Status = "File index is empty or still loading; Ctrl+O opens a path"
	}
}
func (a *App) findPrompt() {
	a.prompt("Find ("+a.searchMode()+")", a.Query, func(q string) { a.Query = q; a.runSearch(false, "") })
}
func (a *App) searchMode() string {
	if a.Regex {
		return "regex"
	}
	return "literal"
}
func (a *App) beginReplace() {
	a.prompt("Replace: find ("+a.searchMode()+")", a.Query, func(q string) {
		a.Query = q
		a.prompt("Replace with · preview before applying", "", func(replacement string) { a.runSearch(true, replacement) })
	})
}
func (a *App) runSearch(replace bool, replacement string) {
	if a.searchCancel != nil {
		a.searchCancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.searchCancel = cancel
	b := a.Current()
	snapshot := b.Doc.Text
	version := b.Doc.Version
	query, regex := a.Query, a.Regex
	a.Status = "Searching…"
	go func() {
		source := snapshot.String()
		var edits []text.Edit
		var err error
		if replace {
			edits, err = text.Replacements(ctx, source, query, replacement, regex)
		} else {
			edits, err = text.Search(ctx, source, query, regex)
		}
		a.post(func() {
			if ctx.Err() != nil || b != a.Current() || b.Doc.Version != version {
				return
			}
			if err != nil {
				a.Status = err.Error()
				return
			}
			if replace {
				a.showPreview("Replace "+query, b, version, edits)
			} else {
				a.Matches = edits
				a.matchBuffer = b
				a.matchVersion = version
				a.Status = fmt.Sprintf("%d matches · F3 next · Alt+A select all", len(edits))
				a.nextMatch()
			}
		})
	}()
}
func (a *App) nextMatch() {
	b := a.Current()
	if a.matchBuffer != b || a.matchVersion != b.Doc.Version || len(a.Matches) == 0 {
		return
	}
	p := b.Doc.Selections[0].End()
	chosen := a.Matches[0]
	for _, e := range a.Matches {
		if e.Start >= p {
			chosen = e
			break
		}
	}
	b.Doc.Selections = []text.Selection{{Anchor: chosen.Start, Head: chosen.End}}
	a.reveal()
}
func (a *App) selectMatches() {
	b := a.Current()
	if a.matchBuffer != b || a.matchVersion != b.Doc.Version || len(a.Matches) == 0 {
		a.Status = "Find text first, then select matches"
		return
	}
	b.Doc.Selections = nil
	for _, e := range a.Matches {
		b.Doc.Selections = append(b.Doc.Selections, text.Selection{Anchor: e.Start, Head: e.End})
	}
	a.Status = fmt.Sprintf("%d selections · type to edit together · Escape collapses", len(b.Doc.Selections))
}
func (a *App) addOccurrence() {
	d := a.Current().Doc
	if len(d.Selections) == 1 && d.Selections[0].Start() == d.Selections[0].End() {
		a.wordSelection()
		return
	}
	first := d.Selections[0]
	needle := d.Text.Slice(first.Start(), first.End())
	if needle == "" {
		return
	}
	if len(d.Selections) >= text.MaxMatches {
		a.Status = "Selection limit reached"
		return
	}
	last := d.Selections[len(d.Selections)-1]
	source := d.Text.String()
	start := last.End()
	i := strings.Index(source[start:], needle)
	if i < 0 {
		start = 0
		i = strings.Index(source, needle)
	}
	if i < 0 {
		return
	}
	p := start + i
	for _, s := range d.Selections {
		if p < s.End() && p+len(needle) > s.Start() {
			a.Status = "All occurrences selected"
			return
		}
	}
	d.Selections = append(d.Selections, text.Selection{Anchor: p, Head: p + len(needle)})
}
func (a *App) showPreview(title string, b *Buffer, version uint64, edits []text.Edit) {
	if len(edits) == 0 {
		a.Status = "No changes to preview"
		return
	}
	checked := make([]bool, len(edits))
	for i := range checked {
		checked[i] = true
	}
	a.Dialog = nil
	a.Preview = &preview{Title: title, Buffer: b, Version: version, Edits: edits, Checked: checked}
}
func (a *App) transform(name string, f func(string) string) {
	b := a.Current()
	snapshot := b.Doc.Text
	version := b.Doc.Version
	sels := append([]text.Selection(nil), b.Doc.Selections...)
	if len(sels) == 1 && sels[0].Start() == sels[0].End() {
		sels = []text.Selection{{Anchor: 0, Head: snapshot.Len()}}
	}
	a.Status = "Preparing " + name + " preview…"
	go func() {
		edits := []text.Edit{}
		for _, s := range sels {
			if a.ctx.Err() != nil {
				return
			}
			original := snapshot.Slice(s.Start(), s.End())
			replacement := f(original)
			if original != replacement {
				edits = append(edits, text.Edit{Start: s.Start(), End: s.End(), Text: replacement})
			}
		}
		a.post(func() {
			if b != a.Current() || b.Doc.Version != version {
				a.Status = "Discarded outdated preview"
				return
			}
			a.showPreview(name, b, version, edits)
		})
	}()
}

func (a *App) copy(cut bool) {
	b := a.Current()
	parts := []string{}
	edits := []text.Edit{}
	for _, s := range b.Doc.Selections {
		start, end := s.Start(), s.End()
		if start == end {
			line := b.Doc.Text.LineAt(start)
			start = b.Doc.Text.LineStart(line)
			end = b.Doc.Text.LineStart(line + 1)
		}
		parts = append(parts, b.Doc.Text.Slice(start, end))
		edits = append(edits, text.Edit{Start: start, End: end})
	}
	a.clipboard = strings.Join(parts, b.Newline)
	a.Screen.SetClipboard([]byte(a.clipboard))
	a.Status = "Copied to Shack; terminal clipboard requested"
	if os.Getenv("SSH_CONNECTION") == "" && os.Getenv("SSH_TTY") == "" {
		if cmd := clipboardCommand(true); cmd != nil {
			value := a.clipboard
			go func() {
				ctx, cancel := context.WithTimeout(a.ctx, time.Second)
				defer cancel()
				p := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
				p.Stdin = strings.NewReader(value)
				if p.Run() == nil {
					a.post(func() { a.Status = "Copied to system clipboard" })
				}
			}()
		}
	}
	if cut {
		a.apply(edits)
	}
}
func clipboardCommand(write bool) []string {
	options := [][]string{{"pbpaste"}, {"wl-paste", "--no-newline"}, {"xclip", "-selection", "clipboard", "-o"}, {"xsel", "--clipboard", "--output"}}
	if write {
		options = [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard", "-i"}, {"xsel", "--clipboard", "--input"}}
	}
	for _, cmd := range options {
		if cmd[0] == "wl-copy" || cmd[0] == "wl-paste" {
			if os.Getenv("WAYLAND_DISPLAY") == "" {
				continue
			}
		}
		if cmd[0] == "xclip" || cmd[0] == "xsel" {
			if os.Getenv("DISPLAY") == "" {
				continue
			}
		}
		if _, err := exec.LookPath(cmd[0]); err == nil {
			return cmd
		}
	}
	return nil
}
func (a *App) pasteClipboard() {
	if os.Getenv("SSH_CONNECTION") == "" && os.Getenv("SSH_TTY") == "" {
		if cmd := clipboardCommand(false); cmd != nil {
			b := a.Current()
			version := b.Doc.Version
			selections := append([]text.Selection(nil), b.Doc.Selections...)
			go func() {
				ctx, cancel := context.WithTimeout(a.ctx, time.Second)
				defer cancel()
				data, err := exec.CommandContext(ctx, cmd[0], cmd[1:]...).Output()
				a.post(func() {
					if b != a.Current() || b.Doc.Version != version || !slices.Equal(selections, b.Doc.Selections) || a.Dialog != nil || a.Preview != nil {
						return
					}
					if err != nil {
						a.Status = "Clipboard unavailable; use terminal paste"
						return
					}
					a.pasteText(string(data))
				})
			}()
			return
		}
	}
	a.clipboardTarget = a.Current()
	a.clipboardVersion = a.Current().Doc.Version
	a.clipboardSelections = append([]text.Selection(nil), a.Current().Doc.Selections...)
	a.clipboardRequested = true
	a.Screen.GetClipboard()
	a.Status = "Requested terminal clipboard; if blocked, use terminal paste (Ctrl+Shift+V / Cmd+V)"
}
