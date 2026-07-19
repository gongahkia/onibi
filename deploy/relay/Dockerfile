FROM rust:1.93.0-bookworm AS build

WORKDIR /workspace
COPY . .
RUN cargo build --release --locked --package yeokcham-relay --bin yeokcham-relay \
    && install -d -o 65532 -g 65532 /var/lib/yeokcham

FROM gcr.io/distroless/cc-debian12:nonroot

COPY --from=build --chown=65532:65532 /workspace/target/release/yeokcham-relay /usr/local/bin/yeokcham-relay
COPY --from=build --chown=65532:65532 /var/lib/yeokcham /var/lib/yeokcham

USER 65532:65532
WORKDIR /var/lib/yeokcham
VOLUME ["/var/lib/yeokcham"]
EXPOSE 50051
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/usr/local/bin/yeokcham-relay", "healthcheck", "--address", "127.0.0.1:8080"]
ENTRYPOINT ["/usr/local/bin/yeokcham-relay"]
CMD ["run", "--config", "/etc/yeokcham/relay.conf", "--identity", "/run/secrets/yeokcham-relay-identity"]
