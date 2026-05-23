import { describe, expect, it } from "vitest";
import { evaluatePolicy, parsePolicyYaml, validatePolicyExpression } from "../src/index.js";

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

  it("rejects malformed YAML and unsupported expressions before runtime", () => {
    for (const yaml of [
      'rules:\n  - id: missing-action\n    when: tool == "Bash"\n',
      'rules:\n  - id: bad-action\n    when: tool == "Bash"\n    action: quarantine\n',
      'rules:\n  - id: unsupported\n    when: process.env == "prod"\n    action: deny\n'
    ]) {
      expect(() => parsePolicyYaml(yaml)).toThrow();
    }
  });

  it("handles regex edge cases without silently allowing unsafe rules", () => {
    expect(() => validatePolicyExpression('args.command =~ "["')).toThrow(/Invalid policy regex/u);
    expect(
      evaluatePolicy(
        {
          tool: "Bash",
          args: { command: 'printf "a && b" && echo done' }
        },
        {
          rules: [
            {
              id: "quoted-and-regex",
              when: 'tool == "Bash" && args.command =~ "a && b"',
              action: "deny"
            }
          ]
        }
      )
    ).toMatchObject({
      action: "deny",
      matchedRuleIds: ["quoted-and-regex"]
    });
  });

  it("selects deny over lower-severity matches", () => {
    const decision = evaluatePolicy(
      {
        tool: "Bash",
        args: { command: "rm -rf /tmp/demo" }
      },
      {
        rules: [
          {
            id: "allow-bash",
            when: 'tool == "Bash"',
            action: "allow"
          },
          {
            id: "log-bash",
            when: 'tool == "Bash"',
            action: "log-only"
          },
          {
            id: "deny-rm",
            when: 'args.command =~ "^rm -rf"',
            action: "deny"
          }
        ]
      }
    );

    expect(decision).toMatchObject({
      action: "deny",
      matchedRuleIds: ["allow-bash", "deny-rm", "log-bash"]
    });
  });
});
