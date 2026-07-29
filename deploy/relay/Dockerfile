FROM rust:1.93.0-bookworm AS build

WORKDIR /workspace
COPY . .
RUN cargo build --release --locked --package arachne-relay --bin arachne-relay \
    && install -d -o 65532 -g 65532 /var/lib/arachne

FROM gcr.io/distroless/cc-debian12:nonroot

COPY --from=build --chown=65532:65532 /workspace/target/release/arachne-relay /usr/local/bin/arachne-relay
COPY --from=build --chown=65532:65532 /var/lib/arachne /var/lib/arachne

USER 65532:65532
WORKDIR /var/lib/arachne
VOLUME ["/var/lib/arachne"]
EXPOSE 50051
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD ["/usr/local/bin/arachne-relay", "healthcheck", "--address", "127.0.0.1:8080"]
ENTRYPOINT ["/usr/local/bin/arachne-relay"]
CMD ["run", "--config", "/etc/arachne/relay.conf", "--identity", "/run/secrets/arachne-relay-identity"]
