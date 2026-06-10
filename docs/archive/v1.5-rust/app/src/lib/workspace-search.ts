import { invoke } from "@tauri-apps/api/core";

export interface WorkspaceSearchResult {
  path: string;
  line: number;
  column: number;
  preview: string;
}

export function searchWorkspace(
  root: string,
  query: string,
): Promise<WorkspaceSearchResult[]> {
  return invoke<WorkspaceSearchResult[]>("fs_search_workspace", { root, query });
}
