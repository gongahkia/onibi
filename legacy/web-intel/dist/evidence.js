import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { stableJsonStringify } from "@kelpclaw/workflow-spec";
import { hashJson } from "./client.js";
export async function writeWebEvidenceFiles(outDir, bundle) {
    await mkdir(outDir, { recursive: true });
    const files = [
        ["web-evidence.json", `${JSON.stringify(bundle, null, 2)}\n`],
        [
            "web-events.jsonl",
            `${bundle.events.map((event) => JSON.stringify(event)).join("\n")}${bundle.events.length ? "\n" : ""}`
        ],
        ["web-bom.json", `${JSON.stringify(webBom(bundle), null, 2)}\n`],
        ["web-evidence.html", renderWebEvidenceHtml(bundle)]
    ];
    for (const [file, content] of files) {
        await writeFile(join(outDir, file), content, "utf8");
    }
    return files.map(([file]) => file);
}
export async function readWebEvidenceBundle(path) {
    const metadata = await stat(path);
    const evidencePath = metadata.isDirectory() ? join(path, "web-evidence.json") : path;
    return JSON.parse(await readFile(evidencePath, "utf8"));
}
export function webBom(bundle) {
    return {
        schemaVersion: "1.0.0",
        generatedAt: bundle.generatedAt,
        provider: bundle.selectedProvider,
        operations: [...new Set(bundle.events.map((event) => event.operation))],
        sourceCount: bundle.sources.length,
        sources: bundle.sources.map((source) => ({
            url: source.url,
            title: source.title,
            contentHash: source.contentHash,
            fullContentStored: source.fullContentStored,
            redacted: source.redacted
        })),
        eventHashes: bundle.events.map((event) => hashJson(event)),
        bundleHash: hashJson(bundle)
    };
}
export function renderWebEvidenceHtml(bundle) {
    const sourceRows = bundle.sources
        .map((source) => `<tr>
  <td>${escapeHtml(source.provider)}</td>
  <td>${escapeHtml(source.title ?? "")}</td>
  <td>${source.url ? `<a href="${escapeHtml(source.url)}">${escapeHtml(source.url)}</a>` : ""}</td>
  <td><code>${escapeHtml(source.contentHash)}</code></td>
  <td>${source.fullContentStored ? "yes" : "no"}</td>
  <td>${source.redacted ? "yes" : "no"}</td>
</tr>`)
        .join("\n");
    return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>KelpClaw Web Evidence</title>
  <style>
    body { font: 14px/1.45 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 32px; color: #172026; }
    h1, h2 { line-height: 1.15; }
    code, pre { background: #f3f5f7; border-radius: 6px; padding: 2px 5px; }
    table { border-collapse: collapse; width: 100%; margin-top: 12px; }
    th, td { border-bottom: 1px solid #d9e0e7; padding: 8px; text-align: left; vertical-align: top; }
    th { background: #f7f9fb; }
  </style>
</head>
<body>
  <h1>KelpClaw Web Evidence</h1>
  <p><strong>Generated:</strong> ${escapeHtml(bundle.generatedAt)}</p>
  <p><strong>Provider:</strong> ${escapeHtml(bundle.selectedProvider)} · <strong>Escalation:</strong> ${escapeHtml(bundle.escalationLevel)}</p>
  <h2>Summary</h2>
  <pre>${escapeHtml(stableJsonStringify(bundle.summary))}</pre>
  <h2>Request</h2>
  <pre>${escapeHtml(stableJsonStringify(bundle.request))}</pre>
  <h2>Sources</h2>
  <table>
    <thead><tr><th>Provider</th><th>Title</th><th>URL</th><th>Hash</th><th>Stored</th><th>Redacted</th></tr></thead>
    <tbody>${sourceRows}</tbody>
  </table>
  <h2>Events</h2>
  <pre>${escapeHtml(stableJsonStringify(bundle.events))}</pre>
</body>
</html>
`;
}
function escapeHtml(value) {
    return value
        .replace(/&/gu, "&amp;")
        .replace(/</gu, "&lt;")
        .replace(/>/gu, "&gt;")
        .replace(/"/gu, "&quot;");
}
//# sourceMappingURL=evidence.js.map