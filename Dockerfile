# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS modules
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY --from=modules /go/pkg /go/pkg
COPY . ./
RUN playwright_version="$(go list -m -f '{{.Version}}' github.com/mxschmitt/playwright-go)" \
    && go install github.com/mxschmitt/playwright-go/cmd/playwright@"${playwright_version}" \
    && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/kaypoh ./cmd/kaypoh

FROM node:24-bookworm-slim AS node-deps
WORKDIR /app
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts

FROM ubuntu:24.04 AS runtime
ARG DEBIAN_FRONTEND=noninteractive
ENV PLAYWRIGHT_DRIVER_PATH=/opt/playwright/driver \
    PLAYWRIGHT_BROWSERS_PATH=/opt/playwright/browsers \
    NODE_PATH=/opt/kaypoh/node_modules \
    TZ=Asia/Singapore \
    XDG_CONFIG_HOME=/etc \
    XDG_DATA_HOME=/var/lib

COPY --from=build /go/bin/playwright /usr/local/bin/playwright
COPY --from=node-deps /usr/local/bin/node /usr/local/bin/node
COPY --from=node-deps /app/node_modules /opt/kaypoh/node_modules

# The Playwright CLI downloads the driver, Chromium, and the Linux libraries
# required by browser-backed partner readers at image-build time.
RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates tzdata \
    && playwright install --with-deps chromium \
    && groupadd --gid 10001 kaypoh \
    && useradd --create-home --uid 10001 --gid kaypoh --shell /usr/sbin/nologin kaypoh \
    && install --directory --owner=kaypoh --group=kaypoh --mode=0750 /etc/kaypoh /var/lib/kaypoh \
    && rm -rf /var/lib/apt/lists/*

# Keep the large Chromium layer independent from application source changes.
COPY --from=build /out/kaypoh /usr/local/bin/kaypoh

COPY docker/entrypoint.sh /usr/local/bin/kaypoh-entrypoint
RUN chmod 0755 /usr/local/bin/kaypoh-entrypoint

WORKDIR /var/lib/kaypoh
VOLUME ["/var/lib/kaypoh"]
ENTRYPOINT ["/usr/local/bin/kaypoh-entrypoint"]
CMD ["daemon"]
