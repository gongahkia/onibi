package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"html"
	htmltemplate "html/template"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type document struct {
	Source     string
	Output     string
	Title      string
	Body       htmltemplate.HTML
	HasMermaid bool
}

type link struct {
	Title  string
	Href   string
	Active bool
}

type page struct {
	Title       string
	Description string
	Body        htmltemplate.HTML
	Stylesheet  string
	Responsive  string
	Favicon     string
	Home        string
	Source      string
	Top         []link
	Documents   []link
	HasMermaid  bool
}

var (
	headline = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)
	hrefs    = regexp.MustCompile(`href="([^"]*)"`)
	pageTmpl = htmltemplate.Must(htmltemplate.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="description" content="{{.Description}}">
  <link rel="icon" href="{{.Favicon}}" type="image/svg+xml">
  <link rel="stylesheet" href="{{.Stylesheet}}">
	<link rel="stylesheet" href="{{.Responsive}}">
  <title>{{.Title}} · Onibi</title>
</head>
<body>
  <a class="skip-link" href="#content">Skip to content</a>
  <a class="github-corner" href="https://github.com/gongahkia/onibi" target="_blank" rel="noopener" aria-label="View Onibi on GitHub"><svg viewBox="0 0 250 250" aria-hidden="true"><path d="M0,0 L115,115 L130,115 L142,142 L250,250 L250,0 Z"></path><path class="octo-arm" d="M128.3,109C113.8,99.7,119,89.6,119,89.6c3-7.6,1.5-11,1.5-11-1.3-6.6,2.9-2.3,2.9-2.3,3.9,4.6,2.1,11,2.1,11-2.9,10.3,4.5,14.6,8.9,15.9" fill="currentColor"></path><path class="octo-body" d="M115,115c-.1.1,3,1.5,4.8.4l13.9-13.8c3.2-2.4,6.2-3.2,8.5-3-8.4-10.6-8.5-24.6,1.6-40.6,4.7-4.6,10.2-6.9,15.9-7,1.3-1.6,2.7-7.6,10.9-10.9,0,0,4.7,1.9,7.4,15.9,4.1,1.9,7.3,5.1,11,8.8,3.7,3.7,6.9,10.2,9.3,14.6,13.7,2.6,16.2,5.5,16.2,5.5-4.2,8.2-7.6,11.1-9.1,11.7.2,5.8-2,10.4-6.7,15.9-16.4,16.4-30.6,9-41.2,1.6.2,2.8-1.8,6.8-5.8,10.8L141,136.5c-1.2,1.2.8,5.2.8,5.3Z" fill="currentColor"></path></svg></a>
  <header class="site-header"><div class="site-header-inner"><a class="brand" href="{{.Home}}">Onibi <span>docs</span></a><nav class="top-nav" aria-label="Primary">{{range .Top}}<a href="{{.Href}}"{{if .Active}} aria-current="page"{{end}}>{{.Title}}</a>{{end}}</nav></div></header>
  <div class="docs-shell">
    <aside class="docs-sidebar"><p class="sidebar-label">Documentation</p><nav aria-label="All documentation">{{range .Documents}}<a href="{{.Href}}"{{if .Active}} aria-current="page"{{end}}>{{.Title}}</a>{{end}}</nav></aside>
    <main id="content" class="prose">{{.Body}}</main>
  </div>
{{if .HasMermaid}}  <script type="module">
    import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11.16.0/dist/mermaid.esm.min.mjs";
    mermaid.initialize({startOnLoad:true,securityLevel:"strict",theme:"base",themeVariables:{background:"#f7f5ef",primaryColor:"#eee8dc",primaryTextColor:"#171511",primaryBorderColor:"#b9431c",lineColor:"#706b62",secondaryColor:"#f7f5ef",tertiaryColor:"#f7f5ef"},flowchart:{htmlLabels:false,useMaxWidth:true}});
  </script>
{{end}}
</body>
</html>
`))
)

func main() {
	root := flag.String("root", "docs", "documentation root")
	check := flag.Bool("check", false, "fail when generated pages are stale")
	flag.Parse()
	if err := renderDocs(*root, *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func renderDocs(root string, check bool) error {
	root = filepath.Clean(root)
	docs, err := collectDocuments(root)
	if err != nil {
		return err
	}
	known := make(map[string]struct{}, len(docs))
	for _, doc := range docs {
		known[doc.Source] = struct{}{}
	}
	for i := range docs {
		sourcePath := filepath.Join(root, filepath.FromSlash(docs[i].Source))
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		docs[i].HasMermaid = hasMermaidFence(string(source))
		docs[i].Body = htmltemplate.HTML(renderMarkdown(string(source), docs[i].Source, known))
	}
	for _, doc := range docs {
		data := page{
			Title:       doc.Title,
			Description: description(doc.Title),
			Body:        doc.Body,
			Stylesheet:  relativeLink(doc.Output, "assets/site.css"),
			Responsive:  relativeLink(doc.Output, "assets/responsive.css"),
			Favicon:     relativeLink(doc.Output, "favicon.svg"),
			Home:        relativeLink(doc.Output, "index.html"),
			Source:      relativeLink(doc.Output, doc.Source),
			Top:         navigation(doc, docs, []string{"getting-started.md", "transports.md", "web-push.md", "security.md"}),
			Documents:   navigation(doc, docs, nil),
			HasMermaid:  doc.HasMermaid,
		}
		var rendered bytes.Buffer
		if err := pageTmpl.Execute(&rendered, data); err != nil {
			return err
		}
		destination := filepath.Join(root, filepath.FromSlash(doc.Output))
		if check {
			existing, err := os.ReadFile(destination)
			if errors.Is(err, fs.ErrNotExist) || !bytes.Equal(existing, rendered.Bytes()) {
				return fmt.Errorf("generated page is stale: %s (run go run ./scripts/render-docs)", filepath.ToSlash(destination))
			}
			if err != nil {
				return err
			}
			continue
		}
		if err := os.WriteFile(destination, rendered.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func collectDocuments(root string) ([]document, error) {
	var docs []document
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		source := filepath.ToSlash(rel)
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		title := firstHeading(string(contents))
		if title == "" {
			return fmt.Errorf("missing H1: %s", filepath.ToSlash(path))
		}
		docs = append(docs, document{Source: source, Output: strings.TrimSuffix(source, ".md") + ".html", Title: title})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Title < docs[j].Title })
	return docs, nil
}

func firstHeading(source string) string {
	matches := headline.FindStringSubmatch(source)
	if len(matches) != 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func description(title string) string {
	return title + " documentation for Onibi, a single-user local coding-agent host."
}

func navigation(current document, docs []document, selected []string) []link {
	selectedSet := make(map[string]struct{}, len(selected))
	for _, source := range selected {
		selectedSet[source] = struct{}{}
	}
	links := make([]link, 0, len(docs))
	for _, doc := range docs {
		if len(selected) > 0 {
			if _, ok := selectedSet[doc.Source]; !ok {
				continue
			}
		}
		links = append(links, link{Title: doc.Title, Href: relativeLink(current.Output, doc.Output), Active: current.Source == doc.Source})
	}
	return links
}

func relativeLink(from, to string) string {
	link, err := filepath.Rel(filepath.Dir(from), to)
	if err != nil {
		panic(err)
	}
	return filepath.ToSlash(link)
}

func rewriteDocLinks(rendered, source string, known map[string]struct{}) string {
	return hrefs.ReplaceAllStringFunc(rendered, func(attribute string) string {
		match := hrefs.FindStringSubmatch(attribute)
		if len(match) != 2 {
			return attribute
		}
		original := html.UnescapeString(match[1])
		rewritten := rewriteDocLink(original, source, known)
		return `href="` + html.EscapeString(rewritten) + `"`
	})
}

func renderMarkdown(source, doc string, known map[string]struct{}) string {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	var out strings.Builder
	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}
		if marker, language, ok := fencedCode(line); ok {
			i++
			var code []string
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), marker) {
				code = append(code, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			if language == "mermaid" {
				fmt.Fprintf(&out, "<pre class=\"mermaid\">%s</pre>\n", html.EscapeString(strings.Join(code, "\n")+"\n"))
			} else {
				fmt.Fprintf(&out, "<pre><code class=\"language-%s\">%s</code></pre>\n", html.EscapeString(language), html.EscapeString(strings.Join(code, "\n")+"\n"))
			}
			continue
		}
		if level, heading, ok := heading(line); ok {
			fmt.Fprintf(&out, "<h%d>%s</h%d>\n", level, renderInline(heading, doc, known), level)
			i++
			continue
		}
		if isRule(line) {
			out.WriteString("<hr>\n")
			i++
			continue
		}
		if i+1 < len(lines) && strings.Contains(line, "|") && isTableSeparator(lines[i+1]) {
			head := tableCells(line)
			i += 2
			out.WriteString("<div class=\"table-wrap\"><table><thead><tr>")
			for _, cell := range head {
				fmt.Fprintf(&out, "<th>%s</th>", renderInline(cell, doc, known))
			}
			out.WriteString("</tr></thead><tbody>")
			for i < len(lines) && strings.Contains(lines[i], "|") && strings.TrimSpace(lines[i]) != "" {
				out.WriteString("<tr>")
				for _, cell := range tableCells(lines[i]) {
					fmt.Fprintf(&out, "<td>%s</td>", renderInline(cell, doc, known))
				}
				out.WriteString("</tr>")
				i++
			}
			out.WriteString("</tbody></table></div>\n")
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "> ") {
			var quote []string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "> ") {
				quote = append(quote, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "> ")))
				i++
			}
			fmt.Fprintf(&out, "<blockquote><p>%s</p></blockquote>\n", renderInline(strings.Join(quote, " "), doc, known))
			continue
		}
		if _, _, ok := listItem(line, false); ok {
			out.WriteString("<ul>\n")
			for i < len(lines) {
				_, item, ok := listItem(lines[i], false)
				if !ok {
					break
				}
				fmt.Fprintf(&out, "<li>%s</li>\n", renderInline(item, doc, known))
				i++
			}
			out.WriteString("</ul>\n")
			continue
		}
		if number, _, ok := listItem(line, true); ok {
			out.WriteString("<ol start=\"")
			out.WriteString(number)
			out.WriteString("\">\n")
			for i < len(lines) {
				_, item, ok := listItem(lines[i], true)
				if !ok {
					break
				}
				fmt.Fprintf(&out, "<li>%s</li>\n", renderInline(item, doc, known))
				i++
			}
			out.WriteString("</ol>\n")
			continue
		}
		var paragraph []string
		for i < len(lines) && strings.TrimSpace(lines[i]) != "" && !startsBlock(lines, i) {
			paragraph = append(paragraph, strings.TrimSpace(lines[i]))
			i++
		}
		if len(paragraph) == 0 {
			paragraph = append(paragraph, strings.TrimSpace(lines[i]))
			i++
		}
		fmt.Fprintf(&out, "<p>%s</p>\n", renderInline(strings.Join(paragraph, " "), doc, known))
	}
	return out.String()
}

func hasMermaidFence(source string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n") {
		_, language, ok := fencedCode(line)
		if ok && language == "mermaid" {
			return true
		}
	}
	return false
}

func startsBlock(lines []string, i int) bool {
	if i >= len(lines) {
		return true
	}
	line := lines[i]
	if _, _, ok := fencedCode(line); ok {
		return true
	}
	if _, _, ok := heading(line); ok {
		return true
	}
	if isRule(line) || strings.HasPrefix(strings.TrimSpace(line), "> ") {
		return true
	}
	if _, _, ok := listItem(line, false); ok {
		return true
	}
	if _, _, ok := listItem(line, true); ok {
		return true
	}
	return i+1 < len(lines) && strings.Contains(line, "|") && isTableSeparator(lines[i+1])
}

func fencedCode(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "```") {
		return "", "", false
	}
	return "```", strings.TrimSpace(strings.TrimPrefix(trimmed, "```")), true
}

func heading(line string) (int, string, bool) {
	trimmed := strings.TrimSpace(line)
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || len(trimmed) <= level || trimmed[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(trimmed[level:]), true
}

func isRule(line string) bool {
	trimmed := strings.TrimSpace(line)
	return len(trimmed) >= 3 && strings.Trim(trimmed, "-*_ ") == "" && (strings.Contains(trimmed, "---") || strings.Contains(trimmed, "***"))
}

func isTableSeparator(line string) bool {
	cells := tableCells(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.Trim(cell, ":-")
		if cell != "" || !strings.Contains(line, "-") {
			return false
		}
	}
	return true
}

func tableCells(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func listItem(line string, ordered bool) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if !ordered {
		for _, marker := range []string{"- ", "* ", "+ "} {
			if strings.HasPrefix(trimmed, marker) {
				return "", strings.TrimSpace(strings.TrimPrefix(trimmed, marker)), true
			}
		}
		return "", "", false
	}
	end := 0
	for end < len(trimmed) && trimmed[end] >= '0' && trimmed[end] <= '9' {
		end++
	}
	if end == 0 || len(trimmed) <= end+1 || trimmed[end] != '.' || trimmed[end+1] != ' ' {
		return "", "", false
	}
	return trimmed[:end], strings.TrimSpace(trimmed[end+2:]), true
}

func renderInline(source, doc string, known map[string]struct{}) string {
	var out strings.Builder
	for i := 0; i < len(source); {
		if source[i] == '\\' && i+1 < len(source) {
			out.WriteString(html.EscapeString(source[i+1 : i+2]))
			i += 2
			continue
		}
		if source[i] == '`' {
			ticks := 1
			for i+ticks < len(source) && source[i+ticks] == '`' {
				ticks++
			}
			marker := strings.Repeat("`", ticks)
			end := strings.Index(source[i+ticks:], marker)
			if end >= 0 {
				end += i + ticks
				fmt.Fprintf(&out, "<code>%s</code>", html.EscapeString(source[i+ticks:end]))
				i = end + ticks
				continue
			}
		}
		if strings.HasPrefix(source[i:], "**") || strings.HasPrefix(source[i:], "__") {
			marker := source[i : i+2]
			if end := strings.Index(source[i+2:], marker); end >= 0 {
				end += i + 2
				fmt.Fprintf(&out, "<strong>%s</strong>", renderInline(source[i+2:end], doc, known))
				i = end + 2
				continue
			}
		}
		if source[i] == '!' && i+1 < len(source) && source[i+1] == '[' {
			if alt, destination, next, ok := markdownLink(source, i+1); ok {
				fmt.Fprintf(&out, "<img src=\"%s\" alt=\"%s\" loading=\"lazy\">", html.EscapeString(destination), html.EscapeString(alt))
				i = next
				continue
			}
		}
		if source[i] == '[' {
			if label, destination, next, ok := markdownLink(source, i); ok {
				fmt.Fprintf(&out, "<a href=\"%s\">%s</a>", html.EscapeString(rewriteDocLink(destination, doc, known)), renderInline(label, doc, known))
				i = next
				continue
			}
		}
		if source[i] == '<' {
			if end := strings.IndexByte(source[i+1:], '>'); end >= 0 {
				end += i + 1
				destination := source[i+1 : end]
				if strings.HasPrefix(destination, "https://") || strings.HasPrefix(destination, "http://") || strings.HasPrefix(destination, "mailto:") {
					fmt.Fprintf(&out, "<a href=\"%s\">%s</a>", html.EscapeString(destination), html.EscapeString(destination))
					i = end + 1
					continue
				}
			}
		}
		runeValue, size := utf8.DecodeRuneInString(source[i:])
		out.WriteString(html.EscapeString(string(runeValue)))
		i += size
	}
	return out.String()
}

func markdownLink(source string, start int) (string, string, int, bool) {
	labelEnd := strings.IndexByte(source[start+1:], ']')
	if labelEnd < 0 {
		return "", "", 0, false
	}
	labelEnd += start + 1
	if labelEnd+1 >= len(source) || source[labelEnd+1] != '(' {
		return "", "", 0, false
	}
	destinationEnd := strings.IndexByte(source[labelEnd+2:], ')')
	if destinationEnd < 0 {
		return "", "", 0, false
	}
	destinationEnd += labelEnd + 2
	return source[start+1 : labelEnd], strings.TrimSpace(source[labelEnd+2 : destinationEnd]), destinationEnd + 1, true
}

func rewriteDocLink(link, source string, known map[string]struct{}) string {
	base, suffix := link, ""
	if cut := strings.IndexAny(base, "?#"); cut >= 0 {
		base, suffix = base[:cut], base[cut:]
	}
	if !strings.HasSuffix(base, ".md") || strings.HasPrefix(base, "/") || strings.Contains(base, "://") {
		return link
	}
	target := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(source), base)))
	if _, ok := known[target]; !ok {
		return link
	}
	return strings.TrimSuffix(base, ".md") + ".html" + suffix
}
