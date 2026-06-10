import { open } from "@tauri-apps/plugin-dialog";
import { workspaceFromPath, type Workspace } from "./sessions";

export async function chooseWorkspaceFolder(): Promise<Workspace | null> {
  const selected = await open({
    directory: true,
    multiple: false,
    title: "Choose workspace folder",
  });

  if (!selected || Array.isArray(selected)) {
    return null;
  }

  return workspaceFromPath(selected);
}
