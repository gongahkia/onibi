package editor

import (
	"context"
	"fmt"
	"github.com/gongahkia/shack/internal/process"
	"github.com/gongahkia/shack/internal/text"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (a *App) developmentCommands() {
	a.Commands = append(a.Commands, command{"Run build / test task", "", a.taskPicker}, command{"Run shell command", "", func() {
		a.prompt("Run shell command in project · no interactive input", "", func(s string) {
			if strings.TrimSpace(s) != "" {
				a.confirmTask(s, []string{"sh", "-c", s})
			}
		})
	}},
		command{"Cancel running task", "", func() {
			if a.taskCancel != nil {
				a.taskCancel()
				a.Status = "Cancelling task"
			} else {
				a.Status = "No task is running"
			}
		}},
		command{"Task output locations", "", a.outputLocations}, command{"Git status", "", a.gitStatus}, command{"Git diff", "", func() { a.gitDiff(false) }}, command{"Git staged diff", "", func() { a.gitDiff(true) }},
		command{"Git stage current file", "", func() { a.gitStage(false) }}, command{"Git unstage current file", "", func() { a.gitStage(true) }})
}
func (a *App) taskPicker() {
	tasks := map[string][]string{}
	for name, args := range a.Config.Tasks {
		tasks[name] = append([]string(nil), args...)
	}
	exists := func(name string) bool {
		st, e := os.Stat(filepath.Join(a.Root, name))
		return e == nil && st.Mode().IsRegular()
	}
	presets := map[string][]string{}
	switch {
	case exists("go.mod"):
		presets["Build Go project"] = []string{"go", "build", "./..."}
		presets["Test Go project"] = []string{"go", "test", "./..."}
	case exists("Cargo.toml"):
		presets["Build Rust project"] = []string{"cargo", "build"}
		presets["Test Rust project"] = []string{"cargo", "test"}
	case exists("package.json"):
		presets["Build npm project"] = []string{"npm", "run", "build"}
		presets["Test npm project"] = []string{"npm", "test"}
	case exists("Makefile"):
		presets["Build with make"] = []string{"make"}
		presets["Test with make"] = []string{"make", "test"}
	}
	for name, args := range presets {
		if _, ok := tasks[name]; !ok {
			tasks[name] = args
		}
	}
	names := []string{}
	for name := range tasks {
		names = append(names, name)
	}
	sort.Strings(names)
	items := []choice{}
	for _, name := range names {
		argv := tasks[name]
		items = append(items, choice{name + " · " + strings.Join(argv, " "), func() { a.confirmTask(name, argv) }})
	}
	if len(items) == 0 {
		a.Status = "No tasks detected; configure tasks or use Run shell command"
		return
	}
	a.list("Run task in project · starts only on Enter", items)
}
func (a *App) confirmTask(name string, argv []string) {
	if a.taskCancel != nil {
		a.Status = "Cancel the running task first"
		return
	}
	dirty := false
	for _, b := range a.Buffers {
		dirty = dirty || (!b.Transient && b.Doc.Dirty())
	}
	if dirty {
		a.list("Task reads files from disk; some buffers are unsaved", []choice{{"Save all and run", func() { a.saveAll(func() { a.startTask(name, argv) }) }}, {"Run using files currently on disk", func() { a.startTask(name, argv) }}, {"Cancel", func() {}}})
		return
	}
	a.startTask(name, argv)
}
func (a *App) startTask(name string, argv []string) {
	if a.taskCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.taskCancel = cancel
	a.taskSeq++
	seq := a.taskSeq
	root := a.Root
	header := "$ " + strings.Join(argv, " ") + "\nDirectory: " + root + "\n\n"
	a.report("Task output", header)
	b := a.Current()
	a.taskBuffer = b
	a.taskRoot = root
	update := func(body string, finished bool, err error) {
		a.post(func() {
			if seq != a.taskSeq {
				return
			}
			tail := ""
			if finished {
				tail = "\n[completed successfully]\n"
				if err != nil {
					tail = "\n[" + err.Error() + "]\n"
				}
				a.taskCancel = nil
				cancel()
				a.Status = name + strings.TrimSpace(tail)
			}
			found := false
			for _, open := range a.Buffers {
				found = found || open == b
			}
			if !found {
				return
			}
			// A report is a read-only view, not an edit history that grows with every update.
			d, _ := text.NewDocument(header + stripANSI(body) + tail)
			old := b.Doc.Selections[0].Head
			b.Doc = d
			old = min(old, d.Text.Len())
			for !d.Text.Boundary(old) {
				old--
			}
			b.Doc.Selections = []text.Selection{{Anchor: old, Head: old}}
		})
	}
	a.Status = "Task running · Cancel running task stops its process group"
	go func() {
		body, err := process.Run(ctx, argv, root, 1<<20, func(s string) { update(s, false, nil) })
		update(body, true, err)
	}()
}

var ansi = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\))`)

func stripANSI(s string) string { return strings.ReplaceAll(ansi.ReplaceAllString(s, ""), "\r", "") }

var outputLocation = regexp.MustCompile(`^\s*(.+?):(\d+)(?::(\d+))?(?::|\s|$)`)

func (a *App) outputLocations() {
	b := a.taskBuffer
	if b == nil {
		a.Status = "No task output"
		return
	}
	items := []choice{}
	for _, line := range strings.Split(b.Doc.Text.String(), "\n") {
		m := outputLocation.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		path := m[1]
		if !filepath.IsAbs(path) {
			path = filepath.Join(a.taskRoot, path)
		}
		lineNo, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		label := line
		items = append(items, choice{label, func() {
			a.rememberJump()
			if err := a.Open(path); err != nil {
				a.Status = err.Error()
				return
			}
			b := a.Current()
			ln := max(0, min(b.Doc.Text.Lines()-1, lineNo-1))
			p := b.Doc.Text.LineStart(ln) + max(0, col-1)
			p = min(p, b.Doc.Text.LineEnd(ln))
			for !b.Doc.Text.Boundary(p) {
				p--
			}
			a.jumpTo(b, p)
		}})
		if len(items) >= 2000 {
			break
		}
	}
	if len(items) == 0 {
		a.Status = "No path:line[:column] locations in output"
		return
	}
	a.list("Task output locations", items)
}
func (a *App) gitRun(args []string, done func(string)) {
	root := a.Root
	a.Status = "Git…"
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		out, err := process.Run(ctx, append([]string{"git", "--no-pager", "-C", root}, args...), root, 1<<20, nil)
		a.post(func() {
			if err != nil {
				a.Status = "Git: " + err.Error() + " " + displayExcerpt(out)
				return
			}
			done(out)
		})
	}()
}
func (a *App) gitStatus() {
	root := a.Root
	a.gitRun([]string{"status", "--porcelain=v1", "-z"}, func(out string) {
		parts := strings.Split(out, "\x00")
		items := []choice{}
		for i := 0; i < len(parts); i++ {
			entry := parts[i]
			if len(entry) < 4 {
				continue
			}
			state, path := entry[:2], entry[3:]
			label := state + " " + path
			if strings.ContainsAny(state, "RC") && i+1 < len(parts) {
				i++
				label += " ← " + parts[i]
			}
			items = append(items, choice{label, func() {
				if strings.Contains(state, "D") {
					a.Status = "Deleted file; inspect Git diff"
					return
				}
				a.rememberJump()
				if err := a.Open(filepath.Join(root, path)); err != nil {
					a.Status = err.Error()
					return
				}
				a.jumpTo(a.Current(), 0)
			}})
		}
		if len(items) == 0 {
			a.Status = "Git working tree is clean"
			return
		}
		a.list("Git status · Enter opens a file", items)
	})
}
func (a *App) gitDiff(staged bool) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--color=never"}
	title := "Git diff (saved files)"
	if staged {
		args = append(args, "--cached")
		title = "Git staged diff"
	}
	args = append(args, "--")
	a.gitRun(args, func(out string) {
		if out == "" {
			a.Status = "No diff"
			return
		}
		a.report(title, out)
	})
}
func (a *App) gitStage(unstage bool) {
	b := a.Current()
	if a.Readonly || b.Transient || b.File.Path == "" {
		a.Status = "Open a file in writable mode first"
		return
	}
	if b.Doc.Dirty() {
		a.Status = "Save the file before staging"
		return
	}
	args := []string{"add", "--", b.File.Path}
	if unstage {
		args = []string{"reset", "-q", "HEAD", "--", b.File.Path}
	}
	a.gitRun(args, func(string) { a.Status = fmt.Sprintf("Git index updated for %s", bufferName(b)); a.indexFiles() })
}
