import {
  createWorkflowSpecDiff,
  gmailReceiptsToSheetsWorkflowFixture,
  scheduledScrapingWorkflowFixture,
  timeSensitiveAlertDeliveryWorkflowFixture,
  workflowIdFromPrompt
} from "@kelpclaw/workflow-spec";
import type {
  WorkflowPlanRequest,
  WorkflowRepromptNodeRequest,
  WorkflowRepromptNodeResponse,
  WorkflowSpec
} from "@kelpclaw/workflow-spec";

export function planWorkflowDraft(request: WorkflowPlanRequest): WorkflowSpec {
  const prompt = request.prompt.trim();
  const template = chooseTemplate(prompt);
  const now = new Date().toISOString();
  const currentWorkflow = request.currentWorkflow;
  const preservedNodes = new Map(
    currentWorkflow?.nodes
      .filter((node) => request.preserveNodeIds?.includes(node.id))
      .map((node) => [node.id, node]) ?? []
  );
  const nodes = template.nodes.map((node) => preservedNodes.get(node.id) ?? node);
  const workflowId = currentWorkflow?.id ?? workflowIdFromPrompt(prompt);

  return {
    ...template,
    id: workflowId,
    name: titleFromPrompt(prompt),
    prompt,
    revision: currentWorkflow ? currentWorkflow.revision + 1 : 1,
    nodes,
    approval: null,
    createdAt: currentWorkflow?.createdAt ?? now,
    updatedAt: now
  };
}

export function repromptWorkflowNode(
  workflow: WorkflowSpec,
  request: WorkflowRepromptNodeRequest
): WorkflowRepromptNodeResponse["after"] {
  const before = workflow.nodes.find((node) => node.id === request.nodeId);
  if (!before) {
    throw new Error(`Workflow node '${request.nodeId}' was not found.`);
  }

  const now = new Date().toISOString();
  const nodePrompt = request.prompt.trim();
  const after = {
    ...before,
    label: labelFromPrompt(nodePrompt, before.label),
    description: nodePrompt || before.description,
    config: {
      ...before.config,
      nodePrompt,
      repromptedAt: now
    },
    ...(before.codegen
      ? {
          codegen: {
            ...before.codegen,
            provenance: {
              ...before.codegen.provenance,
              generatedAt: now,
              sourcePrompt: nodePrompt || before.codegen.provenance.sourcePrompt
            },
            replay: {
              ...before.codegen.replay,
              seed: `${before.id}.reprompt`
            }
          }
        }
      : {})
  };

  return after;
}

export function repromptWorkflow(
  workflow: WorkflowSpec,
  request: WorkflowRepromptNodeRequest
): {
  readonly workflow: WorkflowSpec;
  readonly before: WorkflowRepromptNodeResponse["before"];
  readonly after: WorkflowRepromptNodeResponse["after"];
  readonly diff: WorkflowRepromptNodeResponse["diff"];
} {
  const before = workflow.nodes.find((node) => node.id === request.nodeId);
  if (!before) {
    throw new Error(`Workflow node '${request.nodeId}' was not found.`);
  }

  const after = repromptWorkflowNode(workflow, request);
  const nextWorkflow: WorkflowSpec = {
    ...workflow,
    nodes: workflow.nodes.map((node) => (node.id === request.nodeId ? after : node)),
    approval: null,
    updatedAt: new Date().toISOString()
  };

  return {
    workflow: nextWorkflow,
    before,
    after,
    diff: createWorkflowSpecDiff(workflow, nextWorkflow)
  };
}

function chooseTemplate(prompt: string): WorkflowSpec {
  const normalizedPrompt = prompt.toLowerCase();
  if (
    normalizedPrompt.includes("alert") ||
    normalizedPrompt.includes("telegram") ||
    normalizedPrompt.includes("whatsapp") ||
    normalizedPrompt.includes("support")
  ) {
    return timeSensitiveAlertDeliveryWorkflowFixture;
  }

  if (
    normalizedPrompt.includes("scrape") ||
    normalizedPrompt.includes("status page") ||
    normalizedPrompt.includes("code") ||
    normalizedPrompt.includes("artifact")
  ) {
    return scheduledScrapingWorkflowFixture;
  }

  return gmailReceiptsToSheetsWorkflowFixture;
}

function titleFromPrompt(prompt: string): string {
  return prompt
    .split(/[^a-z0-9]+/iu)
    .filter(Boolean)
    .slice(0, 5)
    .map((token) => token.charAt(0).toUpperCase() + token.slice(1).toLowerCase())
    .join(" ");
}

function labelFromPrompt(prompt: string, fallback: string): string {
  const title = titleFromPrompt(prompt);
  return title || fallback;
}
