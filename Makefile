.PHONY: build install test vet tidy run clean

BINARY := onibi
NOTIFY_BINARY := onibi-notify
BUILD_DIR := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/gongahkia/onibi/internal/buildinfo.Version=$(VERSION) -X github.com/gongahkia/onibi/internal/buildinfo.Commit=$(COMMIT) -X github.com/gongahkia/onibi/internal/buildinfo.Date=$(DATE)

build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/onibi
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(NOTIFY_BINARY) ./clients/onibi-notify

install: build
	install -d $(HOME)/.local/bin
	install -m 0755 $(BUILD_DIR)/$(BINARY) $(HOME)/.local/bin/$(BINARY)
	install -m 0755 $(BUILD_DIR)/$(NOTIFY_BINARY) $(HOME)/.local/bin/$(NOTIFY_BINARY)

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

tidy:
	go mod tidy

run: build
	$(BUILD_DIR)/$(BINARY) start

clean:
	rm -rf $(BUILD_DIR)
