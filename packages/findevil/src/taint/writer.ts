import { appendFile, mkdir } from "node:fs/promises";
import { dirname } from "node:path";
import { taintLedgerEntrySchema, type TaintLedgerEntry } from "../types/taint.js";

export async function appendTaintLedgerEntry(path: string, entry: TaintLedgerEntry): Promise<void> {
  await appendTaintLedgerEntries(path, [entry]);
}

export async function appendTaintLedgerEntries(
  path: string,
  entries: readonly TaintLedgerEntry[]
): Promise<void> {
  if (entries.length === 0) {
    return;
  }
  await mkdir(dirname(path), { recursive: true });
  const payload = entries
    .map((entry) => JSON.stringify(taintLedgerEntrySchema.parse(entry)))
    .join("\n");
  await appendFile(path, `${payload}\n`, "utf8");
}
