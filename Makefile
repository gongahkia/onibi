.PHONY: build test fmt check demo extension-docs

build:
	dune build

test:
	dune runtest

fmt:
	dune fmt

check:
	dune build @fmt
	dune build
	dune runtest

demo:
	dune exec bin/zenbu_headless.exe -- demo

extension-docs:
	dune exec bin/zenbu_headless.exe -- extension-api > docs/generated/EXTENSION_API.md
	dune exec bin/zenbu_headless.exe -- extension-sdk > sdk/lua/zenbu.lua
