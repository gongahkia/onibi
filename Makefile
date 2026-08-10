.PHONY: bootstrap build test fmt check demo benchmark extension-docs wasm-runtime wasm-runtime-ready install release release-check

LOCAL_OPAM_BIN := $(CURDIR)/_opam/bin
ifneq ($(wildcard $(LOCAL_OPAM_BIN)/dune),)
OPAM_ENV := eval "$$(opam env --switch=$(CURDIR) --set-switch)";
endif

WASMTIME_C_API_DIR ?= $(CURDIR)/.zenbu/wasmtime-v47.0.3-x86_64-linux-c-api
OCAML_COMPILER ?= ocaml-system
BOOTSTRAP_TMP ?= $(CURDIR)/.zenbu/tmp
ZENBU_WASMTIME_C_API_RPATH ?= $$ORIGIN/../../../.zenbu/wasmtime-v47.0.3-x86_64-linux-c-api/lib:$$ORIGIN/../lib/zenbu
export ZENBU_WASMTIME_C_API_DIR := $(WASMTIME_C_API_DIR)
export ZENBU_WASMTIME_C_API_INCLUDE_DIR := $(WASMTIME_C_API_DIR)/include
export ZENBU_WASMTIME_C_API_LIB_DIR := $(WASMTIME_C_API_DIR)/lib
export ZENBU_WASMTIME_C_API_RPATH

wasm-runtime:
	./scripts/fetch_wasmtime_c_api.sh

wasm-runtime-ready:
	@test -f "$(WASMTIME_C_API_DIR)/include/wasmtime.h" \
		&& test -f "$(WASMTIME_C_API_DIR)/lib/libwasmtime.so" \
		|| { \
			echo "missing pinned Wasmtime C API at $(WASMTIME_C_API_DIR); run make wasm-runtime first" >&2; \
			exit 2; \
		}

bootstrap:
	@command -v opam >/dev/null 2>&1 || { echo "opam is required; install Fedora's opam package first" >&2; exit 2; }
	@if test ! -d "$(CURDIR)/_opam"; then opam switch create "$(CURDIR)" "$(OCAML_COMPILER)" --no-install; fi
	@mkdir -p "$(BOOTSTRAP_TMP)"
	@export TMPDIR="$(BOOTSTRAP_TMP)"; eval "$$(opam env --switch=$(CURDIR) --set-switch)"; opam install . --deps-only --with-test -y
	@$(MAKE) wasm-runtime
	@$(MAKE) check

build: wasm-runtime-ready
	$(OPAM_ENV) dune build

test: wasm-runtime-ready
	$(OPAM_ENV) dune runtest

fmt:
	$(OPAM_ENV) dune fmt

check: wasm-runtime-ready
	$(OPAM_ENV) dune build @fmt
	$(OPAM_ENV) dune build
	$(OPAM_ENV) dune runtest

demo: wasm-runtime-ready
	$(OPAM_ENV) dune exec bin/zenbu_headless.exe -- demo

benchmark: wasm-runtime-ready
	$(OPAM_ENV) dune exec bin/zenbu_headless.exe -- benchmark

extension-docs: wasm-runtime-ready
	$(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-api > docs/generated/EXTENSION_API.md
	$(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-sdk > sdk/lua/zenbu.lua
	$(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-wit > docs/wit/zenbu-plugin.wit

install: wasm-runtime-ready
	$(OPAM_ENV) dune install zenbu
	@$(OPAM_ENV) prefix="$$(opam var prefix)"; \
	install_dir="$$prefix/lib/zenbu"; \
	mkdir -p "$$install_dir"; \
	install -m 755 "$(WASMTIME_C_API_DIR)/lib/libwasmtime.so" "$$install_dir/libwasmtime.so"; \
	echo "installed Zenbu to $$prefix/bin with Wasmtime at $$install_dir/libwasmtime.so"

release: wasm-runtime-ready release-check
	$(OPAM_ENV) dune build --profile release bin/zenbu.exe bin/zenbu_headless.exe
	@echo "release-profile artifacts: _build/release/bin/zenbu.exe and _build/release/bin/zenbu_headless.exe"
	@echo "they require libwasmtime.so beside the installed prefix at ../lib/zenbu; make install supplies it"

release-check: check
	@tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	$(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-api > "$$tmp/EXTENSION_API.md"; \
	$(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-sdk > "$$tmp/zenbu.lua"; \
	$(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-wit > "$$tmp/zenbu-plugin.wit"; \
	cmp -s "$$tmp/EXTENSION_API.md" docs/generated/EXTENSION_API.md \
		&& cmp -s "$$tmp/zenbu.lua" sdk/lua/zenbu.lua \
		&& cmp -s "$$tmp/zenbu-plugin.wit" docs/wit/zenbu-plugin.wit \
		|| { echo "generated extension contract artifacts are stale; run make extension-docs" >&2; exit 1; }
