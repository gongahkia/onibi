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
  }
};

async function postJson<TResponse>(url: string, body: unknown): Promise<TResponse> {
  const response = await fetch(url, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      ...authHeader()
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
