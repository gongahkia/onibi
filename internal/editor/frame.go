package editor

import "github.com/gdamore/tcell/v2"

// Compose off-screen before touching tcell's display buffer. tcell marks a cell
// dirty on intermediate writes (including clearing it), even if a later write
// restores its previous value. Only final frame differences belong on the wire.
func (a *App) paintCell(x, y int, value string, style tcell.Style) {
	if a.composing {
		a.frame.Put(x, y, value, style)
	} else {
		a.Screen.Put(x, y, value, style)
	}
}
func (a *App) paintRune(x, y int, main rune, combining []rune, style tcell.Style) {
	a.paintCell(x, y, string(append([]rune{main}, combining...)), style)
}
func (a *App) finishFrame() {
	a.composing = false
	w, h := a.frame.Size()
	for y := 0; y < h; y++ {
		for x := 0; x < w; {
			value, style, width := a.frame.Get(x, y)
			old, oldStyle, oldWidth := a.Screen.Get(x, y)
			if value != old || style != oldStyle || width != oldWidth {
				a.Screen.Put(x, y, value, style)
			}
			x += max(1, width)
		}
	}
	a.Screen.Show()
}
