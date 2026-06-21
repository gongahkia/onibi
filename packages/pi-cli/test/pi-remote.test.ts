import { describe, expect, it } from "vitest";
import { runPiCliCommand } from "../src/index.js";

const runWithHost = process.env.KELP_PI_HOST ? it : it.skip;
const fullAcceptanceEnv =
  [
    "KELP_PI_HOST",
    "KELP_PI_TARGET_IP",
    "KELP_PI_TARGET_URL",
    "KELP_PI_NUCLEI_APPROVAL_TOKEN",
    "KELP_PI_CLIENT_A",
    "KELP_PI_CLIENT_B"
  ].every((name) => Boolean(process.env[name])) &&
  Boolean(process.env.KELP_PI_CONTROL_URL || process.env.KELP_PI_CONTROL_ENDPOINT);
const runFullAcceptance = fullAcceptanceEnv ? it : it.skip;

describe("Kelp Pi remote acceptance", () => {
  runWithHost("connects to kelp-pi-agent over SSH", async () => {
    await expect(runPiCliCommand(["connect"])).resolves.toMatchObject({
      ok: true,
      host: process.env.KELP_PI_HOST,
      user: process.env.KELP_PI_SSH_USER ?? process.env.KELP_PI_USER ?? "pi",
      agentVersion: expect.stringContaining("kelp-pi-agent")
    });
  });

  runFullAcceptance("runs managed-AP field acceptance", async () => {
    const result = await runPiCliCommand([
      "validate",
      "--profile",
      "managed-ap",
      "--forbidden-ip",
      process.env.KELP_PI_FORBIDDEN_IPS?.split(/[,\s]+/u).find(Boolean) ?? "198.51.100.10"
    ]);
    expect(result).toMatchObject({
      ok: true,
      profile: "managed-ap",
      host: process.env.KELP_PI_HOST,
      artifactPath: expect.stringContaining("acceptance.json"),
      checks: expect.arrayContaining([expect.objectContaining({ status: "pass" })])
    });
  });
});
