from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(REPO_ROOT / "src") not in sys.path:
    sys.path.insert(0, str(REPO_ROOT / "src"))

from eval.fixture_validation import (
    cleanup_output_dir,
    load_ground_truth_entry,
    validate_entry,
)


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run Piranesi against a single fixture and compare the result to one ground-truth entry."
    )
    parser.add_argument("--fixture", type=Path, help="Fixture directory. Defaults to the GT entry root.")
    parser.add_argument("--fixtures-dir", type=Path, help="Optional base directory for relative fixture paths.")
    parser.add_argument("--gt", type=Path, required=True, help="Ground-truth YAML file.")
    parser.add_argument(
        "--output-dir",
        type=Path,
        help="Keep stage artifacts in this directory instead of a temporary one.",
    )
    parser.add_argument("--keep-output", action="store_true", help="Keep temporary stage artifacts.")
    parser.add_argument("--json", action="store_true", help="Emit the result as JSON.")
    parser.add_argument("--verbose", action="store_true", help="Stream Piranesi command output.")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    entry = load_ground_truth_entry(args.gt)
    result = validate_entry(
        entry,
        fixture_root=args.fixture,
        fixtures_dir=args.fixtures_dir,
        output_dir=args.output_dir,
        verbose=args.verbose,
        keep_output=args.keep_output,
    )

    if args.json:
        print(json.dumps(result.as_dict(), indent=2))
    else:
        print(result.message)

    if args.output_dir is None and not args.keep_output:
        cleanup_output_dir(result.output_dir)

    return 0 if result.passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
