import { createHash } from "node:crypto";
import { basename, extname } from "node:path";
import { taintLedgerEntrySchema, type TaintLedgerEntry } from "../types/taint.js";

export interface TaintInputFile {
  readonly path: string;
  readonly sha256: string;
  readonly content: string;
  readonly runId?: string | undefined;
}

interface TextSegment {
  readonly text: string;
  readonly start: number;
  readonly end: number;
}

export function extractTaintSpans(file: TaintInputFile): TaintLedgerEntry[] {
  return [...extractFilenameTaint(file), ...extractContentTaint(file)];
}

export function extractFilenameTaint(file: TaintInputFile): TaintLedgerEntry[] {
  const name = basename(file.path).trim();
  if (!name) {
    return [];
  }
  return [
    createTaintEntry(file, {
      text: name,
      locator: "filename",
      extractionTool: "findevil.taint.filenames",
      span: { start: 0, end: name.length }
    })
  ];
}

export function extractLogFileTaint(file: TaintInputFile): TaintLedgerEntry[] {
  return lineSegments(file.content).map((line) =>
    createTaintEntry(file, {
      text: line.text,
      locator: `line:${line.lineNumber}`,
      extractionTool: "findevil.taint.logFile",
      span: { start: line.start, end: line.end }
    })
  );
}

export function extractTimelineCsvTaint(file: TaintInputFile): TaintLedgerEntry[] {
  return lineSegments(file.content).map((row) =>
    createTaintEntry(file, {
      text: row.text,
      locator: `row:${row.lineNumber}`,
      extractionTool: "findevil.taint.timelineCsv",
      span: { start: row.start, end: row.end }
    })
  );
}

export function extractGenericTextTaint(file: TaintInputFile): TaintLedgerEntry[] {
  const paragraphs = paragraphSegments(file.content);
  if (paragraphs.length === 0) {
    return [];
  }

  const windowSize = 2;
  const windows =
    paragraphs.length === 1
      ? [{ first: 0, last: 0 }]
      : paragraphs.slice(0, -1).map((_, index) => ({
          first: index,
          last: Math.min(index + windowSize - 1, paragraphs.length - 1)
        }));

  return windows.map(({ first, last }) => {
    const firstParagraph = paragraphs[first];
    const lastParagraph = paragraphs[last];
    if (!firstParagraph || !lastParagraph) {
      throw new Error("Invalid generic text paragraph window.");
    }
    const text = file.content.slice(firstParagraph.start, lastParagraph.end).trim();
    return createTaintEntry(file, {
      text,
      locator: first === last ? `paragraph:${first + 1}` : `paragraphs:${first + 1}-${last + 1}`,
      extractionTool: "findevil.taint.genericText",
      span: { start: firstParagraph.start, end: lastParagraph.end }
    });
  });
}

function extractContentTaint(file: TaintInputFile): TaintLedgerEntry[] {
  const extension = extname(file.path).toLowerCase();
  if (extension === ".csv") {
    return extractTimelineCsvTaint(file);
  }
  if (isLogLikePath(file.path)) {
    return extractLogFileTaint(file);
  }
  return extractGenericTextTaint(file);
}

function createTaintEntry(
  file: TaintInputFile,
  options: {
    readonly text: string;
    readonly locator: string;
    readonly extractionTool: string;
    readonly span: { readonly start: number; readonly end: number };
  }
): TaintLedgerEntry {
  const sourceHash = normalizeSha256(file.sha256, file.content);
  return taintLedgerEntrySchema.parse({
    id: taintId(file.path, sourceHash, options.locator, options.text),
    ...(file.runId ? { runId: file.runId } : {}),
    source: {
      kind: "case_artifact",
      path: file.path,
      sha256: sourceHash,
      locator: options.locator
    },
    text: options.text,
    extractionTool: options.extractionTool,
    extractedAt: new Date().toISOString(),
    sensitivity: "case-data",
    span: options.span
  });
}

function isLogLikePath(path: string): boolean {
  return /\.(?:log|trace|out|err)$/iu.test(path) || /(?:eventlog|syslog|audit)/iu.test(path);
}

function normalizeSha256(input: string, fallbackContent: string): string {
  const value = input.toLowerCase();
  if (/^sha256:[a-f0-9]{64}$/u.test(value)) {
    return value;
  }
  if (/^[a-f0-9]{64}$/u.test(value)) {
    return `sha256:${value}`;
  }
  return `sha256:${createHash("sha256").update(fallbackContent).digest("hex")}`;
}

function taintId(path: string, sha256: string, locator: string, text: string): string {
  const digest = createHash("sha256")
    .update(path)
    .update("\0")
    .update(sha256)
    .update("\0")
    .update(locator)
    .update("\0")
    .update(text)
    .digest("hex")
    .slice(0, 20);
  return `taint-${digest}`;
}

function lineSegments(content: string): (TextSegment & { readonly lineNumber: number })[] {
  const segments: (TextSegment & { readonly lineNumber: number })[] = [];
  let lineStart = 0;
  let lineNumber = 1;
  for (let index = 0; index <= content.length; index += 1) {
    if (index < content.length && content[index] !== "\n") {
      continue;
    }
    const rawEnd = content[index - 1] === "\r" ? index - 1 : index;
    const raw = content.slice(lineStart, rawEnd);
    const trimmedStartOffset = raw.length - raw.trimStart().length;
    const trimmed = raw.trim();
    if (trimmed) {
      segments.push({
        lineNumber,
        text: trimmed,
        start: lineStart + trimmedStartOffset,
        end: lineStart + trimmedStartOffset + trimmed.length
      });
    }
    lineStart = index + 1;
    lineNumber += 1;
  }
  return segments;
}

function paragraphSegments(content: string): TextSegment[] {
  const lines = lineSegmentsIncludingEmpty(content);
  const paragraphs: TextSegment[] = [];
  let start: number | undefined;
  let end = 0;

  for (const line of lines) {
    if (!line.text.trim()) {
      if (start !== undefined) {
        paragraphs.push({ text: content.slice(start, end).trim(), start, end });
        start = undefined;
      }
      continue;
    }
    start ??= line.start;
    end = line.end;
  }

  if (start !== undefined) {
    paragraphs.push({ text: content.slice(start, end).trim(), start, end });
  }
  return paragraphs;
}

function lineSegmentsIncludingEmpty(content: string): TextSegment[] {
  const segments: TextSegment[] = [];
  let lineStart = 0;
  for (let index = 0; index <= content.length; index += 1) {
    if (index < content.length && content[index] !== "\n") {
      continue;
    }
    const rawEnd = content[index - 1] === "\r" ? index - 1 : index;
    const raw = content.slice(lineStart, rawEnd);
    const trimmedStartOffset = raw.length - raw.trimStart().length;
    const trimmedEndOffset = raw.trimEnd().length;
    const start = lineStart + trimmedStartOffset;
    const end = lineStart + trimmedEndOffset;
    segments.push({
      text: content.slice(start, end),
      start,
      end
    });
    lineStart = index + 1;
  }
  return segments;
}

export * from "./writer.js";
