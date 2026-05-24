import { expect, test, type Page } from "@playwright/test";

test("plans, edits, validates, deploys, and runs a workflow", async ({ page }) => {
  await page.goto("/");

  await page.getByRole("button", { name: /Commands/ }).click();
  await page.getByRole("option", { name: /Plan Workflow/ }).click();
  await page
    .getByLabel("Workflow prompt")
    .fill("extract transaction details from Gmail receipts into Sheets");
  await page.getByRole("button", { name: "Submit Plan" }).click();
  await expect(page.getByText("Read Gmail Receipts")).toBeVisible();

  await page.getByText("Read Gmail Receipts").click();
  const labelInput = page.getByLabel("Label");
  await labelInput.fill("Read Gmail Purchases");
  await expect(labelInput).toHaveValue("Read Gmail Purchases");

  await runCommand(page, "Validate Workflow");
  await expect(page.locator(".canvas-footer")).toContainText("valid");

  await runCommand(page, "Evaluate Draft");
  await runCommand(page, "Approve Workflow");
  await runCommand(page, "Deploy Workflow");
  await expect(page.getByText("Deployment deployed: runner.configuration")).toBeVisible();

  await runCommand(page, "Run Workflow");
  await expect(page.getByText("Workflow run queued.")).toBeVisible();
  await expect(page.getByRole("button", { name: "succeeded" })).toBeVisible();
});

async function runCommand(page: Page, name: string): Promise<void> {
  await page.getByRole("button", { name: /Commands/ }).click();
  const command = page.getByRole("option", { name: new RegExp(name) });
  await expect(command).toBeEnabled({ timeout: 30_000 });
  await command.click();
}
