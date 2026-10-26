// Package languages contains isolated workspace fixtures for optional live tests.
package languages

type Case struct {
	Name, Env, Program, Language, Path, Source, Bad string
	Args                                            []string
	Files                                           map[string]string
	Format                                          bool
}

func Cases() []Case {
	return []Case{{"go", "SHACK_GOPLS", "gopls", "go", "sample.go", "package sample\nfunc greet(name string) string {return name}\nfunc use() { _ = \"😀\"; value:=greet(\"界\"); _ = value }\n", "var broken int = \"bad\"\n", nil, map[string]string{"go.mod": "module example.invalid/shacklive\n\ngo 1.24\n"}, true},
		{"rust", "SHACK_RUST_ANALYZER", "rust-analyzer", "rust", "lib.rs", "pub fn greet(name:&str)->&str {name}\npub fn use_it(){ let _ = \"😀\"; let value=greet(\"界\"); println!(\"{}\",value); }\n", "pub fn broken( {\n", nil, map[string]string{"Cargo.toml": "[package]\nname = \"shack_live\"\nversion = \"0.1.0\"\nedition = \"2024\"\n[lib]\npath = \"lib.rs\"\n"}, true},
		{"pyright", "SHACK_PYRIGHT", "pyright-langserver", "python", "sample.py", "def greet(name: str) -> str:\n return name\n\ndef use_it():\n value = \"😀\"; value = greet(\"界\"); return value\n", "broken = missing_symbol\n", []string{"--stdio"}, map[string]string{"pyproject.toml": "[project]\nname = \"shack-live\"\nversion = \"0.1.0\"\n"}, false},
		{"pylsp", "SHACK_PYLSP", "pylsp", "python", "sample.py", "def greet(name: str) -> str:\n return name\n\ndef use_it():\n value = \"😀\"; value = greet(\"界\"); return value\n", "broken = missing_symbol\n", nil, map[string]string{"pyproject.toml": "[project]\nname = \"shack-live\"\nversion = \"0.1.0\"\n"}, true},
		{"typescript", "SHACK_TYPESCRIPT_LS", "typescript-language-server", "typescript", "sample.ts", "function greet(name: string): string{return name;}\nexport function useIt(){const icon=\"😀\";const value=greet(icon);return value;}\n", "const broken: number = \"bad\";\n", []string{"--stdio"}, map[string]string{"package.json": "{\"name\":\"shack-live\",\"private\":true}", "tsconfig.json": "{\"compilerOptions\":{\"strict\":true}}"}, true},
	}
}
