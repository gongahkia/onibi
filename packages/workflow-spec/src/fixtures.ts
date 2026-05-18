import type { WorkflowSpec } from "./types.js";

export const staticContentWorkflowFixture: WorkflowSpec = {
  metadata: {
    id: "workflow.static-content",
    name: "Static Content Review",
    version: "1.0.0",
    createdAt: "2026-05-18T00:00:00.000Z"
  },
  nodes: [
    {
      id: "collect-brief",
      type: "skill",
      label: "Collect Brief",
      skillId: "skill.read-brief",
      docker: {
        image: "node:20-alpine",
        command: ["node", "collect-brief.js"]
      },
      outputs: ["brief.json"]
    },
    {
      id: "draft-copy",
      type: "codegen",
      label: "Draft Copy",
      skillId: "skill.codegen.typescript",
      docker: {
        image: "node:20-alpine",
        command: ["node", "draft-copy.js"]
      },
      inputs: {
        style: "concise"
      },
      outputs: ["draft.md"]
    },
    {
      id: "owner-approval",
      type: "approval",
      label: "Owner Approval"
    },
    {
      id: "send-email",
      type: "adapter",
      label: "Send Email",
      adapterId: "adapter.email.fake",
      docker: {
        image: "node:20-alpine",
        command: ["node", "send-email.js"]
      }
    }
  ],
  edges: [
    { source: "collect-brief", target: "draft-copy" },
    { source: "draft-copy", target: "owner-approval" },
    { source: "owner-approval", target: "send-email" }
  ],
  approvals: [
    {
      id: "approval.owner-approval",
      nodeId: "owner-approval",
      label: "Approve generated copy",
      requiredRole: "owner"
    }
  ]
};

export const cyclicWorkflowFixture: WorkflowSpec = {
  ...staticContentWorkflowFixture,
  metadata: {
    ...staticContentWorkflowFixture.metadata,
    id: "workflow.cyclic"
  },
  edges: [
    { source: "collect-brief", target: "draft-copy" },
    { source: "draft-copy", target: "send-email" },
    { source: "send-email", target: "collect-brief" }
  ]
};

export const missingEdgeTargetWorkflowFixture: WorkflowSpec = {
  ...staticContentWorkflowFixture,
  metadata: {
    ...staticContentWorkflowFixture.metadata,
    id: "workflow.missing-edge-target"
  },
  edges: [{ source: "collect-brief", target: "missing-node" }]
};
