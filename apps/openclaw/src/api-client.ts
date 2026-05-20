import type {
  JsonRecord,
  WorkflowAcceptPlanRequest,
  WorkflowAcceptPlanResponse,
  WorkflowApproveRequest,
  WorkflowApproveResponse,
  WorkflowBranchMergePreviewRequest,
  WorkflowBranchMergePreviewResponse,
  WorkflowBranchMergeRequest,
  WorkflowBranchMergeResponse,
  WorkflowBranchPlanRequest,
  WorkflowBranchPlanResponse,
  WorkflowBranchRepromptNodeRequest,
  WorkflowBranchRepromptNodeResponse,
  WorkflowCreateBranchRequest,
  WorkflowCreateBranchResponse,
  WorkflowDraftEvaluation,
  WorkflowFeedbackRequest,
  WorkflowFeedbackResponse,
  WorkflowGetBranchResponse,
  WorkflowListBranchesResponse,
  WorkflowFetchRunResponse,
  WorkflowPlanRequest,
  WorkflowPlanResponse,
  WorkflowPlanSuccessResponse,
  WorkflowPlannerSuggestionDecisionRequest,
  WorkflowPlannerSuggestionDecisionResponse,
  WorkflowRepromptNodeRequest,
  WorkflowRepromptNodeResponse,
  WorkflowJob,
  WorkflowJobEvent,
  WorkflowWorkspace,
  WorkflowDeploymentKind,
  WorkflowDeploymentRecord,
  WorkflowReuseCandidatesResponse,
  WorkflowStartRunRequest,
  WorkflowStartRunResponse,
  WorkflowUpdateBranchRequest,
  WorkflowUpdateBranchResponse,
  WorkflowValidateRequest,
  WorkflowValidateResponse
} from "@kelpclaw/workflow-spec";

export interface DeploymentActivationSummaryResponse {
  readonly ok: true;
  readonly activeDeployments: readonly WorkflowDeploymentRecord[];
  readonly activeSchedules: readonly JsonRecord[];
  readonly runnerConfigurations: readonly JsonRecord[];
  readonly skillPublications: readonly JsonRecord[];
  readonly integrationBindings: readonly JsonRecord[];
  readonly bundles: readonly JsonRecord[];
  readonly generatedServices: readonly JsonRecord[];
}

export interface CodegenReviewRequest {
  readonly status: "approved" | "rejected";
  readonly reviewedBy: string;
  readonly notes?: string | undefined;
  readonly branchId?: string | undefined;
}

export interface CodegenReviewResponse {
  readonly ok: true;
  readonly workflow: WorkflowPlanSuccessResponse["workflow"];
  readonly draftRevision: WorkflowPlanSuccessResponse["draftRevision"];
  readonly validation: WorkflowValidateResponse["validation"];
  readonly node: unknown;
}

export interface CodegenPromotionResponse {
  readonly ok: true;
  readonly skill: {
    readonly id: string;
    readonly name: string;
  };
  readonly artifact: {
    readonly path: string;
    readonly checksum: string;
    readonly contentType: string;
  };
}

export interface CodegenBuildResponse {
  readonly ok: true;
  readonly workflow: WorkflowPlanSuccessResponse["workflow"];
  readonly draftRevision: WorkflowPlanSuccessResponse["draftRevision"];
  readonly validation: WorkflowValidateResponse["validation"];
  readonly job: WorkflowJob;
  readonly workspace: WorkflowWorkspace;
  readonly agentRuns: readonly unknown[];
  readonly artifacts: readonly unknown[];
  readonly testReport: unknown;
  readonly evalReport: unknown;
}

export interface CodegenEvalsResponse {
  readonly ok: true;
  readonly agentRuns: readonly unknown[];
  readonly agentArtifacts: readonly unknown[];
  readonly testReports: readonly unknown[];
  readonly evalReports: readonly unknown[];
}

export interface SecretMetadata {
  readonly name: string;
  readonly createdAt: string;
  readonly updatedAt: string;
}

export interface IntegrationReadiness {
  readonly id: string;
  readonly ready: boolean;
  readonly requiredSecrets: readonly string[];
}

export interface SecretListResponse {
  readonly ok: true;
  readonly secrets: readonly SecretMetadata[];
  readonly integrations: readonly IntegrationReadiness[];
}

export interface GoogleIntegrationStatusResponse {
  readonly ok: true;
  readonly connected: boolean;
}

export interface GoogleConnectResponse {
  readonly ok: true;
  readonly url: string;
  readonly state: string;
}

export class OpenClawApiError extends Error {
  public readonly status: number;

  public constructor(status: number, message: string) {
    super(message);
    this.name = "OpenClawApiError";
    this.status = status;
  }
}

export const openClawApi = {
  plan(request: WorkflowPlanRequest, jobId?: string | undefined): Promise<WorkflowPlanResponse> {
    return postJson(
      "/api/workflows/plan",
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  validate(
    workflowId: string,
    request: WorkflowValidateRequest
  ): Promise<WorkflowValidateResponse> {
    return postJson(`/api/workflows/${encodeURIComponent(workflowId)}/validate`, request);
  },

  repromptNode(
    workflowId: string,
    request: WorkflowRepromptNodeRequest
  ): Promise<WorkflowRepromptNodeResponse> {
    return postJson(`/api/workflows/${encodeURIComponent(workflowId)}/reprompt-node`, request);
  },

  feedback(
    workflowId: string,
    request: WorkflowFeedbackRequest,
    jobId?: string | undefined
  ): Promise<WorkflowFeedbackResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/feedback`,
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  decideSuggestion(
    workflowId: string,
    feedbackId: string,
    suggestionId: string,
    request: WorkflowPlannerSuggestionDecisionRequest
  ): Promise<WorkflowPlannerSuggestionDecisionResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/feedback/${encodeURIComponent(feedbackId)}/suggestions/${encodeURIComponent(suggestionId)}/decision`,
      request
    );
  },

  evaluateDraft(
    workflowId: string,
    request: {
      readonly workflow: WorkflowPlanSuccessResponse["workflow"];
      readonly mockOnly: true;
      readonly branchId?: string | undefined;
    },
    jobId?: string | undefined
  ): Promise<{ readonly ok: true; readonly evaluation: WorkflowDraftEvaluation }> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/evaluate-draft`,
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  approve(workflowId: string, request: WorkflowApproveRequest): Promise<WorkflowApproveResponse> {
    return postJson(`/api/workflows/${encodeURIComponent(workflowId)}/approve`, request);
  },

  acceptPlan(
    workflowId: string,
    request: WorkflowAcceptPlanRequest
  ): Promise<WorkflowAcceptPlanResponse> {
    return postJson(`/api/workflows/${encodeURIComponent(workflowId)}/accept-plan`, request);
  },

  createBranch(
    workflowId: string,
    request: WorkflowCreateBranchRequest
  ): Promise<WorkflowCreateBranchResponse> {
    return postJson(`/api/workflows/${encodeURIComponent(workflowId)}/branches`, request);
  },

  listBranches(workflowId: string): Promise<WorkflowListBranchesResponse> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/branches`);
  },

  fetchBranch(workflowId: string, branchId: string): Promise<WorkflowGetBranchResponse> {
    return getJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(branchId)}`
    );
  },

  updateBranch(
    workflowId: string,
    branchId: string,
    request: WorkflowUpdateBranchRequest
  ): Promise<WorkflowUpdateBranchResponse> {
    return patchJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(branchId)}`,
      request
    );
  },

  planBranch(
    workflowId: string,
    branchId: string,
    request: WorkflowBranchPlanRequest,
    jobId?: string | undefined
  ): Promise<WorkflowBranchPlanResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(branchId)}/plan`,
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  repromptBranchNode(
    workflowId: string,
    branchId: string,
    request: WorkflowBranchRepromptNodeRequest
  ): Promise<WorkflowBranchRepromptNodeResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(branchId)}/reprompt-node`,
      request
    );
  },

  acceptBranchPlan(
    workflowId: string,
    branchId: string,
    request: WorkflowAcceptPlanRequest
  ): Promise<WorkflowAcceptPlanResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(branchId)}/accept-plan`,
      request
    );
  },

  previewBranchMerge(
    workflowId: string,
    sourceBranchId: string,
    request: WorkflowBranchMergePreviewRequest
  ): Promise<WorkflowBranchMergePreviewResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(sourceBranchId)}/merge-preview`,
      request
    );
  },

  mergeBranch(
    workflowId: string,
    sourceBranchId: string,
    request: WorkflowBranchMergeRequest
  ): Promise<WorkflowBranchMergeResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(sourceBranchId)}/merge`,
      request
    );
  },

  fetchReuseCandidates(
    workflowId: string,
    branchId: string
  ): Promise<WorkflowReuseCandidatesResponse> {
    return getJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(branchId)}/reuse-candidates`
    );
  },

  reviewCodegen(
    workflowId: string,
    nodeId: string,
    request: CodegenReviewRequest
  ): Promise<CodegenReviewResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/codegen/${encodeURIComponent(nodeId)}/review`,
      request
    );
  },

  promoteCodegen(workflowId: string, nodeId: string): Promise<CodegenPromotionResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/codegen/${encodeURIComponent(nodeId)}/promote`,
      {}
    );
  },

  buildCodegen(
    workflowId: string,
    nodeId: string,
    request: {
      readonly maxIterations?: number;
      readonly maxWallClockSeconds?: number;
      readonly maxModelCostUsd?: number;
      readonly runTestsInDocker?: boolean;
      readonly branchId?: string | undefined;
    },
    jobId?: string | undefined
  ): Promise<CodegenBuildResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/codegen/${encodeURIComponent(nodeId)}/build`,
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  fetchCodegenEvals(workflowId: string, nodeId: string): Promise<CodegenEvalsResponse> {
    return getJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/codegen/${encodeURIComponent(nodeId)}/evals`
    );
  },

  startRun(
    workflowId: string,
    request: WorkflowStartRunRequest,
    jobId?: string | undefined
  ): Promise<WorkflowStartRunResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/runs`,
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  fetchRun(workflowId: string, runId: string): Promise<WorkflowFetchRunResponse> {
    return getJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/runs/${encodeURIComponent(runId)}`
    );
  },

  listSecrets(): Promise<SecretListResponse> {
    return getJson("/api/secrets");
  },

  upsertSecret(
    name: string,
    value: string
  ): Promise<{ readonly ok: true; readonly secret: SecretMetadata }> {
    return putJson("/api/secrets", { name, value });
  },

  deleteSecret(name: string): Promise<{ readonly ok: true; readonly deleted: boolean }> {
    return deleteJson(`/api/secrets/${encodeURIComponent(name)}`);
  },

  googleStatus(): Promise<GoogleIntegrationStatusResponse> {
    return getJson("/api/integrations/google/status");
  },

  googleConnect(): Promise<GoogleConnectResponse> {
    return getJson("/api/integrations/google/connect");
  },

  googleRevoke(): Promise<{ readonly ok: true; readonly deleted: boolean }> {
    return postJson("/api/integrations/google/revoke", {});
  },

  createJob(request: {
    readonly type: WorkflowJob["type"];
    readonly workflowId?: string;
    readonly revisionId?: string;
    readonly nodeId?: string;
    readonly maxAttempts?: number;
  }): Promise<{ readonly ok: true; readonly job: WorkflowJob }> {
    return postJson("/api/jobs", request);
  },

  fetchJob(jobId: string): Promise<{ readonly ok: true; readonly job: WorkflowJob }> {
    return getJson(`/api/jobs/${encodeURIComponent(jobId)}`);
  },

  cancelJob(
    jobId: string,
    reason: string
  ): Promise<{ readonly ok: true; readonly job: WorkflowJob }> {
    return postJson(`/api/jobs/${encodeURIComponent(jobId)}/cancel`, { reason });
  },

  fetchWorkspace(
    workspaceId: string
  ): Promise<{ readonly ok: true; readonly workspace: WorkflowWorkspace }> {
    return getJson(`/api/workspaces/${encodeURIComponent(workspaceId)}`);
  },

  fetchDeployments(
    workflowId: string
  ): Promise<{ readonly ok: true; readonly deployments: readonly WorkflowDeploymentRecord[] }> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/deployments`);
  },

  fetchActiveDeployments(workflowId: string): Promise<DeploymentActivationSummaryResponse> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/deployments/active`);
  },

  deployWorkflow(
    workflowId: string,
    request: {
      readonly approvedRevisionId: string;
      readonly kind: WorkflowDeploymentKind;
      readonly createdBy: string;
      readonly rollbackPlan: string;
      readonly branchId?: string | undefined;
      readonly metadata?: Record<string, unknown>;
    },
    jobId?: string | undefined
  ): Promise<{ readonly ok: true; readonly deployment: WorkflowDeploymentRecord }> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/deployments`,
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  async streamJobEvents(
    jobId: string,
    onEvent: (event: WorkflowJobEvent | WorkflowJob) => void
  ): Promise<void> {
    const response = await fetch(`/api/jobs/${encodeURIComponent(jobId)}/events`, {
      headers: authHeader()
    });
    if (!response.ok || !response.body) {
      await parseJsonResponse(response);
      return;
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      buffer += decoder.decode(value, { stream: true });
      const chunks = buffer.split("\n\n");
      buffer = chunks.pop() ?? "";
      for (const chunk of chunks) {
        const line = chunk.split("\n").find((candidate) => candidate.startsWith("data: "));
        if (line) {
          onEvent(JSON.parse(line.slice("data: ".length)) as WorkflowJobEvent | WorkflowJob);
        }
      }
    }
  }
};

async function postJson<TResponse>(
  url: string,
  body: unknown,
  extraHeaders: Record<string, string> | undefined = undefined
): Promise<TResponse> {
  const response = await fetch(url, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      ...authHeader(),
      ...(extraHeaders ?? {})
    },
    body: JSON.stringify(body)
  });

  return parseJsonResponse<TResponse>(response);
}

async function putJson<TResponse>(url: string, body: unknown): Promise<TResponse> {
  const response = await fetch(url, {
    method: "PUT",
    headers: {
      "content-type": "application/json",
      ...authHeader()
    },
    body: JSON.stringify(body)
  });

  return parseJsonResponse<TResponse>(response);
}

async function patchJson<TResponse>(url: string, body: unknown): Promise<TResponse> {
  const response = await fetch(url, {
    method: "PATCH",
    headers: {
      "content-type": "application/json",
      ...authHeader()
    },
    body: JSON.stringify(body)
  });

  return parseJsonResponse<TResponse>(response);
}

async function deleteJson<TResponse>(url: string): Promise<TResponse> {
  const response = await fetch(url, {
    method: "DELETE",
    headers: authHeader()
  });

  return parseJsonResponse<TResponse>(response);
}

async function getJson<TResponse>(url: string): Promise<TResponse> {
  const response = await fetch(url, {
    headers: authHeader()
  });
  return parseJsonResponse<TResponse>(response);
}

async function parseJsonResponse<TResponse>(response: Response): Promise<TResponse> {
  const payload = (await response.json()) as { readonly message?: string; readonly error?: string };
  if (!response.ok) {
    throw new OpenClawApiError(
      response.status,
      payload.message ?? payload.error ?? `OpenClaw API request failed with ${response.status}.`
    );
  }

  return payload as TResponse;
}

export function readOpenClawAdminToken(): string {
  const stored = readLocalStorage("kelpclaw.adminToken");
  const env = (import.meta as ImportMeta & { readonly env?: Record<string, string | undefined> })
    .env;
  return stored || env?.VITE_OPENCLAW_ADMIN_TOKEN || "";
}

export function saveOpenClawAdminToken(token: string): void {
  writeLocalStorage("kelpclaw.adminToken", token.trim());
}

function authHeader(): Record<string, string> {
  const token = readOpenClawAdminToken();
  return token ? { authorization: `Bearer ${token}` } : {};
}

function readLocalStorage(key: string): string {
  try {
    return globalThis.localStorage?.getItem(key) ?? "";
  } catch {
    return "";
  }
}

function writeLocalStorage(key: string, value: string): void {
  try {
    if (value.length === 0) {
      globalThis.localStorage?.removeItem(key);
    } else {
      globalThis.localStorage?.setItem(key, value);
    }
  } catch {
    // The token remains in component state when storage is unavailable.
  }
}
