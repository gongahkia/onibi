package markup

import (
	"context"
	"strings"
	"testing"
)

// These fixtures exercise the terminal rendering of every CommonMark node
// family and the enabled extensions, rather than retesting Goldmark's HTML.
func TestMarkdownSyntaxCoverage(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{"atx and setext headings", "# One\n\nTwo\n===\n\nThree\n---\n", []string{"One\n\nTwo\n\nThree"}},
		{"paragraphs and line breaks", "soft\nline\n\nfirst  \nsecond\\\nthird\n", []string{"soft line\n\nfirst\nsecond\nthird"}},
		{"escapes and entities", "\\*literal\\* &amp; &#65; &#x1F600; &unknown;", []string{"*literal* & A 😀 &unknown;"}},
		{"nested emphasis", "***both*** __strong__ *italic* a_b_c ~~gone~~", []string{"both strong italic a_b_c gone"}},
		{"code spans", "`` a ` b `` and `a\nb` and ` &amp; \\* `", []string{"a ` b and a b and &amp; \\*"}},
		{"fenced code", "~~~go\n**literal** &amp;\n~~~\n", []string{"┌ go\n**literal** &amp;"}},
		{"indented code", "    <raw> &amp;\n    second\n", []string{"<raw> &amp;\nsecond"}},
		{"thematic break", "before\n\n***\n\nafter", []string{"before\n\n────────────────────────\n\nafter"}},
		{"reference links", "[full][target] [target][] [target]\n\n[target]: https://example.com \"title\"", []string{"full target target"}},
		{"autolinks", "<https://example.com> <hello@example.com> www.example.org", []string{"https://example.com", "hello@example.com", "www.example.org"}},
		{"images", "![**alt**](pic.png) ![][img]\n\n[img]: https://example.com/pic.png", []string{"[image: alt] (pic.png)", "[image: ] (https://example.com/pic.png)"}},
		{"raw HTML", "before <b>inline</b>\n\n<div>block</div>\n\n<!-- comment -->", []string{"<b>inline</b>", "<div>block</div>", "<!-- comment -->"}},
		{"ordered list start", "7. seven\n8. eight\n", []string{"7. seven\n8. eight"}},
		{"nested list", "- parent\n  - child\n    1. deep\n- sibling\n", []string{"• parent\n  • child\n    1. deep\n• sibling"}},
		{"empty parent list", "- - child\n", []string{"• \n  • child"}},
		{"loose list continuation", "- first\n\n  second  \n  third\n\n- next\n", []string{"• first\n\n  second\n  third\n\n• next"}},
		{"nested quotes", "> first  \n> second\n>\n> > nested\n>\n> last\n", []string{"▎ first\n▎ second", "▎ ▎ nested", "▎ last"}},
		{"quote in list", "- > quote\n  > next  \n  > last\n", []string{"• ▎ quote next\n  ▎ last"}},
		{"list in quote", "> - first\n> - second\n", []string{"▎ • first\n▎ • second"}},
		{"heading in list", "- # **Heading**\n\n  body\n", []string{"• Heading\n\n  body"}},
		{"code in list", "- ```\n  line one\n  line two\n  ```\n", []string{"• line one\n  line two"}},
		{"tasks", "- [x] done\n- [X] also\n- [ ] pending", []string{"• ☑ done", "• ☑ also", "• ☐ pending"}},
		{"escaped table pipes", "| A | B |\n|---|---|\n| x\\|y | `a\\|b` |", []string{"x|y", "a|b"}},
		{"footnotes", "See[^note], again[^note], then[^other].\n\n[^note]: **First** note.\n\n    Second paragraph.\n\n[^other]: Other note.\n", []string{"See[1], again[1], then[2].", "Footnotes", "[1] First note.\n\n    Second paragraph.", "[2] Other note."}},
		{"undefined footnote", "Unknown[^missing].", []string{"Unknown[^missing]."}},
		{"definition lists", "Term\n: First definition.\n: Another definition.\n\nOther\n: **Bold** description.\n", []string{"Term\n  First definition.", "  Another definition.", "Other\n  Bold description."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := Render(context.Background(), tc.source, "markdown")
			if err != nil {
				t.Fatal(err)
			}
			validate(t, d)
			for _, want := range tc.want {
				if !strings.Contains(d.Text, want) {
					t.Fatalf("missing %q in %q", want, d.Text)
				}
			}
		})
	}
}

func TestMarkdownAlignedUnicodeTables(t *testing.T) {
	d, err := Render(context.Background(), "| Left | Center | Right |\n|:---|:---:|---:|\n| 界 | é | 7 |\n", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	validate(t, d)
	if !strings.Contains(d.Text, "│ 界   │   é    │     7 │") {
		t.Fatal(d.Text)
	}
}

func TestMarkdownContainerHeadingsAndStyles(t *testing.T) {
	d, err := Render(context.Background(), "- # **Heading**\n\n  > ## Nested\n\n> ## Quoted\n\n[ref][] ***both***\n\n[ref]: https://example.com\n", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	validate(t, d)
	if len(d.Headings) != 3 || d.Headings[0].Title != "Heading" || d.Headings[1].Title != "Nested" || d.Headings[2].Title != "Quoted" {
		t.Fatal(d.Headings)
	}
	link, combined := false, false
	for _, span := range d.Spans {
		link = link || span.Style.Link == "https://example.com"
		combined = combined || span.Style.Bold && span.Style.Italic
	}
	if !link || !combined {
		t.Fatal("lost reference link or nested emphasis styles")
	}
}

func BenchmarkRenderExtendedMarkdown(b *testing.B) {
	benchmarkRender(b, "markdown", "> - first  \n>   second\n>   - child\n\nTerm\n: Definition with **bold**.\n\n| Left | Right |\n|:---|---:|\n| 界 | 123 |\n\n")
}
