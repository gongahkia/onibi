from __future__ import annotations

import re
from dataclasses import dataclass

from packaging.version import InvalidVersion, Version

_COMPARATOR_RE = re.compile(r"^(<=|>=|<|>|=)?\s*(.+?)$")
_QUALIFIER_ORDER = {
    "snapshot": -5,
    "alpha": -4,
    "a": -4,
    "beta": -3,
    "b": -3,
    "milestone": -2,
    "m": -2,
    "rc": -1,
    "cr": -1,
    "sp": 1,
}


def is_vulnerable(
    package_version: str,
    vulnerable_ranges: list[str] | tuple[str, ...],
    ecosystem: str,
) -> bool:
    normalized_ecosystem = ecosystem.strip().lower()
    for range_str in vulnerable_ranges:
        if not range_str.strip():
            continue
        if normalized_ecosystem in {"npm", "go", "crates"}:
            if _match_semver_range(package_version, range_str):
                return True
            continue
        if normalized_ecosystem in {"python", "pypi"}:
            if _match_pep440_range(package_version, range_str):
                return True
            continue
        if normalized_ecosystem in {"rubygems", "ruby"}:
            if _match_rubygems_range(package_version, range_str):
                return True
            continue
        if normalized_ecosystem in {"maven", "java"}:
            if _match_maven_range(package_version, range_str):
                return True
            continue
        if package_version == range_str:
            return True
    return False


@dataclass(frozen=True)
class SemverVersion:
    major: int
    minor: int
    patch: int
    prerelease: tuple[int | str, ...] = ()

    def __lt__(self, other: object) -> bool:
        if not isinstance(other, SemverVersion):
            return NotImplemented
        lhs_core = (self.major, self.minor, self.patch)
        rhs_core = (other.major, other.minor, other.patch)
        if lhs_core != rhs_core:
            return lhs_core < rhs_core
        if not self.prerelease and other.prerelease:
            return False
        if self.prerelease and not other.prerelease:
            return True
        return _compare_prerelease(self.prerelease, other.prerelease) < 0

    def __le__(self, other: object) -> bool:
        return self == other or self < other

    def __gt__(self, other: object) -> bool:
        return not self <= other

    def __ge__(self, other: object) -> bool:
        return not self < other


def parse_semver(value: str) -> SemverVersion:
    normalized = value.strip()
    if normalized.startswith("v"):
        normalized = normalized[1:]
    normalized = normalized.split("+", 1)[0]
    prerelease: tuple[int | str, ...] = ()
    if "-" in normalized:
        normalized, raw_prerelease = normalized.split("-", 1)
        prerelease = tuple(_semver_identifier(part) for part in raw_prerelease.split("."))
    parts = normalized.split(".")
    if len(parts) == 1:
        parts.extend(["0", "0"])
    elif len(parts) == 2:
        parts.append("0")
    if len(parts) < 3:
        raise ValueError(f"invalid semver: {value}")
    return SemverVersion(
        major=int(parts[0]), minor=int(parts[1]), patch=int(parts[2]), prerelease=prerelease
    )


def _match_semver_range(version: str, range_str: str) -> bool:
    candidate = parse_semver(version)
    groups = [group.strip() for group in range_str.split("||") if group.strip()]
    if not groups:
        return False
    return any(_match_semver_group(candidate, group) for group in groups)


def _match_semver_group(version: SemverVersion, group: str) -> bool:
    if group in {"*", ">=0"}:
        return True
    comparators = [token for token in re.split(r"\s+", group.replace(",", " ")) if token]
    expanded: list[str] = []
    for token in comparators:
        expanded.extend(_expand_semver_token(token))
    return all(_evaluate_semver_comparator(version, token) for token in expanded)


def _expand_semver_token(token: str) -> list[str]:
    stripped = token.strip()
    if stripped in {"*", "x", "X"}:
        return [">=0.0.0"]
    if stripped.startswith("^"):
        lower = parse_semver(stripped[1:])
        if lower.major > 0:
            upper = SemverVersion(lower.major + 1, 0, 0)
        elif lower.minor > 0:
            upper = SemverVersion(0, lower.minor + 1, 0)
        else:
            upper = SemverVersion(0, 0, lower.patch + 1)
        return [f">={_render_semver(lower)}", f"<{_render_semver(upper)}"]
    if stripped.startswith("~"):
        lower = parse_semver(stripped[1:])
        upper = (
            SemverVersion(lower.major, lower.minor + 1, 0)
            if "." in stripped[1:]
            else SemverVersion(lower.major + 1, 0, 0)
        )
        return [f">={_render_semver(lower)}", f"<{_render_semver(upper)}"]
    if stripped.endswith(".x") or stripped.endswith(".*"):
        prefix = stripped[:-2]
        parts = prefix.split(".")
        if len(parts) == 1:
            lower = SemverVersion(int(parts[0]), 0, 0)
            upper = SemverVersion(int(parts[0]) + 1, 0, 0)
        else:
            lower = SemverVersion(int(parts[0]), int(parts[1]), 0)
            upper = SemverVersion(int(parts[0]), int(parts[1]) + 1, 0)
        return [f">={_render_semver(lower)}", f"<{_render_semver(upper)}"]
    return [stripped]


def _evaluate_semver_comparator(version: SemverVersion, token: str) -> bool:
    match = _COMPARATOR_RE.match(token)
    if match is None:
        return False
    op = match.group(1) or "="
    other = parse_semver(match.group(2))
    if op == "=":
        return version == other
    if op == ">":
        return version > other
    if op == ">=":
        return version >= other
    if op == "<":
        return version < other
    if op == "<=":
        return version <= other
    return False


def _render_semver(version: SemverVersion) -> str:
    rendered = f"{version.major}.{version.minor}.{version.patch}"
    if version.prerelease:
        rendered += "-" + ".".join(str(item) for item in version.prerelease)
    return rendered


def _compare_prerelease(lhs: tuple[int | str, ...], rhs: tuple[int | str, ...]) -> int:
    for index in range(max(len(lhs), len(rhs))):
        if index >= len(lhs):
            return -1
        if index >= len(rhs):
            return 1
        left = lhs[index]
        right = rhs[index]
        if left == right:
            continue
        if isinstance(left, int) and isinstance(right, str):
            return -1
        if isinstance(left, str) and isinstance(right, int):
            return 1
        if isinstance(left, int) and isinstance(right, int):
            return -1 if left < right else 1
        if isinstance(left, str) and isinstance(right, str):
            return -1 if left < right else 1
        return -1 if str(left) < str(right) else 1
    return 0


def _semver_identifier(value: str) -> int | str:
    return int(value) if value.isdigit() else value


def _match_pep440_range(version: str, range_str: str) -> bool:
    try:
        candidate = Version(version)
    except (InvalidVersion, ValueError):
        return version == range_str
    normalized = range_str.replace(" ", ",")
    normalized = re.sub(r",{2,}", ",", normalized).strip(",")
    specifiers = [item.strip() for item in normalized.split(",") if item.strip()]
    if not specifiers:
        return False
    return all(_pep440_single_match(candidate, item) for item in specifiers)


def _pep440_single_match(candidate: Version, specifier: str) -> bool:
    match = _COMPARATOR_RE.match(specifier)
    if match is None:
        return False
    op = match.group(1) or "=="
    try:
        other = Version(match.group(2))
    except InvalidVersion:
        return False
    if op in {"=", "=="}:
        return candidate == other
    if op == ">":
        return candidate > other
    if op == ">=":
        return candidate >= other
    if op == "<":
        return candidate < other
    if op == "<=":
        return candidate <= other
    if op == "!=":
        return candidate != other
    return False


def _match_maven_range(version: str, range_str: str) -> bool:
    stripped = range_str.strip()
    if stripped.startswith("[") or stripped.startswith("("):
        return _match_maven_interval(version, stripped)
    comparators = [token for token in re.split(r"\s+", stripped.replace(",", " ")) if token]
    if not comparators:
        return False
    return all(_evaluate_maven_comparator(version, token) for token in comparators)


def _match_rubygems_range(version: str, range_str: str) -> bool:
    try:
        candidate = Version(version)
    except (InvalidVersion, ValueError):
        return version == range_str

    groups = [group.strip() for group in range_str.split("||") if group.strip()]
    if not groups:
        return False
    return any(_match_rubygems_group(candidate, group) for group in groups)


def _match_rubygems_group(candidate: Version, group: str) -> bool:
    comparators = [token.strip() for token in group.split(",") if token.strip()]
    if not comparators:
        return False

    expanded: list[str] = []
    for comparator in comparators:
        expanded.extend(_expand_rubygems_comparator(comparator))
    return all(_pep440_single_match(candidate, comparator) for comparator in expanded)


def _expand_rubygems_comparator(token: str) -> list[str]:
    stripped = token.strip()
    if not stripped:
        return []
    if stripped.startswith("~>"):
        raw_version = stripped[2:].strip()
        try:
            lower = Version(raw_version)
        except InvalidVersion:
            return []
        upper = _rubygems_pessimistic_upper_bound(lower)
        return [f">={lower}", f"<{upper}"]
    if stripped.startswith("="):
        return [f"=={stripped[1:].strip()}"]
    return [stripped]


def _rubygems_pessimistic_upper_bound(version: Version) -> Version:
    release = list(version.release)
    if not release:
        raise InvalidVersion(str(version))
    if len(release) == 1:
        release[0] += 1
    else:
        pivot = len(release) - 2
        release[pivot] += 1
        for index in range(pivot + 1, len(release)):
            release[index] = 0
    return Version(".".join(str(part) for part in release))


def _match_maven_interval(version: str, range_str: str) -> bool:
    if len(range_str) < 2:
        return False
    left_inclusive = range_str.startswith("[")
    right_inclusive = range_str.endswith("]")
    body = range_str[1:-1]
    lower_raw, _, upper_raw = body.partition(",")
    lower = lower_raw.strip()
    upper = upper_raw.strip()
    if lower:
        cmp = _compare_maven_versions(version, lower)
        if cmp < 0 or (cmp == 0 and not left_inclusive):
            return False
    if upper:
        cmp = _compare_maven_versions(version, upper)
        if cmp > 0 or (cmp == 0 and not right_inclusive):
            return False
    return True


def _evaluate_maven_comparator(version: str, token: str) -> bool:
    match = _COMPARATOR_RE.match(token)
    if match is None:
        return False
    op = match.group(1) or "="
    other = match.group(2)
    cmp = _compare_maven_versions(version, other)
    if op == "=":
        return cmp == 0
    if op == ">":
        return cmp > 0
    if op == ">=":
        return cmp >= 0
    if op == "<":
        return cmp < 0
    if op == "<=":
        return cmp <= 0
    return False


def _compare_maven_versions(lhs: str, rhs: str) -> int:
    left_tokens = _tokenize_maven_version(lhs)
    right_tokens = _tokenize_maven_version(rhs)
    for index in range(max(len(left_tokens), len(right_tokens))):
        left = left_tokens[index] if index < len(left_tokens) else 0
        right = right_tokens[index] if index < len(right_tokens) else 0
        if left == right:
            continue
        if isinstance(left, int) and isinstance(right, int):
            return -1 if left < right else 1
        if isinstance(left, int):
            return 1
        if isinstance(right, int):
            return -1
        left_rank = _QUALIFIER_ORDER.get(left, 0)
        right_rank = _QUALIFIER_ORDER.get(right, 0)
        if left_rank != right_rank:
            return -1 if left_rank < right_rank else 1
        return -1 if left < right else 1
    return 0


def _tokenize_maven_version(value: str) -> list[int | str]:
    tokens: list[int | str] = []
    for part in re.split(r"[.\-_]", value.strip().lower()):
        if not part:
            continue
        if part.isdigit():
            tokens.append(int(part))
        else:
            tokens.append(part)
    return tokens
