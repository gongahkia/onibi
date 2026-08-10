.PHONY: component check

component:
	cargo component build --release --target wasm32-unknown-unknown
	cp target/wasm32-unknown-unknown/release/zenbu_component_hello.wasm plugin.wasm

check: component
	dune exec --root ../.. bin/zenbu_headless.exe -- plugin-check "$(CURDIR)"
