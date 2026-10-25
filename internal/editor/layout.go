package editor

// A layout leaf refers to a pane; internal nodes split just that subtree.
// Ratios are thousandths, preserving layout across terminal resizes.
type splitNode struct {
	Pane          int  `json:"pane,omitempty"`
	Horizontal    bool `json:"horizontal,omitempty"`
	Ratio         int  `json:"ratio,omitempty"`
	First, Second *splitNode
}

const maxPanes = 16

type divider struct {
	Node         *splitNode
	Area         rectangle
	X, Y, Length int
}

func (n *splitNode) leaf() bool { return n != nil && n.First == nil && n.Second == nil }
func (n *splitNode) find(pane int) *splitNode {
	if n == nil {
		return nil
	}
	if n.leaf() {
		if n.Pane == pane {
			return n
		}
		return nil
	}
	if p := n.First.find(pane); p != nil {
		return p
	}
	return n.Second.find(pane)
}
func (n *splitNode) remove(pane int) *splitNode {
	if n == nil {
		return nil
	}
	if n.leaf() {
		if n.Pane == pane {
			return nil
		}
		if n.Pane > pane {
			n.Pane--
		}
		return n
	}
	n.First = n.First.remove(pane)
	n.Second = n.Second.remove(pane)
	if n.First == nil {
		return n.Second
	}
	if n.Second == nil {
		return n.First
	}
	return n
}
func (n *splitNode) parent(pane int) *splitNode {
	if n == nil || n.leaf() {
		return nil
	}
	if (n.First.leaf() && n.First.Pane == pane) || (n.Second.leaf() && n.Second.Pane == pane) {
		return n
	}
	if p := n.First.parent(pane); p != nil {
		return p
	}
	return n.Second.parent(pane)
}
func legacyLayout(count int, horizontal bool) *splitNode {
	var build func(int, int) *splitNode
	build = func(start, count int) *splitNode {
		if count <= 1 {
			return &splitNode{Pane: start}
		}
		return &splitNode{Horizontal: horizontal, Ratio: 1000 / count, First: &splitNode{Pane: start}, Second: build(start+1, count-1)}
	}
	return build(0, max(1, count))
}
func validLayout(root *splitNode, count int) bool {
	seen := map[int]bool{}
	var check func(*splitNode, int) bool
	check = func(n *splitNode, depth int) bool {
		if n == nil || depth > maxPanes {
			return false
		}
		if n.leaf() {
			if n.Pane < 0 || n.Pane >= count || seen[n.Pane] {
				return false
			}
			seen[n.Pane] = true
			return true
		}
		return n.Ratio >= 1 && n.Ratio <= 999 && check(n.First, depth+1) && check(n.Second, depth+1)
	}
	return check(root, 0) && len(seen) == count
}
func remapLayout(n *splitNode, indices map[int]int) *splitNode {
	if n == nil {
		return nil
	}
	if n.leaf() {
		if i, ok := indices[n.Pane]; ok {
			return &splitNode{Pane: i}
		}
		return nil
	}
	first, second := remapLayout(n.First, indices), remapLayout(n.Second, indices)
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}
	return &splitNode{Horizontal: n.Horizontal, Ratio: n.Ratio, First: first, Second: second}
}
func (n *splitNode) arrange(r rectangle, out *[maxPanes]rectangle, dividers *[]divider) {
	if n == nil {
		return
	}
	if n.leaf() {
		if n.Pane >= 0 && n.Pane < maxPanes {
			out[n.Pane] = r
		}
		return
	}
	first, second := r, r
	ratio := max(1, min(999, n.Ratio))
	if n.Horizontal {
		available := max(0, r.H-1)
		first.H = available * ratio / 1000
		second.Y = r.Y + first.H + 1
		second.H = available - first.H
		if dividers != nil {
			*dividers = append(*dividers, divider{n, r, r.X, r.Y + first.H, r.W})
		}
	} else {
		available := max(0, r.W-1)
		first.W = available * ratio / 1000
		second.X = r.X + first.W + 1
		second.W = available - first.W
		if dividers != nil {
			*dividers = append(*dividers, divider{n, r, r.X + first.W, r.Y, r.H})
		}
	}
	n.First.arrange(first, out, dividers)
	n.Second.arrange(second, out, dividers)
}
func (a *App) workspaceRect() rectangle {
	w, h := a.Screen.Size()
	left, right := a.sidebarWidth(), a.minimapWidth()
	return rectangle{left, 1, max(0, w-left-right), max(0, h-3)}
}
func (a *App) rect(i int) rectangle {
	area := a.workspaceRect()
	if len(a.panes) <= 1 || a.layout == nil {
		return area
	}
	var boxes [maxPanes]rectangle
	a.layout.arrange(area, &boxes, nil)
	if i < 0 || i >= maxPanes {
		return rectangle{}
	}
	r := boxes[i]
	r.Y++
	r.H = max(0, r.H-1)
	return r
}
func (a *App) resizePane(delta int) {
	a.ensurePanes()
	if n := a.layout.parent(a.focus); n != nil {
		if n.First.find(a.focus) == nil {
			delta = -delta
		}
		n.Ratio = max(100, min(900, n.Ratio+delta))
		a.reveal()
	}
}
func (a *App) focusDirection(dx, dy int) {
	a.ensurePanes()
	r := a.rect(a.focus)
	x, y := 2*r.X+r.W, 2*r.Y+r.H
	best, score := -1, int(^uint(0)>>1)
	for i := range a.panes {
		if i == a.focus {
			continue
		}
		other := a.rect(i)
		ox, oy := 2*other.X+other.W-x, 2*other.Y+other.H-y
		if dx != 0 && ox*dx <= 0 || dy != 0 && oy*dy <= 0 {
			continue
		}
		s := ox*ox + oy*oy
		if s < score {
			best, score = i, s
		}
	}
	if best >= 0 {
		a.focusPane(best)
	}
}
