import { describe, expect, it } from "vitest";

/**
 * Mirrors the logic inside StaleBanner without pulling React into a unit test:
 * banner renders iff disconnected && reconnectAttempts >= threshold.
 */
function shouldShowStaleBanner(
  realtimeState: "disconnected" | "connecting" | "authenticating" | "authenticated" | "reconnecting",
  reconnectAttempts: number,
  threshold = 3
): boolean {
  const disconnected = realtimeState === "disconnected" || realtimeState === "reconnecting";
  return disconnected && reconnectAttempts >= threshold;
}

describe("StaleBanner visibility", () => {
  it("hides while authenticated", () => {
    expect(shouldShowStaleBanner("authenticated", 10)).toBe(false);
  });

  it("hides while still under threshold", () => {
    expect(shouldShowStaleBanner("reconnecting", 2)).toBe(false);
  });

  it("shows when reconnecting and past threshold", () => {
    expect(shouldShowStaleBanner("reconnecting", 3)).toBe(true);
  });

  it("shows when disconnected and past threshold", () => {
    expect(shouldShowStaleBanner("disconnected", 5)).toBe(true);
  });
});
