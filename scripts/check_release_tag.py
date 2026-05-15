from __future__ import annotations

import sys
import tomllib
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def expected_version(root: Path = ROOT) -> str:
    with (root / "pyproject.toml").open("rb") as handle:
        data = tomllib.load(handle)
    version = data.get("project", {}).get("version")
    if not isinstance(version, str) or not version:
        raise RuntimeError("pyproject.toml is missing [project].version")
    return version


def main(argv: list[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    if len(args) != 1:
        print("usage: check_release_tag.py <tag>", file=sys.stderr)
        return 2
    tag = args[0].removeprefix("refs/tags/")
    version = expected_version()
    if tag != f"v{version}":
        print(f"release tag {tag!r} does not match pyproject version {version!r}", file=sys.stderr)
        return 1
    print(f"release tag matches version {version}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
