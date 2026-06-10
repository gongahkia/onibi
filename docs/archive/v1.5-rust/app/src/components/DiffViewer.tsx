import { useMemo, type ReactNode } from "react";
import type { DiffViewMode } from "../lib/sessions";

export interface DiffContent {
  path: string;
  oldLabel: string;
  newLabel: string;
  oldText?: string | null;
  newText?: string | null;
  binary: boolean;
}

interface DiffViewerProps {
  diff: DiffContent;
  mode: DiffViewMode;
  actions?: ReactNode;
}

type DiffRowKind = "context" | "add" | "delete" | "change";

interface DiffRow {
  kind: DiffRowKind;
  oldNumber?: number;
  newNumber?: number;
  oldLine?: string;
  newLine?: string;
}

function splitLines(text: string): string[] {
  if (!text) {
    return [];
  }
  return text.replace(/\n$/, "").split(/\r?\n/);
}

function findNext(lines: string[], start: number, needle: string, limit = 40): number {
  const end = Math.min(lines.length, start + limit);
  for (let index = start; index < end; index += 1) {
    if (lines[index] === needle) {
      return index;
    }
  }
  return -1;
}

export function buildDiffRows(oldText = "", newText = ""): DiffRow[] {
  const oldLines = splitLines(oldText);
  const newLines = splitLines(newText);
  const rows: DiffRow[] = [];
  let oldIndex = 0;
  let newIndex = 0;

  while (oldIndex < oldLines.length || newIndex < newLines.length) {
    const oldLine = oldLines[oldIndex];
    const newLine = newLines[newIndex];
    if (oldIndex < oldLines.length && newIndex < newLines.length && oldLine === newLine) {
      rows.push({
        kind: "context",
        oldNumber: oldIndex + 1,
        newNumber: newIndex + 1,
        oldLine,
        newLine,
      });
      oldIndex += 1;
      newIndex += 1;
      continue;
    }

    if (oldIndex >= oldLines.length) {
      rows.push({
        kind: "add",
        newNumber: newIndex + 1,
        newLine,
      });
      newIndex += 1;
      continue;
    }

    if (newIndex >= newLines.length) {
      rows.push({
        kind: "delete",
        oldNumber: oldIndex + 1,
        oldLine,
      });
      oldIndex += 1;
      continue;
    }

    const nextOldMatch = findNext(oldLines, oldIndex + 1, newLine);
    const nextNewMatch = findNext(newLines, newIndex + 1, oldLine);
    if (nextOldMatch >= 0 && (nextNewMatch < 0 || nextOldMatch - oldIndex <= nextNewMatch - newIndex)) {
      while (oldIndex < nextOldMatch) {
        rows.push({
          kind: "delete",
          oldNumber: oldIndex + 1,
          oldLine: oldLines[oldIndex],
        });
        oldIndex += 1;
      }
      continue;
    }
    if (nextNewMatch >= 0) {
      while (newIndex < nextNewMatch) {
        rows.push({
          kind: "add",
          newNumber: newIndex + 1,
          newLine: newLines[newIndex],
        });
        newIndex += 1;
      }
      continue;
    }

    rows.push({
      kind: "change",
      oldNumber: oldIndex + 1,
      newNumber: newIndex + 1,
      oldLine,
      newLine,
    });
    oldIndex += 1;
    newIndex += 1;
  }

  return rows;
}

function UnifiedDiff({ rows }: { rows: DiffRow[] }) {
  return (
    <div className="diff-unified" role="table" aria-label="Unified diff">
      {rows.map((row, index) => (
        <div
          key={`${row.oldNumber ?? "-"}:${row.newNumber ?? "-"}:${index}`}
          className={`diff-row diff-row-${row.kind}`}
          role="row"
        >
          <span className="diff-line-number">{row.oldNumber ?? ""}</span>
          <span className="diff-line-number">{row.newNumber ?? ""}</span>
          <span className="diff-marker">
            {row.kind === "add" ? "+" : row.kind === "delete" ? "-" : " "}
          </span>
          <code>{row.kind === "delete" ? row.oldLine : row.newLine ?? row.oldLine}</code>
        </div>
      ))}
    </div>
  );
}

function SideBySideDiff({ rows }: { rows: DiffRow[] }) {
  return (
    <div className="diff-split" role="table" aria-label="Side by side diff">
      {rows.map((row, index) => (
        <div
          key={`${row.oldNumber ?? "-"}:${row.newNumber ?? "-"}:${index}`}
          className={`diff-split-row diff-row-${row.kind}`}
          role="row"
        >
          <span className="diff-line-number">{row.oldNumber ?? ""}</span>
          <code className="diff-cell old">{row.oldLine ?? ""}</code>
          <span className="diff-line-number">{row.newNumber ?? ""}</span>
          <code className="diff-cell new">{row.newLine ?? ""}</code>
        </div>
      ))}
    </div>
  );
}

export function DiffViewer({ diff, mode, actions }: DiffViewerProps) {
  const rows = useMemo(
    () => buildDiffRows(diff.oldText ?? "", diff.newText ?? ""),
    [diff.newText, diff.oldText],
  );

  return (
    <section className="diff-viewer" data-testid="diff-viewer">
      <header className="editor-header">
        <div className="editor-path" title={diff.path}>
          {diff.path}
        </div>
        <div className="editor-actions">{actions}</div>
      </header>
      {diff.binary ? (
        <div className="editor-message">Binary diff is not previewable.</div>
      ) : (
        <>
          <div className={`diff-labels ${mode === "unified" ? "unified" : ""}`}>
            <span>{diff.oldLabel}</span>
            <span>{diff.newLabel}</span>
          </div>
          {mode === "unified" ? (
            <UnifiedDiff rows={rows} />
          ) : (
            <SideBySideDiff rows={rows} />
          )}
        </>
      )}
    </section>
  );
}
