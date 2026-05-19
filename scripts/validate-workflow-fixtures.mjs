import {
  gmailReceiptsToSheetsWorkflowFixture,
  scheduledScrapingWorkflowFixture,
  timeSensitiveAlertDeliveryWorkflowFixture,
  validateWorkflowSpec
} from "../packages/workflow-spec/dist/index.js";

const fixtures = [
  gmailReceiptsToSheetsWorkflowFixture,
  scheduledScrapingWorkflowFixture,
  timeSensitiveAlertDeliveryWorkflowFixture
];

const failures = fixtures.flatMap((workflow) => {
  const validation = validateWorkflowSpec(workflow);
  return validation.ok
    ? []
    : validation.errors.map((error) => `${workflow.id}: ${error.code} at ${error.path.join(".")}`);
});

if (failures.length > 0) {
  console.error(failures.join("\n"));
  process.exit(1);
}

console.log(`Validated ${fixtures.length} workflow fixtures.`);
