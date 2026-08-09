.PHONY: build test fmt check demo

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

