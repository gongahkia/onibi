from __future__ import annotations

import argparse
import sys
from pathlib import Path

from piranesi.pff import PFF_SCHEMA_VERSION, PffValidationError, load_and_validate_pff_file

DEFAULT_FIXTURE_ROOT = Path("tests/fixtures/pff")


class PffFixtureValidationError(ValueError):
    """Raised when a PFF compatibility fixture is invalid."""


def validate_pff_fixture_corpus(root: Path = DEFAULT_FIXTURE_ROOT) -> list[str]:
    resolved_root = root.resolve(strict=False)
    if not resolved_root.is_dir():
        raise PffFixtureValidationError(f"missing PFF fixture directory: {resolved_root}")

    fixture_paths = sorted(path for path in resolved_root.rglob("*.json") if path.is_file())
    if not fixture_paths:
        raise PffFixtureValidationError(f"no PFF JSON fixtures found under {resolved_root}")

    messages: list[str] = []
    for fixture_path in fixture_paths:
        try:
            document = load_and_validate_pff_file(fixture_path)
        except PffValidationError as exc:
            relative = fixture_path.relative_to(resolved_root).as_posix()
            raise PffFixtureValidationError(f"{relative}: {exc}") from exc
        version = document.get("schema_version")
        if version != PFF_SCHEMA_VERSION:
            relative = fixture_path.relative_to(resolved_root).as_posix()
            raise PffFixtureValidationError(
                f"{relative}: expected schema_version {PFF_SCHEMA_VERSION!r}, got {version!r}"
            )
        messages.append(f"validated {fixture_path.relative_to(resolved_root).as_posix()}")
    return messages


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Validate PFF compatibility fixtures.")
    parser.add_argument(
        "--root",
        type=Path,
        default=DEFAULT_FIXTURE_ROOT,
        help="PFF fixture corpus root.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    try:
        messages = validate_pff_fixture_corpus(args.root)
    except PffFixtureValidationError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    for message in messages:
        print(message)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
