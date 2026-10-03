.PHONY: build install uninstall setup doctor dogfood test unit e2e-live vet tidy run clean

BINARY := onibi
NOTIFY_BINARY := onibi-notify
BUILD_DIR := bin
INSTALL_DIR ?= $(HOME)/.local/bin
# Keep normal install output readable. Use VERBOSE=1 to print every command.
VERBOSE ?= 0
ifeq ($(VERBOSE),1)
Q :=
else
Q := @
endif
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/gongahkia/onibi/internal/buildinfo.Version=$(VERSION) -X github.com/gongahkia/onibi/internal/buildinfo.Commit=$(COMMIT) -X github.com/gongahkia/onibi/internal/buildinfo.Date=$(DATE)

build:
	$(Q)printf '%s\n' '==> Building Onibi'
	$(Q)mkdir -p $(BUILD_DIR)
	$(Q)go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/onibi
	$(Q)go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(NOTIFY_BINARY) ./clients/onibi-notify
	$(Q)printf '%s\n' '    Built bin/onibi and bin/onibi-notify'

install: build
	$(Q)printf '%s\n' '==> Installing Onibi'
	$(Q)install -d $(INSTALL_DIR)
	$(Q)install -m 0755 $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	$(Q)install -m 0755 $(BUILD_DIR)/$(NOTIFY_BINARY) $(INSTALL_DIR)/$(NOTIFY_BINARY)
	$(Q)printf '%s\n' '    Installed to $(INSTALL_DIR)'

# First-time setup stores and validates a BotFather token. It prompts with
# hidden input when ONIBI_TELEGRAM_TOKEN is not already set, so CI can still
# provide the token non-interactively. Use a dedicated dogfooding bot.
setup: build
	$(Q)printf '%s\n' '==> Setting up your Telegram bot'
	$(Q)onibi_setup_token="$$ONIBI_TELEGRAM_TOKEN"; \
	if test -z "$$onibi_setup_token"; then \
		if test ! -t 0; then echo "Set ONIBI_TELEGRAM_TOKEN when setup is non-interactive." >&2; exit 1; fi; \
		printf "BotFather token: " >&2; \
		stty -echo; trap 'stty echo; printf "\\n" >&2; exit 130' HUP INT TERM; \
		IFS= read -r onibi_setup_token; onibi_setup_read_status=$$?; \
		trap - HUP INT TERM; stty echo; printf "\\n" >&2; \
		test "$$onibi_setup_read_status" -eq 0 || exit "$$onibi_setup_read_status"; \
	fi; \
	if test -z "$$onibi_setup_token"; then echo "A BotFather token is required." >&2; exit 1; fi; \
	$(BUILD_DIR)/$(BINARY) telegram setup --token "$$onibi_setup_token"
	$(Q)printf '%s\n' '==> Checking readiness'
	$(Q)$(BUILD_DIR)/$(BINARY) doctor

doctor: build
	$(Q)$(BUILD_DIR)/$(BINARY) doctor

# Runs the readiness checks then starts a foreground daemon. Stop it with Ctrl-C.
dogfood: build
	$(Q)$(BUILD_DIR)/$(BINARY) doctor
	$(Q)printf '%s\n' '==> Starting Onibi in the foreground (Ctrl-C to stop)'
	$(Q)$(BUILD_DIR)/$(BINARY) start

# Stops the managed user service and removes installed binaries. It preserves
# local state and credentials; run `onibi telegram disable` separately to revoke
# the bot token and pairing.
uninstall:
	$(Q)printf '%s\n' '==> Removing the Onibi service and binaries'
	$(Q)if test -x "$(INSTALL_DIR)/$(BINARY)"; then $(INSTALL_DIR)/$(BINARY) system service remove; fi
	$(Q)rm -f $(INSTALL_DIR)/$(BINARY) $(INSTALL_DIR)/$(NOTIFY_BINARY)
	$(Q)printf '%s\n' '    Binaries removed; local state and credentials were preserved'

test:
	$(Q)$(MAKE) unit
	$(Q)$(MAKE) e2e-live

unit:
	$(Q)go test -race -count=1 ./...

e2e-live:
	$(Q)go run ./cmd/onibi-e2e-live

vet:
	$(Q)go vet ./...

tidy:
	$(Q)go mod tidy

run: build
	$(Q)printf '%s\n' '==> Starting Onibi in the foreground (Ctrl-C to stop)'
	$(Q)$(BUILD_DIR)/$(BINARY) start

clean:
	$(Q)rm -rf $(BUILD_DIR)
