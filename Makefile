.PHONY: bootstrap build test fmt check demo benchmark extension-docs wasm-runtime wasm-runtime-ready lua-runtime-ready install release release-archive release-check

LOCAL_OPAM_BIN := $(CURDIR)/_opam/bin
ifneq ($(wildcard $(LOCAL_OPAM_BIN)/dune),)
OPAM_ENV := eval "$$(opam env --switch=$(CURDIR) --set-switch)";
endif

PLATFORM_ENV := eval "$$(./scripts/zenbu-env.sh)";
OCAML_COMPILER ?= ocaml-base-compiler.5.3.0
BOOTSTRAP_TMP ?= $(CURDIR)/.zenbu/tmp
RELEASE_ARTIFACT_DIR ?= $(CURDIR)/dist

wasm-runtime:
	./scripts/fetch_wasmtime_c_api.sh

wasm-runtime-ready:
	@$(PLATFORM_ENV) test -f "$$ZENBU_WASMTIME_C_API_INCLUDE_DIR/wasmtime.h" \
		&& test -f "$$ZENBU_WASMTIME_C_API_LIB_DIR/$$ZENBU_WASMTIME_C_API_LIBRARY" \
		|| { \
			echo "missing pinned Wasmtime C API at $$ZENBU_WASMTIME_C_API_DIR; run make wasm-runtime first" >&2; \
			exit 2; \
		}

lua-runtime-ready:
	@$(PLATFORM_ENV) if test "$$(uname -s)-$$(uname -m)" = Darwin-arm64; then \
		test -n "$$ZENBU_LUA_LIBRARY" && test -f "$$ZENBU_LUA_LIBRARY" \
			|| { echo "macOS requires Homebrew Lua 5.4; run: brew install lua@5.4" >&2; exit 2; }; \
	fi

bootstrap:
	@command -v opam >/dev/null 2>&1 || { echo "opam is required; install it before bootstrapping" >&2; exit 2; }
	@$(MAKE) lua-runtime-ready
	@if test ! -d "$(CURDIR)/_opam"; then opam switch create "$(CURDIR)" "$(OCAML_COMPILER)" --no-install; fi
	@mkdir -p "$(BOOTSTRAP_TMP)"
	@export TMPDIR="$(BOOTSTRAP_TMP)"; eval "$$(opam env --switch=$(CURDIR) --set-switch)"; opam install . --deps-only --with-test -y
	@$(MAKE) wasm-runtime
	@$(MAKE) check

build: wasm-runtime-ready
	$(PLATFORM_ENV) $(OPAM_ENV) dune build

test: wasm-runtime-ready lua-runtime-ready
	$(PLATFORM_ENV) $(OPAM_ENV) dune runtest

fmt:
	$(PLATFORM_ENV) $(OPAM_ENV) dune fmt

check: wasm-runtime-ready lua-runtime-ready
	$(PLATFORM_ENV) $(OPAM_ENV) dune build @fmt
	$(PLATFORM_ENV) $(OPAM_ENV) dune build
	$(PLATFORM_ENV) $(OPAM_ENV) dune runtest

demo: wasm-runtime-ready lua-runtime-ready
	$(PLATFORM_ENV) $(OPAM_ENV) dune exec bin/zenbu_headless.exe -- demo

benchmark: wasm-runtime-ready lua-runtime-ready
	$(PLATFORM_ENV) $(OPAM_ENV) dune exec bin/zenbu_headless.exe -- benchmark
	$(PLATFORM_ENV) $(OPAM_ENV) dune exec test/m11_language_benchmark.exe

extension-docs: wasm-runtime-ready lua-runtime-ready
	$(PLATFORM_ENV) $(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-api > docs/generated/EXTENSION_API.md
	$(PLATFORM_ENV) $(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-sdk > sdk/lua/zenbu.lua
	$(PLATFORM_ENV) $(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-wit > docs/wit/zenbu-plugin.wit

install: wasm-runtime-ready lua-runtime-ready
	$(PLATFORM_ENV) $(OPAM_ENV) dune install zenbu
	@$(PLATFORM_ENV) $(OPAM_ENV) prefix="$$(opam var prefix)"; \
	install_dir="$$prefix/lib/zenbu"; \
	mkdir -p "$$install_dir"; \
	install -m 755 "$$ZENBU_WASMTIME_C_API_LIB_DIR/$$ZENBU_WASMTIME_C_API_LIBRARY" "$$install_dir/$$ZENBU_WASMTIME_C_API_LIBRARY"; \
	echo "installed Zenbu to $$prefix/bin with Wasmtime at $$install_dir/$$ZENBU_WASMTIME_C_API_LIBRARY"

release: wasm-runtime-ready release-check
	$(PLATFORM_ENV) $(OPAM_ENV) dune build --build-dir "$(CURDIR)/.zenbu/release-build" --profile release bin/zenbu.exe bin/zenbu_headless.exe
	@echo "release-profile artifacts: .zenbu/release-build/default/bin/zenbu.exe and .zenbu/release-build/default/bin/zenbu_headless.exe"
	@echo "they require the platform Wasmtime library beside the installed prefix at ../lib/zenbu; make install supplies it"

release-archive: release lua-runtime-ready
	./scripts/package_release_archive.sh "$(CURDIR)/.zenbu/release-build" "$(RELEASE_ARTIFACT_DIR)"

release-check: check
	@tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	$(PLATFORM_ENV) $(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-api > "$$tmp/EXTENSION_API.md"; \
	$(PLATFORM_ENV) $(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-sdk > "$$tmp/zenbu.lua"; \
	$(PLATFORM_ENV) $(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-wit > "$$tmp/zenbu-plugin.wit"; \
	cmp -s "$$tmp/EXTENSION_API.md" docs/generated/EXTENSION_API.md \
		&& cmp -s "$$tmp/zenbu.lua" sdk/lua/zenbu.lua \
		&& cmp -s "$$tmp/zenbu-plugin.wit" docs/wit/zenbu-plugin.wit \
		|| { echo "generated extension contract artifacts are stale; run make extension-docs" >&2; exit 1; }
