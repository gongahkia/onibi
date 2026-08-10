.PHONY: component fixture verify-fixture

component:
	cargo component build --release --target wasm32-unknown-unknown
	cp target/wasm32-unknown-unknown/release/zenbu_component_unauthorized_import.wasm plugin.wasm

fixture: component
	base64 -w 76 plugin.wasm > ../../test/fixtures/m9_unauthorized_import_component.wasm.b64
	$(MAKE) verify-fixture

verify-fixture:
	cd ../../test/fixtures && sha256sum --check m9_unauthorized_import_component.wasm.b64.sha256
