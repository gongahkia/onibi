from __future__ import annotations

import argparse
import ast
import sys
from dataclasses import dataclass
from pathlib import Path

_JOERN_INDICATORS = ("is_joern_installed(",)
_DOCKER_INDICATORS = ('shutil.which("docker")', "docker info", "_docker_available(")
_EXCLUDED_FILES = {"test_environment_marker_policy.py"}


@dataclass(frozen=True, slots=True)
class MarkerViolation:
    path: Path
    line: int
    message: str


def collect_environment_marker_violations(root: Path) -> list[MarkerViolation]:
    violations: list[MarkerViolation] = []
    for test_path in sorted(root.rglob("test_*.py")):
        if test_path.name in _EXCLUDED_FILES:
            continue
        try:
            source_text = test_path.read_text(encoding="utf-8")
        except OSError:
            continue
        try:
            module = ast.parse(source_text, filename=str(test_path))
        except SyntaxError as exc:
            violations.append(
                MarkerViolation(
                    path=test_path,
                    line=exc.lineno or 1,
                    message=f"syntax error while parsing test module: {exc.msg}",
                )
            )
            continue

        lines = source_text.splitlines()
        module_markers = _module_level_markers(module)
        for node in module.body:
            if not isinstance(node, ast.FunctionDef | ast.AsyncFunctionDef):
                continue
            if not node.name.startswith("test_"):
                continue
            function_markers = module_markers | _decorator_markers(node.decorator_list)
            function_source = _node_source(lines, node)
            _append_marker_violations(
                violations,
                test_path,
                node.lineno,
                function_markers,
                function_source,
            )
    return violations


def _append_marker_violations(
    violations: list[MarkerViolation],
    path: Path,
    line: int,
    markers: set[str],
    source: str,
) -> None:
    has_joern_indicator = any(indicator in source for indicator in _JOERN_INDICATORS)
    has_docker_indicator = any(indicator in source for indicator in _DOCKER_INDICATORS)

    if has_joern_indicator and not ({"joern", "e2e"} & markers):
        violations.append(
            MarkerViolation(
                path=path,
                line=line,
                message="Joern-dependent test must declare @pytest.mark.joern",
            )
        )
    if has_joern_indicator and not ({"integration", "e2e"} & markers):
        violations.append(
            MarkerViolation(
                path=path,
                line=line,
                message="Joern-dependent test must declare @pytest.mark.integration",
            )
        )

    if has_docker_indicator and not ({"docker", "e2e", "integration"} & markers):
        violations.append(
            MarkerViolation(
                path=path,
                line=line,
                message=(
                    "Docker-dependent test must declare one of "
                    "@pytest.mark.docker/@pytest.mark.e2e/@pytest.mark.integration"
                ),
            )
        )

    if "joern" in markers and "integration" not in markers:
        violations.append(
            MarkerViolation(
                path=path,
                line=line,
                message="@pytest.mark.joern tests must also declare @pytest.mark.integration",
            )
        )


def _module_level_markers(module: ast.Module) -> set[str]:
    markers: set[str] = set()
    for node in module.body:
        if not isinstance(node, ast.Assign):
            continue
        if len(node.targets) != 1:
            continue
        target = node.targets[0]
        if not isinstance(target, ast.Name) or target.id != "pytestmark":
            continue
        markers |= _markers_from_expression(node.value)
    return markers


def _decorator_markers(decorators: list[ast.expr]) -> set[str]:
    markers: set[str] = set()
    for decorator in decorators:
        markers |= _markers_from_expression(decorator)
    return markers


def _markers_from_expression(expression: ast.expr) -> set[str]:
    if isinstance(expression, ast.List | ast.Tuple | ast.Set):
        markers: set[str] = set()
        for element in expression.elts:
            markers |= _markers_from_expression(element)
        return markers

    if isinstance(expression, ast.Call):
        return _markers_from_expression(expression.func)

    if isinstance(expression, ast.Attribute):
        dotted = _dotted_name(expression)
        prefix = "pytest.mark."
        if dotted.startswith(prefix):
            return {dotted[len(prefix) :]}

    return set()


def _dotted_name(node: ast.AST) -> str:
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        parent = _dotted_name(node.value)
        return f"{parent}.{node.attr}" if parent else node.attr
    return ""


def _node_source(lines: list[str], node: ast.FunctionDef | ast.AsyncFunctionDef) -> str:
    start = max(0, node.lineno - 1)
    end = node.end_lineno if node.end_lineno is not None else node.lineno
    return "\n".join(lines[start:end])


def _parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Validate marker policy for environment-dependent tests.",
    )
    parser.add_argument(
        "--root",
        type=Path,
        default=Path("tests"),
        help="Root directory containing test files.",
    )
    return parser.parse_args()


def main() -> int:
    args = _parse_args()
    violations = collect_environment_marker_violations(args.root)
    if not violations:
        print("environment marker checks passed")
        return 0
    print("environment marker checks failed:", file=sys.stderr)
    for violation in violations:
        print(
            f"- {violation.path}:{violation.line}: {violation.message}",
            file=sys.stderr,
        )
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
