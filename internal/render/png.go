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
	img := drawTerminal(screenLines(buf, opts.Rows, opts.Cols), opts.Rows, opts.Cols, face)
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

func screenLines(buf []byte, rows, cols int) []string {
	clean := strings.ReplaceAll(string(StripANSI(buf)), "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "")
	lines := strings.Split(clean, "\n")
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	out := make([]string, rows)
	for i := range lines {
		out[rows-len(lines)+i] = sanitizeLine(lines[i], cols)
	}
	return out
}

func sanitizeLine(s string, cols int) string {
	var b strings.Builder
	cells := 0
	for _, r := range s {
		if cells >= cols {
			break
		}
		if r == '\t' {
			for n := 0; n < 4 && cells < cols; n++ {
				b.WriteByte(' ')
				cells++
			}
			continue
		}
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.In(r, unicode.Bidi_Control) {
			continue
		}
		b.WriteRune(r)
		cells++
	}
	return b.String()
}

func drawTerminal(lines []string, rows, cols int, face font.Face) *image.RGBA {
	metrics := face.Metrics()
	cellW := maxInt(1, font.MeasureString(face, "M").Round())
	cellH := maxInt(1, metrics.Height.Ceil())
	ascent := metrics.Ascent.Ceil()
	pad := 8
	img := image.NewRGBA(image.Rect(0, 0, cols*cellW+pad*2, rows*cellH+pad*2))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: defaultBG}, image.Point{}, draw.Src)
	for row, line := range lines {
		if line == "" {
			continue
		}
		d := font.Drawer{Dst: img, Src: &image.Uniform{C: defaultFG}, Face: face, Dot: fixed.P(pad, pad+row*cellH+ascent)}
		d.DrawString(line)
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
)
