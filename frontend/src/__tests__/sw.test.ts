import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

const origin = "https://onibi.test";

type FetchHandler = (event: {
  request: Pick<Request, "method" | "url">;
  respondWith: (response: Promise<Response>) => void;
}) => void;

let handlers: Map<string, FetchHandler>;

beforeEach(async () => {
  handlers = new Map();
  const response = { ok: true, clone: () => response } as Response;
  vi.stubGlobal("caches", {
    open: vi.fn().mockResolvedValue({
      match: vi.fn().mockResolvedValue(undefined),
      put: vi.fn().mockResolvedValue(undefined)
    })
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
  vi.stubGlobal("self", {
    addEventListener: (type: string, handler: FetchHandler) => handlers.set(type, handler)
  });
  vi.stubGlobal("location", { origin });
  vi.resetModules();
  await import("../sw");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function respondWithFor(method: string, url: string): ReturnType<typeof vi.fn> {
  const respondWith = vi.fn();
  handlers.get("fetch")?.({ request: { method, url }, respondWith });
  return respondWith;
}

describe("service-worker cache policy", () => {
  test("caches only same-origin query-free static assets", () => {
    expect(respondWithFor("GET", `${origin}/assets/app.js`)).toHaveBeenCalledOnce();
    expect(respondWithFor("GET", `${origin}/assets/app.js?pair=secret`)).not.toHaveBeenCalled();
    expect(respondWithFor("POST", `${origin}/assets/app.js`)).not.toHaveBeenCalled();
    expect(respondWithFor("GET", `${origin}/session-info`)).not.toHaveBeenCalled();
    expect(respondWithFor("GET", "https://other.test/assets/app.js")).not.toHaveBeenCalled();
  });
});
