import type { JsonRecord } from "./types.js";

export const workflowJsonSchema = {
  $schema: "https://json-schema.org/draft/2020-12/schema",
  $id: "https://kelpclaw.dev/schemas/workflow-spec.schema.json",
  title: "KelpClaw Workflow Spec",
  type: "object",
  required: ["metadata", "nodes", "edges"],
  additionalProperties: false,
  properties: {
    metadata: {
      type: "object",
      required: ["id", "name", "version"],
      additionalProperties: false,
      properties: {
        id: { type: "string", minLength: 1 },
        name: { type: "string", minLength: 1 },
        version: { type: "string", minLength: 1 },
        createdAt: { type: "string", format: "date-time" }
      }
    },
    nodes: {
      type: "array",
      minItems: 1,
      items: { $ref: "#/$defs/node" }
    },
    edges: {
      type: "array",
      items: { $ref: "#/$defs/edge" }
    },
    approvals: {
      type: "array",
      items: { $ref: "#/$defs/approvalGate" }
    }
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
    node: {
      type: "object",
      required: ["id", "type", "label"],
      additionalProperties: false,
      properties: {
        id: { type: "string", minLength: 1 },
        type: { enum: ["skill", "adapter", "codegen", "approval"] },
        label: { type: "string", minLength: 1 },
        skillId: { type: "string", minLength: 1 },
        adapterId: { type: "string", minLength: 1 },
        docker: { $ref: "#/$defs/docker" },
        inputs: {
          type: "object",
          additionalProperties: { $ref: "#/$defs/jsonValue" }
        },
        outputs: {
          type: "array",
          items: { type: "string", minLength: 1 }
        }
      }
    },
    edge: {
      type: "object",
      required: ["source", "target"],
      additionalProperties: false,
      properties: {
        id: { type: "string", minLength: 1 },
        source: { type: "string", minLength: 1 },
        target: { type: "string", minLength: 1 }
      }
    },
    docker: {
      type: "object",
      required: ["image", "command"],
      additionalProperties: false,
      properties: {
        image: { type: "string", minLength: 1 },
        command: {
          type: "array",
          minItems: 1,
          items: { type: "string", minLength: 1 }
        },
        env: {
          type: "object",
          additionalProperties: { type: "string" }
        }
      }
    },
    approvalGate: {
      type: "object",
      required: ["id", "nodeId", "label", "requiredRole"],
      additionalProperties: false,
      properties: {
        id: { type: "string", minLength: 1 },
        nodeId: { type: "string", minLength: 1 },
        label: { type: "string", minLength: 1 },
        requiredRole: { enum: ["operator", "owner"] }
      }
    }
  }
} as const satisfies JsonRecord;
