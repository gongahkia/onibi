.PHONY: build install test vet staticcheck tidy run clean release-dry release-smoke

BINARY := onibi
NOTIFY_BINARY := onibi-notify
BUILD_DIR := bin
VERSION ?= $(shell git describe --tags --match 'v[0-9]*.[0-9]*.[0-9]*' --dirty 2>/dev/null || echo v2-dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/gongahkia/onibi/internal/buildinfo.Version=$(VERSION) -X github.com/gongahkia/onibi/internal/buildinfo.Commit=$(COMMIT) -X github.com/gongahkia/onibi/internal/buildinfo.Date=$(DATE)

build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/onibi
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(NOTIFY_BINARY) ./clients/onibi-notify

install: build
	install -m 0755 $(BUILD_DIR)/$(BINARY) $(HOME)/.local/bin/$(BINARY)
	install -m 0755 $(BUILD_DIR)/$(NOTIFY_BINARY) $(HOME)/.local/bin/$(NOTIFY_BINARY)

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

staticcheck:
	@command -v staticcheck >/dev/null 2>&1 || (echo "staticcheck not installed: go install honnef.co/go/tools/cmd/staticcheck@latest" && exit 1)
	staticcheck ./...

tidy:
	go mod tidy

run: build
	$(BUILD_DIR)/$(BINARY) run

clean:
	rm -rf $(BUILD_DIR)

release-dry:
	@command -v goreleaser >/dev/null 2>&1 || (echo "goreleaser not installed: brew install goreleaser" && exit 1)
	goreleaser release --snapshot --clean

release-smoke:
	@command -v goreleaser >/dev/null 2>&1 || (echo "goreleaser not installed: brew install goreleaser" && exit 1)
	goreleaser release --snapshot --clean
	scripts/release-smoke.sh dist
