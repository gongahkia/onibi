import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { isAbsolute, relative, resolve } from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

export interface ReadOnlyMountWarning {
  readonly severity: "warning";
  readonly code: "evidence-root-not-read-only";
  readonly path: string;
  readonly message: string;
}

export interface ReadOnlyMountCheck {
  readonly path: string;
  readonly mounted: boolean;
  readonly readOnly: boolean;
  readonly mountPoint?: string | undefined;
  readonly warnings: readonly ReadOnlyMountWarning[];
}

interface MountEntry {
  readonly mountPoint: string;
  readonly options: readonly string[];
}

export async function checkReadOnlyMount(path: string): Promise<ReadOnlyMountCheck> {
  const absolutePath = resolve(path);
  const mountEntry = await findBestMountEntry(absolutePath);
  const mounted = mountEntry?.mountPoint === absolutePath;
  const readOnly =
    mounted && mountEntry.options.some((option) => option === "ro" || option === "read-only");
  const warnings: ReadOnlyMountWarning[] = readOnly
    ? []
    : [
        {
          severity: "warning",
          code: "evidence-root-not-read-only",
          path: absolutePath,
          message:
            "Evidence root is not detected as its own read-only mount; before/after hashes remain authoritative for this run."
        }
      ];

  return {
    path: absolutePath,
    mounted,
    readOnly,
    ...(mountEntry ? { mountPoint: mountEntry.mountPoint } : {}),
    warnings
  };
}

async function findBestMountEntry(path: string): Promise<MountEntry | undefined> {
  const entries =
    process.platform === "linux" ? await linuxMountEntries() : await posixMountEntries();
  return entries
    .filter((entry) => containsPath(entry.mountPoint, path))
    .sort((left, right) => right.mountPoint.length - left.mountPoint.length)[0];
}

async function linuxMountEntries(): Promise<readonly MountEntry[]> {
  try {
    const mountInfo = await readFile("/proc/self/mountinfo", "utf8");
    return mountInfo
      .split("\n")
      .filter((line) => line.length > 0)
      .flatMap((line) => {
        const parts = line.split(" ");
        const separatorIndex = parts.indexOf("-");
        const mountPoint = parts[4];
        const mountOptions = parts[5];
        const superOptions = separatorIndex >= 0 ? parts[separatorIndex + 3] : undefined;
        if (!mountPoint || !mountOptions) {
          return [];
        }

        return [
          {
            mountPoint: decodeMountInfoPath(mountPoint),
            options: [...mountOptions.split(","), ...(superOptions?.split(",") ?? [])]
          }
        ];
      });
  } catch {
    return posixMountEntries();
  }
}

async function posixMountEntries(): Promise<readonly MountEntry[]> {
  try {
    const { stdout } = await execFileAsync("mount", []);
    return stdout
      .split("\n")
      .filter((line) => line.length > 0)
      .flatMap((line) => {
        const match = / on (.+) \((.+)\)$/u.exec(line);
        if (!match?.[1] || !match[2]) {
          return [];
        }

        return [
          {
            mountPoint: match[1],
            options: match[2].split(",").map((option) => option.trim())
          }
        ];
      });
  } catch {
    return [];
  }
}

function containsPath(parent: string, child: string): boolean {
  const delta = relative(parent, child);
  return delta === "" || (!delta.startsWith("..") && !isAbsolute(delta));
}

function decodeMountInfoPath(value: string): string {
  return value.replace(/\\([0-7]{3})/gu, (_match, octal: string) =>
    String.fromCharCode(Number.parseInt(octal, 8))
  );
}
