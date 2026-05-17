from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest

import piranesi.verify.sandbox as sandbox


class FakeImage:
    def __init__(
        self, *, image_id: str = "sha256:test", labels: dict[str, str] | None = None
    ) -> None:
        self.id = image_id
        self.labels = labels or {}
        self.attrs = {"Config": {"Labels": dict(self.labels)}}


class FakeNetwork:
    def __init__(self, network_id: str) -> None:
        self.id = network_id
        self.removed = False

    def remove(self) -> None:
        self.removed = True


class FakeContainer:
    def __init__(self, *, network_id: str, host_port: str = "49152") -> None:
        self.id = "container-123"
        self.attrs: dict[str, Any] = {
            "NetworkSettings": {
                "Ports": {"4567/tcp": [{"HostPort": host_port}]},
                "Networks": {"sandbox": {"NetworkID": network_id}},
            },
            "HostConfig": {"Binds": None},
            "Mounts": [],
            "State": {"ExitCode": 0},
        }
        self.stop_calls: list[int] = []
        self.remove_calls: list[bool] = []

    def reload(self) -> None:
        return None

    def logs(self, *, stdout: bool, stderr: bool) -> bytes:
        if stdout and not stderr:
            return b"stdout"
        if stderr and not stdout:
            return b"stderr"
        return b"stdout\nstderr"

    def diff(self) -> list[dict[str, object]]:
        return [{"Kind": 1, "Path": "/tmp/pwned.txt"}]  # noqa: S108

    def stop(self, *, timeout: int) -> None:
        self.stop_calls.append(timeout)

    def remove(self, *, force: bool) -> None:
        self.remove_calls.append(force)


class FakeImagesForBuild:
    def __init__(self) -> None:
        self.captured: dict[str, Any] = {}

    def build(
        self,
        *,
        path: str,
        dockerfile: str,
        tag: str,
        rm: bool,
        forcerm: bool,
    ) -> tuple[FakeImage, list[dict[str, str]]]:
        build_root = Path(path)
        self.captured = {
            "path": build_root,
            "dockerfile": dockerfile,
            "tag": tag,
            "dockerfile_text": (build_root / dockerfile).read_text(encoding="utf-8"),
            "package_json": json.loads((build_root / "package.json").read_text(encoding="utf-8")),
            "files": {
                file_path.relative_to(build_root).as_posix()
                for file_path in build_root.rglob("*")
                if file_path.is_file()
            },
            "rm": rm,
            "forcerm": forcerm,
        }
        return FakeImage(image_id="sha256:built"), [{"stream": "Step 1/6 : FROM node:20-slim"}]


class FakeImagesForRun:
    def __init__(self, port: int) -> None:
        self._port = port

    def get(self, image: str) -> FakeImage:
        return FakeImage(labels={sandbox.INTERNAL_PORT_LABEL: str(self._port)})


class FakeNetworks:
    def __init__(self, network: FakeNetwork) -> None:
        self._network = network
        self.create_calls: list[dict[str, Any]] = []

    def create(
        self, name: str, *, driver: str, internal: bool, labels: dict[str, str]
    ) -> FakeNetwork:
        self.create_calls.append(
            {"name": name, "driver": driver, "internal": internal, "labels": labels}
        )
        return self._network

    def get(self, network_id: str) -> FakeNetwork:
        assert network_id == self._network.id
        return self._network


class FakeContainers:
    def __init__(self, container: FakeContainer) -> None:
        self._container = container
        self.run_kwargs: dict[str, Any] | None = None

    def run(self, **kwargs: Any) -> FakeContainer:
        self.run_kwargs = kwargs
        return self._container


class FakeBuildClient:
    def __init__(self) -> None:
        self.images = FakeImagesForBuild()
        self.closed = False

    def close(self) -> None:
        self.closed = True


class FakeRunClient:
    def __init__(self, *, port: int = 4567) -> None:
        self.network = FakeNetwork("network-123")
        self.container = FakeContainer(network_id=self.network.id)
        self.images = FakeImagesForRun(port)
        self.networks = FakeNetworks(self.network)
        self.containers = FakeContainers(self.container)
        self.closed = False

    def close(self) -> None:
        self.closed = True


@pytest.mark.docker
def test_build_image_generates_hardened_dockerfile(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    target = tmp_path / "target"
    target.mkdir()
    (target / "package.json").write_text(
        json.dumps({"name": "fixture-app", "dependencies": {"express": "^4.19.0"}}),
        encoding="utf-8",
    )
    (target / "server.js").write_text(
        "const express = require('express');\n"
        "const app = express();\n"
        "app.listen(process.env.PORT || 4310);\n",
        encoding="utf-8",
    )
    (target / "Dockerfile").write_text("FROM attacker/owned:latest\n", encoding="utf-8")
    (target / "docker-compose.yml").write_text("services: {}\n", encoding="utf-8")
    (target / ".npmrc").write_text("registry=https://evil.invalid\n", encoding="utf-8")
    (target / ".env").write_text("SECRET=1\n", encoding="utf-8")

    client = FakeBuildClient()
    monkeypatch.setattr(sandbox, "_docker_client", lambda: client)

    image = sandbox.build_image(str(target))

    assert image.startswith("piranesi-target:")
    assert client.closed is True
    assert client.images.captured["dockerfile"] == sandbox.DOCKERFILE_NAME
    assert "FROM node:20-slim" in client.images.captured["dockerfile_text"]
    assert (
        "npm install --production --ignore-scripts --registry https://registry.npmjs.org/"
        in client.images.captured["dockerfile_text"]
    )
    assert (
        "RUN rm -f Dockerfile* docker-compose* .npmrc .env"
        in client.images.captured["dockerfile_text"]
    )
    assert 'CMD ["npm", "start"]' in client.images.captured["dockerfile_text"]
    assert "EXPOSE 4310" in client.images.captured["dockerfile_text"]
    assert client.images.captured["package_json"]["scripts"]["start"] == "node server.js"
    assert "Dockerfile" not in client.images.captured["files"]
    assert "docker-compose.yml" not in client.images.captured["files"]
    assert ".npmrc" not in client.images.captured["files"]
    assert ".env" not in client.images.captured["files"]


@pytest.mark.docker
def test_start_container_uses_hardened_security_config(monkeypatch: pytest.MonkeyPatch) -> None:
    client = FakeRunClient(port=4567)
    monkeypatch.setattr(sandbox, "_docker_client", lambda: client)

    container = sandbox.start_container("piranesi-target:test")

    assert container is client.container  # type: ignore[comparison-overlap]
    assert len(client.networks.create_calls) == 1
    create_call = client.networks.create_calls[0]
    assert create_call["name"].startswith("piranesi-sandbox-")
    assert create_call["driver"] == "bridge"
    assert create_call["internal"] is True
    assert create_call["labels"] == {sandbox.DOCKER_LABEL: sandbox.DOCKER_LABEL_VALUE}

    run_kwargs = client.containers.run_kwargs
    assert run_kwargs is not None
    assert run_kwargs["network"] == client.network.id
    assert run_kwargs["read_only"] is True
    assert run_kwargs["tmpfs"] == {"/tmp": "size=64m"}  # noqa: S108
    assert run_kwargs["cap_drop"] == ["ALL"]
    assert run_kwargs["security_opt"] == ["no-new-privileges"]
    assert run_kwargs["mem_limit"] == "512m"
    assert run_kwargs["cpu_quota"] == 100000
    assert run_kwargs["pids_limit"] == 256
    assert run_kwargs["user"] == "node"
    assert run_kwargs["privileged"] is False
    assert run_kwargs["ports"] == {"4567/tcp": None}
    assert run_kwargs["environment"] == {"NODE_ENV": "production", "PORT": "4567"}
    assert run_kwargs["log_config"] == {
        "type": "json-file",
        "config": {"max-size": "10m", "max-file": "1"},
    }
    assert "volumes" not in run_kwargs
    assert "mounts" not in run_kwargs


@pytest.mark.docker
def test_run_in_sandbox_tears_down_container_and_network_on_failure(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    client = FakeRunClient(port=4567)
    payload = sandbox.SynthesizedPayload(method="GET", url="/health")

    monkeypatch.setattr(sandbox, "_docker_client", lambda: client)
    monkeypatch.setattr(
        sandbox, "_build_image", lambda docker_client, target_path: "piranesi-target:test"
    )
    monkeypatch.setattr(sandbox, "_start_container", lambda docker_client, image: client.container)
    monkeypatch.setattr(sandbox, "_get_host_port", lambda container: 49152)
    monkeypatch.setattr(
        sandbox, "wait_for_ready", lambda host_port, max_wait=sandbox.DEFAULT_READY_WAIT: True
    )

    def _boom(request_payload: sandbox.SynthesizedPayload, host_port: int) -> sandbox.ExploitResult:
        raise RuntimeError("request failed")

    monkeypatch.setattr(sandbox, "fire_payload", _boom)

    with pytest.raises(RuntimeError, match="request failed"):
        sandbox.run_in_sandbox("/tmp/target", [payload])  # noqa: S108

    assert client.container.stop_calls == [5]
    assert client.container.remove_calls == [True]
    assert client.network.removed is True
    assert client.closed is True


def test_run_in_sandbox_uses_target_profile_without_docker(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    payload = sandbox.SynthesizedPayload(method="GET", url="/health", encoding="query")
    profile = sandbox.TargetLaunchProfile(
        name="local-service",
        base_url="http://127.0.0.1:4010",
        startup_timeout_seconds=5,
        readiness_url="/health",
        teardown="never",
    )
    monkeypatch.setattr(
        sandbox,
        "_wait_for_profile_ready",
        lambda **_kwargs: (True, None),
    )
    monkeypatch.setattr(
        sandbox,
        "fire_payload",
        lambda request_payload, host_port, *, base_url=None: sandbox.ExploitResult(
            status_code=200,
            headers={},
            body="ok",
            elapsed_ms=12.0,
            request={
                "method": request_payload.method,
                "url": request_payload.url,
                "base_url": base_url,
                "host_port": host_port,
            },
            error=None,
        ),
    )
    monkeypatch.setattr(
        sandbox,
        "_docker_client",
        lambda: (_ for _ in ()).throw(AssertionError("docker path should not be used")),
    )

    captures = sandbox.run_in_sandbox(
        str(tmp_path),
        [payload],
        target_profile=profile,
        logs_base_dir=tmp_path,
    )

    assert len(captures) == 1
    capture = captures[0]
    assert capture.http_response.status_code == 200
    assert capture.network_isolated is False
    assert capture.launch_profile == "local-service"
