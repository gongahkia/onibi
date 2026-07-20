package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderDocsWritesStyledPagesAndRewritesInternalLinks(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "getting-started.md", "# Getting Started\n\n[Security](security.md#at-rest-state)\n")
	writeDoc(t, root, "security.md", "# Security\n\nSafe.\n")
	writeDoc(t, root, "blog/entry.md", "# Entry\n\n[Guide](../getting-started.md)\n")
	if err := renderDocs(root, false); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(root, "getting-started.html"))
	if err != nil {
		t.Fatal(err)
	}
	output := string(page)
	for _, want := range []string{
		`href="assets/site.css"`,
		`href="security.html#at-rest-state"`,
		`class="prose"`,
		`<h1>Getting Started</h1>`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("rendered page missing %q", want)
		}
	}
	blog, err := os.ReadFile(filepath.Join(root, "blog", "entry.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blog), `href="../getting-started.html"`) {
		t.Fatalf("nested page did not rewrite internal link: %s", blog)
	}
	if err := renderDocs(root, true); err != nil {
		t.Fatal(err)
	}
}

func TestRewriteDocLinkLeavesExternalAndOutOfTreeMarkdownUntouched(t *testing.T) {
	known := map[string]struct{}{"security.md": {}}
	for input, want := range map[string]string{
		"https://example.test/a.md": "https://example.test/a.md",
		"../README.md":              "../README.md",
		"security.md":               "security.html",
	} {
		if got := rewriteDocLink(input, "getting-started.md", known); got != want {
			t.Errorf("rewriteDocLink(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRepositoryPagesAreCurrentAndInternalHTMLLinksResolve(t *testing.T) {
	root := filepath.Join("..", "..", "docs")
	if err := renderDocs(root, true); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(root, func(pagePath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(pagePath) != ".html" {
			return nil
		}
		contents, err := os.ReadFile(pagePath)
		if err != nil {
			return err
		}
		for _, match := range hrefs.FindAllStringSubmatch(string(contents), -1) {
			href := strings.SplitN(match[1], "#", 2)[0]
			href = strings.SplitN(href, "?", 2)[0]
			if !strings.HasSuffix(href, ".html") {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(pagePath), filepath.FromSlash(href))); err != nil {
				t.Errorf("%s links to missing %s", pagePath, href)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeDoc(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
