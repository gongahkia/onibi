from __future__ import annotations

import pytest

from piranesi.rescan.image_policy import ImagePolicyError, validate_replay_image

DIGEST = "sha256:" + "a" * 64


def test_validate_replay_image_accepts_digest_pinned_reference() -> None:
    image = validate_replay_image(f"ghcr.io/acme/nmap:v1.2.3@{DIGEST}")

    assert image.repository == "ghcr.io/acme/nmap"
    assert image.tag == "v1.2.3"
    assert image.digest == DIGEST
    assert image.provenance() == {
        "image_reference": f"ghcr.io/acme/nmap:v1.2.3@{DIGEST}",
        "image_repository": "ghcr.io/acme/nmap",
        "image_tag": "v1.2.3",
        "image_digest": DIGEST,
    }


def test_validate_replay_image_accepts_digest_without_tag() -> None:
    image = validate_replay_image(f"ghcr.io/acme/nuclei@{DIGEST}")

    assert image.repository == "ghcr.io/acme/nuclei"
    assert image.tag is None
    assert image.digest == DIGEST


@pytest.mark.parametrize(
    ("reference", "message"),
    [
        ("nmap:latest", "pinned"),
        ("nmap:v1.0.0", "pinned"),
        (f"nmap:latest@{DIGEST}", "latest tag"),
        ("nmap@sha256:not-a-digest", "sha256 digest"),
        ("", "cannot be empty"),
    ],
)
def test_validate_replay_image_rejects_mutable_or_invalid_references(
    reference: str,
    message: str,
) -> None:
    with pytest.raises(ImagePolicyError, match=message):
        validate_replay_image(reference)
