from __future__ import annotations

import argparse
from collections import Counter
from pathlib import Path

from piranesi.detect.flows import extract_candidate_findings
from piranesi.scan.joern import JoernServer
from piranesi.scan.transpile import transpile_project


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run Piranesi's transpile + detect path and print a compact summary.",
    )
    parser.add_argument("target", type=Path, help="Target project directory.")
    parser.add_argument(
        "--show-limit",
        type=int,
        default=20,
        help="Maximum number of findings to print in the detailed list.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    target = args.target.resolve(strict=False)
    transpiled = transpile_project(target)
    try:
        with JoernServer(startup_timeout_seconds=30, query_timeout_seconds=30) as server:
            server.import_project(transpiled.out_dir)
            findings = extract_candidate_findings(
                server,
                joern_project_root=transpiled.out_dir,
                source_map=transpiled.source_map,
            )
    finally:
        transpiled.cleanup()

    by_cwe = Counter(finding.vuln_class for finding in findings)
    print("Piranesi Detect Summary")
    print(f"Target: {target}")
    print(f"Transpile failures tolerated: {len(transpiled.failed_files)}")
    print(f"Candidate findings: {len(findings)}")
    print("By CWE:")
    for cwe, count in sorted(by_cwe.items()):
        print(f"  {cwe}: {count}")
    print("Findings:")
    for finding in findings[: args.show_limit]:
        source_name = finding.source.parameter_name or finding.source.source_type
        print(
            "  - "
            f"{finding.vuln_class} | source={source_name} | sink={finding.sink.api_name} | "
            f"{finding.sink.location.file}:{finding.sink.location.line}"
        )
    if len(findings) > args.show_limit:
        print(f"  ... {len(findings) - args.show_limit} more")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
