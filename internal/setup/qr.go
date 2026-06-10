package setup

import (
	"fmt"
	"io"

	qrcode "github.com/skip2/go-qrcode"
)

// PrintQR renders a QR code for text to w using Unicode half-block chars
// (matches the tgterm rendering pattern, see docs/tgterm-patterns.md §4).
// Each output line encodes two QR rows. Surrounds with a one-module quiet zone.
func PrintQR(w io.Writer, text string) error {
	q, err := qrcode.New(text, qrcode.Low)
	if err != nil {
		return err
	}
	q.DisableBorder = true
	m := q.Bitmap()
	size := len(m)
	for y := -1; y < size+1; y += 2 {
		for x := -1; x < size+1; x++ {
			top := inside(x, y, size) && m[y][x]
			bot := inside(x, y+1, size) && m[y+1][x]
			switch {
			case top && bot:
				_, _ = fmt.Fprint(w, "█") // █
			case top && !bot:
				_, _ = fmt.Fprint(w, "▀") // ▀
			case !top && bot:
				_, _ = fmt.Fprint(w, "▄") // ▄
			default:
				_, _ = fmt.Fprint(w, " ")
			}
		}
		_, _ = fmt.Fprintln(w)
	}
	return nil
}

func inside(x, y, size int) bool {
	return x >= 0 && x < size && y >= 0 && y < size
}
