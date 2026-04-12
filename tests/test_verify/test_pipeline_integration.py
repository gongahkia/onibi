from __future__ import annotations

import shutil
import subprocess
from pathlib import Path

import pytest

import piranesi.verify.sandbox as sandbox
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource, TaintStep
from piranesi.verify.confirm import build_baseline_payload, confirm_exploit, confirm_responses
from piranesi.verify.constraints import extract_exploit_template
from piranesi.verify.reproducer import generate_reproducer_script, write_reproducer_script
from piranesi.verify.solver import solve_exploit_template

XSS_APP_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "verify" / "xss_app"


def _docker_available() -> bool:
    docker_binary = shutil.which("docker")
    if docker_binary is None:
        return False
    return (
        subprocess.run(
            [docker_binary, "info"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        ).returncode
        == 0
    )


docker_required = pytest.mark.skipif(not _docker_available(), reason="Docker not available")


@docker_required
@pytest.mark.docker
@pytest.mark.integration
def test_xss_pipeline_confirms_payload_in_docker(tmp_path: Path) -> None:
    finding = CandidateFinding(
        id="finding-xss-docker",
        vuln_class="CWE-79",
        source=TaintSource(
            location=SourceLocation(
                file=str(XSS_APP_DIR / "app.js"),
                line=9,
                column=3,
                snippet="const query = req.query.q;",
            ),
            source_type="req.query.q",
            data_categories=["search_query"],
            parameter_name="q",
        ),
        sink=TaintSink(
            location=SourceLocation(
                file=str(XSS_APP_DIR / "app.js"),
                line=10,
                column=3,
                snippet="res.send(`<html><body>Results for: ${query}</body></html>`);",
            ),
            sink_type="html_output",
            api_name="res.send",
        ),
        taint_path=[
            TaintStep(
                location=SourceLocation(
                    file=str(XSS_APP_DIR / "app.js"),
                    line=10,
                    column=3,
                    snippet="res.send(`<html><body>Results for: ${query}</body></html>`);",
                ),
                operation="call_arg",
                taint_state="tainted",
            )
        ],
        path_conditions=[],
        confidence=0.95,
        severity="medium",
    )

    template = extract_exploit_template(finding)
    solve_result = solve_exploit_template(template)

    assert solve_result.status == "SAT"
    payload = solve_result.solutions[0].payload
    image = sandbox.build_image(str(XSS_APP_DIR))
    client = sandbox._docker_client()
    container = None
    network_ids: list[str] = []
    try:
        container = sandbox._start_container(client, image)
        network_ids = sandbox._container_network_ids(container)
        try:
            host_port = sandbox._get_host_port(container)
        except RuntimeError:
            internal_port = sandbox._image_internal_port(client, image)
            assert sandbox._wait_for_ready_in_container(container, internal_port)
            baseline_payload = build_baseline_payload(payload, vuln_class="CWE-79")
            baseline_response = sandbox._fire_payload_in_container(
                container, baseline_payload, internal_port=internal_port
            )
            exploit_response = sandbox._fire_payload_in_container(
                container, payload, internal_port=internal_port
            )
            confirmation = confirm_responses("CWE-79", payload, baseline_response, exploit_response)
            capture = sandbox.capture_results(container, exploit_response)
        else:
            assert sandbox.wait_for_ready(host_port)
            confirmation = confirm_exploit("CWE-79", payload, host_port)
            capture = sandbox.capture_results(container, confirmation.exploit_response)
    finally:
        if container is not None:
            sandbox._teardown_container(container)
        sandbox._teardown_networks(client, network_ids)
        sandbox._close_client(client)

    assert confirmation.level == "CONFIRMED"
    assert capture.http_response.status_code == 200
    assert payload.payload_values["q"] in capture.http_response.body
    assert "<script>" in capture.http_response.body
    assert capture.network_isolated is True
    assert capture.error is None

    script = generate_reproducer_script(
        finding,
        payload=payload,
        target_path=XSS_APP_DIR,
    )
    script_path = write_reproducer_script(tmp_path / "xss-repro.sh", script)
    subprocess.run(["bash", "-n", str(script_path)], check=True)
