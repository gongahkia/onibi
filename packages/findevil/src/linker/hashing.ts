import { checksumArtifactContent } from "@kelpclaw/codegen";

export function hashEvidenceRow(row: unknown): string {
  return checksumArtifactContent(stableStringify(row));
}

function stableStringify(value: unknown): string {
  return JSON.stringify(sortValue(value));
}

function sortValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map((item) => sortValue(item));
  }
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value)
        .filter(([, entryValue]) => entryValue !== undefined)
        .sort(([leftKey], [rightKey]) => leftKey.localeCompare(rightKey))
        .map(([key, entryValue]) => [key, sortValue(entryValue)])
    );
  }
  return value;
}
