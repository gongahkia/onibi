.PHONY: component fixture check verify-fixture

component:
	../../scripts/zenbu-component-package.sh build "$(CURDIR)"

fixture: component
	base64 plugin.wasm | fold -w 76 > ../../test/fixtures/m9_conformance_component.wasm.b64
	$(MAKE) verify-fixture

verify-fixture:
	cd ../../test/fixtures && sha256sum --check m9_conformance_component.wasm.b64.sha256

check:
	../../scripts/zenbu-component-package.sh check "$(CURDIR)"
