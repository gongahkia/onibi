.PHONY: build frontend-install frontend-build frontend-size-check install test vet staticcheck tidy run clean gen-readme gen-readme-check fresh-machine-doc-check release-e2e-gate release-dry release-smoke reproducible-build bench-tolerance install-pages

BINARY := onibi
NOTIFY_BINARY := onibi-notify
BUILD_DIR := bin
INSTALL_PAGES_DIR ?= dist/get-onibi-pages
FRONTEND_JS_GZ_LIMIT := 256000
VERSION ?= $(shell git describe --tags --match 'v[0-9]*.[0-9]*.[0-9]*' --dirty 2>/dev/null || echo v2-dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/gongahkia/onibi/internal/buildinfo.Version=$(VERSION) -X github.com/gongahkia/onibi/internal/buildinfo.Commit=$(COMMIT) -X github.com/gongahkia/onibi/internal/buildinfo.Date=$(DATE)

build: frontend-size-check
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/onibi
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(NOTIFY_BINARY) ./clients/onibi-notify
	$(MAKE) release-e2e-gate

# frontend dist is embedded by Go, so compile it before binaries.
frontend-install:
	npm --prefix frontend install

frontend-build: frontend-install
	npm --prefix frontend run build

frontend-size-check: frontend-build
	@bytes=$$(node -e 'const fs=require("fs"),zlib=require("zlib"),path=require("path");const root="internal/web/static/dist";const manifest=JSON.parse(fs.readFileSync(path.join(root,".vite/manifest.json"),"utf8"));const seen=new Set();function add(key){const entry=manifest[key];if(!entry)return;if(entry.file&&entry.file.endsWith(".js"))seen.add(entry.file);for(const imp of entry.imports||[])add(imp)}for(const [key,entry] of Object.entries(manifest))if(entry.isEntry)add(key);let bytes=0;for(const file of seen)bytes+=zlib.gzipSync(fs.readFileSync(path.join(root,file))).length;process.stdout.write(String(bytes))'); \
	if [ "$$bytes" -gt "$(FRONTEND_JS_GZ_LIMIT)" ]; then \
		echo "frontend js gzip bytes $$bytes exceeds $(FRONTEND_JS_GZ_LIMIT)"; \
		exit 1; \
	fi; \
	echo "frontend entry js gzip bytes: $$bytes"

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

gen-readme:
	go run ./cmd/gen-readme

gen-readme-check:
	go run ./cmd/gen-readme --check

fresh-machine-doc-check:
	scripts/fresh-machine-doc-check.sh

release-e2e-gate:
	@tag=$$(git describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null || true); \
	ONIBI_RELEASE_TAG="$$tag" scripts/release-e2e-gate.sh "$(BUILD_DIR)/$(BINARY)"; \
	ONIBI_RELEASE_TAG="$$tag" scripts/release-e2e-gate.sh "$(BUILD_DIR)/$(NOTIFY_BINARY)"

release-dry:
	@command -v goreleaser >/dev/null 2>&1 || (echo "goreleaser not installed: brew install goreleaser" && exit 1)
	goreleaser release --snapshot --clean --skip=publish --skip=sign

release-smoke:
	@command -v goreleaser >/dev/null 2>&1 || (echo "goreleaser not installed: brew install goreleaser" && exit 1)
	goreleaser release --snapshot --clean --skip=publish --skip=sign
	scripts/release-smoke.sh dist

reproducible-build:
	scripts/reproducible-build.sh

bench-tolerance:
	scripts/bench-tolerance.sh

install-pages:
	scripts/prepare-install-pages.sh "$(INSTALL_PAGES_DIR)"
