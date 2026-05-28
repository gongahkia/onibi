import { invoke } from "@tauri-apps/api/core";

export type GitStatusCode = "?" | "!" | "M" | "A" | "D" | "R" | "C" | "U";

export interface GitStatusEntry {
  path: string;
  originalPath?: string | null;
  fullPath: string;
  indexStatus?: string | null;
  worktreeStatus?: string | null;
}

export interface GitStatus {
  isRepo: boolean;
  repoRoot?: string | null;
  branch?: string | null;
  upstream?: string | null;
  ahead: number;
  behind: number;
  entries: GitStatusEntry[];
}

export interface GitFileDiff {
  path: string;
  oldLabel: string;
  newLabel: string;
  oldText?: string | null;
  newText?: string | null;
  binary: boolean;
}

export interface GitTreeState {
  badge: string;
  label: string;
  tone: "added" | "deleted" | "ignored" | "modified" | "renamed" | "untracked";
}

export function gitStateForEntry(entry: GitStatusEntry): GitTreeState {
  const code = entry.worktreeStatus ?? entry.indexStatus ?? "M";
  switch (code) {
    case "?":
      return { badge: "U", label: "Untracked", tone: "untracked" };
    case "!":
      return { badge: "I", label: "Ignored", tone: "ignored" };
    case "A":
      return { badge: "A", label: "Added", tone: "added" };
    case "D":
      return { badge: "D", label: "Deleted", tone: "deleted" };
    case "R":
    case "C":
      return { badge: "R", label: "Renamed", tone: "renamed" };
    default:
      return { badge: "M", label: "Modified", tone: "modified" };
  }
}

export function gitStateByFullPath(status: GitStatus | null): Record<string, GitTreeState> {
  if (!status?.isRepo) {
    return {};
  }
  return Object.fromEntries(
    status.entries.map((entry) => [entry.fullPath, gitStateForEntry(entry)]),
  );
}

export function hasStagedChange(entry: GitStatusEntry): boolean {
  return Boolean(entry.indexStatus && entry.indexStatus !== "?" && entry.indexStatus !== "!");
}

export function hasWorkingTreeChange(entry: GitStatusEntry): boolean {
  return Boolean(entry.worktreeStatus);
}

export async function getGitStatus(root: string): Promise<GitStatus> {
  return invoke<GitStatus>("git_status", { root });
}

export async function stageGitPaths(root: string, paths: string[]): Promise<string> {
  return invoke<string>("git_stage_paths", { root, paths });
}

export async function unstageGitPaths(root: string, paths: string[]): Promise<string> {
  return invoke<string>("git_unstage_paths", { root, paths });
}

export async function discardGitPaths(root: string, paths: string[]): Promise<string> {
  return invoke<string>("git_discard_paths", { root, paths });
}

export async function commitGit(root: string, message: string): Promise<string> {
  return invoke<string>("git_commit", { root, message });
}

export async function syncGit(root: string): Promise<string> {
  return invoke<string>("git_sync", { root });
}

export async function getGitFileDiff(
  root: string,
  path: string,
  stage: "staged" | "working",
): Promise<GitFileDiff> {
  return invoke<GitFileDiff>("git_diff_file", { root, path, stage });
}
