import type { ClientScope } from "./types";
import { normalizeBaseUrl } from "./utils";

export class HttpStatusError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

export function authedFetch(
  baseUrl: string,
  token: string,
  path: string,
  init: RequestInit = {},
): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("authorization", `Bearer ${token}`);
  if (init.body && !headers.has("content-type")) {
    headers.set("content-type", "application/json");
  }
  return fetch(`${normalizeBaseUrl(baseUrl)}${path}`, { ...init, headers }).then((response) => {
    if (!response.ok) {
      throw new HttpStatusError(`${path} failed with HTTP ${response.status}`, response.status);
    }
    return response;
  });
}

export function shouldMarkAuthInvalid(error: unknown, scope: ClientScope): boolean {
  return (
    error instanceof HttpStatusError &&
    (error.status === 401 || (scope === "full" && error.status === 403))
  );
}
