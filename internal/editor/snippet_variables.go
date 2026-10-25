package editor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gongahkia/shack/internal/text"
)

// Called on the snippet worker with immutable editor snapshots. CLIPBOARD uses
// Shack's existing copy; expansion never asks the terminal for clipboard access.
func snippetVariables(source text.Rope, selection text.Selection, path, root, clipboard string, now time.Time) map[string]string {
	line := source.LineAt(selection.Head)
	start, end := source.LineStart(line), source.LineEnd(line)
	// Keep at least one extra byte beyond the expansion limit, so using an
	// oversized variable fails explicitly instead of silently inserting a prefix.
	const window = (128 << 10) + 4
	bounded := func(lo, hi int) string {
		hi = min(hi, lo+window)
		for hi > lo && !source.Boundary(hi) {
			hi--
		}
		return source.Slice(lo, hi)
	}
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_' }
	left, right := max(start, selection.Head-window), min(end, selection.Head+window)
	for left < selection.Head && !source.Boundary(left) {
		left++
	}
	for right > selection.Head && !source.Boundary(right) {
		right--
	}
	prefix, suffix := source.Slice(left, selection.Head), source.Slice(selection.Head, right)
	lo, hi := left, right
	if at := strings.LastIndexFunc(prefix, func(r rune) bool { return !word(r) }); at >= 0 {
		_, n := utf8.DecodeRuneInString(prefix[at:])
		lo = left + at + n
	}
	if at := strings.IndexFunc(suffix, func(r rune) bool { return !word(r) }); at >= 0 {
		hi = selection.Head + at
	}
	filename, directory, relative := "", "", ""
	if path != "" {
		filename = filepath.Base(path)
		directory = filepath.Dir(path)
		relative, _ = filepath.Rel(root, path)
	}
	base := filename
	if dot := strings.LastIndexByte(filename, '.'); dot > 0 {
		base = filename[:dot]
	}
	vars := map[string]string{
		"TM_SELECTED_TEXT": bounded(selection.Start(), selection.End()), "TM_CURRENT_LINE": strings.TrimSuffix(bounded(start, end), "\r"), "TM_CURRENT_WORD": bounded(lo, hi),
		"TM_LINE_INDEX": strconv.Itoa(line), "TM_LINE_NUMBER": strconv.Itoa(line + 1), "TM_FILENAME": filename, "TM_FILENAME_BASE": base, "TM_DIRECTORY": directory, "TM_FILEPATH": path,
		"RELATIVE_FILEPATH": relative, "WORKSPACE_NAME": filepath.Base(root), "WORKSPACE_FOLDER": root, "CLIPBOARD": clipboard, "CURSOR_INDEX": "0", "CURSOR_NUMBER": "1",
		"CURRENT_YEAR": now.Format("2006"), "CURRENT_YEAR_SHORT": now.Format("06"), "CURRENT_MONTH": now.Format("01"), "CURRENT_MONTH_NAME": now.Format("January"), "CURRENT_MONTH_NAME_SHORT": now.Format("Jan"), "CURRENT_DATE": now.Format("02"), "CURRENT_DAY_NAME": now.Format("Monday"), "CURRENT_DAY_NAME_SHORT": now.Format("Mon"), "CURRENT_HOUR": now.Format("15"), "CURRENT_MINUTE": now.Format("04"), "CURRENT_SECOND": now.Format("05"), "CURRENT_MILLISECOND": fmt.Sprintf("%03d", now.Nanosecond()/1000000), "CURRENT_SECONDS_UNIX": strconv.FormatInt(now.Unix(), 10), "CURRENT_MILLISECONDS_UNIX": strconv.FormatInt(now.UnixMilli(), 10), "CURRENT_TIMEZONE_OFFSET": now.Format("-07:00"), "CURRENT_TIMEZONE_NAME": snippetTimezone(now),
		"BLOCK_COMMENT_START": "", "BLOCK_COMMENT_END": "", "LINE_COMMENT": "",
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".c", ".h", ".cc", ".cpp", ".hpp", ".rs", ".js", ".jsx", ".ts", ".tsx", ".java", ".cs", ".swift", ".kt":
		vars["LINE_COMMENT"] = "//"
		vars["BLOCK_COMMENT_START"] = "/*"
		vars["BLOCK_COMMENT_END"] = "*/"
	case ".css":
		vars["BLOCK_COMMENT_START"] = "/*"
		vars["BLOCK_COMMENT_END"] = "*/"
	case ".py", ".sh", ".bash", ".zsh", ".rb", ".pl", ".yaml", ".yml", ".toml":
		vars["LINE_COMMENT"] = "#"
	case ".sql":
		vars["LINE_COMMENT"] = "--"
		vars["BLOCK_COMMENT_START"] = "/*"
		vars["BLOCK_COMMENT_END"] = "*/"
	case ".lua":
		vars["LINE_COMMENT"] = "--"
		vars["BLOCK_COMMENT_START"] = "--[["
		vars["BLOCK_COMMENT_END"] = "]]"
	case ".html", ".xml", ".md":
		vars["BLOCK_COMMENT_START"] = "<!--"
		vars["BLOCK_COMMENT_END"] = "-->"
	case ".ml", ".mli":
		vars["BLOCK_COMMENT_START"] = "(*"
		vars["BLOCK_COMMENT_END"] = "*)"
	}
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err == nil {
		vars["RANDOM_HEX"] = hex.EncodeToString(bytes[:3])
		bytes[6] = (bytes[6] & 15) | 64
		bytes[8] = (bytes[8] & 63) | 128
		encoded := hex.EncodeToString(bytes)
		vars["UUID"] = encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
	}
	if n, err := rand.Int(rand.Reader, big.NewInt(1000000)); err == nil {
		vars["RANDOM"] = fmt.Sprintf("%06d", n.Int64())
	}
	return vars
}

func snippetTimezone(now time.Time) string {
	name := now.Location().String()
	if name != "Local" {
		return name
	}
	if zone := os.Getenv("TZ"); zone != "" && !strings.HasPrefix(zone, ":") && !filepath.IsAbs(zone) {
		return zone
	}
	if path, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if _, zone, ok := strings.Cut(path, "/zoneinfo/"); ok {
			return zone
		}
	}
	return name
}
