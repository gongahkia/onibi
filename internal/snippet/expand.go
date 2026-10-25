package snippet

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Range struct{ Start, End int }
type Stop struct {
	Number  int
	Ranges  []Range
	Choices []string
}
type Mirror struct {
	ID    int
	Range Range
}
type Expansion struct {
	Text    string
	Stops   []Stop
	Mirrors []Mirror
	Cache   map[int]string
	Values  map[int]string
}

// Resolve freezes variables for the lifetime of one insertion. Unknown names
// become editable placeholders; known empty variables can use nested defaults.
func (p *Program) Resolve(variables map[string]string) (*Program, error) {
	largest := 0
	var scan func([]*node)
	scan = func(nodes []*node) {
		for _, n := range nodes {
			if n.Placeholder {
				largest = max(largest, n.Number)
			}
			scan(n.Children)
		}
	}
	scan(p.roots)
	unknown := map[string]int{}
	var clone func([]*node) ([]*node, error)
	clone = func(nodes []*node) ([]*node, error) {
		out := make([]*node, 0, len(nodes))
		for _, original := range nodes {
			n := *original
			children, err := clone(n.Children)
			if err != nil {
				return nil, err
			}
			n.Children = children
			if n.Variable != "" && n.Transform == nil {
				value, known := variables[n.Variable]
				if value != "" {
					n.Text = value
					n.Variable = ""
					n.Children = nil
					n.Default = false
				} else if n.Default {
					n.Variable = ""
				} else if known {
					n.Variable = ""
					n.Text = ""
				} else {
					number, ok := unknown[n.Variable]
					if !ok {
						largest++
						number = largest
						unknown[n.Variable] = number
					}
					n.Text = ""
					n.Placeholder = true
					n.Number = number
					n.Default = true
					n.Children = []*node{{Text: n.Variable}}
					n.Variable = ""
				}
			} else if n.Variable != "" {
				n.Text = variables[n.Variable]
			}
			out = append(out, &n)
		}
		return out, nil
	}
	roots, err := clone(p.roots)
	if err != nil {
		return nil, err
	}
	result := &Program{roots: roots, definitions: map[int]*node{}}
	var collect func([]*node)
	collect = func(nodes []*node) {
		for _, n := range nodes {
			if n.Placeholder && n.Transform == nil {
				old := result.definitions[n.Number]
				if old == nil || !old.Default && n.Default {
					result.definitions[n.Number] = n
				}
			}
			if n.Transform != nil {
				result.transforms++
			}
			collect(n.Children)
		}
	}
	collect(roots)
	if result.transforms > 128 {
		return nil, fmt.Errorf("snippet exceeds 128 transform expressions")
	}
	return result, nil
}

// Expand evaluates transforms only when requested. Ordinary placeholder typing
// reuses Cache and does no regex work. The program and caller maps are immutable.
func (p *Program) Expand(ctx context.Context, overrides map[int]string, cache map[int]string, evaluate bool) (Expansion, error) {
	if p.definitions == nil {
		return Expansion{}, fmt.Errorf("resolve snippet variables before expansion")
	}
	if evaluate {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
	}
	e := expander{program: p, ctx: ctx, overrides: overrides, cache: map[int]string{}, values: map[int]string{}, visiting: map[int]bool{}, eval: evaluate, stops: map[int]*Stop{}, out: &strings.Builder{}}
	for id, v := range cache {
		if id > 0 {
			e.cache[id] = v
		}
	}
	if err := e.emit(p.roots, 0, true); err != nil {
		return Expansion{}, err
	}
	if e.stops[0] == nil {
		e.stops[0] = &Stop{Number: 0, Ranges: []Range{{e.out.Len(), e.out.Len()}}}
	}
	numbers := []int{}
	for n := range e.stops {
		if n != 0 {
			numbers = append(numbers, n)
		}
	}
	sort.Ints(numbers)
	numbers = append(numbers, 0)
	stops := make([]Stop, 0, len(numbers))
	for _, n := range numbers {
		stops = append(stops, *e.stops[n])
	}
	for id := range e.cache {
		if id < 0 {
			delete(e.cache, id)
		}
	}
	return Expansion{Text: e.out.String(), Stops: stops, Mirrors: e.mirrors, Cache: e.cache, Values: e.values}, nil
}

type expander struct {
	program                  *Program
	ctx                      context.Context
	overrides, cache, values map[int]string
	visiting                 map[int]bool
	eval                     bool
	out                      *strings.Builder
	stops                    map[int]*Stop
	mirrors                  []Mirror
	nodes, valueBytes        int
}

func (e *expander) check(depth int) error {
	e.nodes++
	if e.nodes > 32768 || depth > maxDepth*2 {
		return fmt.Errorf("snippet expansion exceeds nesting/work limits")
	}
	return e.ctx.Err()
}
func (e *expander) write(value string) error {
	if e.out.Len()+len(value) > MaxExpansion {
		return fmt.Errorf("snippet expansion exceeds 128 KiB")
	}
	e.out.WriteString(value)
	return nil
}
func (e *expander) value(number, depth int) (string, error) {
	if v, ok := e.overrides[number]; ok {
		return v, nil
	}
	if v, ok := e.values[number]; ok {
		return v, nil
	}
	if e.visiting[number] {
		return "", fmt.Errorf("cyclic snippet placeholder %d", number)
	}
	if err := e.check(depth); err != nil {
		return "", err
	}
	e.visiting[number] = true
	defer delete(e.visiting, number)
	definition := e.program.definitions[number]
	value := ""
	if definition != nil {
		if len(definition.Choices) > 0 {
			value = definition.Choices[0]
		} else {
			// A separate builder computes canonical values without adding tabstop ranges.
			saved := e.out
			e.out = &strings.Builder{}
			err := e.emit(definition.Children, depth+1, false)
			value = e.out.String()
			e.out = saved
			if err != nil {
				return "", err
			}
		}
	}
	e.valueBytes += len(value)
	if e.valueBytes > 1<<20 {
		return "", fmt.Errorf("snippet default values exceed 1 MiB")
	}
	e.values[number] = value
	return value, nil
}
func (e *expander) transformed(n *node, depth int) (string, error) {
	if !e.eval {
		return e.cache[n.ID], nil
	}
	// A node can appear in several linked occurrences; compute its transform once.
	if value, ok := e.cache[-n.ID]; ok {
		return value, nil
	}
	input := n.Text
	var err error
	if n.Placeholder {
		input, err = e.value(n.Number, depth+1)
		if err != nil {
			return "", err
		}
	}
	value, err := n.Transform.apply(e.ctx, input)
	if err != nil {
		return "", fmt.Errorf("snippet transform: %w", err)
	}
	e.cache[n.ID] = value
	e.cache[-n.ID] = value
	return value, nil
}
func (e *expander) emit(nodes []*node, depth int, record bool) error {
	for _, n := range nodes {
		if err := e.check(depth); err != nil {
			return err
		}
		if n.Transform != nil {
			from := e.out.Len()
			value, err := e.transformed(n, depth)
			if err != nil {
				return err
			}
			if err = e.write(value); err != nil {
				return err
			}
			if record {
				e.mirrors = append(e.mirrors, Mirror{ID: n.ID, Range: Range{from, e.out.Len()}})
			}
			continue
		}
		if n.Placeholder {
			from := e.out.Len()
			definition := e.program.definitions[n.Number]
			value, err := e.value(n.Number, depth+1)
			if err != nil {
				return err
			}
			_, overridden := e.overrides[n.Number]
			if !overridden && definition != nil && len(definition.Choices) == 0 {
				if e.visiting[n.Number] {
					return fmt.Errorf("cyclic snippet placeholder %d", n.Number)
				}
				e.visiting[n.Number] = true
				err = e.emit(definition.Children, depth+1, record)
				delete(e.visiting, n.Number)
			} else {
				err = e.write(value)
			}
			if err != nil {
				return err
			}
			if record {
				stop := e.stops[n.Number]
				if stop == nil {
					stop = &Stop{Number: n.Number}
					if definition != nil {
						stop.Choices = definition.Choices
					}
					e.stops[n.Number] = stop
				}
				stop.Ranges = append(stop.Ranges, Range{from, e.out.Len()})
				if len(stop.Ranges) > 1024 {
					return fmt.Errorf("more than 1024 linked snippet occurrences")
				}
			}
			continue
		}
		if err := e.write(n.Text); err != nil {
			return err
		}
		if err := e.emit(n.Children, depth+1, record); err != nil {
			return err
		}
	}
	return nil
}
