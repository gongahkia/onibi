import type { SkillMetadata } from "./types.js";

export const builtinSkills: readonly SkillMetadata[] = [
  {
    id: "skill.read-brief",
    name: "Read Brief",
    version: "1.0.0",
    summary: "Normalizes an operator brief into structured workflow inputs.",
    deterministic: true,
    nodeTypes: ["skill"],
    capabilities: ["brief-ingestion"],
    inputContract: {
      briefText: "string"
    },
    outputContract: {
      "brief.json": "normalized brief payload"
    },
    metaprompt:
      "Extract only explicit requirements from the operator brief. Preserve source wording for ambiguous constraints.",
    docker: {
      image: "node:20-alpine",
      command: ["node", "/workspace/skills/read-brief.js"]
    }
  },
  {
    id: "skill.validate-workflow",
    name: "Validate Workflow",
    version: "1.0.0",
    summary: "Checks a workflow spec for schema validity, stable ids, and DAG safety.",
    deterministic: true,
    nodeTypes: ["skill"],
    capabilities: ["workflow-validation"],
    inputContract: {
      workflow: "WorkflowSpec"
    },
    outputContract: {
      "validation.json": "stable validation result"
    },
    metaprompt:
      "Validate workflow structure deterministically. Return stable error codes without proposing fixes.",
    docker: {
      image: "node:20-alpine",
      command: ["node", "/workspace/skills/validate-workflow.js"]
    }
  },
  {
    id: "skill.codegen.typescript",
    name: "TypeScript Codegen",
    version: "1.0.0",
    summary: "Generates diffable TypeScript artifacts from approved workflow inputs.",
    deterministic: true,
    nodeTypes: ["codegen"],
    capabilities: ["typescript-codegen"],
    inputContract: {
      spec: "WorkflowSpec",
      replayPolicy: "ReplayPolicy"
    },
    outputContract: {
      artifacts: "GeneratedArtifact[]"
    },
    metaprompt:
      "Generate minimal TypeScript artifacts from the approved workflow spec. Keep output ordering stable.",
    docker: {
      image: "node:20-alpine",
      command: ["node", "/workspace/skills/codegen-typescript.js"]
    }
  },
  {
    id: "skill.approval.owner",
    name: "Owner Approval Gate",
    version: "1.0.0",
    summary: "Blocks downstream execution until an owner approves the workflow gate.",
    deterministic: true,
    nodeTypes: ["approval"],
    capabilities: ["approval-routing"],
    inputContract: {
      approvalId: "string",
      requiredRole: "owner"
    },
    outputContract: {
      decision: "approved | rejected | pending"
    },
    metaprompt:
      "Represent the current approval state exactly. Do not infer approval from generated content.",
    docker: {
      image: "node:20-alpine",
      command: ["node", "/workspace/skills/approval-owner.js"]
    }
  },
  {
    id: "skill.adapter.dispatch",
    name: "Adapter Dispatch",
    version: "1.0.0",
    summary: "Routes a prepared payload to a configured fake adapter in local test mode.",
    deterministic: true,
    nodeTypes: ["adapter"],
    capabilities: ["adapter-dispatch"],
    inputContract: {
      adapterId: "string",
      payload: "JsonRecord"
    },
    outputContract: {
      delivery: "fake adapter delivery record"
    },
    metaprompt:
      "Dispatch only to configured fake adapters. Never request or use live external credentials.",
    docker: {
      image: "node:20-alpine",
      command: ["node", "/workspace/skills/adapter-dispatch.js"]
    }
  }
];
