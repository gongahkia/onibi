import type {
  WorkflowApproveRequest,
  WorkflowApproveResponse,
  WorkflowFetchRunResponse,
  WorkflowPlanRequest,
  WorkflowPlanResponse,
  WorkflowRepromptNodeRequest,
  WorkflowRepromptNodeResponse,
  WorkflowStartRunRequest,
  WorkflowStartRunResponse,
  WorkflowValidateRequest,
  WorkflowValidateResponse
} from "@kelpclaw/workflow-spec";

export interface CodegenReviewRequest {
  readonly status: "approved" | "rejected";
  readonly reviewedBy: string;
  readonly notes?: string | undefined;
}

export interface CodegenReviewResponse {
  readonly ok: true;
  readonly workflow: WorkflowPlanResponse["workflow"];
  readonly draftRevision: WorkflowPlanResponse["draftRevision"];
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

export class OpenClawApiError extends Error {
  public readonly status: number;

  public constructor(status: number, message: string) {
    super(message);
    this.name = "OpenClawApiError";
    this.status = status;
  }
}

export const openClawApi = {
  plan(request: WorkflowPlanRequest): Promise<WorkflowPlanResponse> {
    return postJson("/api/workflows/plan", request);
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

  approve(workflowId: string, request: WorkflowApproveRequest): Promise<WorkflowApproveResponse> {
    return postJson(`/api/workflows/${encodeURIComponent(workflowId)}/approve`, request);
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

  startRun(
    workflowId: string,
    request: WorkflowStartRunRequest
  ): Promise<WorkflowStartRunResponse> {
    return postJson(`/api/workflows/${encodeURIComponent(workflowId)}/runs`, request);
  },

  fetchRun(workflowId: string, runId: string): Promise<WorkflowFetchRunResponse> {
    return getJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/runs/${encodeURIComponent(runId)}`
    );
  }
};

async function postJson<TResponse>(url: string, body: unknown): Promise<TResponse> {
  const response = await fetch(url, {
    method: "POST",
    headers: {
      "content-type": "application/json"
    },
    body: JSON.stringify(body)
  });

  return parseJsonResponse<TResponse>(response);
}

async function getJson<TResponse>(url: string): Promise<TResponse> {
  const response = await fetch(url);
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
