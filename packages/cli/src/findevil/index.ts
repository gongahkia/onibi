import { runFindEvilFirewallCommand } from "./firewall.js";
import { runFindEvilSentinelCommand } from "./sentinel.js";
import { runFindEvilVerifyCommand } from "./verify.js";

export async function runFindEvilCommand(args: readonly string[]): Promise<void> {
  const [command, ...commandArgs] = args;
  switch (command) {
    case "verify":
      return runFindEvilVerifyCommand(commandArgs);
    case "firewall":
      return runFindEvilFirewallCommand(commandArgs);
    case "sentinel":
      return runFindEvilSentinelCommand(commandArgs);
    default:
      throw new Error("Usage: kelp-claw findevil <verify|firewall|sentinel>");
  }
}
