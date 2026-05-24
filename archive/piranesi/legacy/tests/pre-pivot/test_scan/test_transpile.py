from __future__ import annotations

import json
import logging
import os
from pathlib import Path
from subprocess import CompletedProcess

import pytest

from piranesi.scan.transpile import (
    SourceMap,
    TranspiledProject,
    TypeScriptCompilerNotFoundError,
    collect_transpilable_files,
    prepare_transpile_workspace,
    transpile_project,
)


def _write_enum_fixture(tmp_path: Path) -> tuple[Path, Path, Path]:
    ts_file = tmp_path / "sample.ts"
    ts_file.write_text(
        "export enum Direction {\n"
        '  Up = "UP",\n'
        '  Down = "DOWN",\n'
        "}\n"
        "\n"
        "export function describe(direction: Direction): string {\n"
        "  switch (direction) {\n"
        "    case Direction.Up:\n"
        '      return "up";\n'
        "    default:\n"
        '      return "down";\n'
        "  }\n"
        "}\n",
        encoding="utf-8",
    )

    out_dir = tmp_path / "out"
    out_dir.mkdir()

    js_file = out_dir / "sample.js"
    js_file.write_text(
        '"use strict";\n'
        'Object.defineProperty(exports, "__esModule", { value: true });\n'
        "exports.Direction = void 0;\n"
        "exports.describe = describe;\n"
        "var Direction;\n"
        "(function (Direction) {\n"
        '    Direction["Up"] = "UP";\n'
        '    Direction["Down"] = "DOWN";\n'
        "})(Direction || (exports.Direction = Direction = {}));\n"
        "function describe(direction) {\n"
        "    switch (direction) {\n"
        "        case Direction.Up:\n"
        '            return "up";\n'
        "        default:\n"
        '            return "down";\n'
        "    }\n"
        "}\n"
        "//# sourceMappingURL=sample.js.map\n",
        encoding="utf-8",
    )
    (out_dir / "sample.js.map").write_text(
        '{"version":3,"file":"sample.js","sourceRoot":"","sources":["../sample.ts"],'
        '"names":[],"mappings":";;;AAKA,4BAOC;AAZD,IAAY,SAGX;AAHD,WAAY,SAAS;'
        "IACnB,sBAAS,CAAA;IACT,0BAAa,CAAA;AACf,CAAC,EAHW,SAAS,yBAAT,SAAS,QAGpB;"
        "AAED,SAAgB,QAAQ,CAAC,SAAoB;IAC3C,QAAQ,SAAS,EAAE,CAAC;QAClB,KAAK,SAAS,CAAC,EAAE;"
        'YACf,OAAO,IAAI,CAAC;QACd;YACE,OAAO,MAAM,CAAC;IAClB,CAAC;AACH,CAAC"}',
        encoding="utf-8",
    )
    return ts_file, js_file, out_dir


def _write_emitted_output(workspace_root: Path, *, source_name: str = "c.ts") -> None:
    out_dir = workspace_root / "out"
    out_dir.mkdir(parents=True, exist_ok=True)
    js_file = out_dir / f"{Path(source_name).stem}.js"
    js_file.write_text('"use strict";\nexports.value = void 0;\n', encoding="utf-8")
    (out_dir / f"{Path(source_name).stem}.js.map").write_text(
        json.dumps(
            {
                "version": 3,
                "file": js_file.name,
                "sourceRoot": "",
                "sources": [f"../{source_name}"],
                "names": [],
                "mappings": "AAAA;AACA",
            }
        ),
        encoding="utf-8",
    )


def test_prepare_transpile_workspace_writes_isolated_tsconfig(tmp_path: Path) -> None:
    target_dir = tmp_path / "target"
    target_dir.mkdir()
    (target_dir / "app.ts").write_text("export const value = 1;\n", encoding="utf-8")
    (target_dir / "tsconfig.json").write_text(
        '{"compilerOptions":{"plugins":["evil"]}}', encoding="utf-8"
    )
    for filename in [".npmrc", ".node-version", ".nvmrc", ".tool-versions"]:
        (target_dir / filename).write_text("ignored\n", encoding="utf-8")

    workspace = prepare_transpile_workspace(target_dir)
    try:
        payload = json.loads(workspace.tsconfig_path.read_text(encoding="utf-8"))

        assert workspace.root_dir != target_dir
        assert workspace.root_dir.name.startswith("piranesi-tsconfig-")
        assert payload["compilerOptions"] == {
            "target": "ES2020",
            "module": "commonjs",
            "rootDir": str(target_dir.resolve()),
            "outDir": str(workspace.out_dir),
            "declaration": False,
            "sourceMap": True,
            "allowJs": True,
            "esModuleInterop": True,
            "experimentalDecorators": True,
            "emitDecoratorMetadata": True,
            "resolveJsonModule": True,
            "strict": False,
            "skipLibCheck": True,
            "noEmit": False,
        }
        assert payload["include"] == [
            str(target_dir.resolve() / "**" / "*.ts"),
            str(target_dir.resolve() / "**" / "*.tsx"),
            str(target_dir.resolve() / "**" / "*.js"),
            str(target_dir.resolve() / "**" / "*.jsx"),
        ]
        assert payload["exclude"] == [
            str(target_dir.resolve() / "node_modules" / "**"),
            str(target_dir.resolve() / "piranesi-output" / "**"),
            str(target_dir.resolve() / ".piranesi-cache" / "**"),
            str(target_dir.resolve() / ".piranesi-out" / "**"),
            str(target_dir.resolve() / ".piranesi-trace*"),
        ]
        assert "plugins" not in workspace.tsconfig_path.read_text(encoding="utf-8")
        for filename in [".npmrc", ".node-version", ".nvmrc", ".tool-versions"]:
            assert not (workspace.root_dir / filename).exists()
    finally:
        workspace.cleanup()


def test_collect_transpilable_files_skips_piranesi_output_dirs_and_trace_files(
    tmp_path: Path,
) -> None:
    project = tmp_path / "project"
    source_file = project / "src" / "index.ts"
    output_file = project / "piranesi-output" / "_cpg_cache" / "foo" / "transpiled" / "bar.ts"
    cache_file = project / ".piranesi-cache" / "cached.ts"
    out_file = project / ".piranesi-out" / "out.ts"
    trace_file = project / ".piranesi-trace.jsonl"
    trace_source_like_file = project / ".piranesi-trace.ts"

    for path in (
        source_file,
        output_file,
        cache_file,
        out_file,
        trace_file,
        trace_source_like_file,
    ):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("export const value = 1;\n", encoding="utf-8")

    assert collect_transpilable_files(project) == [source_file.resolve(strict=False)]


def test_prepare_transpile_workspace_limits_tsconfig_to_changed_files(tmp_path: Path) -> None:
    target_dir = tmp_path / "target"
    target_dir.mkdir()
    changed_file = target_dir / "app.ts"
    unchanged_file = target_dir / "other.ts"
    changed_file.write_text("export const value = 1;\n", encoding="utf-8")
    unchanged_file.write_text("export const other = 2;\n", encoding="utf-8")

    workspace = prepare_transpile_workspace(target_dir, changed_files={Path("app.ts")})
    try:
        payload = json.loads(workspace.tsconfig_path.read_text(encoding="utf-8"))

        assert payload["files"] == [str(changed_file.resolve())]
        assert "include" not in payload
    finally:
        workspace.cleanup()


def test_source_map_parsing_builds_bidirectional_line_mapping(tmp_path: Path) -> None:
    ts_file, js_file, out_dir = _write_enum_fixture(tmp_path)

    source_map = SourceMap.from_directory(out_dir)

    assert source_map.resolve(js_file, 5) == (ts_file.resolve(), 1)
    assert source_map.resolve(js_file, 10) == (ts_file.resolve(), 6)
    assert source_map.resolve(js_file, 13) == (ts_file.resolve(), 9)
    assert source_map.resolve(js_file, 18) == (ts_file.resolve(), 13)
    assert source_map.reverse_resolve(ts_file, 6) == (
        (js_file.resolve(), 4),
        (js_file.resolve(), 10),
    )


def test_transpile_project_raises_clear_error_when_tsc_missing(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    target_dir = tmp_path / "target"
    target_dir.mkdir()
    (target_dir / "app.ts").write_text("export const value = 1;\n", encoding="utf-8")

    calls: list[tuple[tuple[str, ...], Path, dict[str, str]]] = []

    def _run_subprocess(
        cmd: list[str],
        *,
        cwd: str | Path | None = None,
        timeout: int = 60,
        env: dict[str, str] | None = None,
        logger: logging.Logger | None = None,
    ) -> CompletedProcess[str]:
        del timeout, logger
        assert cwd is not None
        assert env is not None
        calls.append((tuple(cmd), Path(cwd), env))
        if cmd[0] == "tsc":
            raise FileNotFoundError("tsc")
        return CompletedProcess(cmd, 1, stdout="npx failed", stderr="")

    monkeypatch.setattr("piranesi.scan.transpile.run_subprocess", _run_subprocess)

    with pytest.raises(TypeScriptCompilerNotFoundError, match="Install TypeScript"):
        transpile_project(target_dir)

    assert calls[0][0][:2] == ("tsc", "--project")
    assert calls[1][0][:2] == ("npx", "tsc")
    assert calls[0][1] != target_dir
    assert calls[0][2]["NPM_CONFIG_USERCONFIG"] == os.devnull
    assert calls[0][2]["NPM_CONFIG_CACHE"].endswith(".npm-cache")


def test_transpile_project_logs_failed_files_and_warning_threshold(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
    caplog: pytest.LogCaptureFixture,
) -> None:
    target_dir = tmp_path / "target"
    target_dir.mkdir()
    for name in ["a.ts", "b.ts", "c.ts", "d.ts"]:
        (target_dir / name).write_text("export const value = 1;\n", encoding="utf-8")

    a_ts = target_dir / "a.ts"
    b_ts = target_dir / "b.ts"
    failure_output = (
        f"{a_ts}(1,1): error TS2322: "
        "Type 'number' is not assignable to type 'string'.\n"
        f"{b_ts}(1,1): error TS7006: "
        "Parameter 'value' implicitly has an 'any' type.\n"
    )
    calls: list[tuple[str, ...]] = []

    def _run_subprocess(
        cmd: list[str],
        *,
        cwd: str | Path | None = None,
        timeout: int = 60,
        env: dict[str, str] | None = None,
        logger: logging.Logger | None = None,
    ) -> CompletedProcess[str]:
        del timeout, env, logger
        calls.append(tuple(cmd))
        assert cwd is not None
        if len(calls) == 1:
            return CompletedProcess(cmd, 2, stdout=failure_output, stderr="")
        _write_emitted_output(Path(cwd), source_name="c.ts")
        return CompletedProcess(cmd, 2, stdout=failure_output, stderr="")

    monkeypatch.setattr("piranesi.scan.transpile.run_subprocess", _run_subprocess)

    result: TranspiledProject | None = None
    with caplog.at_level(logging.WARNING, logger="piranesi.scan.transpile"):
        result = transpile_project(target_dir)

    assert result is not None
    try:
        assert result.failed_files == (
            (target_dir / "a.ts").resolve(),
            (target_dir / "b.ts").resolve(),
        )
        assert calls[0][:2] == ("tsc", "--project")
        assert calls[1][-3:] == ("--skipLibCheck", "--noEmit", "false")
        messages = [record.getMessage() for record in caplog.records]
        assert any("reported 2 failed files" in message for message in messages)
        assert any("exceed 20% of source files" in message for message in messages)
        failed_file_records = [
            record
            for record in caplog.records
            if getattr(record, "event", "") == "transpile_failed_files"
        ]
        assert failed_file_records
        assert failed_file_records[-1].failed_files == [  # type: ignore[attr-defined]
            str((target_dir / "a.ts").resolve()),
            str((target_dir / "b.ts").resolve()),
        ]
    finally:
        result.cleanup()
