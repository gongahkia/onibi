from __future__ import annotations

import json
import shutil
import sqlite3
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from datetime import UTC, datetime
from hashlib import sha256
from pathlib import Path

from piranesi.advisory.models import (
    Advisory,
    AffectedPackage,
    ExploitStatus,
    exploit_status_rank,
    normalize_severity,
    severity_rank,
)

_SCHEMA = """
CREATE TABLE IF NOT EXISTS advisories (
    advisory_id TEXT PRIMARY KEY,
    cve_id TEXT,
    ghsa_id TEXT,
    cwe_ids TEXT NOT NULL DEFAULT '[]',
    affected_packages TEXT NOT NULL DEFAULT '[]',
    affected_versions TEXT NOT NULL DEFAULT '[]',
    title TEXT NOT NULL DEFAULT '',
    description TEXT,
    severity TEXT NOT NULL,
    cvss_score REAL,
    cvss_vector TEXT,
    epss_score REAL,
    epss_percentile REAL,
    exploit_available TEXT NOT NULL DEFAULT 'none',
    exploit_sources TEXT NOT NULL DEFAULT '[]',
    fix_version TEXT,
    source TEXT NOT NULL DEFAULT '[]',
    published_date TEXT,
    modified_date TEXT,
    "references" TEXT NOT NULL DEFAULT '[]',
    fetched_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS affected_packages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    advisory_id TEXT NOT NULL REFERENCES advisories(advisory_id) ON DELETE CASCADE,
    ecosystem TEXT NOT NULL,
    name TEXT NOT NULL,
    vulnerable_ranges TEXT NOT NULL DEFAULT '[]',
    fixed_versions TEXT NOT NULL DEFAULT '[]',
    fix_available INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sync_metadata (
    source TEXT PRIMARY KEY,
    last_sync TEXT NOT NULL,
    last_cursor TEXT,
    record_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS advisory_snapshot_provenance (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    source_path TEXT,
    snapshot_sha256 TEXT,
    manifest_path TEXT,
    manifest_sha256 TEXT,
    signature_scheme TEXT,
    signature_signer TEXT,
    signature_value TEXT,
    verified INTEGER NOT NULL DEFAULT 0,
    verification_reason TEXT,
    imported_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_affected_pkg ON affected_packages(ecosystem, name);
CREATE INDEX IF NOT EXISTS idx_advisory_cve ON advisories(cve_id);
CREATE INDEX IF NOT EXISTS idx_advisory_ghsa ON advisories(ghsa_id);
CREATE INDEX IF NOT EXISTS idx_advisory_severity ON advisories(severity);
CREATE INDEX IF NOT EXISTS idx_advisory_epss ON advisories(epss_score);
"""
_ADVISORY_DB_SCHEMA_VERSION = 2
_DEFAULT_STALE_AFTER_DAYS = 14


@dataclass(frozen=True)
class SyncMetadata:
    source: str
    last_sync: str
    last_cursor: str | None
    record_count: int


@dataclass(frozen=True)
class AdvisoryRow:
    advisory: Advisory
    fetched_at: str


@dataclass(frozen=True)
class AdvisoryDBStatus:
    path: Path
    exists: bool
    schema_version: int | None
    advisory_count: int
    affected_package_count: int
    sources: tuple[str, ...]
    last_updated: str | None
    checksum_sha256: str | None
    freshness: str
    stale_after_days: int
    age_days: float | None
    trust_state: str
    provenance_verified: bool | None
    provenance_signature_scheme: str | None
    provenance_signature_signer: str | None
    provenance_snapshot_sha256: str | None
    provenance_manifest_sha256: str | None
    provenance_imported_at: str | None
    warnings: tuple[str, ...]


@dataclass(frozen=True)
class AdvisorySnapshotProvenance:
    source_path: str | None
    snapshot_sha256: str | None
    manifest_path: str | None
    manifest_sha256: str | None
    signature_scheme: str | None
    signature_signer: str | None
    signature_value: str | None
    verified: bool
    verification_reason: str | None
    imported_at: str


def advisory_db_path(project_root: Path) -> Path:
    return project_root.resolve(strict=False) / ".piranesi-cache" / "advisory.db"


def utc_now() -> str:
    return datetime.now(UTC).isoformat().replace("+00:00", "Z")


class AdvisoryDB:
    def __init__(self, path: Path) -> None:
        self.path = path
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self._conn = sqlite3.connect(self.path)
        self._conn.row_factory = sqlite3.Row
        self._conn.execute("PRAGMA foreign_keys = ON")
        self.ensure_schema()

    @classmethod
    def for_project(cls, project_root: Path) -> AdvisoryDB:
        return cls(advisory_db_path(project_root))

    def close(self) -> None:
        self._conn.close()

    def __enter__(self) -> AdvisoryDB:
        return self

    def __exit__(self, _exc_type: object, _exc: object, _tb: object) -> None:
        self.close()

    def ensure_schema(self) -> None:
        self._conn.executescript(_SCHEMA)
        self._conn.execute(f"PRAGMA user_version = {_ADVISORY_DB_SCHEMA_VERSION}")
        self._conn.commit()

    def schema_version(self) -> int:
        row = self._conn.execute("PRAGMA user_version").fetchone()
        if row is None:
            return 0
        return int(row[0])

    def advisory_count(self) -> int:
        row = self._conn.execute("SELECT COUNT(*) AS count FROM advisories").fetchone()
        return 0 if row is None else int(row["count"])

    def affected_package_count(self) -> int:
        row = self._conn.execute("SELECT COUNT(*) AS count FROM affected_packages").fetchone()
        return 0 if row is None else int(row["count"])

    def has_data(self) -> bool:
        return self.advisory_count() > 0

    def get_sync_metadata(self, source: str) -> SyncMetadata | None:
        row = self._conn.execute(
            "SELECT source, last_sync, last_cursor, record_count "
            "FROM sync_metadata WHERE source = ?",
            (source,),
        ).fetchone()
        if row is None:
            return None
        return SyncMetadata(
            source=str(row["source"]),
            last_sync=str(row["last_sync"]),
            last_cursor=_nullable_text(row["last_cursor"]),
            record_count=int(row["record_count"]),
        )

    def upsert_sync_metadata(
        self,
        *,
        source: str,
        last_sync: str,
        last_cursor: str | None,
        record_count: int,
    ) -> None:
        self._conn.execute(
            """
            INSERT INTO sync_metadata (source, last_sync, last_cursor, record_count)
            VALUES (?, ?, ?, ?)
            ON CONFLICT(source) DO UPDATE SET
                last_sync = excluded.last_sync,
                last_cursor = excluded.last_cursor,
                record_count = excluded.record_count
            """,
            (source, last_sync, last_cursor, record_count),
        )
        self._conn.commit()

    def get_advisory(self, advisory_id: str) -> AdvisoryRow | None:
        row = self._conn.execute(
            "SELECT * FROM advisories WHERE advisory_id = ?",
            (advisory_id,),
        ).fetchone()
        if row is None:
            return None
        packages = self._load_affected_packages(advisory_id)
        return AdvisoryRow(
            advisory=_advisory_from_row(row, packages=packages),
            fetched_at=str(row["fetched_at"]),
        )

    def iter_all_advisories(self) -> list[AdvisoryRow]:
        rows = self._conn.execute("SELECT * FROM advisories ORDER BY advisory_id").fetchall()
        advisories: list[AdvisoryRow] = []
        for row in rows:
            advisory_id = str(row["advisory_id"])
            advisories.append(
                AdvisoryRow(
                    advisory=_advisory_from_row(
                        row, packages=self._load_affected_packages(advisory_id)
                    ),
                    fetched_at=str(row["fetched_at"]),
                )
            )
        return advisories

    def upsert_advisories(self, advisories: Sequence[Advisory]) -> int:
        updated = 0
        for advisory in advisories:
            existing = self.get_advisory(advisory.advisory_id)
            merged = merge_advisories(existing.advisory if existing else None, advisory)
            self._write_advisory(merged)
            updated += 1
        self._conn.commit()
        return updated

    def _write_advisory(self, advisory: Advisory) -> None:
        fetched_at = utc_now()
        self._conn.execute(
            """
            INSERT INTO advisories (
                advisory_id,
                cve_id,
                ghsa_id,
                cwe_ids,
                affected_packages,
                affected_versions,
                title,
                description,
                severity,
                cvss_score,
                cvss_vector,
                epss_score,
                epss_percentile,
                exploit_available,
                exploit_sources,
                fix_version,
                source,
                published_date,
                modified_date,
                "references",
                fetched_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(advisory_id) DO UPDATE SET
                cve_id = excluded.cve_id,
                ghsa_id = excluded.ghsa_id,
                cwe_ids = excluded.cwe_ids,
                affected_packages = excluded.affected_packages,
                affected_versions = excluded.affected_versions,
                title = excluded.title,
                description = excluded.description,
                severity = excluded.severity,
                cvss_score = excluded.cvss_score,
                cvss_vector = excluded.cvss_vector,
                epss_score = excluded.epss_score,
                epss_percentile = excluded.epss_percentile,
                exploit_available = excluded.exploit_available,
                exploit_sources = excluded.exploit_sources,
                fix_version = excluded.fix_version,
                source = excluded.source,
                published_date = excluded.published_date,
                modified_date = excluded.modified_date,
                "references" = excluded."references",
                fetched_at = excluded.fetched_at
            """,
            (
                advisory.advisory_id,
                advisory.cve_id,
                advisory.ghsa_id,
                _json_dumps(advisory.cwe_ids),
                _json_dumps(
                    [
                        {"ecosystem": pkg.ecosystem, "name": pkg.name}
                        for pkg in advisory.affected_packages
                    ]
                ),
                _json_dumps(
                    [
                        {
                            "ecosystem": pkg.ecosystem,
                            "name": pkg.name,
                            "ranges": list(pkg.vulnerable_ranges),
                            "fixed_versions": list(pkg.fixed_versions),
                        }
                        for pkg in advisory.affected_packages
                    ]
                ),
                advisory.title,
                advisory.description,
                normalize_severity(advisory.severity),
                advisory.cvss_score,
                advisory.cvss_vector,
                advisory.epss_score,
                advisory.epss_percentile,
                advisory.exploit_status.value,
                _json_dumps(advisory.exploit_sources),
                advisory.fix_version,
                _json_dumps(advisory.sources),
                advisory.published_date,
                advisory.modified_date,
                _json_dumps(advisory.references),
                fetched_at,
            ),
        )
        self._conn.execute(
            "DELETE FROM affected_packages WHERE advisory_id = ?", (advisory.advisory_id,)
        )
        for pkg in advisory.affected_packages:
            self._conn.execute(
                """
                INSERT INTO affected_packages (
                    advisory_id,
                    ecosystem,
                    name,
                    vulnerable_ranges,
                    fixed_versions,
                    fix_available
                ) VALUES (?, ?, ?, ?, ?, ?)
                """,
                (
                    advisory.advisory_id,
                    pkg.ecosystem,
                    pkg.name,
                    _json_dumps(pkg.vulnerable_ranges),
                    _json_dumps(pkg.fixed_versions),
                    1 if pkg.fixed_versions else 0,
                ),
            )

    def _load_affected_packages(self, advisory_id: str) -> tuple[AffectedPackage, ...]:
        rows = self._conn.execute(
            """
            SELECT ecosystem, name, vulnerable_ranges, fixed_versions
            FROM affected_packages
            WHERE advisory_id = ?
            ORDER BY ecosystem, name
            """,
            (advisory_id,),
        ).fetchall()
        packages: list[AffectedPackage] = []
        for row in rows:
            packages.append(
                AffectedPackage(
                    ecosystem=str(row["ecosystem"]),
                    name=str(row["name"]),
                    vulnerable_ranges=tuple(_json_loads(row["vulnerable_ranges"])),
                    fixed_versions=tuple(_json_loads(row["fixed_versions"])),
                )
            )
        return tuple(packages)

    def find_advisories_for_package(self, ecosystem: str, package_name: str) -> list[Advisory]:
        rows = self._conn.execute(
            """
            SELECT advisory_id
            FROM affected_packages
            WHERE ecosystem = ? AND name = ?
            ORDER BY advisory_id
            """,
            (ecosystem, package_name),
        ).fetchall()
        advisories: list[Advisory] = []
        for row in rows:
            advisory_row = self.get_advisory(str(row["advisory_id"]))
            if advisory_row is not None:
                advisories.append(advisory_row.advisory)
        return advisories

    def iter_cve_ids_needing_epss(self, *, max_age_days: int = 7) -> list[str]:
        cutoff = datetime.now(UTC).timestamp() - (max_age_days * 24 * 60 * 60)
        rows = self._conn.execute(
            """
            SELECT cve_id, fetched_at, epss_score
            FROM advisories
            WHERE cve_id IS NOT NULL
            ORDER BY cve_id
            """
        ).fetchall()
        cve_ids: list[str] = []
        for row in rows:
            cve_id = _nullable_text(row["cve_id"])
            fetched_at = _nullable_text(row["fetched_at"])
            if cve_id is None or fetched_at is None:
                continue
            if row["epss_score"] is None:
                cve_ids.append(cve_id)
                continue
            try:
                fetched_ts = _parse_utc_timestamp(fetched_at).timestamp()
            except ValueError:
                cve_ids.append(cve_id)
                continue
            if fetched_ts < cutoff:
                cve_ids.append(cve_id)
        return cve_ids

    def update_epss_scores(self, scores: Mapping[str, tuple[float, float]]) -> int:
        updated = 0
        for cve_id, (score, percentile) in scores.items():
            cursor = self._conn.execute(
                """
                UPDATE advisories
                SET epss_score = ?, epss_percentile = ?, fetched_at = ?
                WHERE cve_id = ?
                """,
                (score, percentile, utc_now(), cve_id),
            )
            updated += cursor.rowcount
        self._conn.commit()
        return updated

    def iter_cve_ids_for_exploit_check(self, cve_ids: Sequence[str] | None = None) -> list[str]:
        if cve_ids is not None:
            return sorted({cve_id for cve_id in cve_ids if cve_id})
        rows = self._conn.execute(
            "SELECT cve_id FROM advisories WHERE cve_id IS NOT NULL ORDER BY cve_id"
        ).fetchall()
        return [str(row["cve_id"]) for row in rows]

    def update_exploit_statuses(
        self,
        statuses: Mapping[str, tuple[ExploitStatus, Sequence[str]]],
    ) -> int:
        updated = 0
        for cve_id, (status, sources) in statuses.items():
            cursor = self._conn.execute(
                """
                UPDATE advisories
                SET exploit_available = ?, exploit_sources = ?, fetched_at = ?
                WHERE cve_id = ?
                """,
                (status.value, _json_dumps(tuple(sources)), utc_now(), cve_id),
            )
            updated += cursor.rowcount
        self._conn.commit()
        return updated

    def export_to(
        self,
        destination: Path,
        *,
        ecosystems: Sequence[str] | None = None,
        since: str | None = None,
    ) -> None:
        destination.parent.mkdir(parents=True, exist_ok=True)
        if not ecosystems and since is None:
            if destination.resolve(strict=False) == self.path.resolve(strict=False):
                return
            shutil.copy2(self.path, destination)
            return

        with AdvisoryDB(destination) as export_db:
            export_db.clear()
            advisories = self.iter_all_advisories()
            for advisory_row in advisories:
                advisory = advisory_row.advisory
                if ecosystems and not any(
                    pkg.ecosystem in set(ecosystems) for pkg in advisory.affected_packages
                ):
                    continue
                if since is not None:
                    modified = advisory.modified_date or advisory.published_date
                    if modified is None or modified < since:
                        continue
                export_db.upsert_advisories((advisory,))
            for metadata in self.list_sync_metadata():
                export_db.upsert_sync_metadata(
                    source=metadata.source,
                    last_sync=metadata.last_sync,
                    last_cursor=metadata.last_cursor,
                    record_count=metadata.record_count,
                )

    def import_from(
        self,
        source_path: Path,
        *,
        merge: bool = False,
        provenance: AdvisorySnapshotProvenance | None = None,
    ) -> None:
        if not merge:
            if source_path.resolve(strict=False) == self.path.resolve(strict=False):
                return
            self.close()
            self.path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source_path, self.path)
            self._conn = sqlite3.connect(self.path)
            self._conn.row_factory = sqlite3.Row
            self._conn.execute("PRAGMA foreign_keys = ON")
            self.ensure_schema()
            if provenance is not None:
                self.upsert_snapshot_provenance(provenance)
            return
        with AdvisoryDB(source_path) as source_db:
            self.upsert_advisories([row.advisory for row in source_db.iter_all_advisories()])
            for metadata in source_db.list_sync_metadata():
                self.upsert_sync_metadata(
                    source=metadata.source,
                    last_sync=metadata.last_sync,
                    last_cursor=metadata.last_cursor,
                    record_count=metadata.record_count,
                )
        if provenance is not None:
            self.upsert_snapshot_provenance(provenance)

    def list_sync_metadata(self) -> list[SyncMetadata]:
        rows = self._conn.execute(
            "SELECT source, last_sync, last_cursor, record_count FROM sync_metadata ORDER BY source"
        ).fetchall()
        return [
            SyncMetadata(
                source=str(row["source"]),
                last_sync=str(row["last_sync"]),
                last_cursor=_nullable_text(row["last_cursor"]),
                record_count=int(row["record_count"]),
            )
            for row in rows
        ]

    def get_snapshot_provenance(self) -> AdvisorySnapshotProvenance | None:
        row = self._conn.execute(
            """
            SELECT
                source_path,
                snapshot_sha256,
                manifest_path,
                manifest_sha256,
                signature_scheme,
                signature_signer,
                signature_value,
                verified,
                verification_reason,
                imported_at
            FROM advisory_snapshot_provenance
            WHERE id = 1
            """
        ).fetchone()
        if row is None:
            return None
        return AdvisorySnapshotProvenance(
            source_path=_nullable_text(row["source_path"]),
            snapshot_sha256=_nullable_text(row["snapshot_sha256"]),
            manifest_path=_nullable_text(row["manifest_path"]),
            manifest_sha256=_nullable_text(row["manifest_sha256"]),
            signature_scheme=_nullable_text(row["signature_scheme"]),
            signature_signer=_nullable_text(row["signature_signer"]),
            signature_value=_nullable_text(row["signature_value"]),
            verified=bool(row["verified"]),
            verification_reason=_nullable_text(row["verification_reason"]),
            imported_at=str(row["imported_at"]),
        )

    def upsert_snapshot_provenance(self, provenance: AdvisorySnapshotProvenance) -> None:
        self._conn.execute(
            """
            INSERT INTO advisory_snapshot_provenance (
                id,
                source_path,
                snapshot_sha256,
                manifest_path,
                manifest_sha256,
                signature_scheme,
                signature_signer,
                signature_value,
                verified,
                verification_reason,
                imported_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(id) DO UPDATE SET
                source_path = excluded.source_path,
                snapshot_sha256 = excluded.snapshot_sha256,
                manifest_path = excluded.manifest_path,
                manifest_sha256 = excluded.manifest_sha256,
                signature_scheme = excluded.signature_scheme,
                signature_signer = excluded.signature_signer,
                signature_value = excluded.signature_value,
                verified = excluded.verified,
                verification_reason = excluded.verification_reason,
                imported_at = excluded.imported_at
            """,
            (
                1,
                provenance.source_path,
                provenance.snapshot_sha256,
                provenance.manifest_path,
                provenance.manifest_sha256,
                provenance.signature_scheme,
                provenance.signature_signer,
                provenance.signature_value,
                1 if provenance.verified else 0,
                provenance.verification_reason,
                provenance.imported_at,
            ),
        )
        self._conn.commit()

    def clear_snapshot_provenance(self) -> None:
        self._conn.execute("DELETE FROM advisory_snapshot_provenance")
        self._conn.commit()

    def latest_updated_at(self) -> str | None:
        candidates: list[datetime] = []
        for raw_value in (
            self._conn.execute("SELECT MAX(fetched_at) FROM advisories").fetchone(),
            self._conn.execute("SELECT MAX(last_sync) FROM sync_metadata").fetchone(),
        ):
            if raw_value is None:
                continue
            candidate = _nullable_text(raw_value[0])
            if candidate is None:
                continue
            try:
                candidates.append(_parse_utc_timestamp(candidate))
            except ValueError:
                continue
        if not candidates:
            return None
        latest = max(candidates)
        return latest.astimezone(UTC).isoformat().replace("+00:00", "Z")

    def search_advisories(
        self,
        *,
        query: str | None = None,
        ecosystem: str | None = None,
        package_name: str | None = None,
        limit: int = 20,
    ) -> list[AdvisoryRow]:
        normalized_query = (query or "").strip().lower()
        normalized_ecosystem = (ecosystem or "").strip().lower()
        normalized_package = (package_name or "").strip().lower()

        rows = self.iter_all_advisories()
        if normalized_ecosystem or normalized_package:
            rows = [
                row
                for row in rows
                if any(
                    (
                        (not normalized_ecosystem or pkg.ecosystem == normalized_ecosystem)
                        and (not normalized_package or normalized_package in pkg.name.lower())
                    )
                    for pkg in row.advisory.affected_packages
                )
            ]

        if normalized_query:
            filtered: list[AdvisoryRow] = []
            for row in rows:
                advisory = row.advisory
                searchable = [
                    advisory.advisory_id,
                    advisory.cve_id or "",
                    advisory.ghsa_id or "",
                    advisory.title,
                    advisory.description,
                    " ".join(pkg.name for pkg in advisory.affected_packages),
                ]
                if any(normalized_query in field.lower() for field in searchable):
                    filtered.append(row)
            rows = filtered

        return rows[: max(limit, 1)]

    def clear(self) -> None:
        self._conn.execute("DELETE FROM affected_packages")
        self._conn.execute("DELETE FROM advisories")
        self._conn.execute("DELETE FROM sync_metadata")
        self._conn.execute("DELETE FROM advisory_snapshot_provenance")
        self._conn.commit()


def merge_advisories(existing: Advisory | None, incoming: Advisory) -> Advisory:
    if existing is None:
        return incoming

    merged_sources = tuple(sorted(set(existing.sources) | set(incoming.sources)))
    packages_by_key: dict[tuple[str, str], AffectedPackage] = {
        (pkg.ecosystem, pkg.name): pkg for pkg in existing.affected_packages
    }
    incoming_is_ghsa = "ghsa" in incoming.sources
    for pkg in incoming.affected_packages:
        key = (pkg.ecosystem, pkg.name)
        current = packages_by_key.get(key)
        if current is None:
            packages_by_key[key] = pkg
            continue
        vulnerable_ranges = current.vulnerable_ranges
        if (incoming_is_ghsa and pkg.vulnerable_ranges) or not vulnerable_ranges:
            vulnerable_ranges = pkg.vulnerable_ranges
        fixed_versions = tuple(sorted(set(current.fixed_versions) | set(pkg.fixed_versions)))
        packages_by_key[key] = AffectedPackage(
            ecosystem=pkg.ecosystem,
            name=pkg.name,
            vulnerable_ranges=vulnerable_ranges,
            fixed_versions=fixed_versions,
        )

    prefer_nvd_cvss = "nvd" in incoming.sources or "nvd" not in existing.sources
    cvss_score: float | None
    if prefer_nvd_cvss and incoming.cvss_score is not None:
        cvss_score = incoming.cvss_score
        cvss_vector = incoming.cvss_vector
    else:
        cvss_score = existing.cvss_score if existing.cvss_score is not None else incoming.cvss_score
        cvss_vector = existing.cvss_vector or incoming.cvss_vector

    severity = (
        incoming.severity
        if severity_rank(incoming.severity) >= severity_rank(existing.severity)
        else existing.severity
    )
    modified_date = max(
        filter(None, [existing.modified_date, incoming.modified_date]), default=None
    )
    exploit_status = (
        incoming.exploit_status
        if exploit_status_rank(incoming.exploit_status)
        >= exploit_status_rank(existing.exploit_status)
        else existing.exploit_status
    )

    epss_score = incoming.epss_score if incoming.epss_score is not None else existing.epss_score
    epss_percentile = (
        incoming.epss_percentile
        if incoming.epss_percentile is not None
        else existing.epss_percentile
    )
    exploit_sources = tuple(sorted(set(existing.exploit_sources) | set(incoming.exploit_sources)))
    references = tuple(sorted(set(existing.references) | set(incoming.references)))
    cwe_ids = tuple(sorted(set(existing.cwe_ids) | set(incoming.cwe_ids)))
    fix_version = incoming.fix_version or existing.fix_version
    fix_available = existing.fix_available or incoming.fix_available or bool(fix_version)

    return Advisory(
        advisory_id=existing.advisory_id,
        cve_id=existing.cve_id or incoming.cve_id,
        ghsa_id=existing.ghsa_id or incoming.ghsa_id,
        cwe_ids=cwe_ids,
        title=incoming.title if len(incoming.title) > len(existing.title) else existing.title,
        description=(
            incoming.description
            if len(incoming.description) > len(existing.description)
            else existing.description
        ),
        affected_packages=tuple(
            sorted(packages_by_key.values(), key=lambda item: (item.ecosystem, item.name))
        ),
        severity=severity,
        cvss_score=cvss_score,
        cvss_vector=cvss_vector,
        epss_score=epss_score,
        epss_percentile=epss_percentile,
        exploit_status=exploit_status,
        exploit_sources=exploit_sources,
        fix_available=fix_available,
        fix_version=fix_version,
        published_date=existing.published_date or incoming.published_date,
        modified_date=modified_date,
        sources=merged_sources,
        references=references,
    )


def _advisory_from_row(row: sqlite3.Row, *, packages: Sequence[AffectedPackage]) -> Advisory:
    return Advisory(
        advisory_id=str(row["advisory_id"]),
        cve_id=_nullable_text(row["cve_id"]),
        ghsa_id=_nullable_text(row["ghsa_id"]),
        cwe_ids=tuple(_json_loads(row["cwe_ids"])),
        title=_nullable_text(row["title"]) or "",
        description=_nullable_text(row["description"]) or "",
        affected_packages=tuple(packages),
        severity=normalize_severity(_nullable_text(row["severity"])),
        cvss_score=_nullable_float(row["cvss_score"]),
        cvss_vector=_nullable_text(row["cvss_vector"]),
        epss_score=_nullable_float(row["epss_score"]),
        epss_percentile=_nullable_float(row["epss_percentile"]),
        exploit_status=ExploitStatus(
            _nullable_text(row["exploit_available"]) or ExploitStatus.NONE.value
        ),
        exploit_sources=tuple(_json_loads(row["exploit_sources"])),
        fix_available=bool(_nullable_text(row["fix_version"]))
        or any(pkg.fixed_versions for pkg in packages),
        fix_version=_nullable_text(row["fix_version"]),
        published_date=_nullable_text(row["published_date"]),
        modified_date=_nullable_text(row["modified_date"]),
        sources=tuple(_json_loads(row["source"])),
        references=tuple(_json_loads(row["references"])),
    )


def _parse_utc_timestamp(value: str) -> datetime:
    normalized = value.replace("Z", "+00:00")
    return datetime.fromisoformat(normalized)


def _nullable_text(value: object) -> str | None:
    if not isinstance(value, str):
        return None
    stripped = value.strip()
    return stripped or None


def _nullable_float(value: object) -> float | None:
    if value is None:
        return None
    if isinstance(value, float):
        return value
    if isinstance(value, int):
        return float(value)
    if isinstance(value, str):
        stripped = value.strip()
        if not stripped:
            return None
        return float(stripped)
    return None


def _json_dumps(value: Sequence[object] | object) -> str:
    return json.dumps(value, separators=(",", ":"), sort_keys=True)


def _json_loads(value: object) -> list[str]:
    if not isinstance(value, str):
        return []
    try:
        loaded = json.loads(value)
    except json.JSONDecodeError:
        return []
    if not isinstance(loaded, list):
        return []
    return [item for item in loaded if isinstance(item, str)]


def get_advisory_db_status(
    db_path: Path,
    *,
    stale_after_days: int = _DEFAULT_STALE_AFTER_DAYS,
    now: datetime | None = None,
) -> AdvisoryDBStatus:
    resolved = db_path.resolve(strict=False)
    if stale_after_days < 1:
        stale_after_days = _DEFAULT_STALE_AFTER_DAYS
    reference_now = now or datetime.now(UTC)

    if not resolved.exists():
        return AdvisoryDBStatus(
            path=resolved,
            exists=False,
            schema_version=None,
            advisory_count=0,
            affected_package_count=0,
            sources=(),
            last_updated=None,
            checksum_sha256=None,
            freshness="missing",
            stale_after_days=stale_after_days,
            age_days=None,
            trust_state="missing",
            provenance_verified=None,
            provenance_signature_scheme=None,
            provenance_signature_signer=None,
            provenance_snapshot_sha256=None,
            provenance_manifest_sha256=None,
            provenance_imported_at=None,
            warnings=(
                "advisory database file is missing; run `piranesi advisory update` "
                "or `piranesi advisory import`.",
            ),
        )

    checksum_sha256 = _file_sha256(resolved)
    try:
        with AdvisoryDB(resolved) as db:
            advisory_count = db.advisory_count()
            affected_package_count = db.affected_package_count()
            sync_metadata = db.list_sync_metadata()
            provenance = db.get_snapshot_provenance()
            sources = tuple(sorted(metadata.source for metadata in sync_metadata))
            last_updated = db.latest_updated_at()
            schema_version = db.schema_version()
    except (OSError, sqlite3.DatabaseError) as exc:
        return AdvisoryDBStatus(
            path=resolved,
            exists=True,
            schema_version=None,
            advisory_count=0,
            affected_package_count=0,
            sources=(),
            last_updated=None,
            checksum_sha256=checksum_sha256,
            freshness="stale",
            stale_after_days=stale_after_days,
            age_days=None,
            trust_state="unknown",
            provenance_verified=None,
            provenance_signature_scheme=None,
            provenance_signature_signer=None,
            provenance_snapshot_sha256=None,
            provenance_manifest_sha256=None,
            provenance_imported_at=None,
            warnings=(f"failed to read advisory database: {exc}",),
        )

    warnings: list[str] = []
    freshness = "fresh"
    age_days: float | None = None
    trust_state = "unknown"
    provenance_verified: bool | None = None
    provenance_signature_scheme: str | None = None
    provenance_signature_signer: str | None = None
    provenance_snapshot_sha256: str | None = None
    provenance_manifest_sha256: str | None = None
    provenance_imported_at: str | None = None

    if advisory_count == 0:
        freshness = "empty"
        warnings.append("advisory database is empty; dependency advisory matches will be limited.")
    if last_updated is None:
        freshness = "stale"
        warnings.append("advisory database has no sync timestamp metadata.")
    else:
        try:
            updated_at = _parse_utc_timestamp(last_updated)
            age_days = (reference_now - updated_at).total_seconds() / 86_400
            if age_days > float(stale_after_days):
                freshness = "stale"
                warnings.append(
                    "advisory database is stale "
                    f"({age_days:.1f} days old; threshold {stale_after_days} days)."
                )
        except ValueError:
            freshness = "stale"
            warnings.append(f"advisory database has an invalid timestamp: {last_updated!r}.")
    if schema_version != _ADVISORY_DB_SCHEMA_VERSION:
        warnings.append(
            "advisory database schema version mismatch "
            f"(db={schema_version}, expected={_ADVISORY_DB_SCHEMA_VERSION})."
        )
    if provenance is None:
        trust_state = "unsigned"
        warnings.append("advisory database has no snapshot provenance metadata.")
    else:
        provenance_verified = provenance.verified
        provenance_signature_scheme = provenance.signature_scheme
        provenance_signature_signer = provenance.signature_signer
        provenance_snapshot_sha256 = provenance.snapshot_sha256
        provenance_manifest_sha256 = provenance.manifest_sha256
        provenance_imported_at = provenance.imported_at
        if provenance.verified:
            trust_state = "verified"
        elif provenance.signature_value:
            trust_state = "unverified"
            detail = provenance.verification_reason or "verification failed"
            warnings.append(f"snapshot signature verification failed: {detail}")
        else:
            trust_state = "unsigned"
            detail = provenance.verification_reason or "snapshot is unsigned"
            warnings.append(f"snapshot signature missing: {detail}")

    return AdvisoryDBStatus(
        path=resolved,
        exists=True,
        schema_version=schema_version,
        advisory_count=advisory_count,
        affected_package_count=affected_package_count,
        sources=sources,
        last_updated=last_updated,
        checksum_sha256=checksum_sha256,
        freshness=freshness,
        stale_after_days=stale_after_days,
        age_days=age_days,
        trust_state=trust_state,
        provenance_verified=provenance_verified,
        provenance_signature_scheme=provenance_signature_scheme,
        provenance_signature_signer=provenance_signature_signer,
        provenance_snapshot_sha256=provenance_snapshot_sha256,
        provenance_manifest_sha256=provenance_manifest_sha256,
        provenance_imported_at=provenance_imported_at,
        warnings=tuple(warnings),
    )


def _file_sha256(path: Path) -> str | None:
    try:
        payload = path.read_bytes()
    except OSError:
        return None
    return _hash_bytes(payload)


def _hash_bytes(payload: bytes) -> str:
    return sha256(payload).hexdigest()
