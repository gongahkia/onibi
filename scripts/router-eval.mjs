import { routerEvalSummary, runRouterEvalCases } from "../apps/api/dist/router-evals.js";

const run = runRouterEvalCases();
console.log(routerEvalSummary(run));

if (!run.passed) {
  process.exitCode = 1;
}
