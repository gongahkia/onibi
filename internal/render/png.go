package render

import (
	"bufio"
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jaguilar/vt100"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	DefaultRows  = 24
	DefaultCols  = 80
	DefaultScale = 2
)

type PNGOptions struct {
	Rows  int
	Cols  int
	Scale int
}

func RenderPNG(buf []byte, opts PNGOptions) ([]byte, error) {
	if opts.Rows <= 0 {
		opts.Rows = DefaultRows
	}
	if opts.Cols <= 0 {
		opts.Cols = DefaultCols
	}
	if opts.Scale <= 0 {
		opts.Scale = DefaultScale
	}
	term := vt100.NewVT100(opts.Rows, opts.Cols)
	replay(term, buf)
	img := drawTerminal(term)
	if opts.Scale > 1 {
		img = scaleNearest(img, opts.Scale)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func replay(v *vt100.VT100, buf []byte) {
	for i := 0; i < len(buf); {
		b := buf[i]
		if b == 0x1b {
			next := i + 1
			if next >= len(buf) {
				return
			}
			switch buf[next] {
			case '[':
				end := next + 1
				for end < len(buf) && (buf[end] < 0x40 || buf[end] > 0x7e) {
					end++
				}
				if end >= len(buf) {
					return
				}
				final := buf[end]
				args := string(buf[next+1 : end])
				if final == 'm' {
					applySGR(&v.Cursor.F, parseSGR(args))
				} else {
					if final == 'h' && strings.Contains(args, "?1049") {
						clear(v)
					}
					process(v, buf[i:end+1])
				}
				i = end + 1
				continue
			case ']':
				end := next + 1
				for end < len(buf) {
					if buf[end] == 0x07 {
						end++
						break
					}
					if buf[end] == 0x1b && end+1 < len(buf) && buf[end+1] == '\\' {
						end += 2
						break
					}
					end++
				}
				i = end
				continue
			case '(', ')':
				end := i + 3
				if end > len(buf) {
					end = len(buf)
				}
				process(v, buf[i:end])
				i = end
				continue
			default:
				end := i + 2
				process(v, buf[i:end])
				i = end
				continue
			}
		}
		if b < 0x20 || b == 0x7f {
			process(v, buf[i:i+1])
			i++
			continue
		}
		r, n := utf8.DecodeRune(buf[i:])
		if r == utf8.RuneError && n == 1 {
			i++
			continue
		}
		process(v, buf[i:i+n])
		i += n
	}
}

func process(v *vt100.VT100, raw []byte) {
	cmd, err := vt100.Decode(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		return
	}
	if err := v.Process(cmd); err != nil {
		var unsupported vt100.UnsupportedError
		if errors.As(err, &unsupported) || errors.Is(err, io.EOF) {
			return
		}
		return
	}
}

func clear(v *vt100.VT100) {
	v.Cursor.Y = 0
	v.Cursor.X = 0
	v.Cursor.F = vt100.Format{}
	for y := 0; y < v.Height; y++ {
		for x := 0; x < v.Width; x++ {
			v.Content[y][x] = ' '
			v.Format[y][x] = vt100.Format{}
		}
	}
}

func drawTerminal(v *vt100.VT100) *image.RGBA {
	face := basicfont.Face7x13
	cellW := face.Advance
	cellH := (face.Metrics().Ascent + face.Metrics().Descent).Ceil()
	ascent := face.Metrics().Ascent.Ceil()
	pad := 8
	img := image.NewRGBA(image.Rect(0, 0, v.Width*cellW+pad*2, v.Height*cellH+pad*2))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: defaultBG}, image.Point{}, draw.Src)
	for y := 0; y < v.Height; y++ {
		for x := 0; x < v.Width; x++ {
			r := v.Content[y][x]
			fg, bg := colorsFor(v.Format[y][x])
			rect := image.Rect(pad+x*cellW, pad+y*cellH, pad+(x+1)*cellW, pad+(y+1)*cellH)
			draw.Draw(img, rect, &image.Uniform{C: bg}, image.Point{}, draw.Src)
			if r == 0 || r == ' ' {
				continue
			}
			d := font.Drawer{
				Dst:  img,
				Src:  &image.Uniform{C: fg},
				Face: face,
				Dot:  fixed.P(pad+x*cellW, pad+y*cellH+ascent),
			}
			d.DrawString(string(r))
		}
	}
	return img
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
	defaultFG = color.RGBA{R: 230, G: 230, B: 230, A: 255}
	defaultBG = color.RGBA{R: 12, G: 14, B: 18, A: 255}
	ansi16    = []color.RGBA{
		{R: 0, G: 0, B: 0, A: 255},
		{R: 205, G: 49, B: 49, A: 255},
		{R: 13, G: 188, B: 121, A: 255},
		{R: 229, G: 229, B: 16, A: 255},
		{R: 36, G: 114, B: 200, A: 255},
		{R: 188, G: 63, B: 188, A: 255},
		{R: 17, G: 168, B: 205, A: 255},
		{R: 229, G: 229, B: 229, A: 255},
		{R: 102, G: 102, B: 102, A: 255},
		{R: 241, G: 76, B: 76, A: 255},
		{R: 35, G: 209, B: 139, A: 255},
		{R: 245, G: 245, B: 67, A: 255},
		{R: 59, G: 142, B: 234, A: 255},
		{R: 214, G: 112, B: 214, A: 255},
		{R: 41, G: 184, B: 219, A: 255},
		{R: 255, G: 255, B: 255, A: 255},
	}
)

func colorsFor(f vt100.Format) (color.RGBA, color.RGBA) {
	fg := normalizeColor(f.Fg, defaultFG)
	bg := normalizeColor(f.Bg, defaultBG)
	if f.Inverse || f.Negative {
		fg, bg = bg, fg
	}
	if f.Intensity == vt100.Dim {
		fg = scaleColor(fg, 0.55)
	}
	if f.Conceal {
		fg = bg
	}
	return fg, bg
}

func normalizeColor(c color.RGBA, fallback color.RGBA) color.RGBA {
	if c.A == 0 {
		return fallback
	}
	c.A = 255
	return c
}

func scaleColor(c color.RGBA, factor float64) color.RGBA {
	return color.RGBA{R: uint8(float64(c.R) * factor), G: uint8(float64(c.G) * factor), B: uint8(float64(c.B) * factor), A: 255}
}

func parseSGR(s string) []int {
	if s == "" {
		return []int{0}
	}
	parts := strings.Split(s, ";")
	out := make([]int, len(parts))
	for i, p := range parts {
		if p == "" {
			out[i] = 0
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(p, "?"))
		if err != nil {
			out[i] = 0
			continue
		}
		out[i] = n
	}
	return out
}

func applySGR(f *vt100.Format, args []int) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == 0:
			*f = vt100.Format{}
		case a == 1:
			f.Intensity = vt100.Bright
		case a == 2:
			f.Intensity = vt100.Dim
		case a == 22:
			f.Intensity = vt100.Normal
		case a == 4:
			f.Underscore = true
		case a == 24:
			f.Underscore = false
		case a == 7:
			f.Inverse = true
		case a == 27:
			f.Inverse = false
		case a == 8:
			f.Conceal = true
		case a == 28:
			f.Conceal = false
		case a >= 30 && a <= 37:
			f.Fg = ansi16[a-30]
		case a == 39:
			f.Fg = color.RGBA{}
		case a >= 40 && a <= 47:
			f.Bg = ansi16[a-40]
		case a == 49:
			f.Bg = color.RGBA{}
		case a >= 90 && a <= 97:
			f.Fg = ansi16[8+a-90]
		case a >= 100 && a <= 107:
			f.Bg = ansi16[8+a-100]
		case a == 38 && i+2 < len(args) && args[i+1] == 5:
			f.Fg = xterm256(args[i+2])
			i += 2
		case a == 48 && i+2 < len(args) && args[i+1] == 5:
			f.Bg = xterm256(args[i+2])
			i += 2
		case a == 38 && i+4 < len(args) && args[i+1] == 2:
			f.Fg = color.RGBA{R: clamp(args[i+2]), G: clamp(args[i+3]), B: clamp(args[i+4]), A: 255}
			i += 4
		case a == 48 && i+4 < len(args) && args[i+1] == 2:
			f.Bg = color.RGBA{R: clamp(args[i+2]), G: clamp(args[i+3]), B: clamp(args[i+4]), A: 255}
			i += 4
		}
	}
}

func xterm256(n int) color.RGBA {
	if n < 0 {
		n = 0
	}
	if n > 255 {
		n = 255
	}
	if n < 16 {
		return ansi16[n]
	}
	if n >= 232 {
		v := uint8(8 + (n-232)*10)
		return color.RGBA{R: v, G: v, B: v, A: 255}
	}
	n -= 16
	levels := []uint8{0, 95, 135, 175, 215, 255}
	return color.RGBA{R: levels[n/36], G: levels[(n/6)%6], B: levels[n%6], A: 255}
}

func clamp(n int) uint8 {
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return uint8(n)
}
