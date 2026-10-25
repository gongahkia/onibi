package termui

import "strings"

// Each letter is drawn on the same five-row grid. Block cells avoid font
// ligatures between slashes/underscores; the offset layer supplies the shadow.
var letterPixels = map[rune][]string{
	'O': {" ### ", "#   #", "#   #", "#   #", " ### "},
	'N': {"#   #", "##  #", "# # #", "#  ##", "#   #"},
	'I': {"#####", "  #  ", "  #  ", "  #  ", "#####"},
	'B': {"#### ", "#   #", "#### ", "#   #", "#### "},
}

func logoRows(double, ascii bool) []string {
	scale := 1
	if double {
		scale = 2
	}
	width := 5*5*scale + 4*2 + 1
	front := make([][]bool, 6)
	for y := range front {
		front[y] = make([]bool, width)
	}
	for i, letter := range "ONIBI" {
		for y, row := range letterPixels[letter] {
			for x, pixel := range row {
				if pixel == '#' {
					for n := range scale {
						front[y][i*(5*scale+2)+x*scale+n] = true
					}
				}
			}
		}
	}
	rows := make([]string, 6)
	for y := range front {
		var b strings.Builder
		for x := range width {
			switch {
			case front[y][x]:
				if ascii {
					b.WriteByte('#')
				} else {
					b.WriteRune('█')
				}
			case y > 0 && x > 0 && front[y-1][x-1]:
				if ascii {
					b.WriteByte('.')
				} else {
					b.WriteRune('░')
				}
			default:
				b.WriteByte(' ')
			}
		}
		rows[y] = b.String()
	}
	return rows
}

func shadeLogo(row string, styled bool) string {
	if !styled {
		return row
	}
	var b strings.Builder
	style := ""
	for _, r := range row {
		next := "\x1b[2m"
		if r == '█' || r == '#' {
			next = "\x1b[1m"
		}
		if next != style {
			b.WriteString(next)
			style = next
		}
		b.WriteRune(r)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

func (u *UI) Banner(subtitle string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.bannerShown {
		return
	}
	u.bannerShown = true
	if !u.Interactive {
		u.lineLocked("Onibi / %s", Clean(subtitle))
		return
	}
	u.lineLocked("")
	switch {
	case u.columns() >= 64:
		for _, row := range logoRows(true, u.ASCII) {
			u.lineLocked("  %s", shadeLogo(row, !u.NoStyle))
		}
	case u.columns() >= 39:
		for _, row := range logoRows(false, u.ASCII) {
			u.lineLocked("  %s", shadeLogo(row, !u.NoStyle))
		}
	default:
		title := "ONIBI"
		if !u.NoStyle {
			title = "\x1b[1mONIBI\x1b[0m"
		}
		u.lineLocked("  %s", title)
	}
	u.lineLocked("")
	u.textLocked(subtitle, "  ", "  ")
	u.lineLocked("")
}

// Section is deliberately compact. Build + web produces one large banner,
// and nested build work never repeats an install/update command's banner.
func (u *UI) Section(title string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.lineLocked("")
	u.textLocked(title, "  ", "  ")
	if u.Interactive {
		rule := "─"
		if u.ASCII {
			rule = "-"
		}
		u.lineLocked("  %s", strings.Repeat(rule, max(1, min(60, u.columns()-4))))
	}
	u.lineLocked("")
}
