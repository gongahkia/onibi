import { expectedNodeKindsForRoute, routeWorkflowTask, routerClassifierVersion } from "./router.js";
const baseRouterEvalCases = [
    {
        id: "deterministic.manual-format",
        prompt: "prepare a manual checklist from a supplied task payload and format the result",
        expectedRoute: "deterministic",
        minConfidence: 0.55
    },
    {
        id: "adapter.gmail-sheets",
        prompt: "extract transaction details from Gmail receipts into Sheets",
        expectedRoute: "adapter",
        minConfidence: 0.7
    },
    {
        id: "adapter.openapi",
        prompt: "call an imported OpenAPI connector and append the response to a spreadsheet",
        expectedRoute: "adapter",
        minConfidence: 0.6
    },
    {
        id: "adapter.mcp-endpoint",
        prompt: "register an MCP endpoint tool and notify Slack when it returns a result",
        expectedRoute: "adapter",
        minConfidence: 0.55
    },
    {
        id: "codegen.status-scrape",
        prompt: "scrape a custom public status page and summarize incidents",
        expectedRoute: "codegen",
        minConfidence: 0.65
    },
    {
        id: "codegen.regex-parse",
        prompt: "parse custom webhook text with regex and produce a normalized artifact",
        expectedRoute: "codegen",
        minConfidence: 0.6
    },
    {
        id: "agentic.research",
        prompt: "research current API options and prepare a sourced recommendation",
        expectedRoute: "agentic",
        minConfidence: 0.65
    },
    {
        id: "agentic.triage",
        prompt: "triage ambiguous support reports, decide severity, and explain the rationale",
        expectedRoute: "agentic",
        minConfidence: 0.65
    },
    {
        id: "deployment.activate",
        prompt: "deploy the approved workflow and activate the runner configuration",
        expectedRoute: "deployment",
        minConfidence: 0.75
    },
    {
        id: "force-deterministic.suppresses-agentic",
        prompt: "research options but force deterministic local planning",
        expectedRoute: "deterministic",
        minConfidence: 0.35,
        forceDeterministic: true
    }
];
export const routerEvalCases = baseRouterEvalCases.map((testCase) => ({
    ...testCase,
    expectedNodeKinds: testCase.expectedNodeKinds ??
        expectedNodeKindsForRoute(testCase.expectedRoute)
}));
export function runRouterEvalCases(input = {}) {
    const cases = input.cases ?? routerEvalCases;
    const createdAt = input.now ?? new Date().toISOString();
    const results = cases.map((testCase) => {
        const route = routeWorkflowTask({
            prompt: testCase.prompt,
            ...(testCase.forceDeterministic ? { forceDeterministic: true } : {})
        }, {
            correlationId: `router-eval.${testCase.id}`,
            provider: input.provider,
            model: input.model,
            now: createdAt
        });
        const failures = [
            route.route === testCase.expectedRoute
                ? null
                : `Expected route ${testCase.expectedRoute}, got ${route.route}.`,
            route.confidence >= testCase.minConfidence
                ? null
                : `Expected confidence >= ${testCase.minConfidence}, got ${route.confidence}.`,
            sameKinds(route.expectedNodeKinds, testCase.expectedNodeKinds ?? [])
                ? null
                : `Expected node kinds ${(testCase.expectedNodeKinds ?? []).join(",")}, got ${route.expectedNodeKinds.join(",")}.`
        ].filter((failure) => failure !== null);
        return {
            id: testCase.id,
            prompt: testCase.prompt,
            expectedRoute: testCase.expectedRoute,
            actualRoute: route.route,
            confidence: route.confidence,
            passed: failures.length === 0,
            route,
            failures
        };
    });
    const failed = results.filter((result) => !result.passed).length;
    return {
        id: `router-eval.${createdAt}`,
        classifierVersion: routerClassifierVersion,
        createdAt,
        passed: failed === 0,
        total: results.length,
        failed,
        results
    };
}
function sameKinds(left, right) {
    return left.join("\n") === right.join("\n");
}
export function routerEvalSummary(run) {
    const failures = run.results
        .filter((result) => !result.passed)
        .map((result) => `${result.id}: ${result.failures.join(" ")}`);
    return failures.length === 0
        ? `Router eval passed ${run.total}/${run.total} cases with ${run.classifierVersion}.`
        : `Router eval failed ${run.failed}/${run.total} cases:\n${failures.join("\n")}`;
}
export function routeKindFromString(value) {
    return ["deterministic", "adapter", "codegen", "agentic", "deployment"].includes(value)
        ? value
        : undefined;
}
//# sourceMappingURL=router-evals.js.map