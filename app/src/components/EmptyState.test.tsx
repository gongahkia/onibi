import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";
import { EmptyState } from "./EmptyState";

describe("EmptyState", () => {
  test("renders the empty prompt", () => {
    render(<EmptyState />);
    expect(screen.getByTestId("empty-state").textContent).toContain(
      "Open a folder, start a session",
    );
  });
});
