export const routerClassifierVersion = "kelpclaw.router.scored-v1";
export function routeWorkflowTask(request, options) {
    const prompt = request.prompt.trim();
    const normalized = normalizePrompt(prompt);
    const scores = scoreRoutes(normalized);
    const forceDeterministic = request.forceDeterministic === true;
    const selected = selectRoute(scores, forceDeterministic);
    const route = selected.route;
    const requiredModel = modelRequirementForRoute(route, options);
    const modelInvocations = requiredModel.mode === "none"
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
        rationale: rationaleForRoute(route, normalized, selected.positiveSignals),
        requiredModel,
        expectedNodeKinds: expectedNodeKindsForRoute(route),
        dockerSandboxRequired: route === "codegen" || route === "agentic",
        draftTestsRequired: route === "codegen" || route === "agentic" || route === "deployment",
        productionDeterministic: route !== "agentic",
        modelInvocations,
        classifierVersion: routerClassifierVersion,
        confidence: routeConfidence(selected, scores),
        scores: scores.map(routeScoreView),
        alternatives: routeAlternatives(scores, selected, forceDeterministic),
        matchedSignals: selected.positiveSignals
    };
}
function normalizePrompt(prompt) {
    return prompt
        .toLowerCase()
        .normalize("NFKC")
        .replace(/[^\p{L}\p{N}./:_-]+/gu, " ")
        .replace(/\s+/gu, " ")
        .trim();
}
function scoreRoutes(normalizedPrompt) {
    return routeDefinitions
        .map((definition) => {
        const positiveSignals = matchedSignals(definition.positive, normalizedPrompt);
        const negativeSignals = matchedSignals(definition.negative, normalizedPrompt);
        return {
            route: definition.route,
            score: definition.baseline +
                positiveSignals.reduce((sum, signal) => sum + signal.weight, 0) -
                negativeSignals.reduce((sum, signal) => sum + signal.weight, 0),
            positiveSignals: positiveSignals.map((signal) => signal.label),
            negativeSignals: negativeSignals.map((signal) => signal.label)
        };
    })
        .sort(compareScores);
}
function matchedSignals(signals, normalizedPrompt) {
    return signals.filter((signal) => signal.pattern.test(normalizedPrompt));
}
function selectRoute(scores, forceDeterministic) {
    const ranked = forceDeterministic ? scores.filter((score) => score.route !== "agentic") : scores;
    return ranked[0] ?? scores[0] ?? fallbackDeterministicScore;
}
function routeConfidence(selected, scores) {
    const runnerUp = scores.find((score) => score.route !== selected.route);
    const margin = selected.score - (runnerUp?.score ?? 0);
    const positiveSignalBoost = Math.min(selected.positiveSignals.length * 0.08, 0.24);
    return clamp(0.5 + margin / 10 + positiveSignalBoost, 0.35, 0.99);
}
function routeScoreView(score) {
    return {
        route: score.route,
        score: Number(score.score.toFixed(3)),
        positiveSignals: score.positiveSignals,
        negativeSignals: score.negativeSignals
    };
}
function routeAlternatives(scores, selected, forceDeterministic) {
    return scores
        .filter((score) => score.route !== selected.route)
        .slice(0, 4)
        .map((score) => ({
        route: score.route,
        score: Number(score.score.toFixed(3)),
        reason: score.positiveSignals.length > 0
            ? `Matched ${score.positiveSignals.join(", ")}.`
            : "No stronger route-specific signals matched.",
        suppressed: forceDeterministic && score.route === "agentic"
    }));
}
function compareScores(left, right) {
    return (right.score - left.score ||
        routePriority.indexOf(left.route) - routePriority.indexOf(right.route) ||
        left.route.localeCompare(right.route));
}
function clamp(value, min, max) {
    return Math.min(max, Math.max(min, Number(value.toFixed(3))));
}
function modelRequirementForRoute(route, options) {
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
function createModelInvocation(input) {
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
function roleForRoute(route) {
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
export function expectedNodeKindsForRoute(route) {
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
function rationaleForRoute(route, normalizedPrompt, matched) {
    const suffix = matched.length > 0 ? ` Signals: ${matched.join(", ")}.` : "";
    switch (route) {
        case "deterministic":
            return `Prompt can be represented as a fixed workflow graph without live model planning.${suffix}`;
        case "adapter":
            return `Prompt references provider-backed integrations that match adapter workflow templates.${suffix}`;
        case "codegen":
            return `Prompt requests custom deterministic behavior that requires generated node artifacts.${suffix}`;
        case "agentic":
            return `Prompt asks for runtime investigation, tool use, or adaptive decisions that require bounded agentic behavior.${suffix}`;
        case "deployment":
            return normalizedPrompt.includes("publish")
                ? `Prompt asks to publish or activate an approved workflow artifact.${suffix}`
                : `Prompt asks for workflow-native deployment or activation.${suffix}`;
    }
}
const routePriority = [
    "deployment",
    "agentic",
    "codegen",
    "adapter",
    "deterministic"
];
const fallbackDeterministicScore = {
    route: "deterministic",
    score: 1,
    positiveSignals: [],
    negativeSignals: []
};
const routeDefinitions = [
    {
        route: "deployment",
        baseline: 0,
        positive: [
            signal("deploy", /\b(deploy|deployment|activate|activation|publish|release)\b/u, 5),
            signal("schedule deployment", /\bschedule deployment\b/u, 4),
            signal("runner configuration", /\brunner[ -]?configuration\b/u, 4),
            signal("approved artifact", /\bapproved (workflow|artifact|revision)\b/u, 3)
        ],
        negative: [signal("research", /\b(research|investigate)\b/u, 2)]
    },
    {
        route: "agentic",
        baseline: 0,
        positive: [
            signal("research", /\b(research|investigate|source|sources|brief|recommendation)\b/u, 4),
            signal("adaptive decision", /\b(decide|triage|reason|compare|adapt|classify)\b/u, 3),
            signal("agent", /\b(agent|agentic|autonomous)\b/u, 4),
            signal("tool use", /\b(tool use|use tools|mcp tool|web search|browse)\b/u, 3),
            signal("uncertain task", /\b(unknown|ambiguous|open-ended|evaluate options)\b/u, 2)
        ],
        negative: [
            signal("fixed adapter", /\b(gmail|sheets|email|telegram|whatsapp|slack)\b/u, 1),
            signal("custom code", /\b(code|scrape|regex|parse)\b/u, 1)
        ]
    },
    {
        route: "codegen",
        baseline: 0,
        positive: [
            signal("scrape", /\b(scrape|crawler|crawl|status page)\b/u, 4),
            signal("custom code", /\b(custom code|write code|generated code|artifact|codegen)\b/u, 4),
            signal("parse", /\b(regex|parse custom|extract with regex|api call)\b/u, 3),
            signal("http transform", /\b(custom http|public api|json api)\b/u, 2)
        ],
        negative: [
            signal("plain email", /\b(email|gmail|sheets)\b/u, 1),
            signal("research", /\b(research|investigate)\b/u, 1)
        ]
    },
    {
        route: "adapter",
        baseline: 0,
        positive: [
            signal("gmail", /\bgmail\b/u, 4),
            signal("sheets", /\b(sheets|spreadsheet|google sheet)\b/u, 4),
            signal("delivery adapter", /\b(email|telegram|whatsapp|slack|discord|github|notion|linear|jira|airtable|webhook|smtp)\b/u, 3),
            signal("database", /\b(database|db|sql|sqlite|postgres|postgresql|mysql)\b/u, 4),
            signal("connector", /\b(adapter|integration|connector|openapi|mcp endpoint)\b/u, 3),
            signal("sync", /\b(sync|append|send|notify|extract transaction|receipt)\b/u, 2)
        ],
        negative: [
            signal("open-ended", /\b(research|investigate|decide|reason)\b/u, 2),
            signal("custom code", /\b(custom code|regex|scrape)\b/u, 2)
        ]
    },
    {
        route: "deterministic",
        baseline: 1,
        positive: [
            signal("manual task", /\b(manual|checklist|task|prepare|transform|format)\b/u, 2),
            signal("fixed graph", /\b(deterministic|fixed|local|offline)\b/u, 3)
        ],
        negative: [
            signal("live integration", /\b(gmail|sheets|telegram|whatsapp|slack|discord|github|notion|linear|jira|airtable|webhook|database|db|sql|openapi|mcp)\b/u, 1),
            signal("live model", /\b(research|agent|custom code|scrape)\b/u, 1)
        ]
    }
];
function signal(label, pattern, weight) {
    return { label, pattern, weight };
}
//# sourceMappingURL=router.js.map