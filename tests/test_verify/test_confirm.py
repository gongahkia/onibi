from __future__ import annotations

from piranesi.verify.confirm import build_baseline_payload, confirm_exploit, confirm_responses
from piranesi.verify.sandbox import ExploitResult, SynthesizedPayload


def test_build_baseline_payload_replaces_malicious_values() -> None:
    payload = SynthesizedPayload(
        method="GET",
        url="/files/..%2F..%2F..%2Fetc%2Fpasswd",
        headers={},
        body=None,
        payload_values={"file": "../../../etc/passwd"},
        encoding="path",
    )

    baseline = build_baseline_payload(payload, vuln_class="CWE-22")

    assert baseline.url == "/files/piranesi.txt"
    assert baseline.headers == {}
    assert baseline.body is None
    assert baseline.payload_values == {"file": "piranesi.txt"}


def test_confirm_exploit_fires_baseline_before_exploit() -> None:
    payload = SynthesizedPayload(
        method="GET",
        url="/search",
        body={"q": "' OR 1=1--"},
        payload_values={"q": "' OR 1=1--"},
        encoding="query",
    )
    calls: list[SynthesizedPayload] = []
    responses = iter(
        [
            _response(status=200, body="[]"),
            _response(status=500, body="SQL syntax error near 'OR 1=1'"),
        ]
    )

    def _executor(request_payload: SynthesizedPayload, host_port: int) -> ExploitResult:
        assert host_port == 3000
        calls.append(request_payload)
        return next(responses)

    result = confirm_exploit("CWE-89", payload, 3000, fire_request=_executor)

    assert result.level == "CONFIRMED"
    assert calls[0].body == {"q": "piranesi"}
    assert calls[1].body == {"q": "' OR 1=1--"}


def test_sqli_confirmed_on_sql_error_message() -> None:
    result = confirm_responses(
        "CWE-89",
        _sqli_payload(),
        _response(status=200, body="[]"),
        _response(status=500, body="SQLite error: syntax error near UNION SELECT"),
    )

    assert result.level == "CONFIRMED"


def test_sqli_likely_on_ambiguous_response_difference() -> None:
    result = confirm_responses(
        "CWE-89",
        _sqli_payload(),
        _response(status=200, body="[]"),
        _response(status=302, body="redirecting to /results"),
    )

    assert result.level == "LIKELY"


def test_sqli_not_vulnerable_when_response_matches_baseline() -> None:
    result = confirm_responses(
        "CWE-89",
        _sqli_payload(),
        _response(status=200, body="[]"),
        _response(status=200, body="[]"),
    )

    assert result.level == "UNVERIFIABLE"


def test_xss_confirmed_on_unescaped_script_tag() -> None:
    result = confirm_responses(
        "CWE-79",
        _xss_payload(),
        _response(status=200, body="<div>hello</div>"),
        _response(status=200, body="<div><script>alert(1)</script></div>"),
    )

    assert result.level == "CONFIRMED"


def test_xss_likely_on_transformed_markup() -> None:
    payload = SynthesizedPayload(
        method="GET",
        url="/search",
        body={"q": '"><img src=x onerror=alert(1)>'},
        payload_values={"q": '"><img src=x onerror=alert(1)>'},
        encoding="query",
    )
    result = confirm_responses(
        "CWE-79",
        payload,
        _response(status=200, body="<div>safe</div>"),
        _response(status=200, body='<div><img src="x" title="alert(1)"></div>'),
    )

    assert result.level == "LIKELY"


def test_xss_not_vulnerable_when_payload_is_html_encoded() -> None:
    result = confirm_responses(
        "CWE-79",
        _xss_payload(),
        _response(status=200, body="<div>hello</div>"),
        _response(status=200, body="<div>&lt;script&gt;alert(1)&lt;/script&gt;</div>"),
    )

    assert result.level == "UNVERIFIABLE"


def test_cmdi_confirmed_on_id_output() -> None:
    result = confirm_responses(
        "CWE-78",
        _cmdi_payload(),
        _response(status=200, body="ok"),
        _response(status=200, body="uid=1000(node) gid=1000(node) groups=1000(node)"),
    )

    assert result.level == "CONFIRMED"


def test_cmdi_likely_on_response_difference_without_output() -> None:
    result = confirm_responses(
        "CWE-78",
        _cmdi_payload(),
        _response(status=200, body="ok"),
        _response(status=500, body="internal error"),
    )

    assert result.level == "LIKELY"


def test_cmdi_not_vulnerable_when_no_command_output_is_observed() -> None:
    result = confirm_responses(
        "CWE-78",
        _cmdi_payload(),
        _response(status=200, body="ok"),
        _response(status=200, body="ok"),
    )

    assert result.level == "UNVERIFIABLE"


def test_path_traversal_confirmed_on_passwd_contents() -> None:
    result = confirm_responses(
        "CWE-22",
        _path_traversal_payload(),
        _response(status=404, body="not found"),
        _response(status=200, body="root:x:0:0:root:/root:/bin/bash"),
    )

    assert result.level == "CONFIRMED"


def test_path_traversal_likely_when_baseline_404_becomes_200() -> None:
    result = confirm_responses(
        "CWE-22",
        _path_traversal_payload(),
        _response(status=404, body="not found"),
        _response(status=200, body="application config"),
    )

    assert result.level == "LIKELY"


def test_path_traversal_not_vulnerable_when_request_stays_404() -> None:
    result = confirm_responses(
        "CWE-22",
        _path_traversal_payload(),
        _response(status=404, body="not found"),
        _response(status=404, body="not found"),
    )

    assert result.level == "UNVERIFIABLE"


def _response(
    *,
    status: int,
    body: str,
    elapsed_ms: float = 50.0,
    error: str | None = None,
) -> ExploitResult:
    return ExploitResult(
        status_code=status,
        headers={},
        body=body,
        elapsed_ms=elapsed_ms,
        request={},
        error=error,
    )


def _sqli_payload() -> SynthesizedPayload:
    return SynthesizedPayload(
        method="GET",
        url="/search",
        body={"q": "' UNION SELECT NULL,NULL--"},
        payload_values={"q": "' UNION SELECT NULL,NULL--"},
        encoding="query",
    )


def _xss_payload() -> SynthesizedPayload:
    return SynthesizedPayload(
        method="GET",
        url="/search",
        body={"q": "<script>alert(1)</script>"},
        payload_values={"q": "<script>alert(1)</script>"},
        encoding="query",
    )


def _cmdi_payload() -> SynthesizedPayload:
    return SynthesizedPayload(
        method="GET",
        url="/run",
        headers={"X-Command": "; id"},
        body=None,
        payload_values={"x-command": "; id"},
        encoding="json",
    )


def _path_traversal_payload() -> SynthesizedPayload:
    return SynthesizedPayload(
        method="GET",
        url="/files/..%2F..%2F..%2Fetc%2Fpasswd",
        body=None,
        payload_values={"file": "../../../etc/passwd"},
        encoding="path",
    )
