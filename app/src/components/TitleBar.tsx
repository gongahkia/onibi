import { useEffect, useMemo } from "react";
import { useSessionStore } from "../lib/sessions";

function basename(path: string): string {
  return path.split("/").filter(Boolean).pop() ?? path;
}

function selectedTitle(selection: ReturnType<typeof useSessionStore.getState>["selectedFile"]): string | null {
  if (!selection) return null;
  if (selection.type === "file") return selection.name || basename(selection.path);
  if (selection.type === "git-diff") return `${selection.name} (diff)`;
  if (selection.type === "agent-review") return `${selection.name} (review)`;
  if (selection.type === "web") return selection.name || selection.url;
  return null;
}

export function TitleBar() {
  const selection = useSessionStore((state) => state.selectedFile);
  const workspaces = useSessionStore((state) => state.workspaces);
  const sessions = useSessionStore((state) => state.sessions);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);

  const activeWorkspace = useMemo(() => {
    if (selection && "workspaceId" in selection) {
      return workspaces.find((w) => w.id === selection.workspaceId) ?? null;
    }
    const session = sessions.find((s) => s.id === activeSessionId);
    return workspaces.find((w) => w.id === session?.workspaceId) ?? null;
  }, [selection, workspaces, sessions, activeSessionId]);

  const fileTitle = selectedTitle(selection);
  const workspaceTitle = activeWorkspace?.name ?? null;
  const composed = fileTitle && workspaceTitle
    ? `${fileTitle} — ${workspaceTitle}`
    : fileTitle ?? workspaceTitle ?? "Onibi";

  useEffect(() => {
    document.title = composed;
  }, [composed]);

  return (
    <div className="title-bar" role="banner">
      <div className="title-bar-spacer" />
      <div className="title-bar-center" title={composed}>
        {composed}
      </div>
      <div className="title-bar-actions" />
    </div>
  );
}
