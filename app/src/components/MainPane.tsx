import { Fragment, useCallback, useEffect, useState } from "react";
import {
  Group as PanelGroup,
  Panel,
  Separator as PanelResizeHandle,
} from "react-resizable-panels";
import { AgentReviewBuffer, GitDiffBuffer } from "./DiffBuffer";
import { EmptyState } from "./EmptyState";
import { EditorBuffer } from "./EditorBuffer";
import { NewSessionDialog } from "./NewSessionDialog";
import { TerminalView } from "./TerminalView";
import { stopAgentReview } from "../lib/agent-review";
import {
  AGENT_LABELS,
  sessionTitle,
  useSessionStore,
  type Session,
  type TerminalPaneNode,
  type TerminalPanePlacement,
  type TerminalSplitDirection,
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

function panelOrientation(direction: TerminalSplitDirection): "horizontal" | "vertical" {
  return direction === "vertical" ? "horizontal" : "vertical";
}

function fallbackLayout(session: Session): TerminalPaneNode {
  return { type: "leaf", paneId: `fallback-${session.id}`, sessionId: session.id };
}

function TerminalPaneTree({
  node,
  sessions,
  activeSessionId,
  terminalVisible,
  settings,
  onTerminalExit,
}: {
  node: TerminalPaneNode;
  sessions: Session[];
  activeSessionId: string | null;
  terminalVisible: boolean;
  settings: ReturnType<typeof useSessionStore.getState>["settings"];
  onTerminalExit: (session: Session) => void;
}) {
  const setActiveTerminalPane = useSessionStore((state) => state.setActiveTerminalPane);
  if (node.type === "split") {
    return (
      <PanelGroup orientation={panelOrientation(node.direction)}>
        {node.children.map((child, index) => (
          <Fragment key={child.paneId}>
            <Panel minSize={12}>
              <TerminalPaneTree
                node={child}
                sessions={sessions}
                activeSessionId={activeSessionId}
                terminalVisible={terminalVisible}
                settings={settings}
                onTerminalExit={onTerminalExit}
              />
            </Panel>
            {index < node.children.length - 1 ? (
              <PanelResizeHandle className="panel-resize-handle terminal-resize-handle" />
            ) : null}
          </Fragment>
        ))}
      </PanelGroup>
    );
  }

  const session = sessions.find((item) => item.id === node.sessionId) ?? null;
  if (!session) {
    return <div className="editor-message">Terminal session is no longer available.</div>;
  }
  const active = activeSessionId === session.id;
  return (
    <section
      className={`terminal-pane ${active ? "active" : ""}`}
      onPointerDown={() => setActiveTerminalPane(node.paneId)}
    >
      <TerminalView
        ptyId={session.id}
        fontFamily={settings.terminalFontFamily}
        fontSize={settings.terminalFontSize}
        settings={settings}
        visible={terminalVisible && active}
        onExit={() => onTerminalExit(session)}
        onOpenLink={(url) => useSessionStore.getState().openWebUrl(url, session.id)}
      />
    </section>
  );
}

function WebBuffer({ url }: { url: string }) {
  return (
    <section className="web-buffer" data-testid="web-buffer">
      <header className="editor-header">
        <div className="editor-path" title={url}>
          {url}
        </div>
        <div className="editor-actions">
          <button
            type="button"
            className="text-button"
            onClick={() => window.open(url, "_blank", "noopener,noreferrer")}
          >
            Open External
          </button>
          <button
            type="button"
            className="text-button"
            onClick={() => useSessionStore.getState().selectFile(null)}
          >
            Close
          </button>
        </div>
      </header>
      <iframe className="web-buffer-frame" src={url} title={url} />
    </section>
  );
}

export function MainPane() {
  const selectedFile = useSessionStore((state) => state.selectedFile);
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const activeTerminalPaneId = useSessionStore((state) => state.activeTerminalPaneId);
  const terminalLayout = useSessionStore((state) => state.terminalLayout);
  const settings = useSessionStore((state) => state.settings);
  const replaceSession = useSessionStore((state) => state.replaceSession);
  const updateSession = useSessionStore((state) => state.updateSession);
  const appendSessionEvent = useSessionStore((state) => state.appendSessionEvent);
  const session = sessions.find((item) => item.id === activeSessionId) ?? null;
  const [splitPlacement, setSplitPlacement] = useState<TerminalPanePlacement | null>(
    null,
  );

  const requestSplit = useCallback(
    (direction: TerminalSplitDirection) => {
      if (!session || !activeTerminalPaneId) {
        return;
      }
      setSplitPlacement({
        type: "split",
        targetPaneId: activeTerminalPaneId,
        direction,
      });
    },
    [activeTerminalPaneId, session],
  );

  useEffect(() => {
    function handleSplitShortcut(event: KeyboardEvent) {
      const primary = event.metaKey || event.ctrlKey;
      if (
        event.defaultPrevented ||
        event.key.toLowerCase() !== "d" ||
        !primary ||
        event.altKey
      ) {
        return;
      }
      if ((event.target as HTMLElement | null)?.closest(".modal-panel")) {
        return;
      }
      event.preventDefault();
      requestSplit(event.shiftKey ? "horizontal" : "vertical");
    }
    window.addEventListener("keydown", handleSplitShortcut);
    return () => window.removeEventListener("keydown", handleSplitShortcut);
  }, [requestSplit]);

  const handleTerminalExit = useCallback(
    (exitedSession: Session) => {
      if (exitedSession.agent === "shell") {
        updateSession(exitedSession.id, { status: "completed" });
        appendSessionEvent({
          type: "session-stopped",
          workspaceId: exitedSession.workspaceId,
          sessionId: exitedSession.id,
          agent: exitedSession.agent,
          summary: `${exitedSession.title} exited`,
        });
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
      appendSessionEvent({
        type: "session-stopped",
        workspaceId: exitedSession.workspaceId,
        sessionId: exitedSession.id,
        agent: exitedSession.agent,
        summary: `${exitedSession.title} exited`,
      });
      void stopAgentReview(exitedSession.id).catch(() => undefined);
      void spawnShellReplacement(exitedSession, targetWorkspace)
        .then((replacement) => replaceSession(exitedSession.id, replacement))
        .catch((error) => {
          console.warn("failed to start fallback shell", error);
          updateSession(exitedSession.id, { status: "error" });
        });
    },
    [appendSessionEvent, replaceSession, updateSession, workspaces],
  );

  if (session) {
    const terminalVisible = selectedFile === null;
    const layout =
      terminalLayout && sessions.some((item) => item.id === session.id)
        ? terminalLayout
        : fallbackLayout(session);
    const splitWorkspaceId = session.workspaceId;
    return (
      <>
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
            <TerminalPaneTree
              node={layout}
              sessions={sessions}
              activeSessionId={activeSessionId}
              terminalVisible={terminalVisible}
              settings={settings}
              onTerminalExit={handleTerminalExit}
            />
          </section>
          {selectedFile ? (
            <section className="main-pane-surface editor-surface">
              {selectedFile.type === "web" ? (
                <WebBuffer url={selectedFile.url} />
              ) : selectedFile.type === "git-diff" ? (
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
        <NewSessionDialog
          open={splitPlacement !== null}
          defaultWorkspaceId={splitWorkspaceId}
          placement={splitPlacement}
          onClose={() => setSplitPlacement(null)}
        />
      </>
    );
  }

  if (selectedFile) {
    return (
      <main className="main-pane" data-testid="main-pane-editor">
        {selectedFile.type === "web" ? (
          <WebBuffer url={selectedFile.url} />
        ) : selectedFile.type === "git-diff" ? (
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
