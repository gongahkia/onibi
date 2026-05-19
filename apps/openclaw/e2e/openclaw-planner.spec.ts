import { expect, test } from "@playwright/test";

test("plans, edits, validates, approves, runs, and inspects a workflow", async ({ page }) => {
  await page.goto("/");

  await page
    .getByLabel("Workflow Prompt")
    .fill("extract transaction details from Gmail receipts into Sheets");
  await page.getByRole("button", { name: /^Plan$/ }).click();
  await expect(
    page.getByRole("heading", { name: "Extract Transaction Details From Gmail" })
  ).toBeVisible();
  await expect(page.getByText("Read Gmail Receipts")).toBeVisible();

  await page.getByLabel("Label").fill("Read Gmail Purchases");
  await expect(page.getByText("Read Gmail Purchases")).toBeVisible();

  await page.getByRole("button", { name: "Validate" }).click();
  await expect(page.locator(".status-valid", { hasText: "valid" })).toBeVisible();

  await page.getByRole("button", { name: "Approve" }).click();
  await expect(page.getByText("Frozen approval metadata changed.")).toBeVisible();
  await expect(page.getByTestId("approval-diff")).toContainText("approval");

  await page.getByRole("button", { name: "Run" }).click();
  await expect(page.locator(".status-succeeded", { hasText: "succeeded" })).toBeVisible();
  await expect(page.locator(".event-list").getByText("NanoClaw run finished.")).toBeVisible();
  await expect(page.locator(".result-view")).toContainText("execution.workflow");
});
