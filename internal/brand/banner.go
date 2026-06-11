package brand

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	_ "embed"

	"golang.org/x/term"
)

//go:embed onibi-v2-logo.png
var logoPNG []byte

const (
	defaultWidth = 50
	maxWidth     = 60
	minWidth     = 12
)

func ANSI() string {
	width, color := terminalWidth(os.Stdout)
	return render(width, color)
}

func ANSIForWriter(w io.Writer) string {
	width, color := terminalWidth(w)
	return render(width, color)
}

func Render(width int) string {
	return render(width, false)
}

func render(width int, color bool) string {
	if env := strings.TrimSpace(os.Getenv("ONIBI_BANNER_WIDTH")); env != "" {
		if n, err := strconv.Atoi(env); err == nil {
			width = n
		}
	}
	art := renderPlain(clampWidth(width))
	if color {
		return "\x1b[38;5;37m" + art + "\x1b[0m"
	}
	return art
}

func renderPlain(width int) string {
	img, err := png.Decode(bytes.NewReader(logoPNG))
	if err != nil {
		return fallback
	}
	img = cropAlpha(img)
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return fallback
	}
	height := int(math.Round(float64(b.Dy()) / float64(b.Dx()) * float64(width) * 0.48))
	if height < 1 {
		height = 1
	}
	const ramp = " .:-=+*#%@"
	var out strings.Builder
	for y := 0; y < height; y++ {
		var line strings.Builder
		for x := 0; x < width; x++ {
			sx := b.Min.X + int((float64(x)+0.5)*float64(b.Dx())/float64(width))
			sy := b.Min.Y + int((float64(y)+0.5)*float64(b.Dy())/float64(height))
			lum := compositeLum(img.At(sx, sy))
			idx := (255 - lum) * (len(ramp) - 1) / 255
			line.WriteByte(ramp[idx])
		}
		out.WriteString(strings.TrimRight(line.String(), " "))
		if y != height-1 {
			out.WriteByte('\n')
		}
	}
	return strings.TrimRight(out.String(), "\n")
}

func compositeLum(c color.Color) int {
	r, g, b, a := c.RGBA()
	alpha := int(a >> 8)
	if alpha == 0 {
		return 255
	}
	rr := int(r >> 8)
	gg := int(g >> 8)
	bb := int(b >> 8)
	rr = (rr*alpha + 255*(255-alpha)) / 255
	gg = (gg*alpha + 255*(255-alpha)) / 255
	bb = (bb*alpha + 255*(255-alpha)) / 255
	return (299*rr + 587*gg + 114*bb) / 1000
}

func cropAlpha(img image.Image) image.Image {
	b := img.Bounds()
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a>>8 <= 8 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x+1 > maxX {
				maxX = x + 1
			}
			if y+1 > maxY {
				maxY = y + 1
			}
		}
	}
	if minX >= maxX || minY >= maxY {
		return img
	}
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return img
	}
	return sub.SubImage(image.Rect(minX, minY, maxX, maxY))
}

func terminalWidth(w io.Writer) (int, bool) {
	f, ok := w.(interface{ Fd() uintptr })
	if !ok {
		return defaultWidth, false
	}
	fd := int(f.Fd())
	if !term.IsTerminal(fd) {
		return defaultWidth, false
	}
	width, _, err := term.GetSize(fd)
	if err != nil {
		return defaultWidth, true
	}
	return width - 4, true
}

func clampWidth(width int) int {
	if width <= 0 {
		width = defaultWidth
	}
	if width > maxWidth {
		return maxWidth
	}
	if width < minWidth {
		return minWidth
	}
	return width
}

const fallback = `            .**########**.
         .*#####*******####*.
       .####*.           .*###*
      *###.                .*###
     ###*                    *###      ..
    *###      **.       ..*   *##*    .##   .#*
    ###.     ####.    .####   .###    .#*  *##.
   .###      .**.      ...*    ###*.      .#*  ...
   .###         ..   ..      .*#######*..    .###*
   .###         *######.    *###.. ..*####*.
    ###.          ....     *##*         .*##*
    ###.                  *##* .#####*    ###
    ###.        ..       ###. .##.  ##*  .##*
   .###        .##*..   ###.   ##*.*##. .##*
   *##*         .*########.     .***.  *###
  *###             ...*### .##.       *####*
 *##*                  *##. ..  ##*  *##*.##.
###*               .**###**#*    .  *###*###
###*...***.        ###**. .*. *#*  *#####*.
 *##########.   .*###*        ..  *##.
    ..   .*###########*..       .###.
           ..***.   .*####**..*###*.
                       ..*######*.`
