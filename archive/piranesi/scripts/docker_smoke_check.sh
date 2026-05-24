#!/usr/bin/env bash
set -euo pipefail

IMAGE_TAG="${1:-piranesi:local}"

printf 'Building Docker image %s\n' "${IMAGE_TAG}"
docker build -t "${IMAGE_TAG}" .

printf 'Running smoke checks\n'
docker run --rm "${IMAGE_TAG}" --version
docker run --rm "${IMAGE_TAG}" --help >/dev/null

printf 'Docker smoke checks passed for %s\n' "${IMAGE_TAG}"
