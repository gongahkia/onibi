.PHONY: build test fmt check demo extension-docs wasm-runtime

WASMTIME_C_API_DIR ?= $(CURDIR)/.zenbu/wasmtime-v47.0.3-x86_64-linux-c-api
export ZENBU_WASMTIME_C_API_DIR := $(WASMTIME_C_API_DIR)

wasm-runtime:
	./scripts/fetch_wasmtime_c_api.sh

build: wasm-runtime
	dune build

test: wasm-runtime
	dune runtest

fmt:
	dune fmt

check: wasm-runtime
	dune build @fmt
	dune build
	dune runtest

demo: wasm-runtime
	dune exec bin/zenbu_headless.exe -- demo

extension-docs: wasm-runtime
	dune exec bin/zenbu_headless.exe -- extension-api > docs/generated/EXTENSION_API.md
	dune exec bin/zenbu_headless.exe -- extension-sdk > sdk/lua/zenbu.lua
