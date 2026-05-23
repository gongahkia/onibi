import { describe, expect, it } from "vitest";
import { evaluatePolicy, parsePolicyYaml } from "../src/index.js";

describe("policy evaluator", () => {
  it("denies Bash rm -rf with a parsed yaml rule", () => {
    const ruleset = parsePolicyYaml(`
rules:
  - id: deny-rm-rf
    when: tool == "Bash" && args.command =~ "^rm -rf"
    action: deny
`);

    expect(
      evaluatePolicy(
        {
          tool: "Bash",
          args: { command: "rm -rf /tmp/demo" }
        },
        ruleset
      )
    ).toMatchObject({
      action: "deny",
      matchedRuleIds: ["deny-rm-rf"]
    });
  });

  it("requires approval for matching adapter prefixes", () => {
    const decision = evaluatePolicy(
      {
        tool: "adapter.gmail.send.message",
        args: {}
      },
      {
        rules: [
          {
            id: "gate-email-send",
            when: 'tool startsWith "adapter.gmail.send"',
            action: "require-approval",
            approverRole: "reviewer"
          }
        ]
      }
    );

    expect(decision).toMatchObject({
      action: "require-approval",
      approverRole: "reviewer"
    });
  });
});
