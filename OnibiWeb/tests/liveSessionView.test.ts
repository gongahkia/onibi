import { describe, expect, it } from "vitest";
import { isLiveTerminalInputDisabled } from "../src/views/LiveSessionView";

describe("isLiveTerminalInputDisabled", () => {
  it("allows terminal paste and input only for authenticated running controllable sessions", () => {
    expect(
      isLiveTerminalInputDisabled("authenticated", {
        isControllable: true,
        status: "running"
      })
    ).toBe(false);
  });

  it("disables terminal paste while realtime is disconnected", () => {
    expect(
      isLiveTerminalInputDisabled("disconnected", {
        isControllable: true,
        status: "running"
      })
    ).toBe(true);
  });

  it("disables terminal paste for non-running or non-controllable sessions", () => {
    expect(
      isLiveTerminalInputDisabled("authenticated", {
        isControllable: true,
        status: "exited"
      })
    ).toBe(true);
    expect(
      isLiveTerminalInputDisabled("authenticated", {
        isControllable: false,
        status: "running"
      })
    ).toBe(true);
  });
});
