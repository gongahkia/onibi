package render

import (
	"bytes"
	_ "embed"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	DefaultRows  = 24
	DefaultCols  = 80
	DefaultScale = 2

	FontJetBrainsMonoNerd = "jetbrains-mono-nerd"
	FontCaskaydiaCoveNerd = "caskaydia-cove-nerd"
	FontGoMonoNerd        = "go-mono-nerd"
	FontCustom            = "custom"
)

type FontChoice struct {
	ID    string
	Label string
}

func BuiltinFonts() []FontChoice {
	return []FontChoice{
		{ID: FontJetBrainsMonoNerd, Label: "JetBrainsMono Nerd Font Mono"},
		{ID: FontCaskaydiaCoveNerd, Label: "Caskaydia Cove Nerd Font Mono"},
		{ID: FontGoMonoNerd, Label: "Go Mono Nerd Font Mono"},
	}
}

type PNGOptions struct {
	Rows     int
	Cols     int
	Scale    int
	Font     string
	FontPath string
}

//go:embed fonts/JetBrainsMonoNerdFontMono-Regular.ttf
var jetBrainsMonoNerdTTF []byte

//go:embed fonts/CaskaydiaCoveNerdFontMono-Regular.ttf
var caskaydiaCoveNerdTTF []byte

//go:embed fonts/GoMonoNerdFontMono-Regular.ttf
var goMonoNerdTTF []byte

var parsedFonts sync.Map // map[string]*opentype.Font

func RenderPNG(buf []byte, opts PNGOptions) ([]byte, error) {
	opts = normalizedPNGOptions(opts)
	face, err := fontFace(opts)
	if err != nil {
		return nil, err
	}
	defer face.Close()
	img := drawTerminal(screenCells(buf, opts.Rows, opts.Cols), opts.Rows, opts.Cols, face)
	if opts.Scale > 1 {
		img = scaleNearest(img, opts.Scale)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func normalizedPNGOptions(opts PNGOptions) PNGOptions {
	if opts.Rows <= 0 {
		opts.Rows = DefaultRows
	}
	if opts.Cols <= 0 {
		opts.Cols = DefaultCols
	}
	if opts.Scale <= 0 {
		opts.Scale = DefaultScale
	}
	if opts.Font == "" {
		opts.Font = FontJetBrainsMonoNerd
	}
	return opts
}

func fontFace(opts PNGOptions) (font.Face, error) {
	data, key, err := fontData(opts.Font, opts.FontPath)
	if err != nil {
		return nil, err
	}
	parsed, ok := parsedFonts.Load(key)
	if !ok {
		f, err := opentype.Parse(data)
		if err != nil {
			return nil, err
		}
		parsed, _ = parsedFonts.LoadOrStore(key, f)
	}
	return opentype.NewFace(parsed.(*opentype.Font), &opentype.FaceOptions{Size: 12, DPI: 72, Hinting: font.HintingFull})
}

func ValidateFont(name, path string) error {
	data, _, err := fontData(name, path)
	if err != nil {
		return err
	}
	_, err = opentype.Parse(data)
	return err
}

func fontData(name, path string) ([]byte, string, error) {
	switch name {
	case FontJetBrainsMonoNerd:
		return jetBrainsMonoNerdTTF, name, nil
	case FontCaskaydiaCoveNerd:
		return caskaydiaCoveNerdTTF, name, nil
	case FontGoMonoNerd:
		return goMonoNerdTTF, name, nil
	case FontCustom:
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() <= 0 || info.Size() > 32<<20 {
			return nil, "", errors.New("custom screen font must be a readable TTF or OTF no larger than 32 MiB")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", err
		}
		return data, path + ":" + info.ModTime().UTC().String() + ":" + strconv.FormatInt(info.Size(), 10), nil
	default:
		return nil, "", errors.New("unknown screen font")
	}
}

type terminalStyle struct {
	fg color.RGBA
	bg color.RGBA
}

type screenCell struct {
	r  rune
	fg color.RGBA
	bg color.RGBA
}

func defaultTerminalStyle() terminalStyle { return terminalStyle{fg: defaultFG, bg: defaultBG} }

func screenCells(buf []byte, rows, cols int) [][]screenCell {
	lines := parseScreenCells(buf, cols)
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	out := make([][]screenCell, rows)
	copy(out[rows-len(lines):], lines)
	return out
}

func screenLines(buf []byte, rows, cols int) []string {
	cells := screenCells(buf, rows, cols)
	out := make([]string, len(cells))
	for i, line := range cells {
		var b strings.Builder
		for _, cell := range line {
			b.WriteRune(cell.r)
		}
		out[i] = b.String()
	}
	return out
}

func parseScreenCells(buf []byte, cols int) [][]screenCell {
	style := defaultTerminalStyle()
	lines := make([][]screenCell, 0, 1)
	line := make([]screenCell, 0, cols)
	for i := 0; i < len(buf); {
		if buf[i] == 0x1b {
			next, params, sgr := ansiSequence(buf, i)
			if sgr {
				style = applySGR(style, params)
			}
			i = next
			continue
		}
		switch buf[i] {
		case '\r':
			i++
			continue
		case '\n':
			lines = append(lines, line)
			line = make([]screenCell, 0, cols)
			i++
			continue
		case '\t':
			for n := 0; n < 4 && len(line) < cols; n++ {
				line = append(line, screenCell{r: ' ', fg: style.fg, bg: style.bg})
			}
			i++
			continue
		}
		r, size := utf8.DecodeRune(buf[i:])
		i += size
		if len(line) >= cols || r == utf8.RuneError || unicode.IsControl(r) || unicode.In(r, unicode.Bidi_Control) {
			continue
		}
		line = append(line, screenCell{r: r, fg: style.fg, bg: style.bg})
	}
	return append(lines, line)
}

func ansiSequence(buf []byte, start int) (next int, params []int, sgr bool) {
	if start+1 >= len(buf) {
		return start + 1, nil, false
	}
	switch buf[start+1] {
	case '[':
		end := start + 2
		for end < len(buf) && (buf[end] < 0x40 || buf[end] > 0x7e) {
			end++
		}
		if end >= len(buf) {
			return len(buf), nil, false
		}
		if buf[end] != 'm' {
			return end + 1, nil, false
		}
		return end + 1, parseSGRParams(buf[start+2 : end]), true
	case ']':
		end := start + 2
		for end < len(buf) {
			if buf[end] == 0x07 {
				return end + 1, nil, false
			}
			if buf[end] == 0x1b && end+1 < len(buf) && buf[end+1] == '\\' {
				return end + 2, nil, false
			}
			end++
		}
		return len(buf), nil, false
	case '(', ')':
		return minInt(len(buf), start+3), nil, false
	default:
		return start + 2, nil, false
	}
}

func parseSGRParams(raw []byte) []int {
	if len(raw) == 0 {
		return []int{0}
	}
	parts := strings.Split(string(raw), ";")
	params := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			params = append(params, 0)
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			params = append(params, -1)
			continue
		}
		params = append(params, n)
	}
	return params
}

func applySGR(style terminalStyle, params []int) terminalStyle {
	for i := 0; i < len(params); i++ {
		code := params[i]
		switch {
		case code == 0:
			style = defaultTerminalStyle()
		case code == 39:
			style.fg = defaultFG
		case code == 49:
			style.bg = defaultBG
		case code >= 30 && code <= 37:
			style.fg = ansiColor(code - 30)
		case code >= 40 && code <= 47:
			style.bg = ansiColor(code - 40)
		case code >= 90 && code <= 97:
			style.fg = ansiColor(code - 90 + 8)
		case code >= 100 && code <= 107:
			style.bg = ansiColor(code - 100 + 8)
		case code == 38 || code == 48:
			isFG := code == 38
			if i+2 < len(params) && params[i+1] == 5 {
				if c, ok := ansi256Color(params[i+2]); ok {
					if isFG {
						style.fg = c
					} else {
						style.bg = c
					}
				}
				i += 2
				continue
			}
			if i+4 < len(params) && params[i+1] == 2 {
				c := color.RGBA{R: byte(clampByte(params[i+2])), G: byte(clampByte(params[i+3])), B: byte(clampByte(params[i+4])), A: 255}
				if isFG {
					style.fg = c
				} else {
					style.bg = c
				}
				i += 4
			}
		}
	}
	return style
}

func ansiColor(index int) color.RGBA {
	return ansiPalette[index]
}

func ansi256Color(index int) (color.RGBA, bool) {
	if index < 0 || index > 255 {
		return color.RGBA{}, false
	}
	if index < len(ansiPalette) {
		return ansiColor(index), true
	}
	if index < 232 {
		index -= 16
		levels := [...]uint8{0, 95, 135, 175, 215, 255}
		return color.RGBA{R: levels[index/36], G: levels[index/6%6], B: levels[index%6], A: 255}, true
	}
	shade := uint8(8 + (index-232)*10)
	return color.RGBA{R: shade, G: shade, B: shade, A: 255}, true
}

func clampByte(n int) int {
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return n
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func drawTerminal(lines [][]screenCell, rows, cols int, face font.Face) *image.RGBA {
	metrics := face.Metrics()
	cellW := maxInt(1, font.MeasureString(face, "M").Round())
	cellH := maxInt(1, metrics.Height.Ceil())
	ascent := metrics.Ascent.Ceil()
	pad := 8
	img := image.NewRGBA(image.Rect(0, 0, cols*cellW+pad*2, rows*cellH+pad*2))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: defaultBG}, image.Point{}, draw.Src)
	for row, line := range lines {
		for col, cell := range line {
			x := pad + col*cellW
			y := pad + row*cellH
			if cell.bg != defaultBG {
				draw.Draw(img, image.Rect(x, y, x+cellW, y+cellH), &image.Uniform{C: cell.bg}, image.Point{}, draw.Src)
			}
			if cell.r == 0 || cell.r == ' ' {
				continue
			}
			d := font.Drawer{Dst: img, Src: &image.Uniform{C: cell.fg}, Face: face, Dot: fixed.P(x, y+ascent)}
			d.DrawString(string(cell.r))
		}
	}
	drawActiveBorder(img)
	return img
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func drawActiveBorder(img *image.RGBA) {
	b := img.Bounds()
	drawBox(img, b, 2, activeBorder)
	drawBox(img, b.Inset(4), 1, activeBorderInner)
}

func drawBox(img *image.RGBA, b image.Rectangle, width int, c color.RGBA) {
	if width <= 0 || b.Empty() {
		return
	}
	draw.Draw(img, image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Min.Y+width), &image.Uniform{C: c}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(b.Min.X, b.Max.Y-width, b.Max.X, b.Max.Y), &image.Uniform{C: c}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(b.Min.X, b.Min.Y, b.Min.X+width, b.Max.Y), &image.Uniform{C: c}, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(b.Max.X-width, b.Min.Y, b.Max.X, b.Max.Y), &image.Uniform{C: c}, image.Point{}, draw.Src)
}

func scaleNearest(src *image.RGBA, scale int) *image.RGBA {
	if scale <= 1 {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, src.Bounds().Dx()*scale, src.Bounds().Dy()*scale))
	for y := 0; y < dst.Bounds().Dy(); y++ {
		for x := 0; x < dst.Bounds().Dx(); x++ {
			dst.Set(x, y, src.At(x/scale, y/scale))
		}
	}
	return dst
}

var (
	defaultFG         = color.RGBA{R: 230, G: 230, B: 230, A: 255}
	defaultBG         = color.RGBA{R: 12, G: 14, B: 18, A: 255}
	activeBorder      = color.RGBA{R: 156, G: 255, B: 184, A: 255}
	activeBorderInner = color.RGBA{R: 50, G: 110, B: 74, A: 255}
	ansiPalette       = [...]color.RGBA{
		{R: 40, G: 44, B: 52, A: 255}, {R: 224, G: 108, B: 117, A: 255}, {R: 152, G: 195, B: 121, A: 255}, {R: 229, G: 192, B: 123, A: 255},
		{R: 97, G: 175, B: 239, A: 255}, {R: 198, G: 120, B: 221, A: 255}, {R: 86, G: 182, B: 194, A: 255}, {R: 171, G: 178, B: 191, A: 255},
		{R: 92, G: 99, B: 112, A: 255}, {R: 224, G: 108, B: 117, A: 255}, {R: 152, G: 195, B: 121, A: 255}, {R: 229, G: 192, B: 123, A: 255},
		{R: 97, G: 175, B: 239, A: 255}, {R: 198, G: 120, B: 221, A: 255}, {R: 86, G: 182, B: 194, A: 255}, {R: 255, G: 255, B: 255, A: 255},
	}
)
