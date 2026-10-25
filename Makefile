.PHONY: build test check release
VERSION ?= 0.1.0-dev
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o build/shack ./cmd/shack
test:
	go test ./...
check:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	go test -race ./...
release:
	./scripts/release.sh $(VERSION)
