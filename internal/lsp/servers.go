package lsp

import (
	"github.com/gongahkia/shack/internal/config"
	"os"
	"os/exec"
	"path/filepath"
)

func ServerFor(path string, overrides map[string]config.Server) (config.Server, bool) {
	ext := filepath.Ext(path)
	if custom, ok := overrides[ext]; ok {
		return custom, len(custom.Command) > 0
	}
	choices := map[string][]config.Server{
		".go":  {{Command: []string{"gopls"}, Language: "go"}},
		".rs":  {{Command: []string{"rust-analyzer"}, Language: "rust"}},
		".py":  {{Command: []string{"pyright-langserver", "--stdio"}, Language: "python"}, {Command: []string{"pylsp"}, Language: "python"}},
		".ts":  {{Command: []string{"typescript-language-server", "--stdio"}, Language: "typescript"}},
		".tsx": {{Command: []string{"typescript-language-server", "--stdio"}, Language: "typescriptreact"}},
		".js":  {{Command: []string{"typescript-language-server", "--stdio"}, Language: "javascript"}},
		".jsx": {{Command: []string{"typescript-language-server", "--stdio"}, Language: "javascriptreact"}},
		".c":   {{Command: []string{"clangd"}, Language: "c"}}, ".h": {{Command: []string{"clangd"}, Language: "c"}}, ".cpp": {{Command: []string{"clangd"}, Language: "cpp"}},
		".sh": {{Command: []string{"bash-language-server", "start"}, Language: "shellscript"}},
		".ml": {{Command: []string{"ocamllsp"}, Language: "ocaml"}}, ".mli": {{Command: []string{"ocamllsp"}, Language: "ocaml"}},
		".json": {{Command: []string{"vscode-json-languageserver", "--stdio"}, Language: "json"}},
	}
	servers := choices[ext]
	for _, s := range servers {
		if _, err := exec.LookPath(s.Command[0]); err == nil {
			return s, true
		}
	}
	if len(servers) > 0 {
		return servers[0], false
	}
	return config.Server{}, false
}
func Root(path string) string {
	start := filepath.Dir(path)
	dir := start
	for {
		for _, marker := range []string{".git", "go.mod", "Cargo.toml", "package.json", "pyproject.toml", "dune-project", "compile_commands.json"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}
