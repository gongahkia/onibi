FROM python:3.12.8-slim-bookworm

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ARG NODE_MAJOR=20
ARG TYPESCRIPT_VERSION=5.7.3
ARG JOERN_VERSION=2.0.411
ARG UV_VERSION=0.6.14
ARG PIRANESI_UID=10001
ARG PIRANESI_GID=10001

ENV DEBIAN_FRONTEND=noninteractive \
    PATH=/opt/joern/joern-cli:${PATH} \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_ROOT_USER_ACTION=ignore \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIRANESI_OUTPUT_OUTPUT_DIR=/workspace/piranesi-output \
    PIRANESI_TRACE_FILE_PATH=/workspace/.piranesi-trace.jsonl

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        gnupg \
        openjdk-17-jre-headless \
        unzip \
    && curl -fsSL "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && npm install --global "typescript@${TYPESCRIPT_VERSION}" \
    && curl -fsSL "https://github.com/joernio/joern/releases/download/v${JOERN_VERSION}/joern-install.sh" \
        | bash -s -- --install-dir=/opt/joern --without-plugins \
    && python -m pip install --no-cache-dir "uv==${UV_VERSION}" \
    && groupadd --gid "${PIRANESI_GID}" piranesi \
    && useradd --uid "${PIRANESI_UID}" --gid piranesi --create-home --shell /bin/bash piranesi \
    && mkdir -p /opt/piranesi /workspace \
    && chown -R piranesi:piranesi /opt/piranesi /workspace /home/piranesi \
    && rm -rf /var/lib/apt/lists/* /root/.cache /tmp/*

WORKDIR /opt/piranesi

COPY --chown=piranesi:piranesi LICENSE README.md pyproject.toml uv.lock ./
COPY --chown=piranesi:piranesi src ./src
COPY --chown=piranesi:piranesi rules ./rules

RUN uv pip install --system --no-cache --locked . \
    && python - <<'PY'
from pathlib import Path
import shutil
import sys

source = Path('/opt/piranesi/rules')
target = (
    Path(sys.base_prefix)
    / 'lib'
    / f'python{sys.version_info.major}.{sys.version_info.minor}'
    / 'rules'
)
if target.exists():
    shutil.rmtree(target)
shutil.copytree(source, target)
PY

USER piranesi
WORKDIR /workspace

ENTRYPOINT ["piranesi"]
CMD ["--help"]
