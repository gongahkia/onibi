from __future__ import annotations

import re
from dataclasses import dataclass

_SHA256_DIGEST_RE = re.compile(r"^sha256:[0-9a-f]{64}$")


class ImagePolicyError(ValueError):
    """Raised when a replay image reference violates rescan image policy."""


@dataclass(frozen=True, slots=True)
class AcceptedImage:
    reference: str
    repository: str
    digest: str
    tag: str | None = None

    def provenance(self) -> dict[str, str | None]:
        return {
            "image_reference": self.reference,
            "image_repository": self.repository,
            "image_tag": self.tag,
            "image_digest": self.digest,
        }


def validate_replay_image(reference: str) -> AcceptedImage:
    normalized = reference.strip()
    if not normalized:
        raise ImagePolicyError(_error("image reference cannot be empty"))
    repository_and_tag, separator, digest = normalized.partition("@")
    if not separator:
        raise ImagePolicyError(_error("image reference must be pinned with @sha256:<digest>"))
    if not _SHA256_DIGEST_RE.match(digest):
        raise ImagePolicyError(_error("image digest must be a sha256 digest"))
    repository, tag = _split_repository_tag(repository_and_tag)
    if not repository:
        raise ImagePolicyError(_error("image repository cannot be empty"))
    if tag == "latest":
        raise ImagePolicyError(_error("mutable latest tag is not allowed even with a digest"))
    return AcceptedImage(
        reference=normalized,
        repository=repository,
        tag=tag,
        digest=digest,
    )


def _split_repository_tag(value: str) -> tuple[str, str | None]:
    slash_index = value.rfind("/")
    colon_index = value.rfind(":")
    if colon_index > slash_index:
        return value[:colon_index], value[colon_index + 1 :]
    return value, None


def _error(reason: str) -> str:
    return (
        f"{reason}. Rescan images must use immutable references such as "
        "`ghcr.io/org/scanner:v1@sha256:<64-hex-digest>`."
    )


__all__ = ["AcceptedImage", "ImagePolicyError", "validate_replay_image"]
