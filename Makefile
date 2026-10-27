.PHONY: help build install update upgrade uninstall install-status setup doctor dogfood test unit test-browser vet tidy run clean

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

doctor: build
	$(Q)$(BUILD_DIR)/$(BINARY) web doctor

# Removes only manifest-owned binaries and their login services. State stays.
uninstall:
	@sh scripts/onibi-dev.sh uninstall $(LIFECYCLE_FLAGS)

test: unit

test-browser:
	$(Q)npm test --prefix web-tests

unit:
	$(Q)go test -race -count=1 ./...

vet:
	$(Q)go vet ./...

tidy:
	$(Q)go mod tidy

run: build
	$(Q)$(BUILD_DIR)/$(BINARY) web --no-banner

setup: doctor
	@printf '%s\n' 'Start locally: ./bin/onibi web' 'Private phone access: see docs/web.md for Tailscale HTTPS and pairing.'

dogfood: doctor
	$(Q)$(BUILD_DIR)/$(BINARY) web

clean:
	$(Q)rm -rf $(BUILD_DIR)

help:
	@sh scripts/onibi-dev.sh help
