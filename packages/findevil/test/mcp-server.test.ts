import { mkdir, mkdtemp, realpath, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";
import { createFindEvilMcpServer, type ReadOnlyCommandRunner } from "../src/mcp/server.js";

describe("Find Evil read-only MCP server", () => {
  it("lists typed forensic tools without exposing shell", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-mcp-tools-"));
    const server = createFindEvilMcpServer({ evidenceRoot: directory });

    const response = await server.handleRequest({
      jsonrpc: "2.0",
      id: 1,
      method: "tools/list",
      params: {}
    });

    expect(JSON.stringify(response)).toContain("findevil.get_partition_table");
    expect(JSON.stringify(response)).not.toContain("execute_shell");
  });

  it("hashes only files contained by evidenceRoot", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-mcp-hash-"));
    await writeFile(join(directory, "evidence.txt"), "Mr. Evil\n", "utf8");
    const server = createFindEvilMcpServer({ evidenceRoot: directory });

    const result = await server.callTool("findevil.hash_evidence_file", {
      path: "evidence.txt"
    });

    expect(result).toMatchObject({
      ok: true,
      path: "evidence.txt",
      algorithm: "sha256",
      digest: createHash("sha256").update("Mr. Evil\n").digest("hex"),
      readOnly: true
    });
    await expect(
      server.callTool("findevil.hash_evidence_file", { path: "../outside.txt" })
    ).rejects.toThrow("Path escapes evidenceRoot");
  });

  it("rejects symlink escapes while still reading normal contained files", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-mcp-symlink-"));
    const evidenceRoot = join(directory, "evidence");
    await mkdir(evidenceRoot);
    await writeFile(join(evidenceRoot, "inside.txt"), "inside\n", "utf8");
    await writeFile(join(directory, "outside.txt"), "outside\n", "utf8");
    await symlink(join(directory, "outside.txt"), join(evidenceRoot, "escape.txt"));
    const server = createFindEvilMcpServer({ evidenceRoot });

    await expect(
      server.callTool("findevil.hash_evidence_file", { path: "escape.txt" })
    ).rejects.toThrow("Path escapes evidenceRoot");
    await expect(server.callTool("findevil.hash_evidence_file", { path: "inside.txt" })).resolves
      .toMatchObject({
        ok: true,
        path: "inside.txt",
        digest: createHash("sha256").update("inside\n").digest("hex")
      });
  });

  it("wraps Sleuth Kit commands through an allowlisted runner", async () => {
    const directory = await mkdtemp(join(tmpdir(), "findevil-mcp-sleuthkit-"));
    await writeFile(join(directory, "disk.E01"), "ewf", "utf8");
    const calls: string[] = [];
    const runner: ReadOnlyCommandRunner = async (command, args) => {
      calls.push(`${command} ${args.join(" ")}`);
      return {
        command,
        args,
        exitCode: 0,
        stdout:
          "DOS Partition Table\n" +
          "Units are in 512-byte sectors\n" +
          "000:  Meta      0000000000   0000000000   0000000001   Primary Table (#0)\n" +
          "001:  -----     0000000063   0002096479   0002096417   NTFS (0x07)\n",
        stderr: "",
        truncated: false
      };
    };
    const server = createFindEvilMcpServer({ evidenceRoot: directory, runner });

    const result = await server.callTool("findevil.get_partition_table", {
      imagePath: "disk.E01"
    });

    expect(calls).toEqual([`mmls ${await realpath(join(directory, "disk.E01"))}`]);
    expect(result.partitions).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          slot: 1,
          startSector: 63,
          lengthSectors: 2096417
        })
      ])
    );
  });
});
