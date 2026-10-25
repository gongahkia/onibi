package editor

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/dap"
	"github.com/gongahkia/shack/internal/text"
)

func (a *App) breakpointPaths() []string {
	paths := []string{}
	for path := range a.breakpoints {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
func (a *App) breakpointSpecs(path string) []dap.SourceBreakpoint {
	specs := []dap.SourceBreakpoint{}
	for _, point := range a.breakpoints[path] {
		specs = append(specs, point.Spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Line < specs[j].Line })
	return specs
}
func (a *App) toggleBreakpoint() {
	b := a.Current()
	if b.Transient || b.File.Path == "" {
		a.Status = "Breakpoints need a saved source file"
		return
	}
	line := b.Doc.Text.LineAt(b.Doc.Selections[0].Head) + 1
	a.toggleBreakpointAt(b, line)
}
func (a *App) toggleBreakpointAt(b *Buffer, line int) {
	if b.Transient || b.File.Path == "" {
		return
	}
	if a.breakpoints == nil {
		a.breakpoints = map[string]map[int]*debugBreakpoint{}
	}
	points := a.breakpoints[b.File.Path]
	if points == nil {
		points = map[int]*debugBreakpoint{}
		a.breakpoints[b.File.Path] = points
	}
	if _, ok := points[line]; ok {
		delete(points, line)
		a.Status = fmt.Sprintf("Removed breakpoint at %s:%d", filepath.Base(b.File.Path), line)
	} else {
		count := 0
		for _, set := range a.breakpoints {
			count += len(set)
		}
		if count >= 1024 || len(points) >= 256 {
			a.Status = "Breakpoint limit: 256 per file, 1,024 per session"
			return
		}
		points[line] = &debugBreakpoint{Spec: dap.SourceBreakpoint{Line: line}, Buffer: b, Offset: b.Doc.Text.LineStart(line - 1), Version: b.Doc.Version, Message: "Pending adapter verification"}
		a.Status = fmt.Sprintf("Breakpoint at %s:%d", filepath.Base(b.File.Path), line)
	}
	if b.Doc.Dirty() {
		a.Status += " · save to synchronize"
		return
	}
	a.syncBreakpoints(b.File.Path)
}
func (a *App) rebaseDebug(b *Buffer) {
	points := a.breakpoints[b.File.Path]
	if len(points) == 0 {
		return
	}
	changed := false
	updated := map[int]*debugBreakpoint{}
	for _, point := range points {
		if point.Buffer == b && point.Version != b.Doc.Version {
			for _, edits := range b.Doc.LastChanges {
				point.Offset = text.Rebase(point.Offset, edits)
			}
			point.Offset = min(b.Doc.Text.Len(), max(0, point.Offset))
			point.Spec.Line = b.Doc.Text.LineAt(point.Offset) + 1
			point.Version = b.Doc.Version
			point.Verified = false
			point.Message = "Source changed; save to synchronize breakpoints; restart to reload code"
			changed = true
		}
		updated[point.Spec.Line] = point
	}
	if changed {
		a.breakpoints[b.File.Path] = updated
	}
}
func (a *App) adoptDebugBreakpoints(b *Buffer) {
	for _, point := range a.breakpoints[b.File.Path] {
		if point.Buffer != b {
			point.Buffer = b
			point.Offset = b.Doc.Text.LineStart(max(0, point.Spec.Line-1))
			point.Version = b.Doc.Version
		}
	}
}
func (a *App) syncAllBreakpoints() {
	for _, path := range a.breakpointPaths() {
		a.syncBreakpoints(path)
	}
}
func (a *App) syncBreakpoints(path string) {
	d := a.debug
	if d == nil || d.Client == nil || !d.Ready || d.Ended {
		return
	}
	for _, b := range a.Buffers {
		if b.File.Path == path && b.Doc.Dirty() {
			return
		}
	}
	if d.Syncing[path] {
		d.Again[path] = true
		return
	}
	if len(d.Syncing) >= 4 {
		if !d.Queued[path] {
			d.Queued[path] = true
			d.SyncQueue = append(d.SyncQueue, path)
		}
		return
	}
	specs := a.breakpointSpecs(path)
	for _, point := range specs {
		if point.Condition != "" && !d.Caps.ConditionalBreakpoints {
			a.Status = "Adapter does not support conditional breakpoints"
			return
		}
	}
	d.Syncing[path] = true
	c := d.Client
	go func() {
		ctx, stop := context.WithTimeout(d.Context, 8*time.Second)
		defer stop()
		raw, err := c.Request(ctx, "setBreakpoints", map[string]any{"source": dap.Source{Path: path}, "breakpoints": specs})
		a.post(func() {
			if a.debug != d || d.Ended {
				return
			}
			delete(d.Syncing, path)
			if err != nil {
				a.Status = "Breakpoints: " + err.Error()
				for _, point := range a.breakpoints[path] {
					point.Verified = false
					point.Message = err.Error()
				}
			} else {
				a.acceptBreakpoints(path, specs, raw)
			}
			if d.Again[path] {
				delete(d.Again, path)
				a.syncBreakpoints(path)
			}
			for len(d.Syncing) < 4 && len(d.SyncQueue) > 0 {
				next := d.SyncQueue[0]
				d.SyncQueue = d.SyncQueue[1:]
				delete(d.Queued, next)
				a.syncBreakpoints(next)
			}
		})
	}()
}
func (a *App) acceptBreakpoints(path string, specs []dap.SourceBreakpoint, raw json.RawMessage) {
	var result struct {
		Breakpoints []dap.Breakpoint `json:"breakpoints"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return
	}
	points := a.breakpoints[path]
	if len(points) == 0 && len(specs) == 0 {
		delete(a.breakpoints, path)
		return
	}
	for i, bp := range result.Breakpoints {
		if i >= len(specs) {
			break
		}
		point := points[specs[i].Line]
		if point == nil || point.Spec != specs[i] {
			continue
		}
		point.Verified = bp.Verified
		point.Message = bp.Message
		point.ID = bp.ID
		if bp.Verified && bp.Line > 0 && bp.Line != point.Spec.Line {
			if _, collision := points[bp.Line]; !collision {
				delete(points, point.Spec.Line)
				point.Spec.Line = bp.Line
				points[bp.Line] = point
				if point.Buffer != nil {
					point.Offset = point.Buffer.Doc.Text.LineStart(bp.Line - 1)
				}
			}
			point.Message = fmt.Sprintf("Adapter bound breakpoint to line %d", bp.Line)
		}
	}
}
func (a *App) debugBreakpoints() {
	items := []choice{}
	for _, path := range a.breakpointPaths() {
		for _, spec := range a.breakpointSpecs(path) {
			point := a.breakpoints[path][spec.Line]
			mark := "○"
			if point.Verified {
				mark = "●"
			}
			label := fmt.Sprintf("%s %s:%d", mark, path, spec.Line)
			if point.Spec.Condition != "" {
				label += " if " + point.Spec.Condition
			}
			if point.Message != "" {
				label += " · " + point.Message
			}
			items = append(items, choice{label, func() {
				a.list("Breakpoint · "+filepath.Base(path), []choice{{"Show source", func() {
					if err := a.Open(path); err != nil {
						a.Status = err.Error()
						return
					}
					a.gotoLine(fmt.Sprint(point.Spec.Line))
				}},
					{"Remove breakpoint", func() { delete(a.breakpoints[path], point.Spec.Line); a.syncBreakpoints(path) }},
					{"Edit condition", func() { a.editBreakpointCondition(path, point) }},
					{"Back to breakpoints", a.debugBreakpoints}})
			}})
		}
	}
	if len(items) == 0 {
		a.Status = "No breakpoints · F9 toggles the current source line"
		return
	}
	a.list("Debug breakpoints · ○ pending/unverified · ● verified", items)
}
func (a *App) debugCondition() {
	b := a.Current()
	line := b.Doc.Text.LineAt(b.Doc.Selections[0].Head) + 1
	point := a.breakpoints[b.File.Path][line]
	if point == nil {
		a.toggleBreakpoint()
		point = a.breakpoints[b.File.Path][line]
	}
	if point != nil {
		a.editBreakpointCondition(b.File.Path, point)
	}
}
func (a *App) editBreakpointCondition(path string, point *debugBreakpoint) {
	if d := a.debug; d != nil && d.Ready && !d.Ended && !d.Caps.ConditionalBreakpoints {
		a.Status = "Adapter does not support conditional breakpoints"
		return
	}
	a.prompt("Breakpoint condition (empty means always)", point.Spec.Condition, func(value string) {
		if len(value) > 4096 {
			a.Status = "Condition exceeds 4 KiB"
			return
		}
		if a.breakpoints[path][point.Spec.Line] != point {
			return
		}
		point.Spec.Condition = value
		point.Verified = false
		a.syncBreakpoints(path)
	})
}
func (a *App) debugGutter(b *Buffer, line int, p palette) (string, tcell.Style) {
	if d := a.debug; d != nil && d.Stopped && !d.Ended && !b.Doc.Dirty() && d.Frame.Source.Path == b.File.Path && d.Frame.Line == line {
		return "▶", p.accent
	}
	if point := a.breakpoints[b.File.Path][line]; point != nil {
		if point.Verified && !b.Doc.Dirty() {
			return "●", p.warning
		}
		return "○", p.warning
	}
	return "", p.dim
}
