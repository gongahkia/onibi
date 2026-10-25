package editor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gongahkia/shack/internal/config"
	"github.com/gongahkia/shack/internal/dap"
	"github.com/gongahkia/shack/internal/text"
)

type debugSession struct {
	Initialized, InitializeDone        bool
	Context                            context.Context
	Cancel                             context.CancelFunc
	Client                             *dap.Client
	Name, Request, Phase               string
	Caps                               dap.Capabilities
	Ready, Configuring, Stopped, Ended bool
	Epoch                              uint64
	Thread                             int
	Frame                              dap.StackFrame
	FrameReady                         bool
	AfterFrame                         func()
	Stack                              []dap.StackFrame
	OutputBuffer                       *Buffer
	WantsOutput                        atomic.Bool
	Syncing                            map[string]bool
	Again                              map[string]bool
	Queued                             map[string]bool
	SyncQueue                          []string
}
type debugBreakpoint struct {
	ID       int
	Spec     dap.SourceBreakpoint
	Buffer   *Buffer
	Offset   int
	Version  uint64
	Verified bool
	Message  string
}

func (a *App) debugCommands() {
	a.Commands = append(a.Commands,
		command{"Debug start or continue", "F5", a.debugContinue},
		command{"Debug toggle breakpoint", "F9", a.toggleBreakpoint},
		command{"Debug step over", "F10", func() { a.debugControl("next") }},
		command{"Debug step into", "F11", func() { a.debugControl("stepIn") }},
		command{"Debug step out", "Shift+F11", func() { a.debugControl("stepOut") }},
		command{"Debug pause", "", func() { a.debugControl("pause") }},
		command{"Debug stop or detach", "Shift+F5", a.stopDebugger},
		command{"Debug threads", "", a.debugThreads}, command{"Debug stack frames", "", a.debugStack},
		command{"Debug scopes and variables", "", a.debugScopes}, command{"Debug evaluate expression", "", a.debugEvaluate},
		command{"Debug breakpoints", "", a.debugBreakpoints}, command{"Debug edit breakpoint condition", "", a.debugCondition},
		command{"Debug synchronize breakpoints", "", a.syncAllBreakpoints},
		command{"Debug output", "", a.debugOutput}, command{"Debug status", "", a.debugStatus},
		command{"Debug current location", "", func() {
			if d := a.debug; d != nil && d.Stopped {
				a.debugLocation(d, d.Frame)
			}
		}})
}
func (a *App) debugContinue() {
	if a.debug == nil || a.debug.Ended {
		a.debugPicker()
		return
	}
	a.debugControl("continue")
}
func (a *App) debugPicker() {
	profiles := map[string]config.DebugProfile{}
	for name, p := range a.Config.Debuggers {
		profiles[name] = p
	}
	if filepath.Ext(a.Current().File.Path) == ".py" {
		if python, err := exec.LookPath("python3"); err == nil {
			name := "Python current file (requires debugpy)"
			if _, exists := profiles[name]; !exists {
				profiles[name] = config.DebugProfile{Command: []string{python, "-m", "debugpy.adapter"}, Request: "launch", Arguments: map[string]any{"program": "${file}", "cwd": "${workspaceFolder}", "python": []any{python}, "console": "internalConsole", "redirectOutput": true, "subProcess": false}}
			}
		}
	}
	names := []string{}
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	items := []choice{}
	for _, name := range names {
		p := profiles[name]
		label := strings.Join(p.Command, " ")
		if p.Address != "" {
			label = p.Address
		}
		items = append(items, choice{name + " · " + p.Request + " · " + label, func() { a.prepareDebug(name, p) }})
	}
	if len(items) == 0 {
		a.Status = "Configure a debugger profile with a DAP command/address and launch/attach arguments"
		return
	}
	a.list("Debug profile · starts only on Enter", items)
}
func expandDebug(value any, root, file string, depth int) (any, error) {
	if depth > 32 {
		return nil, errors.New("debug arguments are nested too deeply")
	}
	switch v := value.(type) {
	case string:
		if file == "" && (strings.Contains(v, "${file}") || strings.Contains(v, "${fileDirname}") || strings.Contains(v, "${fileBasename}")) {
			return nil, errors.New("debug profile needs a saved current file")
		}
		v = strings.NewReplacer("${workspaceFolder}", root, "${file}", file, "${fileDirname}", filepath.Dir(file), "${fileBasename}", filepath.Base(file)).Replace(v)
		return v, nil
	case []any:
		result := make([]any, len(v))
		for i, x := range v {
			e, err := expandDebug(x, root, file, depth+1)
			if err != nil {
				return nil, err
			}
			result[i] = e
		}
		return result, nil
	case map[string]any:
		result := map[string]any{}
		for k, x := range v {
			e, err := expandDebug(x, root, file, depth+1)
			if err != nil {
				return nil, err
			}
			result[k] = e
		}
		return result, nil
	default:
		return value, nil
	}
}
func (a *App) prepareDebug(name string, p config.DebugProfile) {
	if a.debug != nil && (!a.debug.Ended || a.debug.Phase == "stopping") {
		a.Status = "Stop or detach the current debug session first"
		return
	}
	selected := a.Current()
	for _, b := range a.Buffers {
		if !b.Transient && b.Doc.Dirty() {
			a.list("Debugging reads saved files", []choice{{"Save all and start debugging", func() {
				a.saveAll(func() {
					for i, open := range a.Buffers {
						if open == selected {
							a.Active = i
							break
						}
					}
					a.prepareDebug(name, p)
				})
			}}, {"Cancel", func() {}}})
			return
		}
	}
	if err := p.Validate(); err != nil {
		a.Status = err.Error()
		return
	}
	expanded, err := expandDebug(p.Arguments, a.Root, a.Current().File.Path, 0)
	if err != nil {
		a.Status = err.Error()
		return
	}
	args, _ := expanded.(map[string]any)
	if args == nil {
		args = map[string]any{}
	}
	argv := append([]string(nil), p.Command...)
	for i, s := range argv {
		v, e := expandDebug(s, a.Root, a.Current().File.Path, 0)
		if e != nil {
			a.Status = e.Error()
			return
		}
		argv[i] = v.(string)
	}
	ctx, cancel := context.WithCancel(a.ctx)
	d := &debugSession{Context: ctx, Cancel: cancel, Name: name, Request: p.Request, Phase: "starting", Syncing: map[string]bool{}, Again: map[string]bool{}, Queued: map[string]bool{}}
	a.debug = d
	root := a.Root
	a.Status = "Starting debugger · " + name
	for _, points := range a.breakpoints {
		for _, point := range points {
			point.Verified = false
			point.Message = "Pending adapter verification"
		}
	}
	go func() {
		var c *dap.Client
		var err error
		if p.Address != "" {
			c, err = dap.Dial(ctx, p.Address)
		} else {
			c, err = dap.Start(ctx, argv, root)
		}
		a.post(func() {
			if a.debug != d || d.Ended {
				if c != nil {
					c.Close()
				}
				return
			}
			if err != nil {
				d.Ended = true
				d.Phase = err.Error()
				cancel()
				a.Status = "Debugger: " + err.Error()
				return
			}
			d.Client = c
			go a.debugEvents(d, c)
			go func() {
				requestCtx, stop := context.WithTimeout(ctx, 8*time.Second)
				caps, e := c.Initialize(requestCtx)
				stop()
				if e != nil {
					a.debugFailure(d, e)
					return
				}
				a.post(func() {
					if a.debug == d && !d.Ended {
						d.Caps = caps
						d.InitializeDone = true
						d.Phase = "configuring"
						if d.Initialized {
							a.configureDebug(d)
						}
					}
				})
				requestCtx, stop = context.WithTimeout(ctx, 30*time.Second)
				_, e = c.Request(requestCtx, p.Request, args)
				stop()
				if e != nil {
					a.debugFailure(d, e)
					return
				}
				a.post(func() {
					if a.debug == d && !d.Ended && !d.Stopped {
						d.Phase = "running"
						a.Status = "Debugger running · F9 breakpoint · Debug pause to suspend"
					}
				})
			}()
		})
	}()
}
func (a *App) debugFailure(d *debugSession, err error) {
	a.post(func() {
		if a.debug == d && !d.Ended {
			d.Ended = true
			d.Stopped = false
			d.Epoch++
			d.Phase = err.Error()
			a.Status = "Debugger: " + err.Error()
			if d.Client == nil {
				d.Cancel()
				return
			}
			go func() {
				ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
				defer stop()
				_ = d.Client.Disconnect(ctx, d.Request == "launch")
				d.Cancel()
			}()
		}
	})
}
func (a *App) debugEvents(d *debugSession, c *dap.Client) {
	var timer *time.Timer
	var tick <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case event := <-c.Events:
			a.post(func() {
				if a.debug == d && !d.Ended {
					a.debugEvent(d, event)
				}
			})
		case <-c.OutputReady:
			if tick == nil {
				timer = time.NewTimer(100 * time.Millisecond)
				tick = timer.C
			}
		case <-tick:
			tick = nil
			if !d.WantsOutput.Load() {
				continue
			}
			value := c.Output()
			a.post(func() {
				if a.debug == d {
					a.updateDebugOutput(d, value)
				}
			})
		case <-c.Done():
		drain:
			for {
				select {
				case event := <-c.Events:
					a.post(func() {
						if a.debug == d && !d.Ended {
							a.debugEvent(d, event)
						}
					})
				default:
					break drain
				}
			}
			value := c.Output()
			a.post(func() {
				if a.debug == d {
					a.updateDebugOutput(d, value)
					if !d.Ended {
						d.Ended = true
						d.Stopped = false
						d.Epoch++
						d.Phase = fmt.Sprint(c.Err())
						a.Status = "Debugger ended · " + d.Phase
					}
					d.Cancel()
				}
			})
			return
		case <-d.Context.Done():
			return
		}
	}
}
func (a *App) debugEvent(d *debugSession, event dap.Message) {
	switch event.Event {
	case "initialized":
		d.Initialized = true
		if d.InitializeDone {
			a.configureDebug(d)
		}
	case "stopped":
		var stop dap.Stopped
		if json.Unmarshal(event.Body, &stop) != nil {
			return
		}
		d.Stopped = true
		d.Epoch++
		d.Thread = stop.ThreadID
		d.Frame = dap.StackFrame{}
		d.FrameReady = false
		d.AfterFrame = nil
		d.Stack = nil
		d.Phase = "stopped: " + stop.Reason
		a.Status = "Debugger " + d.Phase + " · F10 over · F11 into · Shift+F11 out · F5 continue"
		if d.Thread > 0 {
			a.loadDebugStack(d, d.Thread, true, 0)
		} else {
			a.debugRequest("threads", nil, func(raw json.RawMessage) {
				var result struct {
					Threads []dap.Thread `json:"threads"`
				}
				if json.Unmarshal(raw, &result) == nil && len(result.Threads) > 0 {
					d.Thread = result.Threads[0].ID
					a.loadDebugStack(d, d.Thread, true, 0)
				}
			})
		}
	case "continued":
		d.Stopped = false
		d.Epoch++
		d.Phase = "running"
	case "exited":
		var value struct {
			Code int `json:"exitCode"`
		}
		_ = json.Unmarshal(event.Body, &value)
		d.Phase = fmt.Sprintf("exited (%d)", value.Code)
	case "terminated":
		d.Ended = true
		d.Stopped = false
		d.Epoch++
		if !strings.HasPrefix(d.Phase, "exited") {
			d.Phase = "terminated"
		}
		a.Status = "Debugger " + d.Phase
		go func() {
			ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			_ = d.Client.Disconnect(ctx, false)
		}()
	case "breakpoint":
		var result struct {
			Breakpoint dap.Breakpoint `json:"breakpoint"`
		}
		if json.Unmarshal(event.Body, &result) != nil {
			return
		}
		bp := result.Breakpoint
		for path, points := range a.breakpoints {
			for _, point := range points {
				if (bp.ID != 0 && point.ID == bp.ID) || (bp.Source.Path == path && bp.Line == point.Spec.Line) {
					point.Verified = bp.Verified
					point.Message = bp.Message
				}
			}
		}
	}
}
func (a *App) configureDebug(d *debugSession) {
	if d.Configuring || d.Ready {
		return
	}
	d.Configuring = true
	paths := a.breakpointPaths()
	snapshots := map[string][]dap.SourceBreakpoint{}
	for _, path := range paths {
		snapshots[path] = a.breakpointSpecs(path)
	}
	caps := d.Caps
	go func() {
		for _, path := range paths {
			specs := snapshots[path]
			for _, point := range specs {
				if point.Condition != "" && !caps.ConditionalBreakpoints {
					a.debugFailure(d, errors.New("adapter does not support conditional breakpoints"))
					return
				}
			}
			ctx, stop := context.WithTimeout(d.Context, 8*time.Second)
			raw, err := d.Client.Request(ctx, "setBreakpoints", map[string]any{"source": dap.Source{Path: path}, "breakpoints": specs})
			stop()
			if err != nil {
				a.debugFailure(d, err)
				return
			}
			a.post(func() {
				if a.debug == d && !d.Ended {
					a.acceptBreakpoints(path, specs, raw)
				}
			})
		}
		if caps.ConfigurationDone {
			ctx, stop := context.WithTimeout(d.Context, 8*time.Second)
			_, err := d.Client.Request(ctx, "configurationDone", map[string]any{})
			stop()
			if err != nil {
				a.debugFailure(d, err)
				return
			}
		}
		a.post(func() {
			if a.debug == d && !d.Ended {
				d.Ready = true
				a.syncAllBreakpoints()
			}
		})
	}()
}
func (a *App) debugRequest(method string, args any, done func(json.RawMessage)) {
	d := a.debug
	if d == nil || d.Client == nil || d.Ended {
		a.Status = "No active debug session"
		return
	}
	epoch := d.Epoch
	c := d.Client
	go func() {
		ctx, stop := context.WithTimeout(d.Context, 8*time.Second)
		defer stop()
		raw, err := c.Request(ctx, method, args)
		a.post(func() {
			if a.debug != d || d.Ended || d.Epoch != epoch {
				return
			}
			if err != nil {
				a.Status = "Debugger: " + err.Error()
				return
			}
			if done != nil {
				done(raw)
			}
		})
	}()
}
func (a *App) debugControl(method string) {
	d := a.debug
	if d == nil || d.Client == nil || d.Ended || !d.Ready {
		a.Status = "Debugger is not ready"
		return
	}
	if method == "pause" {
		if d.Stopped {
			a.Status = "Debugger is already stopped"
			return
		}
	} else if !d.Stopped {
		a.Status = "Debugger is running; use Debug pause first"
		return
	}
	if d.Thread == 0 {
		if method == "pause" {
			a.debugRequest("threads", nil, func(raw json.RawMessage) {
				var r struct {
					Threads []dap.Thread `json:"threads"`
				}
				if json.Unmarshal(raw, &r) == nil && len(r.Threads) > 0 {
					d.Thread = r.Threads[0].ID
					a.debugControl(method)
				}
			})
		} else {
			a.Status = "Choose a debug thread first"
		}
		return
	}
	wasStopped := d.Stopped
	d.Epoch++
	epoch := d.Epoch
	if method != "pause" {
		d.Stopped = false
		d.Phase = "running"
	}
	c, thread := d.Client, d.Thread
	go func() {
		ctx, stop := context.WithTimeout(d.Context, 8*time.Second)
		defer stop()
		_, err := c.Request(ctx, method, map[string]int{"threadId": thread})
		a.post(func() {
			if a.debug != d || d.Ended || d.Epoch != epoch {
				return
			}
			if err != nil {
				d.Stopped = wasStopped
				d.Phase = "request failed"
				a.Status = "Debugger: " + err.Error()
			} else {
				a.Status = "Debugger: " + method
			}
		})
	}()
}
func (a *App) stopDebugger() {
	d := a.debug
	if d == nil || d.Ended {
		a.Status = "No active debug session"
		return
	}
	d.Ended = true
	d.Stopped = false
	d.Epoch++
	d.Phase = "stopping"
	if d.Client == nil {
		d.Cancel()
		d.Phase = "stopped"
		return
	}
	c := d.Client
	go func() {
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		err := c.Disconnect(ctx, d.Request == "launch")
		d.Cancel()
		a.post(func() {
			if a.debug != d {
				return
			}
			d.Phase = "stopped"
			if d.Request == "attach" {
				d.Phase = "detached"
			}
			a.Status = "Debugger " + d.Phase
			if err != nil {
				a.Status += " · adapter disconnect: " + err.Error()
			}
		})
	}()
}
func (a *App) closeDebugger() {
	d := a.debug
	if d == nil {
		return
	}
	defer d.Cancel()
	if d.Client != nil {
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = d.Client.Disconnect(ctx, d.Request == "launch")
	}
}
func (a *App) requireStopped() bool {
	if d := a.debug; d != nil && d.Client != nil && d.Stopped && !d.Ended {
		return true
	}
	a.Status = "Pause the debugger before inspecting frames or variables"
	return false
}
func (a *App) debugThreads() {
	if !a.requireStopped() {
		return
	}
	d := a.debug
	epoch := d.Epoch
	a.debugRequest("threads", nil, func(raw json.RawMessage) {
		var r struct {
			Threads []dap.Thread `json:"threads"`
		}
		if json.Unmarshal(raw, &r) != nil {
			return
		}
		items := []choice{}
		for _, thread := range r.Threads[:min(256, len(r.Threads))] {
			items = append(items, choice{fmt.Sprintf("%d · %s", thread.ID, thread.Name), func() {
				if a.debug != d || d.Epoch != epoch || !d.Stopped || d.Ended {
					return
				}
				d.Epoch++
				d.Thread = thread.ID
				a.loadDebugStack(d, thread.ID, true, 0)
			}})
		}
		a.list("Debug threads (up to 256)", items)
	})
}
func (a *App) loadDebugStack(d *debugSession, thread int, navigate bool, start int) {
	if navigate {
		d.FrameReady = false
	}
	a.debugRequest("stackTrace", map[string]int{"threadId": thread, "startFrame": start, "levels": 100}, func(raw json.RawMessage) {
		var r struct {
			Frames []dap.StackFrame `json:"stackFrames"`
			Total  int              `json:"totalFrames"`
		}
		if json.Unmarshal(raw, &r) != nil {
			return
		}
		if len(r.Frames) > 100 {
			r.Frames = r.Frames[:100]
		}
		d.Stack = r.Frames
		if navigate && len(r.Frames) > 0 {
			d.Frame = r.Frames[0]
			d.FrameReady = true
			if a.Dialog == nil && a.Preview == nil && a.Menu == nil {
				a.debugLocation(d, d.Frame)
			}
			if d.AfterFrame != nil {
				run := d.AfterFrame
				d.AfterFrame = nil
				run()
			}
			return
		}
		items := []choice{}
		epoch := d.Epoch
		for _, frame := range r.Frames {
			items = append(items, choice{fmt.Sprintf("%s · %s:%d", frame.Name, filepath.Base(frame.Source.Path), frame.Line), func() {
				if a.debug != d || d.Epoch != epoch || !d.Stopped {
					return
				}
				d.Epoch++
				d.Frame = frame
				d.FrameReady = true
				a.debugLocation(d, frame)
			}})
		}
		if start+len(r.Frames) < r.Total {
			items = append(items, choice{"More stack frames…", func() {
				if a.debug == d && d.Epoch == epoch {
					a.loadDebugStack(d, thread, false, start+len(r.Frames))
				}
			}})
		}
		a.list("Debug stack frames", items)
	})
}
func (a *App) debugStack() {
	if a.requireStopped() {
		a.loadDebugStack(a.debug, a.debug.Thread, false, 0)
	}
}
func (a *App) debugLocation(d *debugSession, frame dap.StackFrame) {
	if d != a.debug || !d.Stopped || d.Ended {
		return
	}
	if frame.Source.Path != "" && frame.Source.Reference == 0 {
		a.rememberJump()
		path := frame.Source.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(a.Root, path)
		}
		if err := a.Open(path); err != nil {
			a.Status = err.Error()
			return
		}
		b := a.Current()
		if d.Frame.ID == frame.ID {
			d.Frame.Source.Path = b.File.Path
		}
		offset := b.Doc.Text.LineStart(max(0, frame.Line-1))
		b.Doc.Selections = []text.Selection{{Anchor: offset, Head: offset}}
		a.reveal()
		a.schedule()
		a.Status = fmt.Sprintf("Debugger %s · %s:%d", d.Phase, frame.Name, frame.Line)
		if b.Doc.Dirty() {
			a.Status += " · file has unsaved edits; location is from running code"
		}
		return
	}
	if frame.Source.Reference > 0 {
		a.debugRequest("source", map[string]any{"source": frame.Source, "sourceReference": frame.Source.Reference}, func(raw json.RawMessage) {
			var r struct {
				Content string `json:"content"`
			}
			if json.Unmarshal(raw, &r) == nil {
				a.report("Debug source · "+frame.Source.Name, shortDebug(r.Content, 1<<20))
				a.gotoLine(fmt.Sprint(frame.Line))
			}
		})
	}
}
func (a *App) debugScopes() {
	if !a.requireStopped() {
		return
	}
	d := a.debug
	if !d.FrameReady {
		d.AfterFrame = a.debugScopes
		a.Status = "Loading the stopped frame…"
		return
	}
	epoch := d.Epoch
	a.debugRequest("scopes", map[string]int{"frameId": d.Frame.ID}, func(raw json.RawMessage) {
		var r struct {
			Scopes []dap.Scope `json:"scopes"`
		}
		if json.Unmarshal(raw, &r) != nil {
			return
		}
		items := []choice{}
		for _, scope := range r.Scopes[:min(64, len(r.Scopes))] {
			label := scope.Name
			if scope.Expensive {
				label += " (loads on selection)"
			}
			items = append(items, choice{label, func() {
				if a.debug == d && d.Epoch == epoch {
					a.debugVariables(scope.Name, scope.Reference, 0, 0, a.debugScopes)
				}
			}})
		}
		a.list("Debug scopes · "+d.Frame.Name, items)
	})
}
func shortDebug(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	return strings.ToValidUTF8(value, "�") + "…"
}
func (a *App) debugVariables(name string, reference, start, depth int, back func()) {
	if !a.requireStopped() {
		return
	}
	if depth >= 32 {
		a.Status = "Variable expansion depth limit (32) reached"
		return
	}
	d := a.debug
	epoch := d.Epoch
	a.debugRequest("variables", map[string]int{"variablesReference": reference, "start": start, "count": 100}, func(raw json.RawMessage) {
		var r struct {
			Variables []dap.Variable `json:"variables"`
		}
		if json.Unmarshal(raw, &r) != nil {
			return
		}
		items := []choice{{"← Back", back}}
		for _, v := range r.Variables[:min(100, len(r.Variables))] {
			label := v.Name + " = " + shortDebug(v.Value, 500)
			if v.Type != "" {
				label += " · " + shortDebug(v.Type, 80)
			}
			if v.Reference > 0 {
				label = "▸ " + label
			}
			items = append(items, choice{label, func() {
				if a.debug != d || d.Epoch != epoch || !d.Stopped || d.Ended {
					return
				}
				if v.Reference > 0 {
					a.debugVariables(v.Name, v.Reference, 0, depth+1, func() { a.debugVariables(name, reference, start, depth, back) })
				} else {
					a.report("Debug variable · "+v.Name, shortDebug(v.Value, 1<<20))
				}
			}})
		}
		if len(r.Variables) >= 100 {
			items = append(items, choice{"Next 100 variables…", func() {
				if a.debug == d && d.Epoch == epoch {
					a.debugVariables(name, reference, start+100, depth, back)
				}
			}})
		}
		a.list("Debug variables · "+name, items)
	})
}
func (a *App) debugEvaluate() {
	if !a.requireStopped() {
		return
	}
	d := a.debug
	if !d.FrameReady {
		d.AfterFrame = a.debugEvaluate
		a.Status = "Loading the stopped frame…"
		return
	}
	epoch := d.Epoch
	frame := d.Frame.ID
	a.prompt("Evaluate expression in selected frame (may execute code)", "", func(expression string) {
		if a.debug != d || d.Epoch != epoch || !d.Stopped {
			return
		}
		if len(expression) > 16384 {
			a.Status = "Expression exceeds 16 KiB"
			return
		}
		a.debugRequest("evaluate", map[string]any{"expression": expression, "frameId": frame, "context": "repl"}, func(raw json.RawMessage) {
			var r struct {
				Result    string `json:"result"`
				Reference int    `json:"variablesReference"`
			}
			if json.Unmarshal(raw, &r) != nil {
				return
			}
			if r.Reference > 0 {
				a.debugVariables(expression, r.Reference, 0, 0, a.debugScopes)
			} else {
				a.report("Debug evaluation", shortDebug(r.Result, 1<<20))
			}
		})
	})
}
func (a *App) debugOutput() {
	d := a.debug
	if d == nil || d.Client == nil {
		a.Status = "No debugger output"
		return
	}
	a.report("Debug output", stripANSI(d.Client.Output()))
	d.OutputBuffer = a.Current()
	d.WantsOutput.Store(true)
}
func (a *App) updateDebugOutput(d *debugSession, value string) {
	b := d.OutputBuffer
	if b == nil {
		return
	}
	found := false
	for _, open := range a.Buffers {
		found = found || open == b
	}
	if !found {
		d.OutputBuffer = nil
		d.WantsOutput.Store(false)
		return
	}
	doc, err := text.NewDocument(strings.ToValidUTF8(stripANSI(value), "�"))
	if err != nil {
		return
	}
	old := b.Doc.Selections
	b.Doc = doc
	b.Doc.Selections = old
	a.clampSelections(b)
	b.SyntaxReady = false
	if a.Current() == b {
		a.reveal()
	}
}
func (a *App) debugStatus() {
	d := a.debug
	if d == nil {
		a.Status = "No debug session · F5 selects a profile"
		return
	}
	a.report("Debug status", fmt.Sprintf("%s\nRequest: %s\nState: %s\nThread: %d\nFrame: %s:%d\n\nF5 continue · F9 breakpoint · F10 over · F11 into · Shift+F11 out\nUse Debug threads, Debug stack frames, Debug scopes and variables,\nDebug evaluate expression, Debug output and Debug stop or detach.\n\nDebugging uses saved files. Editing does not reload running code.\n", d.Name, d.Request, d.Phase, d.Thread, d.Frame.Name, d.Frame.Line))
}
