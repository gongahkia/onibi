.PHONY: dev-app dev-mobile test build clean

dev-app:
	cd app && pnpm tauri dev

dev-mobile:
	cd mobile && pnpm dev

test:
	cd app/src-tauri && cargo test && cd ../.. && cd mobile && pnpm test && cd ../app && pnpm test

build:
	cd app && pnpm tauri build
	cd mobile && pnpm build

clean:
	rm -rf app/dist app/src-tauri/target mobile/dist node_modules app/node_modules mobile/node_modules
