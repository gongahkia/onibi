import { afterEach, describe, expect, it, vi } from "vitest";
import { runCrossAgentReplaySmoke, runOtlpSmoke } from "../src/index.js";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  process.exitCode = undefined;
});

describe("kelp-claw smoke commands", () => {
  it("summarizes equivalent replay shapes across agent sources", () => {
    const result = runCrossAgentReplaySmoke();

    expect(result).toMatchObject({
      ok: true,
      skillName: "kelpclaw-replay-smoke",
      agents: ["claude-code", "codex-cli", "goose"],
      eventCount: 2,
      tools: ["Bash", "Read"]
    });
    expect(result.agentTags).toEqual([
      ["claude-code", "claude-code"],
      ["codex-cli", "codex-cli"],
      ["goose", "goose"]
    ]);
  });

  it("posts an OTLP trace with one span per smoke tool call", async () => {
    const requests: {
      readonly url: string;
      readonly headers: Record<string, string>;
      readonly body: Record<string, unknown>;
    }[] = [];
    vi.stubEnv("DD_API_KEY", "dd-test-key");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
        requests.push({
          url: String(url),
          headers: init?.headers as Record<string, string>,
          body: JSON.parse(String(init?.body)) as Record<string, unknown>
        });
        return new Response("", { status: 202 });
      })
    );

    const result = await runOtlpSmoke([
      "--endpoint",
      "http://collector.test/v1/traces",
      "--header",
      "x-smoke=local",
      "--run-id",
      "agent-run.test",
      "--skill-id",
      "skill.test",
      "--promoted-at",
      "2026-05-24T00:00:00.000Z"
    ]);

    expect(result).toMatchObject({
      ok: true,
      endpoint: "http://collector.test/v1/traces",
      statusCode: 202,
      traceCount: 1,
      spanCount: 2,
      runId: "agent-run.test",
      skillId: "skill.test",
      headerNames: ["DD-API-KEY", "x-smoke"]
    });
    expect(requests).toHaveLength(1);
    expect(requests[0]?.headers).toMatchObject({
      "DD-API-KEY": "dd-test-key",
      "x-smoke": "local"
    });
    expect(spanNames(requests[0]?.body)).toEqual(["Bash PostToolUse", "Read PostToolUse"]);
  });
});

function spanNames(body: Record<string, unknown> | undefined): readonly string[] {
  const resourceSpans = Array.isArray(body?.resourceSpans) ? body.resourceSpans : [];
  const firstResource = resourceSpans[0] as { readonly scopeSpans?: unknown } | undefined;
  const scopeSpans = Array.isArray(firstResource?.scopeSpans) ? firstResource.scopeSpans : [];
  const firstScope = scopeSpans[0] as { readonly spans?: unknown } | undefined;
  const spans = Array.isArray(firstScope?.spans) ? firstScope.spans : [];
  return spans
    .map((span) =>
      span && typeof span === "object" && "name" in span ? String(span.name) : undefined
    )
    .filter((name): name is string => Boolean(name));
}
