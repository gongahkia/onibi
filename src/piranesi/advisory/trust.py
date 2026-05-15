from __future__ import annotations

import hmac
import json
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
from typing import Any

from piranesi.advisory.db import utc_now

_MANIFEST_SCHEMA_VERSION = 1
_HMAC_SIGNATURE_SCHEME = "hmac-sha256"


@dataclass(frozen=True)
class SnapshotSignature:
    scheme: str
    value: str
    signer: str | None = None


@dataclass(frozen=True)
class SnapshotManifest:
    schema_version: int
    snapshot_path: str
    snapshot_sha256: str
    file_size_bytes: int
    created_at: str
    signature: SnapshotSignature | None = None

    def to_dict(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "schema_version": self.schema_version,
            "snapshot_path": self.snapshot_path,
            "snapshot_sha256": self.snapshot_sha256,
            "file_size_bytes": self.file_size_bytes,
            "created_at": self.created_at,
        }
        if self.signature is not None:
            payload["signature"] = {
                "scheme": self.signature.scheme,
                "value": self.signature.value,
                "signer": self.signature.signer,
            }
        return payload


@dataclass(frozen=True)
class SnapshotVerificationResult:
    verified: bool
    has_signature: bool
    tampered: bool
    reason: str
    snapshot_sha256: str | None
    manifest_sha256: str | None
    signature_scheme: str | None
    signature_signer: str | None
    signature_value: str | None


def write_snapshot_manifest(
    snapshot_path: Path,
    manifest_path: Path,
    *,
    signing_key: str | bytes | None = None,
    signer: str | None = None,
) -> SnapshotManifest:
    resolved_snapshot = snapshot_path.expanduser().resolve(strict=True)
    snapshot_sha = compute_sha256(resolved_snapshot)
    file_size_bytes = resolved_snapshot.stat().st_size
    signature: SnapshotSignature | None = None
    if signing_key is not None:
        digest = _snapshot_signature_digest(snapshot_sha, file_size_bytes)
        signature = SnapshotSignature(
            scheme=_HMAC_SIGNATURE_SCHEME,
            value=_hmac_sign(digest, signing_key),
            signer=signer,
        )
    manifest = SnapshotManifest(
        schema_version=_MANIFEST_SCHEMA_VERSION,
        snapshot_path=str(resolved_snapshot),
        snapshot_sha256=snapshot_sha,
        file_size_bytes=file_size_bytes,
        created_at=utc_now(),
        signature=signature,
    )
    manifest_path.parent.mkdir(parents=True, exist_ok=True)
    manifest_path.write_text(
        json.dumps(manifest.to_dict(), indent=2) + "\n",
        encoding="utf-8",
    )
    return manifest


def load_snapshot_manifest(manifest_path: Path) -> SnapshotManifest:
    payload = json.loads(manifest_path.read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise ValueError(f"invalid snapshot manifest payload: {manifest_path}")
    schema_version = int(payload.get("schema_version") or 0)
    if schema_version != _MANIFEST_SCHEMA_VERSION:
        raise ValueError(
            "unsupported snapshot manifest schema version "
            f"{schema_version} (expected {_MANIFEST_SCHEMA_VERSION})"
        )
    snapshot_sha256 = str(payload.get("snapshot_sha256") or "").strip()
    snapshot_path = str(payload.get("snapshot_path") or "").strip()
    created_at = str(payload.get("created_at") or "").strip()
    file_size_bytes_raw = payload.get("file_size_bytes")
    if not snapshot_sha256:
        raise ValueError("snapshot manifest missing snapshot_sha256")
    if not snapshot_path:
        raise ValueError("snapshot manifest missing snapshot_path")
    if not created_at:
        raise ValueError("snapshot manifest missing created_at")
    if not isinstance(file_size_bytes_raw, int) or file_size_bytes_raw < 0:
        raise ValueError("snapshot manifest has invalid file_size_bytes")
    signature_payload = payload.get("signature")
    signature: SnapshotSignature | None = None
    if signature_payload is not None:
        if not isinstance(signature_payload, dict):
            raise ValueError("snapshot manifest signature must be an object")
        scheme = str(signature_payload.get("scheme") or "").strip()
        value = str(signature_payload.get("value") or "").strip()
        signer = str(signature_payload.get("signer") or "").strip() or None
        if not scheme or not value:
            raise ValueError("snapshot manifest signature is missing scheme/value")
        signature = SnapshotSignature(scheme=scheme, value=value, signer=signer)
    return SnapshotManifest(
        schema_version=schema_version,
        snapshot_path=snapshot_path,
        snapshot_sha256=snapshot_sha256,
        file_size_bytes=file_size_bytes_raw,
        created_at=created_at,
        signature=signature,
    )


def verify_snapshot_manifest(
    snapshot_path: Path,
    manifest_path: Path,
    *,
    verification_key: str | bytes | None = None,
) -> SnapshotVerificationResult:
    resolved_snapshot = snapshot_path.expanduser().resolve(strict=True)
    resolved_manifest = manifest_path.expanduser().resolve(strict=True)
    manifest_sha = compute_sha256(resolved_manifest)
    manifest = load_snapshot_manifest(resolved_manifest)
    observed_sha = compute_sha256(resolved_snapshot)
    tampered = observed_sha != manifest.snapshot_sha256
    if tampered:
        return SnapshotVerificationResult(
            verified=False,
            has_signature=manifest.signature is not None,
            tampered=True,
            reason="snapshot digest mismatch",
            snapshot_sha256=observed_sha,
            manifest_sha256=manifest_sha,
            signature_scheme=None if manifest.signature is None else manifest.signature.scheme,
            signature_signer=None if manifest.signature is None else manifest.signature.signer,
            signature_value=None if manifest.signature is None else manifest.signature.value,
        )
    if manifest.signature is None:
        return SnapshotVerificationResult(
            verified=False,
            has_signature=False,
            tampered=False,
            reason="manifest has no signature",
            snapshot_sha256=observed_sha,
            manifest_sha256=manifest_sha,
            signature_scheme=None,
            signature_signer=None,
            signature_value=None,
        )
    if manifest.signature.scheme != _HMAC_SIGNATURE_SCHEME:
        return SnapshotVerificationResult(
            verified=False,
            has_signature=True,
            tampered=False,
            reason=f"unsupported signature scheme: {manifest.signature.scheme}",
            snapshot_sha256=observed_sha,
            manifest_sha256=manifest_sha,
            signature_scheme=manifest.signature.scheme,
            signature_signer=manifest.signature.signer,
            signature_value=manifest.signature.value,
        )
    if verification_key is None:
        return SnapshotVerificationResult(
            verified=False,
            has_signature=True,
            tampered=False,
            reason="verification key not provided",
            snapshot_sha256=observed_sha,
            manifest_sha256=manifest_sha,
            signature_scheme=manifest.signature.scheme,
            signature_signer=manifest.signature.signer,
            signature_value=manifest.signature.value,
        )
    expected = _hmac_sign(
        _snapshot_signature_digest(manifest.snapshot_sha256, manifest.file_size_bytes),
        verification_key,
    )
    if not hmac.compare_digest(expected, manifest.signature.value):
        return SnapshotVerificationResult(
            verified=False,
            has_signature=True,
            tampered=False,
            reason="signature mismatch",
            snapshot_sha256=observed_sha,
            manifest_sha256=manifest_sha,
            signature_scheme=manifest.signature.scheme,
            signature_signer=manifest.signature.signer,
            signature_value=manifest.signature.value,
        )
    return SnapshotVerificationResult(
        verified=True,
        has_signature=True,
        tampered=False,
        reason="signature verified",
        snapshot_sha256=observed_sha,
        manifest_sha256=manifest_sha,
        signature_scheme=manifest.signature.scheme,
        signature_signer=manifest.signature.signer,
        signature_value=manifest.signature.value,
    )


def compute_sha256(path: Path) -> str:
    payload = path.read_bytes()
    return sha256(payload).hexdigest()


def load_trust_key(path: Path) -> bytes:
    key = path.read_text(encoding="utf-8").strip()
    if not key:
        raise ValueError(f"trust key file is empty: {path}")
    return key.encode("utf-8")


def _snapshot_signature_digest(snapshot_sha256: str, file_size_bytes: int) -> bytes:
    return f"{snapshot_sha256}:{file_size_bytes}".encode()


def _hmac_sign(message: bytes, key: str | bytes) -> str:
    key_bytes = key.encode("utf-8") if isinstance(key, str) else key
    return hmac.new(key_bytes, message, sha256).hexdigest()
