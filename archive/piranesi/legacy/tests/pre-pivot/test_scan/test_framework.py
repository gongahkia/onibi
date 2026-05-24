from __future__ import annotations

import json
from pathlib import Path

from piranesi.scan.framework import detect_frameworks, discover_nextjs_routes, resolve_frameworks


def test_detect_frameworks_finds_fastify_dependency(tmp_path: Path) -> None:
    (tmp_path / "package.json").write_text(
        json.dumps({"dependencies": {"fastify": "^5.0.0"}}),
        encoding="utf-8",
    )

    assert detect_frameworks(tmp_path) == ("fastify",)


def test_detect_frameworks_finds_nestjs_dependency(tmp_path: Path) -> None:
    (tmp_path / "package.json").write_text(
        json.dumps({"dependencies": {"@nestjs/core": "^11.0.0"}}),
        encoding="utf-8",
    )

    assert detect_frameworks(tmp_path) == ("nestjs",)


def test_resolve_frameworks_merges_auto_detection_with_explicit_frameworks(tmp_path: Path) -> None:
    (tmp_path / "package.json").write_text(
        json.dumps({"dependencies": {"fastify": "^5.0.0"}}),
        encoding="utf-8",
    )

    assert resolve_frameworks(tmp_path, ("auto", "koa")) == ("fastify", "koa")


def test_resolve_frameworks_preserves_detected_nestjs_before_explicit_frameworks(
    tmp_path: Path,
) -> None:
    (tmp_path / "package.json").write_text(
        json.dumps({"dependencies": {"@nestjs/core": "^11.0.0"}}),
        encoding="utf-8",
    )

    assert resolve_frameworks(tmp_path, ("auto", "express")) == ("nestjs", "express")


def test_detect_frameworks_requires_next_dependency_and_next_config(tmp_path: Path) -> None:
    (tmp_path / "package.json").write_text(
        json.dumps({"dependencies": {"next": "^15.0.0"}}),
        encoding="utf-8",
    )

    assert detect_frameworks(tmp_path) == ()

    (tmp_path / "next.config.js").write_text("module.exports = {};\n", encoding="utf-8")

    assert detect_frameworks(tmp_path) == ("nextjs",)


def test_detect_frameworks_finds_gin_and_go_stdlib(tmp_path: Path) -> None:
    (tmp_path / "go.mod").write_text(
        "\n".join(
            [
                "module example.com/app",
                "",
                "go 1.21",
                "",
                "require github.com/gin-gonic/gin v1.10.0",
            ]
        ),
        encoding="utf-8",
    )
    (tmp_path / "main.go").write_text("package main\n", encoding="utf-8")

    assert detect_frameworks(tmp_path) == ("gin", "go-stdlib")


def test_detect_frameworks_ignores_vendor_only_go_sources(tmp_path: Path) -> None:
    (tmp_path / "go.mod").write_text("module example.com/app\n\ngo 1.21\n", encoding="utf-8")
    (tmp_path / "vendor" / "example.com" / "dep").mkdir(parents=True)
    (tmp_path / "vendor" / "example.com" / "dep" / "unsafe.go").write_text(
        "package dep\n",
        encoding="utf-8",
    )

    assert detect_frameworks(tmp_path) == ()


def test_discover_nextjs_routes_finds_pages_app_and_server_action_files(tmp_path: Path) -> None:
    (tmp_path / "pages/api/admin").mkdir(parents=True)
    (tmp_path / "app/api/files").mkdir(parents=True)
    (tmp_path / "app/orders").mkdir(parents=True)
    (tmp_path / "pages/api/admin/index.ts").write_text("export default function handler() {}\n")
    (tmp_path / "app/api/files/route.ts").write_text("export async function GET() {}\n")
    (tmp_path / "app/orders/actions.ts").write_text(
        "'use server';\nexport async function submit() {}\n"
    )

    routes = discover_nextjs_routes(tmp_path)

    assert [(route.kind, route.route_pattern) for route in routes] == [
        ("pages_router", "/api/admin"),
        ("app_router", "/api/files"),
        ("server_action", "/orders"),
    ]
