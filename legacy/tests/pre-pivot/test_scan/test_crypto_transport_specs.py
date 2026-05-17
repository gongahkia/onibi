from __future__ import annotations

from piranesi.scan.specs import (
    CRYPTO_TRANSPORT_JWT_PUBLIC_KEY_HINTS,
    CRYPTO_TRANSPORT_NON_SECURITY_HASH_HINTS,
    CRYPTO_TRANSPORT_NON_SECURITY_PRNG_HINTS,
    CRYPTO_TRANSPORT_SECURITY_CONTEXT_HINTS,
    CRYPTO_TRANSPORT_SECURITY_IDENTIFIER_HINTS,
    CRYPTO_TRANSPORT_WEAK_EC_CURVES,
)


def test_crypto_transport_hash_hints_cover_checksum_contexts() -> None:
    assert "checksum" in CRYPTO_TRANSPORT_NON_SECURITY_HASH_HINTS
    assert "etag" in CRYPTO_TRANSPORT_NON_SECURITY_HASH_HINTS
    assert "file_hash" in CRYPTO_TRANSPORT_NON_SECURITY_HASH_HINTS


def test_crypto_transport_security_context_hints_cover_auth_material() -> None:
    assert "password" in CRYPTO_TRANSPORT_SECURITY_CONTEXT_HINTS
    assert "token" in CRYPTO_TRANSPORT_SECURITY_CONTEXT_HINTS
    assert "jwt" in CRYPTO_TRANSPORT_SECURITY_CONTEXT_HINTS


def test_crypto_transport_security_identifier_hints_cover_prng_sinks() -> None:
    assert "secret" in CRYPTO_TRANSPORT_SECURITY_IDENTIFIER_HINTS
    assert "auth_code" in CRYPTO_TRANSPORT_SECURITY_IDENTIFIER_HINTS
    assert "reset_token" in CRYPTO_TRANSPORT_SECURITY_IDENTIFIER_HINTS


def test_crypto_transport_non_security_prng_hints_cover_ui_context() -> None:
    assert "animation" in CRYPTO_TRANSPORT_NON_SECURITY_PRNG_HINTS
    assert "color" in CRYPTO_TRANSPORT_NON_SECURITY_PRNG_HINTS
    assert "style" in CRYPTO_TRANSPORT_NON_SECURITY_PRNG_HINTS


def test_crypto_transport_jwt_public_key_hints_cover_common_names() -> None:
    assert "publicKey" in CRYPTO_TRANSPORT_JWT_PUBLIC_KEY_HINTS
    assert "certificate" in CRYPTO_TRANSPORT_JWT_PUBLIC_KEY_HINTS
    assert "rsaPublicKey" in CRYPTO_TRANSPORT_JWT_PUBLIC_KEY_HINTS


def test_crypto_transport_weak_ec_curve_specs_cover_sub_p256_curves() -> None:
    assert "prime192v1" in CRYPTO_TRANSPORT_WEAK_EC_CURVES
    assert "secp192r1" in CRYPTO_TRANSPORT_WEAK_EC_CURVES
    assert "sect163k1" in CRYPTO_TRANSPORT_WEAK_EC_CURVES
