.PHONY: build test web-sync check-web-sync smoke-mobile-access install-hooks install-hooks-remote uninstall-hooks

build:
	swift build

test:
	swift test
	npm --prefix OnibiWeb test

web-sync:
	./scripts/sync_web_assets.sh

check-web-sync:
	./scripts/check_web_assets_synced.sh

smoke-mobile-access:
	./scripts/smoke_mobile_access.sh

install-hooks:
	./scripts/install_shell_hooks.sh --shell zsh

install-hooks-remote:
	./scripts/install_shell_hooks.sh --shell zsh --remote-control

uninstall-hooks:
	./scripts/install_shell_hooks.sh --shell zsh --uninstall
