import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { App } from "../src/App.js";

afterEach(() => {
  cleanup();
});

describe("OpenClaw planner shell", () => {
  it("renders the workflow planner and status controls", () => {
    render(<App />);

    expect(screen.getByRole("heading", { name: "OpenClaw" })).toBeInTheDocument();
    expect(screen.getByText("Gmail Receipts To Sheets")).toBeInTheDocument();
    expect(screen.getByText("Read Gmail Receipts · skill")).toBeInTheDocument();
    expect(screen.getByText("1.0.0")).toBeInTheDocument();
  });

  it("updates validation, approval, and execution states", () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: "Validate" }));
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    fireEvent.click(screen.getByRole("button", { name: "Run" }));

    expect(screen.getByText("valid")).toBeInTheDocument();
    expect(screen.getByText("approved")).toBeInTheDocument();
    expect(screen.getByText("succeeded")).toBeInTheDocument();
  });
});
