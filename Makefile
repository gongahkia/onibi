.PHONY: build install update upgrade uninstall install-status setup doctor dogfood test unit e2e-live vet tidy run clean

BINARY := onibi
NOTIFY_BINARY := onibi-notify
BUILD_DIR := bin
INSTALL_DIR ?= $(HOME)/.local/bin
# Keep normal install output readable. Use VERBOSE=1 to print every command.
VERBOSE ?= 0
ifeq ($(VERBOSE),1)
Q :=
RUN = $(2)
else
Q := @
RUN = sh scripts/onibi-loader.sh "$(1)" $(2)
endif
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/gongahkia/onibi/internal/buildinfo.Version=$(VERSION) -X github.com/gongahkia/onibi/internal/buildinfo.Commit=$(COMMIT) -X github.com/gongahkia/onibi/internal/buildinfo.Date=$(DATE)

build:
	@sh scripts/onibi-dev.sh build $(if $(filter 1,$(VERBOSE)),--verbose,)

# Source lifecycle controls; previews never fetch, build, stop, or remove files.
DRY_RUN ?= 0
UPDATE_PULL ?= 0
RESTART_WEB ?= 0
LIFECYCLE_FLAGS = --prefix "$(INSTALL_DIR)" $(if $(filter 1,$(DRY_RUN)),--dry-run,) $(if $(filter 1,$(VERBOSE)),--verbose,)

install:
	@sh scripts/onibi-dev.sh install $(LIFECYCLE_FLAGS)

update:
	@sh scripts/onibi-dev.sh update $(LIFECYCLE_FLAGS) $(if $(filter 1,$(UPDATE_PULL)),--pull,) $(if $(filter 1,$(RESTART_WEB)),--restart-web,)

upgrade: update

install-status:
	@sh scripts/onibi-dev.sh status --prefix "$(INSTALL_DIR)"

# First-time setup stores and validates a BotFather token. It prompts with
# hidden input when ONIBI_TELEGRAM_TOKEN is not already set, so CI can still
# provide the token non-interactively. Use a dedicated dogfooding bot.
setup: build
	$(Q)printf '%s\n' '==> 👻 Setting up your Telegram bot'
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
	ONIBI_TELEGRAM_TOKEN="$$onibi_setup_token" $(BUILD_DIR)/$(BINARY) telegram setup
	$(Q)printf '%s\n' '==> 👻 Checking readiness'
	$(Q)$(BUILD_DIR)/$(BINARY) doctor

doctor: build
	$(Q)$(BUILD_DIR)/$(BINARY) doctor

# Runs the readiness checks then starts a foreground daemon. Stop it with Ctrl-C.
dogfood: build
	$(Q)$(BUILD_DIR)/$(BINARY) doctor
	$(Q)printf '%s\n' '==> 👻 Starting Onibi in the foreground (Ctrl-C to stop)'
	$(Q)$(BUILD_DIR)/$(BINARY) start

# Removes only manifest-owned binaries and their login services. State stays.
uninstall:
	@sh scripts/onibi-dev.sh uninstall $(LIFECYCLE_FLAGS)

test:
	$(Q)$(MAKE) unit
	$(Q)$(MAKE) e2e-live

unit:
	$(Q)$(call RUN,Running unit tests,go test -race -count=1 ./...)

e2e-live:
	$(Q)$(call RUN,Running live Telegram E2E test,go run ./cmd/onibi-e2e-live)

vet:
	$(Q)$(call RUN,Running go vet,go vet ./...)

tidy:
	$(Q)$(call RUN,Tidying Go modules,go mod tidy)

run: build
	$(Q)printf '%s\n' '==> 👻 Starting Onibi in the foreground (Ctrl-C to stop)'
	$(Q)$(BUILD_DIR)/$(BINARY) start

clean:
	$(Q)rm -rf $(BUILD_DIR)
