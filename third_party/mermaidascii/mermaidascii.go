// Package mermaidascii renders Mermaid diagrams as ASCII or Unicode box
// drawings.
//
// It is a port of the ASCII renderer of beautiful-mermaid, which in turn goes
// back to mermaid-ascii; see NOTICE for the full lineage. Nothing outside the
// standard library is needed, and no browser or JavaScript runtime is
// involved: the diagram is laid out and drawn in Go.
//
//	out, err := mermaidascii.Render(`flowchart LR
//	    A[Start] --> B{Ready?}
//	    B -->|yes| C[Go]`)
//
// The result is a multi-line string, one rune per cell, ready to print into a
// terminal or a fixed-width document.
package mermaidascii

// Config controls spacing and character set. The zero value is not useful;
// start from DefaultConfig.
type Config struct {
	// UseASCII draws with +, - and | instead of box-drawing characters.
	// Choose it when the output has to survive a font or a pipe that cannot
	// carry Unicode.
	UseASCII bool
	// PaddingX and PaddingY are the free cells between neighbouring nodes,
	// where edges are routed.
	PaddingX, PaddingY int
	// Padding is the space between a label and its box border.
	Padding int
}

// DefaultConfig matches the spacing of the reference implementation.
func DefaultConfig() Config {
	return Config{PaddingX: 5, PaddingY: 5, Padding: 1}
}

// Option modifies a Config.
type Option func(*Config)

// WithASCII selects the plain ASCII character set.
func WithASCII() Option { return func(c *Config) { c.UseASCII = true } }

// WithSpacing sets the horizontal and vertical distance between nodes.
func WithSpacing(x, y int) Option {
	return func(c *Config) { c.PaddingX, c.PaddingY = x, y }
}

// WithPadding sets the space between a label and its box border.
func WithPadding(p int) Option { return func(c *Config) { c.Padding = p } }

// Render draws Mermaid source as a diagram.
//
// The diagram type is taken from the opening line. Anything that is not a
// recognised header is treated as a flowchart, which is what Mermaid does too.
func Render(src string, opts ...Option) (string, error) {
	cfg := DefaultConfig()
	for _, o := range opts {
		o(&cfg)
	}

	if isSequence(src) {
		s, err := ParseSequence(src)
		if err != nil {
			return "", err
		}
		return renderSequence(s, cfg), nil
	}

	g, err := Parse(src)
	if err != nil {
		return "", err
	}
	return renderFlowchart(g, cfg), nil
}

// charsetFor picks the character set a config asks for.
func charsetFor(cfg Config) charset {
	if cfg.UseASCII {
		return asciiSet
	}
	return unicodeSet
}
