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

	"github.com/rivo/uniseg"
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
	img := drawTerminal(parseTerminal(buf, opts.Rows, opts.Cols), face)
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
	fg                  color.RGBA
	bg                  color.RGBA
	bold, faint, italic bool
	underline, strike   bool
	inverse, conceal    bool
	hyperlink           string
}

type screenCell struct {
	r            rune
	text         string
	width        int
	continuation bool
	terminalStyle
}

func defaultTerminalStyle() terminalStyle { return terminalStyle{fg: defaultFG, bg: defaultBG} }

func screenCells(buf []byte, rows, cols int) [][]screenCell {
	cells := parseTerminal(buf, rows, cols).cells
	for row := range cells {
		end := len(cells[row])
		for end > 0 && cells[row][end-1].r == 0 && !cells[row][end-1].continuation {
			end--
		}
		cells[row] = cells[row][:end]
	}
	return cells
}

func screenLines(buf []byte, rows, cols int) []string {
	cells := screenCells(buf, rows, cols)
	out := make([]string, len(cells))
	for i, line := range cells {
		var b strings.Builder
		for _, cell := range line {
			if !cell.continuation && cell.r != 0 {
				b.WriteString(cell.text)
			}
		}
		out[i] = b.String()
	}
	return out
}

func parseScreenCells(buf []byte, cols int) [][]screenCell {
	rows := bytes.Count(buf, []byte{'\n'}) + 1
	return screenCells(buf, maxInt(1, rows), cols)
}

type terminalPage struct {
	cells                   [][]screenCell
	row, col                int
	savedRow, savedCol      int
	scrollTop, scrollBottom int
}

type terminalScreen struct {
	cells         [][]screenCell
	cursorRow     int
	cursorCol     int
	cursorVisible bool
}

type terminalParser struct {
	primary, alternate terminalPage
	active             *terminalPage
	style              terminalStyle
	cursorVisible      bool
}

func newTerminalPage(rows, cols int, bottom bool) terminalPage {
	page := terminalPage{cells: make([][]screenCell, rows), scrollBottom: rows - 1}
	if bottom {
		page.row = rows - 1
	}
	for row := range page.cells {
		page.cells[row] = blankRow(cols)
	}
	return page
}

func blankCell() screenCell { return screenCell{terminalStyle: defaultTerminalStyle()} }

func blankRow(cols int) []screenCell {
	row := make([]screenCell, cols)
	for col := range row {
		row[col] = blankCell()
	}
	return row
}

func parseTerminal(buf []byte, rows, cols int) terminalScreen {
	rows, cols = maxInt(1, rows), maxInt(1, cols)
	p := &terminalParser{style: defaultTerminalStyle(), cursorVisible: true}
	p.primary = newTerminalPage(rows, cols, true)
	p.alternate = newTerminalPage(rows, cols, false)
	p.active = &p.primary
	for i := 0; i < len(buf); {
		if buf[i] == 0x1b {
			i = p.escape(buf, i)
			continue
		}
		switch buf[i] {
		case '\r':
			p.active.col = 0
			i++
		case '\n', '\v', '\f':
			p.active.col = 0
			p.index()
			i++
		case '\b':
			p.backspace()
			i++
		case '\t':
			p.tab()
			i++
		default:
			end := i
			for end < len(buf) && buf[end] != 0x1b && buf[end] >= 0x20 && buf[end] != 0x7f {
				end++
			}
			if end == i {
				i++
				continue
			}
			p.writeText(string(buf[i:end]))
			i = end
		}
	}
	return terminalScreen{cells: p.active.cells, cursorRow: p.active.row, cursorCol: p.active.col, cursorVisible: p.cursorVisible}
}

func (p *terminalParser) writeText(text string) {
	clusters := uniseg.NewGraphemes(text)
	for clusters.Next() {
		cluster := clusters.Str()
		if cluster == "" || !safeCluster(cluster) {
			continue
		}
		p.writeCluster(cluster, uniseg.StringWidth(cluster))
	}
}

func safeCluster(cluster string) bool {
	for _, r := range cluster {
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.In(r, unicode.Bidi_Control) {
			return false
		}
	}
	return true
}

func (p *terminalParser) writeCluster(cluster string, width int) {
	if width <= 0 {
		col := p.active.col - 1
		if col < 0 {
			return
		}
		if p.active.cells[p.active.row][col].continuation && col > 0 {
			col--
		}
		if p.active.cells[p.active.row][col].r != 0 {
			p.active.cells[p.active.row][col].text += cluster
		}
		return
	}
	if width > 2 {
		width = 1
	}
	cols := len(p.active.cells[0])
	if p.active.col >= cols || (width == 2 && p.active.col == cols-1) {
		return
	}
	r, _ := utf8.DecodeRuneInString(cluster)
	cell := screenCell{r: r, text: cluster, width: width, terminalStyle: p.style}
	p.active.cells[p.active.row][p.active.col] = cell
	if width == 2 {
		p.active.cells[p.active.row][p.active.col+1] = screenCell{continuation: true, terminalStyle: p.style}
	}
	p.active.col += width
}

func (p *terminalParser) index() {
	page := p.active
	if page.row < page.scrollBottom {
		page.row++
		return
	}
	p.scrollUp(1)
}

func (p *terminalParser) reverseIndex() {
	page := p.active
	if page.row > page.scrollTop {
		page.row--
		return
	}
	p.scrollDown(1)
}

func (p *terminalParser) backspace() {
	if p.active.col == 0 {
		return
	}
	p.active.col--
	if p.active.cells[p.active.row][p.active.col].continuation && p.active.col > 0 {
		p.active.col--
	}
}

func (p *terminalParser) tab() {
	next := ((p.active.col / 8) + 1) * 8
	for p.active.col < next {
		p.writeCluster(" ", 1)
	}
}

func (p *terminalParser) scrollUp(n int) {
	page := p.active
	n = minInt(maxInt(1, n), page.scrollBottom-page.scrollTop+1)
	copy(page.cells[page.scrollTop:page.scrollBottom+1-n], page.cells[page.scrollTop+n:page.scrollBottom+1])
	for row := page.scrollBottom + 1 - n; row <= page.scrollBottom; row++ {
		p.clearRow(row)
	}
}

func (p *terminalParser) scrollDown(n int) {
	page := p.active
	n = minInt(maxInt(1, n), page.scrollBottom-page.scrollTop+1)
	copy(page.cells[page.scrollTop+n:page.scrollBottom+1], page.cells[page.scrollTop:page.scrollBottom+1-n])
	for row := page.scrollTop; row < page.scrollTop+n; row++ {
		p.clearRow(row)
	}
}

func (p *terminalParser) clearRow(row int) {
	p.active.cells[row] = blankRow(len(p.active.cells[row]))
}

func (p *terminalParser) escape(buf []byte, start int) int {
	if start+1 >= len(buf) {
		return len(buf)
	}
	switch buf[start+1] {
	case '[':
		end, private, params, final, ok := csiSequence(buf, start)
		if ok {
			p.csi(private, params, final)
		}
		return end
	case ']':
		end, value := oscSequence(buf, start)
		p.osc(value)
		return end
	case 'P', '^', '_':
		return stringTerminatedSequence(buf, start+2)
	case '7':
		p.active.savedRow, p.active.savedCol = p.active.row, p.active.col
		return start + 2
	case '8':
		p.active.row, p.active.col = p.active.savedRow, p.active.savedCol
		p.clampCursor()
		return start + 2
	case 'D':
		p.index()
	case 'M':
		p.reverseIndex()
	case 'E':
		p.active.col = 0
		p.index()
	case 'c':
		p.primary = newTerminalPage(len(p.primary.cells), len(p.primary.cells[0]), true)
		p.alternate = newTerminalPage(len(p.primary.cells), len(p.primary.cells[0]), false)
		p.active, p.style, p.cursorVisible = &p.primary, defaultTerminalStyle(), true
	case '#':
		return minInt(len(buf), start+3)
	}
	return start + 2
}

func csiSequence(buf []byte, start int) (int, byte, []int, byte, bool) {
	i := start + 2
	private := byte(0)
	if i < len(buf) && (buf[i] == '?' || buf[i] == '>' || buf[i] == '!') {
		private, i = buf[i], i+1
	}
	paramsStart := i
	for i < len(buf) && (buf[i] < 0x40 || buf[i] > 0x7e) {
		i++
	}
	if i >= len(buf) {
		return len(buf), 0, nil, 0, false
	}
	return i + 1, private, parseSGRParams(buf[paramsStart:i]), buf[i], true
}

func oscSequence(buf []byte, start int) (int, string) {
	i := start + 2
	for i < len(buf) {
		if buf[i] == 0x07 {
			return i + 1, string(buf[start+2 : i])
		}
		if buf[i] == 0x1b && i+1 < len(buf) && buf[i+1] == '\\' {
			return i + 2, string(buf[start+2 : i])
		}
		i++
	}
	return len(buf), ""
}

func stringTerminatedSequence(buf []byte, start int) int {
	for i := start; i < len(buf); i++ {
		if buf[i] == 0x1b && i+1 < len(buf) && buf[i+1] == '\\' {
			return i + 2
		}
	}
	return len(buf)
}

func (p *terminalParser) osc(value string) {
	parts := strings.SplitN(value, ";", 3)
	if len(parts) == 3 && parts[0] == "8" {
		p.style.hyperlink = parts[2]
	}
}

func (p *terminalParser) csi(private byte, params []int, final byte) {
	page, rows, cols := p.active, len(p.active.cells), len(p.active.cells[0])
	param := func(i, fallback int) int {
		if i >= len(params) || params[i] <= 0 {
			return fallback
		}
		return params[i]
	}
	switch final {
	case 'm':
		p.style = applySGR(p.style, params)
	case 'A':
		page.row = maxInt(page.scrollTop, page.row-param(0, 1))
	case 'B', 'e':
		page.row = minInt(page.scrollBottom, page.row+param(0, 1))
	case 'C', 'a':
		page.col = minInt(cols-1, page.col+param(0, 1))
	case 'D':
		page.col = maxInt(0, page.col-param(0, 1))
	case 'E':
		page.row, page.col = minInt(page.scrollBottom, page.row+param(0, 1)), 0
	case 'F':
		page.row, page.col = maxInt(page.scrollTop, page.row-param(0, 1)), 0
	case 'G', '`':
		page.col = minInt(cols-1, param(0, 1)-1)
	case 'd':
		page.row = minInt(rows-1, param(0, 1)-1)
	case 'H', 'f':
		page.row, page.col = minInt(rows-1, param(0, 1)-1), minInt(cols-1, param(1, 1)-1)
	case 'J':
		p.eraseDisplay(param(0, 0))
	case 'K':
		p.eraseLine(param(0, 0))
	case 'L':
		p.insertLines(param(0, 1))
	case 'M':
		p.deleteLines(param(0, 1))
	case 'P':
		p.deleteChars(param(0, 1))
	case '@':
		p.insertChars(param(0, 1))
	case 'X':
		p.eraseChars(param(0, 1))
	case 'S':
		p.scrollUp(param(0, 1))
	case 'T':
		p.scrollDown(param(0, 1))
	case 'r':
		page.scrollTop, page.scrollBottom = maxInt(0, param(0, 1)-1), minInt(rows-1, param(1, rows)-1)
		if page.scrollTop >= page.scrollBottom {
			page.scrollTop, page.scrollBottom = 0, rows-1
		}
		page.row, page.col = page.scrollTop, 0
	case 's':
		page.savedRow, page.savedCol = page.row, page.col
	case 'u':
		page.row, page.col = page.savedRow, page.savedCol
		p.clampCursor()
	case 'h', 'l':
		set := final == 'h'
		for _, mode := range params {
			if private == '?' && mode == 25 {
				p.cursorVisible = set
			}
			if private == '?' && (mode == 47 || mode == 1047 || mode == 1049) {
				p.setAlternate(set)
			}
		}
	}
}

func (p *terminalParser) setAlternate(enable bool) {
	if enable {
		if p.active == &p.alternate {
			return
		}
		p.primary.savedRow, p.primary.savedCol = p.primary.row, p.primary.col
		p.alternate = newTerminalPage(len(p.primary.cells), len(p.primary.cells[0]), false)
		p.active = &p.alternate
		return
	}
	p.active = &p.primary
	p.primary.row, p.primary.col = p.primary.savedRow, p.primary.savedCol
	p.clampCursor()
}

func (p *terminalParser) eraseDisplay(mode int) {
	page := p.active
	switch mode {
	case 1:
		for row := 0; row <= page.row; row++ {
			end := len(page.cells[row])
			if row == page.row {
				end = page.col + 1
			}
			for col := 0; col < end; col++ {
				page.cells[row][col] = blankCell()
			}
		}
	default:
		start := page.row
		for row := start; row < len(page.cells); row++ {
			from := 0
			if row == start {
				from = page.col
			}
			for col := from; col < len(page.cells[row]); col++ {
				page.cells[row][col] = blankCell()
			}
		}
		if mode == 2 || mode == 3 {
			for row := range page.cells {
				p.clearRow(row)
			}
		}
	}
}

func (p *terminalParser) eraseLine(mode int) {
	page := p.active
	start, end := page.col, len(page.cells[page.row])
	if mode == 1 {
		start = 0
		end = page.col + 1
	}
	if mode == 2 {
		start = 0
	}
	for col := start; col < end; col++ {
		page.cells[page.row][col] = blankCell()
	}
}

func (p *terminalParser) eraseChars(n int) {
	page := p.active
	for col := page.col; col < minInt(len(page.cells[page.row]), page.col+n); col++ {
		page.cells[page.row][col] = blankCell()
	}
}

func (p *terminalParser) deleteChars(n int) {
	page, line := p.active, p.active.cells[p.active.row]
	n = minInt(n, len(line)-page.col)
	copy(line[page.col:], line[page.col+n:])
	for col := len(line) - n; col < len(line); col++ {
		line[col] = blankCell()
	}
}

func (p *terminalParser) insertChars(n int) {
	page, line := p.active, p.active.cells[p.active.row]
	n = minInt(n, len(line)-page.col)
	copy(line[page.col+n:], line[page.col:len(line)-n])
	for col := page.col; col < page.col+n; col++ {
		line[col] = blankCell()
	}
}

func (p *terminalParser) insertLines(n int) {
	page := p.active
	if page.row < page.scrollTop || page.row > page.scrollBottom {
		return
	}
	n = minInt(n, page.scrollBottom-page.row+1)
	copy(page.cells[page.row+n:page.scrollBottom+1], page.cells[page.row:page.scrollBottom+1-n])
	for row := page.row; row < page.row+n; row++ {
		p.clearRow(row)
	}
}

func (p *terminalParser) deleteLines(n int) {
	page := p.active
	if page.row < page.scrollTop || page.row > page.scrollBottom {
		return
	}
	n = minInt(n, page.scrollBottom-page.row+1)
	copy(page.cells[page.row:page.scrollBottom+1-n], page.cells[page.row+n:page.scrollBottom+1])
	for row := page.scrollBottom + 1 - n; row <= page.scrollBottom; row++ {
		p.clearRow(row)
	}
}

func (p *terminalParser) clampCursor() {
	p.active.row = minInt(maxInt(0, p.active.row), len(p.active.cells)-1)
	p.active.col = minInt(maxInt(0, p.active.col), len(p.active.cells[0])-1)
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
	parts := strings.FieldsFunc(string(raw), func(r rune) bool { return r == ';' || r == ':' })
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
		case code == 1:
			style.bold = true
		case code == 2:
			style.faint = true
		case code == 3:
			style.italic = true
		case code == 4 || code == 21:
			style.underline = true
		case code == 7:
			style.inverse = true
		case code == 8:
			style.conceal = true
		case code == 9:
			style.strike = true
		case code == 22:
			style.bold, style.faint = false, false
		case code == 23:
			style.italic = false
		case code == 24:
			style.underline = false
		case code == 27:
			style.inverse = false
		case code == 28:
			style.conceal = false
		case code == 29:
			style.strike = false
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

func drawTerminal(screen terminalScreen, face font.Face) *image.RGBA {
	metrics := face.Metrics()
	cellW := maxInt(1, font.MeasureString(face, "M").Round())
	cellH := maxInt(1, metrics.Height.Ceil())
	ascent := metrics.Ascent.Ceil()
	pad := 8
	lines, rows := screen.cells, len(screen.cells)
	cols := 1
	if rows > 0 {
		cols = len(screen.cells[0])
	}
	img := image.NewRGBA(image.Rect(0, 0, cols*cellW+pad*2, rows*cellH+pad*2))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: defaultBG}, image.Point{}, draw.Src)
	for row, line := range lines {
		for col, cell := range line {
			if cell.continuation {
				continue
			}
			x := pad + col*cellW
			y := pad + row*cellH
			width := maxInt(1, cell.width)
			fg, bg := styledColors(cell)
			draw.Draw(img, image.Rect(x, y, x+cellW*width, y+cellH), &image.Uniform{C: bg}, image.Point{}, draw.Src)
			if cell.r != 0 && cell.r != ' ' && !cell.conceal {
				drawGlyph(img, cell.text, x, y, cellW*width, cellH, ascent, face, fg, cell)
			}
			if cell.underline || cell.hyperlink != "" {
				draw.Draw(img, image.Rect(x, y+ascent+1, x+cellW*width, y+ascent+2), &image.Uniform{C: fg}, image.Point{}, draw.Src)
			}
			if cell.strike {
				draw.Draw(img, image.Rect(x, y+cellH/2, x+cellW*width, y+cellH/2+1), &image.Uniform{C: fg}, image.Point{}, draw.Src)
			}
		}
	}
	if screen.cursorVisible && rows > 0 {
		row := minInt(maxInt(0, screen.cursorRow), rows-1)
		col := minInt(maxInt(0, screen.cursorCol), cols-1)
		x, y := pad+col*cellW, pad+row*cellH
		draw.Draw(img, image.Rect(x, y+2, x+2, y+cellH-2), &image.Uniform{C: activeBorder}, image.Point{}, draw.Src)
	}
	drawActiveBorder(img)
	return img
}

func styledColors(cell screenCell) (color.RGBA, color.RGBA) {
	fg, bg := cell.fg, cell.bg
	if fg.A == 0 {
		fg = defaultFG
	}
	if bg.A == 0 {
		bg = defaultBG
	}
	if cell.inverse {
		fg, bg = bg, fg
	}
	if cell.faint {
		fg.A = 150
	}
	if cell.hyperlink != "" && fg == defaultFG {
		fg = color.RGBA{R: 105, G: 175, B: 255, A: 255}
	}
	return fg, bg
}

func drawGlyph(dst *image.RGBA, text string, x, y, width, height, ascent int, face font.Face, fg color.RGBA, cell screenCell) {
	if !cell.italic {
		d := font.Drawer{Dst: dst, Src: &image.Uniform{C: fg}, Face: face, Dot: fixed.P(x, y+ascent)}
		d.DrawString(text)
		if cell.bold {
			d.Dot.X += fixed.I(1)
			d.DrawString(text)
		}
		return
	}
	tmp := image.NewRGBA(image.Rect(0, 0, width+height/4+2, height+2))
	d := font.Drawer{Dst: tmp, Src: &image.Uniform{C: fg}, Face: face, Dot: fixed.P(1, ascent)}
	d.DrawString(text)
	if cell.bold {
		d.Dot.X += fixed.I(1)
		d.DrawString(text)
	}
	for sy := 0; sy < tmp.Bounds().Dy(); sy++ {
		shift := (height - sy) / 6
		draw.Draw(dst, image.Rect(x+shift, y+sy, x+shift+tmp.Bounds().Dx(), y+sy+1), tmp, image.Point{Y: sy}, draw.Over)
	}
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
