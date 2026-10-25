package editor

import (
	"github.com/gdamore/tcell/v2"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type treeNode struct {
	Name, Path string
	Dir        bool
	Children   []*treeNode
}
type treeRow struct {
	Node  *treeNode
	Depth int
}
type fileTree struct {
	Roots      []*treeNode
	Rows       []treeRow
	Expanded   map[string]bool
	Index, Top int
	Prefix     string
	Typed      time.Time
}

func buildTree(files []string, expanded map[string]bool) *fileTree {
	t := &fileTree{Expanded: expanded}
	if t.Expanded == nil {
		t.Expanded = map[string]bool{}
	}
	nodes := map[string]*treeNode{}
	for _, path := range files {
		path = filepath.Clean(path)
		if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			continue
		}
		parts := strings.Split(filepath.ToSlash(path), "/")
		parent := ""
		var previous *treeNode
		for i, name := range parts {
			if name == "" || name == "." {
				continue
			}
			current := filepath.Join(parent, name)
			node := nodes[current]
			if node == nil {
				node = &treeNode{Name: name, Path: current, Dir: i < len(parts)-1}
				nodes[current] = node
				if previous == nil {
					t.Roots = append(t.Roots, node)
				} else {
					previous.Children = append(previous.Children, node)
				}
			}
			previous = node
			parent = current
		}
	}
	var order func([]*treeNode)
	order = func(nodes []*treeNode) {
		sort.Slice(nodes, func(i, j int) bool {
			if nodes[i].Dir != nodes[j].Dir {
				return nodes[i].Dir
			}
			return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
		})
		for _, n := range nodes {
			order(n.Children)
		}
	}
	order(t.Roots)
	t.flatten()
	return t
}
func (t *fileTree) flatten() {
	t.Rows = t.Rows[:0]
	var visit func([]*treeNode, int)
	visit = func(nodes []*treeNode, depth int) {
		for _, n := range nodes {
			t.Rows = append(t.Rows, treeRow{n, depth})
			if n.Dir && t.Expanded[n.Path] {
				visit(n.Children, depth+1)
			}
		}
	}
	visit(t.Roots, 0)
	t.Index = max(0, min(t.Index, len(t.Rows)-1))
}
func (a *App) sidebarWidth() int {
	w, _ := a.Screen.Size()
	if !a.treeVisible || w < 70 {
		return 0
	}
	return min(max(18, a.treeWidth), w/3)
}
func (a *App) rebuildTree() {
	if !a.treeVisible && a.tree == nil {
		return
	}
	var expanded map[string]bool
	path := ""
	if a.tree != nil {
		expanded = a.tree.Expanded
		if len(a.tree.Rows) > 0 {
			path = a.tree.Rows[a.tree.Index].Node.Path
		}
	}
	a.tree = buildTree(a.Files, expanded)
	for i, r := range a.tree.Rows {
		if r.Node.Path == path {
			a.tree.Index = i
			break
		}
	}
}
func (a *App) revealTree() {
	a.treeVisible = true
	if a.tree == nil {
		a.rebuildTree()
	}
	rel, err := filepath.Rel(a.Root, a.Current().File.Path)
	if err != nil {
		return
	}
	dir := filepath.Dir(rel)
	for dir != "." && dir != "" && dir != ".." {
		a.tree.Expanded[dir] = true
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	a.tree.flatten()
	for i, r := range a.tree.Rows {
		if r.Node.Path == rel {
			a.tree.Index = i
			break
		}
	}
}
func (a *App) treeCommands() {
	a.Commands = append(a.Commands, command{"Toggle file tree", "Ctrl+B", func() {
		a.treeVisible = !a.treeVisible
		a.treeFocus = false
		if a.treeVisible && a.tree == nil {
			a.rebuildTree()
		}
		a.reveal()
	}}, command{"Focus file tree", "", func() { a.revealTree(); a.treeFocus = a.sidebarWidth() > 0 }}, command{"Reveal file in tree", "", a.revealTree}, command{"Toggle minimap", "", func() {
		a.minimapVisible = !a.minimapVisible
		if !a.minimapVisible {
			a.miniCache = nil
			a.miniWanted = miniJob{}
		}
		a.reveal()
	}}, command{"Context menu", "Shift+F10", func() { a.openContextAtCaret() }})
}
func (a *App) treeOpen() {
	t := a.tree
	if t == nil || len(t.Rows) == 0 {
		return
	}
	n := t.Rows[t.Index].Node
	if n.Dir {
		t.Expanded[n.Path] = !t.Expanded[n.Path]
		t.flatten()
		return
	}
	if err := a.Open(filepath.Join(a.Root, n.Path)); err != nil {
		a.Status = err.Error()
		return
	}
	a.treeFocus = false
	a.Matches = nil
	a.schedule()
	a.reveal()
}
func (a *App) treeKey(ev *tcell.EventKey) bool {
	if !a.treeFocus || a.sidebarWidth() == 0 {
		return false
	}
	t := a.tree
	if t == nil {
		return false
	}
	_, h := a.Screen.Size()
	switch ev.Key() {
	case tcell.KeyUp:
		t.Index = max(0, t.Index-1)
	case tcell.KeyDown:
		t.Index = min(max(0, len(t.Rows)-1), t.Index+1)
	case tcell.KeyPgUp:
		t.Index = max(0, t.Index-max(1, h-5))
	case tcell.KeyPgDn:
		t.Index = min(max(0, len(t.Rows)-1), t.Index+max(1, h-5))
	case tcell.KeyHome:
		t.Index = 0
	case tcell.KeyEnd:
		t.Index = max(0, len(t.Rows)-1)
	case tcell.KeyEnter:
		a.treeOpen()
	case tcell.KeyTab, tcell.KeyEscape:
		a.treeFocus = false
	case tcell.KeyRight:
		if len(t.Rows) > 0 {
			n := t.Rows[t.Index].Node
			if n.Dir {
				if t.Expanded[n.Path] && t.Index+1 < len(t.Rows) {
					t.Index++
				} else {
					t.Expanded[n.Path] = true
					t.flatten()
				}
			} else {
				a.treeOpen()
			}
		}
	case tcell.KeyLeft, tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(t.Rows) > 0 {
			n := t.Rows[t.Index].Node
			if n.Dir && t.Expanded[n.Path] {
				t.Expanded[n.Path] = false
				t.flatten()
			} else {
				parent := filepath.Dir(n.Path)
				for i, r := range t.Rows {
					if r.Node.Path == parent {
						t.Index = i
						break
					}
				}
			}
		}
	case tcell.KeyRune:
		if ev.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 {
			return false
		}
		if time.Since(t.Typed) > time.Second {
			t.Prefix = ""
		}
		t.Typed = time.Now()
		t.Prefix += strings.ToLower(string(ev.Rune()))
		for step := 0; step < len(t.Rows); step++ {
			i := (t.Index + step) % len(t.Rows)
			if strings.HasPrefix(strings.ToLower(t.Rows[i].Node.Name), t.Prefix) {
				t.Index = i
				break
			}
		}
	case tcell.KeyCtrlX, tcell.KeyCtrlV, tcell.KeyDelete:
		a.Status = "Use the file tree to navigate; Ctrl+O creates or opens a path"
	default:
		return false
	}
	return true
}
func (a *App) drawTree(p palette) {
	width := a.sidebarWidth()
	if width == 0 {
		return
	}
	_, height := a.Screen.Size()
	if a.tree == nil {
		a.rebuildTree()
	}
	t := a.tree
	for y := 1; y < height-2; y++ {
		a.fill(0, y, width-1, p.bar)
		a.Screen.SetContent(width-1, y, '│', nil, p.dim)
	}
	title := filepath.Base(a.Root)
	if a.treeFocus {
		title = "› " + title
	}
	a.put(1, 1, width-3, title, p.accent.Bold(true))
	visible := max(0, height-5)
	if t.Index < t.Top {
		t.Top = t.Index
	}
	if t.Index >= t.Top+visible {
		t.Top = max(0, t.Index-visible+1)
	}
	current, _ := filepath.Rel(a.Root, a.Current().File.Path)
	for row := 0; row < visible && row+t.Top < len(t.Rows); row++ {
		i := row + t.Top
		r := t.Rows[i]
		marker := "  "
		if r.Node.Dir {
			marker = "▸ "
			if t.Expanded[r.Node.Path] {
				marker = "▾ "
			}
		}
		style := p.bar
		if current == r.Node.Path {
			style = style.Bold(true)
		}
		if i == t.Index && a.treeFocus {
			style = p.selection
		}
		a.fill(0, row+2, width-1, style)
		a.put(1, row+2, width-3, strings.Repeat(" ", min(r.Depth*2, width-5))+marker+r.Node.Name, style)
	}
	if len(t.Rows) == 0 {
		a.put(1, 3, width-3, "No indexed files", p.dim)
	}
	if a.treeFocus {
		a.Screen.HideCursor()
	}
}
