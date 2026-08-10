.PHONY: build test fmt check demo extension-docs wasm-runtime wasm-runtime-ready

LOCAL_OPAM_BIN := $(CURDIR)/_opam/bin
ifneq ($(wildcard $(LOCAL_OPAM_BIN)/dune),)
OPAM_ENV := eval "$$(opam env --switch=$(CURDIR) --set-switch)";
endif

WASMTIME_C_API_DIR ?= $(CURDIR)/.zenbu/wasmtime-v47.0.3-x86_64-linux-c-api
export ZENBU_WASMTIME_C_API_DIR := $(WASMTIME_C_API_DIR)
export ZENBU_WASMTIME_C_API_INCLUDE_DIR := $(WASMTIME_C_API_DIR)/include
export ZENBU_WASMTIME_C_API_LIB_DIR := $(WASMTIME_C_API_DIR)/lib
export ZENBU_WASMTIME_C_API_RPATH := $(WASMTIME_C_API_DIR)/lib

wasm-runtime:
	./scripts/fetch_wasmtime_c_api.sh

wasm-runtime-ready:
	@test -f "$(WASMTIME_C_API_DIR)/include/wasmtime.h" \
		&& test -f "$(WASMTIME_C_API_DIR)/lib/libwasmtime.so" \
		|| { \
			echo "missing pinned Wasmtime C API at $(WASMTIME_C_API_DIR); run make wasm-runtime first" >&2; \
			exit 2; \
		}

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

extension-docs: wasm-runtime-ready
	$(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-api > docs/generated/EXTENSION_API.md
	$(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-sdk > sdk/lua/zenbu.lua
	$(OPAM_ENV) dune exec bin/zenbu_headless.exe -- extension-wit > docs/wit/zenbu-plugin.wit
