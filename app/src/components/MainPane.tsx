import { useCallback } from "react";
import { AgentReviewBuffer, GitDiffBuffer } from "./DiffBuffer";
import { EmptyState } from "./EmptyState";
import { EditorBuffer } from "./EditorBuffer";
import { TerminalView } from "./TerminalView";
import { stopAgentReview } from "../lib/agent-review";
import {
  AGENT_LABELS,
  sessionTitle,
  useSessionStore,
  type Session,
  type Workspace,
} from "../lib/sessions";
import { ptySpawn, shellPath } from "../lib/tauri-bridge";

async function spawnShellReplacement(
  session: Session,
  workspace: Workspace,
): Promise<Session> {
  const id = await ptySpawn({
    command: shellPath(),
    args: [],
    cwd: workspace.path,
    env: [],
    rows: 30,
    cols: 100,
  });
  return {
    ...session,
    id,
    agent: "shell",
    title: sessionTitle("shell", workspace),
    status: "running",
    createdAt: Date.now(),
    pendingApprovals: [],
  };
}

export function MainPane() {
  const selectedFile = useSessionStore((state) => state.selectedFile);
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const settings = useSessionStore((state) => state.settings);
  const replaceSession = useSessionStore((state) => state.replaceSession);
  const updateSession = useSessionStore((state) => state.updateSession);
  const session = sessions.find((item) => item.id === activeSessionId) ?? null;

  const handleTerminalExit = useCallback(
    (exitedSession: Session) => {
      if (exitedSession.agent === "shell") {
        updateSession(exitedSession.id, { status: "completed" });
        return;
      }
      const targetWorkspace =
        workspaces.find((item) => item.id === exitedSession.workspaceId) ?? null;
      if (!targetWorkspace) {
        updateSession(exitedSession.id, { status: "completed" });
        return;
      }
      updateSession(exitedSession.id, {
        status: "completed",
        title: `${AGENT_LABELS[exitedSession.agent]} exited`,
      });
      void stopAgentReview(exitedSession.id).catch(() => undefined);
      void spawnShellReplacement(exitedSession, targetWorkspace)
        .then((replacement) => replaceSession(exitedSession.id, replacement))
        .catch((error) => {
          console.warn("failed to start fallback shell", error);
          updateSession(exitedSession.id, { status: "error" });
        });
    },
    [replaceSession, updateSession, workspaces],
  );

  if (session) {
    const terminalVisible = selectedFile === null;
    return (
      <main
        className="main-pane main-pane-stacked"
        data-testid={selectedFile ? "main-pane-editor" : "main-pane-terminal"}
      >
        <section
          className={`main-pane-surface terminal-surface ${
            terminalVisible ? "is-active" : "is-background"
          }`}
          aria-hidden={!terminalVisible}
        >
          <TerminalView
            ptyId={session.id}
            fontFamily={settings.terminalFontFamily}
            fontSize={settings.terminalFontSize}
            settings={settings}
            visible={terminalVisible}
            onExit={() => handleTerminalExit(session)}
          />
        </section>
        {selectedFile ? (
          <section className="main-pane-surface editor-surface">
            {selectedFile.type === "git-diff" ? (
              <GitDiffBuffer selection={selectedFile} mode={settings.diffViewMode} />
            ) : selectedFile.type === "agent-review" ? (
              <AgentReviewBuffer selection={selectedFile} mode={settings.diffViewMode} />
            ) : (
              <EditorBuffer
                path={selectedFile.path}
                workspaceRoot={selectedFile.workspaceRoot}
                fontFamily={settings.editorFontFamily}
              />
            )}
          </section>
        ) : null}
      </main>
    );
  }

  if (selectedFile) {
    return (
      <main className="main-pane" data-testid="main-pane-editor">
        {selectedFile.type === "git-diff" ? (
          <GitDiffBuffer selection={selectedFile} mode={settings.diffViewMode} />
        ) : selectedFile.type === "agent-review" ? (
          <AgentReviewBuffer selection={selectedFile} mode={settings.diffViewMode} />
        ) : (
          <EditorBuffer
            path={selectedFile.path}
            workspaceRoot={selectedFile.workspaceRoot}
            fontFamily={settings.editorFontFamily}
          />
        )}
      </main>
    );
  }

  return (
    <main className="main-pane" data-testid="main-pane-empty">
      <EmptyState />
    </main>
  );
}
