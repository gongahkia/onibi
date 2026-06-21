import { chmod, mkdtemp, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { afterEach, describe, expect, it, vi } from "vitest";
import { runPiCliCommand } from "../src/index.js";

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("Kelp Pi CLI", () => {
  it("exposes first-class Raspberry Pi 5 appliance commands", async () => {
    await expect(runPiCliCommand(["--help"])).resolves.toMatchObject({
      ok: true,
      name: "kelp-claw pi",
      description: "Manage Raspberry Pi 5 field-appliance commands.",
      commands: expect.arrayContaining([
        expect.objectContaining({ name: "connect" }),
        expect.objectContaining({ name: "doctor" }),
        expect.objectContaining({ name: "validate" })
      ])
    });
  });

  it("reports Pi doctor readiness with hardware defaults", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelp-pi-cli-doctor-"));
    const agentBin = join(tempDir, "kelp-pi-agent");
    const helperBin = join(tempDir, "kelp-pi");
    await writeFile(agentBin, "#!/usr/bin/env sh\nprintf 'kelp-pi-agent 0.1.0\\n'\n", "utf8");
    await writeFile(helperBin, "#!/usr/bin/env sh\nprintf 'helper: kelp-pi\\n'\n", "utf8");
    await chmod(agentBin, 0o755);
    await chmod(helperBin, 0o755);

    try {
      await expect(
        runPiCliCommand([
          "doctor",
          "--agent-bin",
          agentBin,
          "--helper-bin",
          helperBin,
          "--host",
          "pi.local",
          "--control-endpoint",
          "control.example.com:443"
        ])
      ).resolves.toMatchObject({
        ok: true,
        profile: "managed-ap",
        host: "pi.local",
        hardware: {
          minimum: "Raspberry Pi 5 4GB",
          recommended: "Raspberry Pi 5 8GB with NVMe"
        },
        checks: expect.arrayContaining([
          expect.objectContaining({ id: "pi-profile", status: "pass" }),
          expect.objectContaining({ id: "pi-release-asset", status: "pass" }),
          expect.objectContaining({ id: "command:kelp-pi-agent", status: "pass" }),
          expect.objectContaining({ id: "command:kelp-pi-helper", status: "pass" })
        ])
      });
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("renders SSH connect and managed-AP validation dry runs", async () => {
    vi.stubEnv("KELP_PI_HOST", "pi.local");
    await expect(runPiCliCommand(["connect", "--dry-run"])).resolves.toMatchObject({
      ok: true,
      host: "pi.local",
      user: "kelp-pi",
      command: expect.arrayContaining(["ssh", "kelp-pi@pi.local", "kelp-pi-agent", "version"]),
      agentBin: expect.stringContaining("kelp-pi@pi.local kelp-pi-agent")
    });
    await expect(
      runPiCliCommand(["validate", "--profile", "managed-ap", "--dry-run"])
    ).resolves.toMatchObject({
      ok: true,
      profile: "managed-ap",
      host: "pi.local",
      command: expect.arrayContaining([
        "ssh",
        "kelp-pi@pi.local",
        "sudo",
        "kelp-pi",
        "validate-node"
      ]),
      acceptance: expect.stringContaining("managed-ap requires")
    });
  });
});
