FROM python:3.12.12-slim-bookworm AS builder

WORKDIR /src
ENV PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1

RUN python -m pip install --no-cache-dir uv==0.8.13

COPY pyproject.toml README.md CHANGELOG.md LICENSE SECURITY.md ./
COPY src ./src
COPY rules ./rules
COPY examples/rule-packs ./examples/rule-packs

RUN uv build --wheel && \
    python -m venv /opt/piranesi && \
    /opt/piranesi/bin/pip install --no-cache-dir dist/*.whl

FROM python:3.12.12-slim-bookworm

LABEL org.opencontainers.image.source="https://github.com/gongahkia/piranesi" \
      org.opencontainers.image.description="Local-first evidence workbench for security review" \
      org.opencontainers.image.licenses="Apache-2.0"

ENV PATH="/opt/piranesi/bin:${PATH}" \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1

RUN useradd --create-home --shell /usr/sbin/nologin --uid 10001 piranesi

COPY --from=builder /opt/piranesi /opt/piranesi

WORKDIR /workspace
USER piranesi

ENTRYPOINT ["piranesi"]
CMD ["--help"]

