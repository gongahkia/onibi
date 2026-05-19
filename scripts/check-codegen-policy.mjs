import { scheduledScrapingWorkflowFixture } from "../packages/workflow-spec/dist/index.js";

const issues = [];

for (const node of scheduledScrapingWorkflowFixture.nodes) {
  if (node.kind !== "codegen" || !node.codegen) {
    continue;
  }

  const dependencies = [
    ...node.codegen.dependencyManifest.dependencies,
    ...node.codegen.dependencyManifest.devDependencies
  ];
  for (const dependency of dependencies) {
    if (!/@[^@]+$/u.test(dependency) || dependency.endsWith("@latest")) {
      issues.push(`${node.id}: dependency '${dependency}' is not pinned.`);
    }
  }

  if (
    node.codegen.dependencyManifest.packageManager !== "none" &&
    node.codegen.dependencyManifest.installCommand.length === 0
  ) {
    issues.push(`${node.id}: package install command is missing.`);
  }

  if (node.codegen.sandbox.network === "none" && node.codegen.sandbox.allowedHosts.length > 0) {
    issues.push(`${node.id}: sandbox denies network but declares allowed hosts.`);
  }
}

if (issues.length > 0) {
  console.error(issues.join("\n"));
  process.exit(1);
}

console.log("Codegen dependency and sandbox policy checks passed.");
