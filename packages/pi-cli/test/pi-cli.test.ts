import { chmod, mkdtemp, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { afterEach, describe, expect, it, vi } from "vitest";
import { runPiCliCommand } from "../src/index.js";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("Kelp Pi CLI", () => {
  it("exposes first-class Raspberry Pi 5 appliance commands", async () => {
    await expect(runPiCliCommand(["--help"])).resolves.toMatchObject({
      ok: true,
      name: "kelp-claw pi",
      description: "Manage Raspberry Pi 5 field-appliance commands.",
      commands: expect.arrayContaining([
        expect.objectContaining({ name: "bootstrap" }),
        expect.objectContaining({ name: "connect" }),
        expect.objectContaining({ name: "doctor" }),
        expect.objectContaining({ name: "lab init" }),
        expect.objectContaining({ name: "recover network" }),
        expect.objectContaining({ name: "validate" })
      ])
    });
  });

  it("reports Pi doctor readiness with hardware defaults", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelp-pi-cli-doctor-"));
    const agentBin = join(tempDir, "kelp-pi-agent");
    const helperBin = join(tempDir, "kelp-pi");
    const sshBin = join(tempDir, "ssh");
    await writeFile(agentBin, "#!/usr/bin/env sh\nprintf 'kelp-pi-agent 0.1.0\\n'\n", "utf8");
    await writeFile(helperBin, "#!/usr/bin/env sh\nprintf 'helper: kelp-pi\\n'\n", "utf8");
    await writeFile(sshBin, "#!/usr/bin/env sh\nprintf 'offline\\n' >&2\nexit 255\n", "utf8");
    await chmod(agentBin, 0o755);
    await chmod(helperBin, 0o755);
    await chmod(sshBin, 0o755);

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
          "--ssh-user",
          "operator",
          "--ssh-bin",
          sshBin,
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
          expect.objectContaining({ id: "command:kelp-pi", status: "pass" }),
          expect.objectContaining({ id: "remote:kelp-pi-agent", status: "warn" })
        ])
      });
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("renders SSH connect and managed-AP validation dry runs", async () => {
    vi.stubEnv("KELP_PI_HOST", "pi.local");
    vi.stubEnv("KELP_PI_SSH_USER", "operator");
    await expect(runPiCliCommand(["connect", "--dry-run"])).resolves.toMatchObject({
      ok: true,
      host: "pi.local",
      user: "operator",
      command: expect.arrayContaining(["ssh", "operator@pi.local", "kelp-pi-agent", "version"]),
      agentBin: expect.stringContaining("operator@pi.local kelp-pi-agent")
    });
    await expect(
      runPiCliCommand([
        "validate",
        "--profile",
        "managed-ap",
        "--dry-run",
        "--target-ip",
        "192.0.2.10",
        "--target-url",
        "http://fixture.local",
        "--nuclei-approval-token",
        "approval-token",
        "--control-url",
        "https://control.example.com/health",
        "--client-a",
        "10.42.0.20",
        "--client-b",
        "10.42.0.21",
        "--forbidden-ip",
        "198.51.100.10"
      ])
    ).resolves.toMatchObject({
      ok: true,
      profile: "managed-ap",
      host: "pi.local",
      command: expect.arrayContaining(["ssh", "operator@pi.local", "sh", "-s"]),
      remoteScript: expect.stringContaining("sudo kelp-pi-validate-field-acceptance"),
      artifactPath: expect.stringContaining(".kelpclaw/pi/pi.local/acceptance.json"),
      acceptance: expect.stringContaining("managed-ap requires")
    });
  });

  it("writes a lab profile and reuses it for dry-run validation", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelp-pi-lab-"));
    const configPath = join(tempDir, "lab.json");
    try {
      await expect(
        runPiCliCommand([
          "lab",
          "init",
          "--out",
          configPath,
          "--host",
          "pi.local",
          "--ssh-user",
          "operator",
          "--control-url",
          "https://control.example.com/health",
          "--target-ip",
          "192.0.2.10",
          "--target-url",
          "http://fixture.local",
          "--nuclei-approval-token",
          "approval-token",
          "--client-a",
          "10.42.0.20",
          "--client-b",
          "10.42.0.21",
          "--forbidden-ip",
          "198.51.100.10"
        ])
      ).resolves.toMatchObject({
        ok: true,
        path: configPath,
        missing: [],
        config: expect.objectContaining({
          host: "pi.local",
          sshUser: "operator",
          profile: "managed-ap"
        })
      });

      await expect(
        runPiCliCommand(["validate", "--config", configPath, "--dry-run"])
      ).resolves.toMatchObject({
        ok: true,
        host: "pi.local",
        user: "operator",
        command: expect.arrayContaining(["ssh", "operator@pi.local", "sh", "-s"]),
        remoteScript: expect.stringContaining("'--target-ip' '192.0.2.10'")
      });
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("renders remote bootstrap dry run", async () => {
    await expect(
      runPiCliCommand([
        "bootstrap",
        "--host",
        "pi.local",
        "--ssh-user",
        "operator",
        "--dry-run",
        "--package-url",
        "https://example.com/kelp-pi-aarch64",
        "--package-sha256",
        "0".repeat(64)
      ])
    ).resolves.toMatchObject({
      ok: true,
      host: "pi.local",
      user: "operator",
      command: expect.arrayContaining(["ssh", "operator@pi.local", "sh", "-s"]),
      installerUrl:
        "https://raw.githubusercontent.com/gongahkia/kelp/main/scripts/install-kelp-pi.sh",
      installerArgs: expect.arrayContaining([
        "--package-url",
        "https://example.com/kelp-pi-aarch64",
        "--package-sha256",
        "0".repeat(64)
      ]),
      remoteScript: expect.stringContaining("curl -fsSL")
    });
    const result = await runPiCliCommand([
      "bootstrap",
      "--host",
      "pi.local",
      "--ssh-user",
      "operator",
      "--dry-run"
    ]);
    expect(result.remoteScript).toContain("kelp-pi doctor --data-dir /var/lib/kelp-pi");
  });

  it("renders remote network recovery dry run", async () => {
    await expect(
      runPiCliCommand([
        "recover",
        "network",
        "--host",
        "pi.local",
        "--ssh-user",
        "operator",
        "--backup",
        "latest",
        "--force",
        "--dry-run"
      ])
    ).resolves.toMatchObject({
      ok: true,
      host: "pi.local",
      user: "operator",
      recover: "network",
      command: expect.arrayContaining([
        "ssh",
        "operator@pi.local",
        "sudo",
        "kelp-pi",
        "recover",
        "network",
        "--backup",
        "latest",
        "--force"
      ])
    });
  });

  it("fails strict doctor when Pi env is not configured", async () => {
    await expect(runPiCliCommand(["doctor", "--strict"])).resolves.toMatchObject({
      ok: false,
      strict: true,
      checks: expect.arrayContaining([
        expect.objectContaining({ id: "env:KELP_PI_HOST", status: "fail" }),
        expect.objectContaining({ id: "env:KELP_PI_CONTROL_URL", status: "fail" })
      ])
    });
  });

  it("checks latest GitHub Pi release asset URLs when requested", async () => {
    const fetchMock = vi.fn(async () => new Response(null, { status: 302 }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(runPiCliCommand(["doctor", "--check-release-online"])).resolves.toMatchObject({
      ok: true,
      checks: expect.arrayContaining([
        expect.objectContaining({
          id: "release-online:kelp-pi-aarch64",
          status: "pass"
        }),
        expect.objectContaining({
          id: "release-online:kelp-pi-aarch64.sha256",
          status: "pass"
        })
      ])
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "https://github.com/gongahkia/kelp/releases/latest/download/kelp-pi-aarch64",
      { method: "HEAD", redirect: "manual" }
    );
  });
});
