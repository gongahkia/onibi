import type { JsonRecord } from "./types.js";

export const workflowJsonSchema = {
  $schema: "https://json-schema.org/draft/2020-12/schema",
  $id: "https://kelpclaw.dev/schemas/workflow-spec.v1.schema.json",
  title: "KelpClaw Workflow Spec v1",
  type: "object",
  required: [
    "id",
    "schemaVersion",
    "name",
    "prompt",
    "revision",
    "nodes",
    "edges",
    "approval",
    "createdAt",
    "updatedAt"
  ],
  additionalProperties: false,
  properties: {
    id: { type: "string", minLength: 1 },
    schemaVersion: { const: "1.0.0" },
    name: { type: "string", minLength: 1 },
    prompt: { type: "string", minLength: 1 },
    revision: { type: "integer", minimum: 1 },
    nodes: {
      type: "array",
      minItems: 1,
      items: { $ref: "#/$defs/node" }
    },
    edges: {
      type: "array",
      items: { $ref: "#/$defs/edge" }
    },
    approval: {
      anyOf: [{ type: "null" }, { $ref: "#/$defs/approval" }]
    },
    createdAt: { type: "string", format: "date-time" },
    updatedAt: { type: "string", format: "date-time" }
  },
  $defs: {
    jsonValue: {
      anyOf: [
        { type: "string" },
        { type: "number" },
        { type: "boolean" },
        { type: "null" },
        { type: "array", items: { $ref: "#/$defs/jsonValue" } },
        {
          type: "object",
          additionalProperties: { $ref: "#/$defs/jsonValue" }
        }
      ]
    },
    jsonRecord: {
      type: "object",
      additionalProperties: { $ref: "#/$defs/jsonValue" }
    },
    schemaShape: {
      type: "object",
      additionalProperties: { $ref: "#/$defs/jsonValue" }
    },
    node: {
      type: "object",
      required: [
        "id",
        "kind",
        "label",
        "description",
        "inputs",
        "outputs",
        "config",
        "runtime",
        "determinism"
      ],
      additionalProperties: false,
      properties: {
        id: { type: "string", minLength: 1 },
        kind: {
          enum: ["trigger", "skill", "codegen", "transform", "approval", "delivery"]
        },
        label: { type: "string", minLength: 1 },
        description: { type: "string", minLength: 1 },
        inputs: {
          type: "object",
          additionalProperties: { $ref: "#/$defs/schemaShape" }
        },
        outputs: {
          type: "object",
          additionalProperties: { $ref: "#/$defs/schemaShape" }
        },
        config: { $ref: "#/$defs/jsonRecord" },
        runtime: { $ref: "#/$defs/runtime" },
        determinism: { $ref: "#/$defs/determinism" },
        skillId: { type: "string", minLength: 1 },
        adapterId: { type: "string", minLength: 1 },
        codegen: { $ref: "#/$defs/codegen" }
      }
    },
    portRef: {
      type: "object",
      required: ["nodeId", "port"],
      additionalProperties: false,
      properties: {
        nodeId: { type: "string", minLength: 1 },
        port: { type: "string", minLength: 1 }
      }
    },
    edge: {
      type: "object",
      required: ["id", "source", "target"],
      additionalProperties: false,
      properties: {
        id: { type: "string", minLength: 1 },
        source: { $ref: "#/$defs/portRef" },
        target: { $ref: "#/$defs/portRef" }
      }
    },
    runtime: {
      type: "object",
      required: ["image", "command", "timeoutSeconds", "retry", "environment", "resources"],
      additionalProperties: false,
      properties: {
        image: { type: "string", minLength: 1 },
        command: {
          type: "array",
          minItems: 1,
          items: { type: "string", minLength: 1 }
        },
        timeoutSeconds: { type: "integer", minimum: 1 },
        retry: {
          type: "object",
          required: ["maxAttempts", "backoffSeconds"],
          additionalProperties: false,
          properties: {
            maxAttempts: { type: "integer", minimum: 0 },
            backoffSeconds: { type: "number", minimum: 0 }
          }
        },
        environment: {
          type: "object",
          additionalProperties: { type: "string" }
        },
        resources: {
          type: "object",
          required: ["cpu", "memoryMb"],
          additionalProperties: false,
          properties: {
            cpu: { type: "string", minLength: 1 },
            memoryMb: { type: "integer", minimum: 1 }
          }
        }
      }
    },
    determinism: {
      type: "object",
      required: ["externalCalls", "seededRandomness", "replayBehavior"],
      additionalProperties: false,
      properties: {
        externalCalls: {
          type: "array",
          items: { type: "string", minLength: 1 }
        },
        seededRandomness: {
          type: "object",
          required: ["enabled"],
          additionalProperties: false,
          properties: {
            enabled: { type: "boolean" },
            seed: { type: "string", minLength: 1 }
          }
        },
        replayBehavior: {
          enum: ["none", "record", "replay", "reuse-if-unchanged", "fail-on-drift"]
        }
      }
    },
    codegen: {
      type: "object",
      required: ["provenance", "replay"],
      additionalProperties: false,
      properties: {
        provenance: {
          type: "object",
          required: [
            "generator",
            "generatedAt",
            "sourcePrompt",
            "artifactPath",
            "artifactChecksum"
          ],
          additionalProperties: false,
          properties: {
            generator: { type: "string", minLength: 1 },
            generatedAt: { type: "string", format: "date-time" },
            sourcePrompt: { type: "string", minLength: 1 },
            artifactPath: { type: "string", minLength: 1 },
            artifactChecksum: { type: "string", pattern: "^sha256:[a-f0-9]{64}$" }
          }
        },
        replay: {
          type: "object",
          required: ["mode", "seed"],
          additionalProperties: false,
          properties: {
            mode: { enum: ["reuse-if-unchanged", "always-regenerate", "fail-on-drift"] },
            seed: { type: "string", minLength: 1 }
          }
        }
      }
    },
    approval: {
      type: "object",
      required: [
        "status",
        "approvedBy",
        "approvedAt",
        "frozenRevision",
        "frozenDagHash",
        "nodeOrder"
      ],
      additionalProperties: false,
      properties: {
        status: { const: "approved" },
        approvedBy: { type: "string", minLength: 1 },
        approvedAt: { type: "string", format: "date-time" },
        frozenRevision: { type: "integer", minimum: 1 },
        frozenDagHash: { type: "string", pattern: "^sha256:[a-f0-9]{64}$" },
        nodeOrder: {
          type: "array",
          minItems: 1,
          items: { type: "string", minLength: 1 }
        }
      }
    }
  }
} as const satisfies JsonRecord;
