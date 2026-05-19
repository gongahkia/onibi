import type {
  WorkflowAgentRole,
  WorkflowModelInvocationRecord,
  WorkflowModelRequirement,
  WorkflowNodeKind,
  WorkflowPlanRequest,
  WorkflowTaskRoute,
  WorkflowTaskRouteKind
} from "@kelpclaw/workflow-spec";

export interface RouteWorkflowTaskOptions {
  readonly correlationId: string;
  readonly provider?: string | undefined;
  readonly model?: string | undefined;
  readonly now?: string | undefined;
}

export function routeWorkflowTask(
  request: WorkflowPlanRequest,
  options: RouteWorkflowTaskOptions
): WorkflowTaskRoute {
  const prompt = request.prompt.trim();
  const normalized = prompt.toLowerCase();
  const route = classifyPrompt(normalized, request.forceDeterministic === true);
  const requiredModel = modelRequirementForRoute(route, options);
  const modelInvocations =
    requiredModel.mode === "none"
      ? []
      : [
          createModelInvocation({
            route,
            prompt,
            requiredModel,
            correlationId: options.correlationId,
            now: options.now
          })
        ];

  return {
    route,
    rationale: rationaleForRoute(route, normalized),
    requiredModel,
    expectedNodeKinds: expectedNodeKindsForRoute(route),
    dockerSandboxRequired: route === "codegen" || route === "agentic",
    draftTestsRequired: route === "codegen" || route === "agentic" || route === "deployment",
    productionDeterministic: route !== "agentic",
    modelInvocations
  };
}

function classifyPrompt(
  normalizedPrompt: string,
  forceDeterministic: boolean
): WorkflowTaskRouteKind {
  if (
    normalizedPrompt.includes("deploy") ||
    normalizedPrompt.includes("activate") ||
    normalizedPrompt.includes("schedule deployment") ||
    normalizedPrompt.includes("publish")
  ) {
    return "deployment";
  }

  if (!forceDeterministic && agenticTerms.some((term) => normalizedPrompt.includes(term))) {
    return "agentic";
  }

  if (codegenTerms.some((term) => normalizedPrompt.includes(term))) {
    return "codegen";
  }

  if (adapterTerms.some((term) => normalizedPrompt.includes(term))) {
    return "adapter";
  }

  return "deterministic";
}

function modelRequirementForRoute(
  route: WorkflowTaskRouteKind,
  options: RouteWorkflowTaskOptions
): WorkflowModelRequirement {
  const retryBudget = {
    maxAttempts: route === "agentic" || route === "codegen" ? 2 : 1,
    maxCostUsd: route === "agentic" ? 2 : route === "codegen" ? 1 : 0
  };

  if (route === "deterministic" || route === "adapter") {
    return {
      mode: "none",
      role: "classifier",
      retryBudget
    };
  }

  return {
    mode: "live",
    role: roleForRoute(route),
    provider: options.provider ?? "anthropic",
    model: options.model ?? "default",
    retryBudget
  };
}

function createModelInvocation(input: {
  readonly route: WorkflowTaskRouteKind;
  readonly prompt: string;
  readonly requiredModel: WorkflowModelRequirement;
  readonly correlationId: string;
  readonly now?: string | undefined;
}): WorkflowModelInvocationRecord {
  return {
    id: `model.${input.route}.${input.correlationId}`,
    role: input.requiredModel.role,
    inputSummary: input.prompt.slice(0, 240),
    outputArtifact: `route:${input.route}`,
    provider: input.requiredModel.provider ?? "none",
    model: input.requiredModel.model ?? "none",
    determinismExpectation: input.route === "agentic" ? "bounded" : "deterministic",
    retryBudget: input.requiredModel.retryBudget,
    correlationId: input.correlationId,
    createdAt: input.now ?? new Date().toISOString()
  };
}

function roleForRoute(route: WorkflowTaskRouteKind): WorkflowAgentRole {
  switch (route) {
    case "codegen":
      return "workflow-architect";
    case "agentic":
      return "agentic-node-designer";
    case "deployment":
      return "planner";
    case "adapter":
    case "deterministic":
      return "classifier";
  }
}

function expectedNodeKindsForRoute(route: WorkflowTaskRouteKind): readonly WorkflowNodeKind[] {
  switch (route) {
    case "deterministic":
      return ["trigger", "transform", "delivery"];
    case "adapter":
      return ["trigger", "skill", "transform", "delivery"];
    case "codegen":
      return ["trigger", "codegen", "transform", "delivery"];
    case "agentic":
      return ["trigger", "skill", "approval", "delivery"];
    case "deployment":
      return ["approval", "delivery"];
  }
}

function rationaleForRoute(route: WorkflowTaskRouteKind, normalizedPrompt: string): string {
  switch (route) {
    case "deterministic":
      return "Prompt can be represented as a fixed workflow graph without live model planning.";
    case "adapter":
      return "Prompt references provider-backed integrations that match existing adapter workflow templates.";
    case "codegen":
      return "Prompt requests custom deterministic behavior that requires generated node artifacts.";
    case "agentic":
      return "Prompt asks for runtime investigation, tool use, or adaptive decisions that require bounded agentic behavior.";
    case "deployment":
      return normalizedPrompt.includes("publish")
        ? "Prompt asks to publish or activate an approved workflow artifact."
        : "Prompt asks for workflow-native deployment or activation.";
  }
}

const adapterTerms = [
  "gmail",
  "sheets",
  "email",
  "telegram",
  "whatsapp",
  "slack",
  "adapter",
  "integration"
] as const;

const codegenTerms = [
  "scrape",
  "regex",
  "custom code",
  "code",
  "artifact",
  "api call",
  "parse custom",
  "generated"
] as const;

const agenticTerms = [
  "investigate",
  "decide",
  "severity",
  "triage",
  "compare multiple",
  "adapt",
  "agent",
  "reason",
  "research"
] as const;
