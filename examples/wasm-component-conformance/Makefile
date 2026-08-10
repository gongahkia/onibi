.PHONY: component fixture check verify-fixture

component:
	cargo component build --release --target wasm32-unknown-unknown
	cp target/wasm32-unknown-unknown/release/zenbu_component_conformance.wasm plugin.wasm

fixture: component
	base64 -w 76 plugin.wasm > ../../test/fixtures/m9_conformance_component.wasm.b64
	$(MAKE) verify-fixture

verify-fixture:
	cd ../../test/fixtures && sha256sum --check m9_conformance_component.wasm.b64.sha256

check: component
	dune exec --root ../.. bin/zenbu_headless.exe -- plugin-check "$(CURDIR)"
