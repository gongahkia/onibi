FROM eclipse-temurin:17-jre-jammy

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

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
        gnupg \
        software-properties-common \
        unzip \
    && add-apt-repository ppa:deadsnakes/ppa \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        python3.12 \
        python3.12-venv \
    && curl -fsSL https://bootstrap.pypa.io/get-pip.py -o /tmp/get-pip.py \
    && python3.12 /tmp/get-pip.py \
    && rm -f /tmp/get-pip.py \
    && curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && npm install --global typescript \
    && ln -sf /usr/bin/python3.12 /usr/local/bin/python \
    && ln -sf /usr/bin/python3.12 /usr/local/bin/python3 \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://github.com/joernio/joern/releases/latest/download/joern-install.sh \
        | bash -s -- --install-dir=/opt/joern --without-plugins \
    && rm -f /joern-cli.zip

WORKDIR /opt/piranesi

COPY LICENSE README.md pyproject.toml piranesi.toml ./
COPY rules ./rules
COPY src ./src

RUN python3.12 -m pip install --no-cache-dir z3-solver==4.13.0.0 \
    && python3.12 -m pip install --no-cache-dir . \
    && python3.12 - <<'PY'
from pathlib import Path
import shutil
import sys

source = Path("/opt/piranesi/rules")
target = Path(sys.base_prefix) / "lib" / f"python{sys.version_info.major}.{sys.version_info.minor}" / "rules"
if target.exists():
    shutil.rmtree(target)
shutil.copytree(source, target)
PY

ENTRYPOINT ["piranesi"]
CMD ["--help"]
