import { workflowSchemaVersion } from "./types.js";
import type {
  JsonRecord,
  JsonSchemaShape,
  WorkflowCodegenMetadata,
  WorkflowDeterminism,
  WorkflowEdge,
  WorkflowNode,
  WorkflowNodeKind,
  WorkflowRuntime,
  WorkflowSpec
} from "./types.js";

const objectSchema: JsonSchemaShape = { type: "object", additionalProperties: true };
const arraySchema: JsonSchemaShape = { type: "array", items: objectSchema };
const stringSchema: JsonSchemaShape = { type: "string" };
const placeholderChecksum = `sha256:${"0".repeat(64)}`;

export const defaultWorkflowRuntime: WorkflowRuntime = {
  image: "node:20-alpine",
  command: ["node", "/workspace/run-node.js"],
  timeoutSeconds: 300,
  retry: {
    maxAttempts: 1,
    backoffSeconds: 0
  },
  environment: {},
  resources: {
    cpu: "1",
    memoryMb: 512
  }
};

export const defaultWorkflowDeterminism: WorkflowDeterminism = {
  externalCalls: [],
  seededRandomness: {
    enabled: false
  },
  replayBehavior: "none"
};

export interface WorkflowNodeFactoryInput {
  readonly id: string;
  readonly kind: WorkflowNodeKind;
  readonly label?: string | undefined;
  readonly description?: string | undefined;
  readonly inputs?: Readonly<Record<string, JsonSchemaShape>> | undefined;
  readonly outputs?: Readonly<Record<string, JsonSchemaShape>> | undefined;
  readonly config?: JsonRecord | undefined;
  readonly runtime?: PartialWorkflowRuntime | undefined;
  readonly determinism?: PartialWorkflowDeterminism | undefined;
  readonly skillId?: string | undefined;
  readonly adapterId?: string | undefined;
  readonly codegen?: WorkflowCodegenMetadata | undefined;
}

export type PartialWorkflowRuntime = Partial<
  Omit<WorkflowRuntime, "retry" | "resources" | "environment" | "command">
> & {
  readonly command?: readonly string[] | undefined;
  readonly environment?: Readonly<Record<string, string>> | undefined;
  readonly retry?: Partial<WorkflowRuntime["retry"]> | undefined;
  readonly resources?: Partial<WorkflowRuntime["resources"]> | undefined;
};

export type PartialWorkflowDeterminism = Partial<
  Omit<WorkflowDeterminism, "seededRandomness" | "externalCalls">
> & {
  readonly externalCalls?: readonly string[] | undefined;
  readonly seededRandomness?: Partial<WorkflowDeterminism["seededRandomness"]> | undefined;
};

export interface WorkflowSpecFactoryInput {
  readonly id: string;
  readonly name: string;
  readonly prompt: string;
  readonly nodes: readonly WorkflowNode[];
  readonly edges: readonly WorkflowEdge[];
  readonly revision?: number | undefined;
  readonly approval?: WorkflowSpec["approval"] | undefined;
  readonly createdAt?: string | undefined;
  readonly updatedAt?: string | undefined;
}

export function createWorkflowRuntime(overrides: PartialWorkflowRuntime = {}): WorkflowRuntime {
  return {
    ...defaultWorkflowRuntime,
    ...overrides,
    command: overrides.command ?? defaultWorkflowRuntime.command,
    environment: overrides.environment ?? defaultWorkflowRuntime.environment,
    retry: {
      ...defaultWorkflowRuntime.retry,
      ...overrides.retry
    },
    resources: {
      ...defaultWorkflowRuntime.resources,
      ...overrides.resources
    }
  };
}

export function createWorkflowDeterminism(
  overrides: PartialWorkflowDeterminism = {}
): WorkflowDeterminism {
  return {
    ...defaultWorkflowDeterminism,
    ...overrides,
    externalCalls: overrides.externalCalls ?? defaultWorkflowDeterminism.externalCalls,
    seededRandomness: {
      ...defaultWorkflowDeterminism.seededRandomness,
      ...overrides.seededRandomness
    }
  };
}

export function createWorkflowNode(input: WorkflowNodeFactoryInput): WorkflowNode {
  const defaults = nodeKindDefaults(input.kind);
  const node: WorkflowNode = {
    id: input.id,
    kind: input.kind,
    label: input.label ?? defaults.label,
    description: input.description ?? defaults.description,
    inputs: input.inputs ?? defaults.inputs,
    outputs: input.outputs ?? defaults.outputs,
    config: input.config ?? defaults.config,
    runtime: createWorkflowRuntime(input.runtime),
    determinism: createWorkflowDeterminism(input.determinism),
    ...(input.skillId ? { skillId: input.skillId } : {}),
    ...(input.adapterId ? { adapterId: input.adapterId } : {}),
    ...((input.codegen ?? defaults.codegen) ? { codegen: input.codegen ?? defaults.codegen } : {})
  };

  return node;
}

export function createWorkflowEdge(input: {
  readonly sourceNodeId: string;
  readonly sourcePort: string;
  readonly targetNodeId: string;
  readonly targetPort: string;
  readonly id?: string | undefined;
}): WorkflowEdge {
  return {
    id:
      input.id ??
      `edge.${input.sourceNodeId}.${input.sourcePort}.${input.targetNodeId}.${input.targetPort}`,
    source: {
      nodeId: input.sourceNodeId,
      port: input.sourcePort
    },
    target: {
      nodeId: input.targetNodeId,
      port: input.targetPort
    }
  };
}

export function createWorkflowSpec(input: WorkflowSpecFactoryInput): WorkflowSpec {
  const now = input.createdAt ?? new Date().toISOString();

  return {
    id: input.id,
    schemaVersion: workflowSchemaVersion,
    name: input.name,
    prompt: input.prompt,
    revision: input.revision ?? 1,
    nodes: input.nodes,
    edges: input.edges,
    approval: input.approval ?? null,
    createdAt: now,
    updatedAt: input.updatedAt ?? now
  };
}

export function workflowIdFromPrompt(prompt: string): string {
  const slug = prompt
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/gu, "-")
    .replace(/^-+|-+$/gu, "")
    .slice(0, 48);

  return `workflow.${slug || "openclaw-draft"}`;
}

export function nodeIdFromLabel(label: string, kind: WorkflowNodeKind): string {
  const slug = label
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/gu, "-")
    .replace(/^-+|-+$/gu, "")
    .slice(0, 40);

  return slug || `${kind}-node`;
}

function nodeKindDefaults(kind: WorkflowNodeKind): {
  readonly label: string;
  readonly description: string;
  readonly inputs: Readonly<Record<string, JsonSchemaShape>>;
  readonly outputs: Readonly<Record<string, JsonSchemaShape>>;
  readonly config: JsonRecord;
  readonly codegen?: WorkflowCodegenMetadata | undefined;
} {
  switch (kind) {
    case "trigger":
      return {
        label: "Manual Trigger",
        description: "Starts the workflow when an operator runs it.",
        inputs: {},
        outputs: { request: objectSchema },
        config: { trigger: "manual" }
      };
    case "skill":
      return {
        label: "Registry Skill",
        description: "Runs a deterministic registry skill.",
        inputs: { request: objectSchema },
        outputs: { result: objectSchema },
        config: { skillMode: "deterministic" }
      };
    case "codegen":
      return {
        label: "Generated Code",
        description: "Runs generated code with frozen provenance and replay policy.",
        inputs: { request: objectSchema },
        outputs: { artifact: objectSchema },
        config: { sandboxPolicy: "network-none", artifactStatus: "draft" },
        codegen: {
          originalPrompt: "Generate deterministic workflow code.",
          latestPrompt: "Generate deterministic workflow code.",
          plannerRationale: "No deterministic registry skill matched the requested operation.",
          provenance: {
            generator: "kelpclaw.codegen.typescript",
            generatedAt: "2026-05-18T00:00:00.000Z",
            sourcePrompt: "Generate deterministic workflow code.",
            artifactPath: "generated/openclaw-node.ts",
            artifactChecksum: placeholderChecksum
          },
          artifacts: [
            {
              path: "generated/openclaw-node.ts",
              checksum: placeholderChecksum,
              contentType: "text/typescript"
            },
            {
              path: "generated/package-manifest.json",
              checksum: placeholderChecksum,
              contentType: "application/json"
            }
          ],
          dependencyManifest: {
            path: "generated/package-manifest.json",
            checksum: placeholderChecksum,
            packageManager: "none",
            dependencies: [],
            devDependencies: [],
            installCommand: []
          },
          sandbox: {
            network: "none",
            allowedHosts: [],
            mounts: [],
            resources: defaultWorkflowRuntime.resources
          },
          review: {
            status: "draft"
          },
          replay: {
            mode: "reuse-if-unchanged",
            seed: "openclaw-default"
          },
          llmBacked: false
        }
      };
    case "transform":
      return {
        label: "Transform Data",
        description: "Transforms upstream data into a downstream shape.",
        inputs: { input: objectSchema },
        outputs: { output: objectSchema },
        config: { mode: "map" }
      };
    case "approval":
      return {
        label: "Approval Gate",
        description: "Pauses execution until the configured owner approves.",
        inputs: { input: objectSchema },
        outputs: { approved: objectSchema },
        config: { requiredRole: "owner" }
      };
    case "delivery":
      return {
        label: "Deliver Result",
        description: "Sends the prepared output through a configured delivery adapter.",
        inputs: { rows: arraySchema },
        outputs: { delivery: objectSchema },
        config: { channel: "sheets", destination: "sheet.receipts" }
      };
  }
}

export const workflowGraphSchemas = {
  object: objectSchema,
  array: arraySchema,
  string: stringSchema
} as const;
