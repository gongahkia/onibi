import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import { buildDiffRows, DiffViewer } from "./DiffViewer";

describe("DiffViewer", () => {
  test("builds line rows for additions and deletions", () => {
    const rows = buildDiffRows("one\ntwo\nthree", "one\nthree\nfour");

    expect(rows.some((row) => row.kind === "delete" && row.oldLine === "two")).toBe(
      true,
    );
    expect(rows.some((row) => row.kind === "add" && row.newLine === "four")).toBe(
      true,
    );
  });

  test("renders a side-by-side diff", () => {
    render(
      <DiffViewer
        mode="side-by-side"
        diff={{
          path: "src/main.ts",
          oldLabel: "HEAD",
          newLabel: "Working tree",
          oldText: "const a = 1;",
          newText: "const a = 2;",
          binary: false,
        }}
      />,
    );

    expect(screen.getByTestId("diff-viewer").textContent).toContain("src/main.ts");
    expect(screen.getByLabelText("Side by side diff").textContent).toContain(
      "const a = 2;",
    );
  });
});
