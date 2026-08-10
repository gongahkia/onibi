.PHONY: build test fmt check demo extension-docs wasm-runtime wasm-runtime-ready

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
	dune build

test: wasm-runtime-ready
	dune runtest

fmt:
	dune fmt

check: wasm-runtime-ready
	dune build @fmt
	dune build
	dune runtest

demo: wasm-runtime-ready
	dune exec bin/zenbu_headless.exe -- demo

extension-docs: wasm-runtime-ready
	dune exec bin/zenbu_headless.exe -- extension-api > docs/generated/EXTENSION_API.md
	dune exec bin/zenbu_headless.exe -- extension-sdk > sdk/lua/zenbu.lua
	dune exec bin/zenbu_headless.exe -- extension-wit > docs/wit/zenbu-plugin.wit
