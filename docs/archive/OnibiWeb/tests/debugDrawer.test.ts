import { describe, expect, it } from "vitest";
import { filterEvents } from "../src/components/DebugDrawer";
import type { RealtimeDebugEvent } from "../src/api/realtimeClient";

const sample: RealtimeDebugEvent[] = [
  { timestamp: 1, level: "info", message: "open" },
  { timestamp: 2, level: "debug", message: "recv foo" },
  { timestamp: 3, level: "warn", message: "slow" },
  { timestamp: 4, level: "error", message: "boom" },
  { timestamp: 5, level: "info", message: "authenticated" }
];

describe("DebugDrawer filterEvents", () => {
  it("returns all events for 'all'", () => {
    expect(filterEvents(sample, "all")).toHaveLength(5);
  });

  it("returns only matching level", () => {
    expect(filterEvents(sample, "error").map((event) => event.message)).toEqual(["boom"]);
    expect(filterEvents(sample, "info")).toHaveLength(2);
  });

  it("returns empty when no match", () => {
    expect(filterEvents([{ timestamp: 1, level: "info", message: "x" }], "error")).toHaveLength(0);
  });
});
