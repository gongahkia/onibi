.PHONY: build install uninstall setup doctor dogfood test unit e2e-live vet tidy run clean

BINARY := onibi
NOTIFY_BINARY := onibi-notify
BUILD_DIR := bin
INSTALL_DIR ?= $(HOME)/.local/bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/gongahkia/onibi/internal/buildinfo.Version=$(VERSION) -X github.com/gongahkia/onibi/internal/buildinfo.Commit=$(COMMIT) -X github.com/gongahkia/onibi/internal/buildinfo.Date=$(DATE)

build:
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/onibi
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(NOTIFY_BINARY) ./clients/onibi-notify

install: build
	install -d $(INSTALL_DIR)
	install -m 0755 $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	install -m 0755 $(BUILD_DIR)/$(NOTIFY_BINARY) $(INSTALL_DIR)/$(NOTIFY_BINARY)

# First-time setup stores and validates a BotFather token. Use a dedicated
# dogfooding bot rather than one that receives sensitive production output.
setup: build
	@test -n "$$ONIBI_TELEGRAM_TOKEN" || (echo "Set ONIBI_TELEGRAM_TOKEN to a BotFather token."; exit 1)
	$(BUILD_DIR)/$(BINARY) telegram setup --token "$$ONIBI_TELEGRAM_TOKEN"
	$(BUILD_DIR)/$(BINARY) doctor

doctor: build
	$(BUILD_DIR)/$(BINARY) doctor

# Runs the readiness checks then starts a foreground daemon. Stop it with Ctrl-C.
dogfood: build
	$(BUILD_DIR)/$(BINARY) doctor
	$(BUILD_DIR)/$(BINARY) start

# Stops the managed user service and removes installed binaries. It preserves
# local state and credentials; run `onibi telegram disable` separately to revoke
# the bot token and pairing.
uninstall:
	@if test -x "$(INSTALL_DIR)/$(BINARY)"; then $(INSTALL_DIR)/$(BINARY) system service remove; fi
	rm -f $(INSTALL_DIR)/$(BINARY) $(INSTALL_DIR)/$(NOTIFY_BINARY)

test:
	$(MAKE) unit
	$(MAKE) e2e-live

unit:
	go test -race -count=1 ./...

e2e-live:
	go run ./cmd/onibi-e2e-live

vet:
	go vet ./...

tidy:
	go mod tidy

run: build
	$(BUILD_DIR)/$(BINARY) start

clean:
	rm -rf $(BUILD_DIR)
