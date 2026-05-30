import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import {
  appendTaintLedgerEntries,
  extractFilenameTaint,
  extractGenericTextTaint,
  extractLogFileTaint,
  extractTaintSpans,
  extractTimelineCsvTaint
} from "../src/index.js";

const sha256 = `sha256:${"a".repeat(64)}`;

describe("taint extractors", () => {
  it("labels filenames as a basename span", () => {
    expect(
      extractFilenameTaint({
        path: "cases/host-a/system.log",
        sha256,
        content: "ignored"
      })
    ).toMatchObject([
      {
        source: { locator: "filename" },
        text: "system.log",
        extractionTool: "findevil.taint.filenames",
        sensitivity: "case-data",
        span: { start: 0, end: 10 }
      }
    ]);
  });

  it("labels log files one entry per non-empty line", () => {
    const entries = extractLogFileTaint({
      path: "cases/host-a/security.log",
      sha256,
      content: "first event\n\n  second event  \n"
    });

    expect(entries).toMatchObject([
      { source: { locator: "line:1" }, text: "first event" },
      { source: { locator: "line:3" }, text: "second event" }
    ]);
  });

  it("labels timeline csv content one entry per non-empty row", () => {
    const entries = extractTimelineCsvTaint({
      path: "cases/timeline.csv",
      sha256,
      content: "timestamp,description\n2026-05-01T00:00:00Z,process start\n"
    });

    expect(entries).toMatchObject([
      { source: { locator: "row:1" }, text: "timestamp,description" },
      { source: { locator: "row:2" }, text: "2026-05-01T00:00:00Z,process start" }
    ]);
  });

  it("labels generic text with sliding paragraph windows", () => {
    const entries = extractGenericTextTaint({
      path: "cases/ransom-note.txt",
      sha256,
      content: "First paragraph.\nStill first.\n\nSecond paragraph.\n\nThird paragraph."
    });

    expect(entries).toMatchObject([
      {
        source: { locator: "paragraphs:1-2" },
        text: "First paragraph.\nStill first.\n\nSecond paragraph."
      },
      {
        source: { locator: "paragraphs:2-3" },
        text: "Second paragraph.\n\nThird paragraph."
      }
    ]);
  });

  it("selects filename plus source-specific content extractors", () => {
    const entries = extractTaintSpans({
      path: "cases/timeline.csv",
      sha256,
      content: "time,event\n1,login"
    });

    expect(entries.map((entry) => entry.source.locator)).toEqual(["filename", "row:1", "row:2"]);
  });

  it("appends taint ledger entries as jsonl", async () => {
    const dir = await mkdtemp(join(tmpdir(), "kelpclaw-taint-"));
    try {
      const path = join(dir, "taint-ledger.jsonl");
      const entries = extractLogFileTaint({
        path: "cases/system.log",
        sha256,
        content: "event one\nevent two"
      });

      await appendTaintLedgerEntries(path, entries);
      const lines = (await readFile(path, "utf8")).trim().split("\n");

      expect(lines).toHaveLength(2);
      expect(JSON.parse(lines[0] ?? "{}")).toMatchObject({ text: "event one" });
      expect(JSON.parse(lines[1] ?? "{}")).toMatchObject({ text: "event two" });
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  });
});
