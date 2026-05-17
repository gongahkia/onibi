from __future__ import annotations

from piranesi.advisory.sources.ghsa import parse_ghsa_response
from piranesi.advisory.sources.go_vuln import fetch_go_vuln_advisories
from piranesi.advisory.sources.nvd import parse_nvd_cve_item
from piranesi.advisory.sources.osv import parse_osv_advisory


def test_nvd_parse_cve_item_extracts_cvss_and_cwe() -> None:
    advisory = parse_nvd_cve_item(
        {
            "id": "CVE-2026-1111",
            "published": "2026-04-01T00:00:00.000Z",
            "lastModified": "2026-04-02T00:00:00.000Z",
            "descriptions": [{"lang": "en", "value": "Prototype Pollution in lodash"}],
            "references": [{"url": "https://nvd.nist.gov/vuln/detail/CVE-2026-1111"}],
            "weaknesses": [{"description": [{"lang": "en", "value": "CWE-1321"}]}],
            "metrics": {
                "cvssMetricV40": [
                    {
                        "baseSeverity": "CRITICAL",
                        "cvssData": {
                            "baseScore": 9.8,
                            "vectorString": (
                                "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H"
                            ),
                        },
                    }
                ],
                "cvssMetricV31": [
                    {
                        "baseSeverity": "HIGH",
                        "cvssData": {
                            "baseScore": 8.8,
                            "vectorString": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
                        },
                    }
                ],
            },
            "configurations": [
                {
                    "nodes": [
                        {
                            "cpeMatch": [
                                {
                                    "criteria": "cpe:2.3:a:lodash:lodash:*:*:*:*:*:*:*:*",
                                    "versionEndExcluding": "4.17.21",
                                }
                            ]
                        }
                    ]
                }
            ],
        }
    )

    assert advisory is not None
    assert advisory.advisory_id == "CVE-2026-1111"
    assert advisory.cvss_score == 9.8
    assert advisory.cvss_vector.startswith("CVSS:4.0/")
    assert advisory.severity == "critical"
    assert advisory.cwe_ids == ("CWE-1321",)
    assert advisory.fix_version == "4.17.21"


def test_ghsa_parse_graphql_response() -> None:
    advisories, cursor, has_next = parse_ghsa_response(
        {
            "data": {
                "securityAdvisories": {
                    "pageInfo": {"hasNextPage": False, "endCursor": "cursor_1"},
                    "nodes": [
                        {
                            "ghsaId": "GHSA-fvqr-27wr-82fm",
                            "summary": "Prototype Pollution in lodash",
                            "description": "Improper input validation.",
                            "severity": "HIGH",
                            "publishedAt": "2026-04-01T00:00:00Z",
                            "updatedAt": "2026-04-02T00:00:00Z",
                            "identifiers": [
                                {"type": "GHSA", "value": "GHSA-fvqr-27wr-82fm"},
                                {"type": "CVE", "value": "CVE-2026-1111"},
                            ],
                            "references": [
                                {"url": "https://github.com/advisories/GHSA-fvqr-27wr-82fm"}
                            ],
                            "cvss": {
                                "score": 8.8,
                                "vectorString": "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
                            },
                            "cwes": {"nodes": [{"cweId": "CWE-1321"}]},
                            "vulnerabilities": {
                                "nodes": [
                                    {
                                        "severity": "HIGH",
                                        "vulnerableVersionRange": "< 4.17.21",
                                        "package": {"ecosystem": "NPM", "name": "lodash"},
                                        "firstPatchedVersion": {"identifier": "4.17.21"},
                                    }
                                ]
                            },
                        }
                    ],
                }
            }
        }
    )

    assert cursor == "cursor_1"
    assert has_next is False
    assert len(advisories) == 1
    assert advisories[0].cve_id == "CVE-2026-1111"
    assert advisories[0].affected_packages[0].vulnerable_ranges == ("< 4.17.21",)


def test_osv_parse_response() -> None:
    advisory = parse_osv_advisory(
        {
            "id": "OSV-2026-1",
            "aliases": ["CVE-2026-1111", "GHSA-fvqr-27wr-82fm"],
            "summary": "Prototype Pollution in lodash",
            "details": "Improper input validation.",
            "published": "2026-04-01T00:00:00Z",
            "modified": "2026-04-02T00:00:00Z",
            "references": [{"url": "https://osv.dev/vulnerability/OSV-2026-1"}],
            "affected": [
                {
                    "package": {"ecosystem": "npm", "name": "lodash"},
                    "ranges": [
                        {
                            "type": "SEMVER",
                            "events": [{"introduced": "0"}, {"fixed": "4.17.21"}],
                        }
                    ],
                }
            ],
            "database_specific": {"cwe_ids": ["CWE-1321"]},
        }
    )

    assert advisory is not None
    assert advisory.advisory_id == "CVE-2026-1111"
    assert advisory.ghsa_id == "GHSA-fvqr-27wr-82fm"
    assert advisory.affected_packages[0].fixed_versions == ("4.17.21",)


def test_go_vuln_fetches_incremental_entries() -> None:
    class _FakeResponse:
        def __init__(self, payload: object) -> None:
            self._payload = payload

        def raise_for_status(self) -> None:
            return None

        def json(self) -> object:
            return self._payload

    class _FakeSession:
        def get(self, url: str, timeout: int = 60):  # type: ignore[no-untyped-def]
            _ = timeout
            if url.endswith("/index/modules.json"):
                return _FakeResponse(
                    [
                        {
                            "path": "github.com/example/lib",
                            "vulns": [{"id": "GO-2026-0001", "modified": "2026-04-02T00:00:00Z"}],
                        }
                    ]
                )
            if url.endswith("/ID/GO-2026-0001.json"):
                return _FakeResponse(
                    {
                        "id": "GO-2026-0001",
                        "aliases": ["CVE-2026-9999"],
                        "summary": "Go advisory",
                        "details": "Details",
                        "published": "2026-04-01T00:00:00Z",
                        "modified": "2026-04-02T00:00:00Z",
                        "affected": [
                            {
                                "package": {
                                    "ecosystem": "Go",
                                    "name": "github.com/example/lib",
                                },
                                "ranges": [
                                    {
                                        "type": "SEMVER",
                                        "events": [
                                            {"introduced": "0"},
                                            {"fixed": "v1.2.4"},
                                        ],
                                    }
                                ],
                            }
                        ],
                    }
                )
            raise AssertionError(f"unexpected URL: {url}")

    advisories, cursor = fetch_go_vuln_advisories(
        since="2026-04-01T00:00:00Z",
        session=_FakeSession(),  # type: ignore[arg-type]
    )

    assert cursor == "2026-04-02T00:00:00Z"
    assert len(advisories) == 1
    assert advisories[0].advisory_id == "CVE-2026-9999"
    assert advisories[0].affected_packages[0].ecosystem == "go"
