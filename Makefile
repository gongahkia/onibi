.PHONY: build test check release
VERSION ?= $(shell cat VERSION)
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o build/shack ./cmd/shack
test:
	go test ./... github.com/coreequip/mermaidascii
check:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	go test -race ./... github.com/coreequip/mermaidascii
release:
	./scripts/release.sh $(VERSION)
