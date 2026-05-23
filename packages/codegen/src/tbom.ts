import type { JsonValue, WorkflowSpec } from "@kelpclaw/workflow-spec";
import type { TrajectoryRun } from "./trajectory-synth.js";

export interface TrajectoryBillOfMaterials {
  readonly kelpclawTbomVersion: "1.0.0";
  readonly sourceAgent: string;
  readonly models: readonly {
    readonly provider: string;
    readonly id: string;
    readonly calls: number;
  }[];
  readonly tools: readonly {
    readonly name: string;
    readonly calls: number;
  }[];
  readonly adapters: readonly {
    readonly id: string;
    readonly calls: number;
  }[];
  readonly externalDomains: readonly string[];
  readonly secretsConsumed: readonly string[];
  readonly classifications: readonly string[];
  readonly auditChainHead: string;
}

export function buildTbom(workflow: WorkflowSpec, run: TrajectoryRun): TrajectoryBillOfMaterials {
  const events = [...run.events].sort((left, right) => left.chainIndex - right.chainIndex);
  const workflowClassifications = workflow.nodes.flatMap(
    (node) => node.agentStep?.classification ?? []
  );
  return {
    kelpclawTbomVersion: "1.0.0",
    sourceAgent: run.sourceAgent,
    models: countNamedRecords(extractModelIds(events)).map((entry) => {
      const [provider = "unknown", id = entry.name] = entry.name.split("/");
      return {
        provider,
        id,
        calls: entry.calls
      };
    }),
    tools: countNamedRecords(events.map((event) => event.toolName)),
    adapters: countNamedRecords(
      events.map((event) => event.toolName).filter((toolName) => toolName.startsWith("adapter."))
    ).map((entry) => ({ id: entry.name, calls: entry.calls })),
    externalDomains: uniqueSorted(
      events.flatMap((event) => extractDomains(event.args, event.result))
    ),
    secretsConsumed: uniqueSorted(
      events.flatMap((event) => extractSecrets(event.args, event.result))
    ),
    classifications: uniqueSorted([
      ...events.flatMap((event) => event.classification ?? []),
      ...workflowClassifications
    ]),
    auditChainHead: events.at(-1)?.prevEventHash ?? `sha256:${"0".repeat(64)}`
  };
}

export function exportTbom(tbom: TrajectoryBillOfMaterials, format: "json" | "pdf"): Buffer {
  if (format === "pdf") {
    return Buffer.from(JSON.stringify(tbom, null, 2), "utf8");
  }
  return Buffer.from(JSON.stringify(tbom, null, 2), "utf8");
}

function countNamedRecords(
  values: readonly string[]
): { readonly name: string; readonly calls: number }[] {
  const counts = new Map<string, number>();
  for (const value of values) {
    counts.set(value, (counts.get(value) ?? 0) + 1);
  }
  return [...counts]
    .map(([name, calls]) => ({ name, calls }))
    .sort((left, right) => left.name.localeCompare(right.name));
}

function extractModelIds(events: TrajectoryRun["events"]): readonly string[] {
  return events.flatMap((event) => {
    const metadata = jsonObject(event.result);
    const provider = stringField(metadata, "provider") ?? stringField(metadata, "modelProvider");
    const model = stringField(metadata, "model") ?? stringField(metadata, "modelId");
    return provider && model ? [`${provider}/${model}`] : [];
  });
}

function extractDomains(...values: readonly (JsonValue | undefined)[]): readonly string[] {
  return extractStrings(values).flatMap((value) => {
    const matches = value.match(/https?:\/\/[^\s"')]+/gu) ?? [value];
    return matches.flatMap((candidate) => {
      try {
        const hostname = new URL(candidate).hostname;
        return hostname ? [hostname] : [];
      } catch {
        return [];
      }
    });
  });
}

function extractSecrets(...values: readonly (JsonValue | undefined)[]): readonly string[] {
  return extractStrings(values).filter((value) => value.startsWith("secret:"));
}

function extractStrings(values: readonly (JsonValue | undefined)[]): readonly string[] {
  const strings: string[] = [];
  for (const value of values) {
    if (typeof value === "string") {
      strings.push(value);
    } else if (Array.isArray(value)) {
      strings.push(...extractStrings(value));
    } else if (value && typeof value === "object") {
      strings.push(...extractStrings(Object.values(value)));
    }
  }
  return strings;
}

function uniqueSorted(values: readonly string[]): readonly string[] {
  return [...new Set(values)].sort();
}

function jsonObject(value: JsonValue | undefined): Record<string, JsonValue> {
  return value && typeof value === "object" && !Array.isArray(value) ? value : {};
}

function stringField(record: Record<string, JsonValue>, key: string): string | undefined {
  const value = record[key];
  return typeof value === "string" ? value : undefined;
}
