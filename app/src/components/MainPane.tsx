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
import { TerminalView, type TerminalShellUpdate } from "./TerminalView";
import { stopAgentReview } from "../lib/agent-review";
import {
  AGENT_LABELS,
  defaultSessionControl,
  closeSession,
  duplicateSession,
  newCommandBlockId,
  restartSession,
  sessionTitle,
  terminalPaneNodeForId,
  useSessionStore,
  type CommandBlock,
  type Session,
  type TerminalTriggerMatch,
  type TerminalPaneNode,
  type TerminalPanePlacement,
  type TerminalSplitDirection,
  type Workspace,
} from "../lib/sessions";
import { getGitStatus } from "../lib/git";
import { persistCommandBlock } from "../lib/command-blocks";
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
    cwd: workspace.path,
    lastExitCode: null,
    lastTrigger: null,
    lastCommandBlockId: null,
    control: defaultSessionControl("shell"),
    restart: {
      command: shellPath(),
      args: [],
      cwd: workspace.path,
      env: [],
    },
  };
}

function panelOrientation(direction: TerminalSplitDirection): "horizontal" | "vertical" {
  return direction === "vertical" ? "horizontal" : "vertical";
}

function fallbackLayout(session: Session): TerminalPaneNode {
  return { type: "leaf", paneId: `fallback-${session.id}`, sessionId: session.id };
}

function commandBlockStatus(exitCode: number | null | undefined): CommandBlock["status"] {
  return exitCode === 0 ? "succeeded" : "failed";
}

function outputPreview(output: string): string {
  return output.trim().replace(/\n{3,}/g, "\n\n").slice(-4000);
}

async function changedFilesForSession(
  session: Session,
  workspaces: Workspace[],
): Promise<string[]> {
  const workspace = workspaces.find((item) => item.id === session.workspaceId);
  if (!workspace) {
    return [];
  }
  try {
    const status = await getGitStatus(workspace.path);
    const paths = new Set<string>();
    for (const entry of status.entries) {
      paths.add(entry.path);
      if (entry.originalPath) {
        paths.add(entry.originalPath);
      }
    }
    return [...paths].slice(0, 30);
  } catch {
    return [];
  }
}

function layoutContainsSession(node: TerminalPaneNode | null, sessionId: string): boolean {
  if (!node) {
    return false;
  }
  if (node.type === "leaf") {
    return node.sessionId === sessionId;
  }
  return node.children.some((child) => layoutContainsSession(child, sessionId));
}

function TerminalPaneTree({
  node,
  sessions,
  activeSessionId,
  terminalVisible,
  settings,
  onTerminalExit,
  onShellUpdate,
  onTerminalTrigger,
  maximizedPaneId,
  onToggleMaximize,
  onRestart,
  onDuplicate,
  onClose,
}: {
  node: TerminalPaneNode;
  sessions: Session[];
  activeSessionId: string | null;
  terminalVisible: boolean;
  settings: ReturnType<typeof useSessionStore.getState>["settings"];
  onTerminalExit: (session: Session) => void;
  onShellUpdate: (session: Session, update: TerminalShellUpdate) => void;
  onTerminalTrigger: (session: Session, match: TerminalTriggerMatch) => void;
  maximizedPaneId: string | null;
  onToggleMaximize: (paneId: string) => void;
  onRestart: (session: Session) => void;
  onDuplicate: (session: Session, paneId: string) => void;
  onClose: (session: Session) => void;
}) {
  const setActiveTerminalPane = useSessionStore((state) => state.setActiveTerminalPane);
  const setSessionControlState = useSessionStore((state) => state.setSessionControlState);
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
                onShellUpdate={onShellUpdate}
                onTerminalTrigger={onTerminalTrigger}
                maximizedPaneId={maximizedPaneId}
                onToggleMaximize={onToggleMaximize}
                onRestart={onRestart}
                onDuplicate={onDuplicate}
                onClose={onClose}
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
      <div className="terminal-pane-toolbar">
        <span title={session.cwd ?? session.title}>
          {session.cwd ?? session.title}
          {session.lastExitCode !== null && session.lastExitCode !== undefined
            ? ` · ${session.lastExitCode}`
            : ""}
        </span>
        {session.lastTrigger ? (
          <span className="terminal-trigger-pill" title={session.lastTrigger.line}>
            {session.lastTrigger.label}
          </span>
        ) : null}
        {session.preview ? (
          <button
            type="button"
            className="terminal-pane-button"
            aria-label="Open session preview"
            title={session.preview.url}
            onClick={(event) => {
              event.stopPropagation();
              useSessionStore.getState().openWebUrl(session.preview!.url, session.id);
            }}
          >
            Preview
          </button>
        ) : null}
        {session.lastCommand?.command ? (
          <button
            type="button"
            className="terminal-pane-button"
            aria-label="Copy last command"
            title={session.lastCommand.command}
            onClick={(event) => {
              event.stopPropagation();
              void navigator.clipboard?.writeText(session.lastCommand!.command);
            }}
          >
            Copy Cmd
          </button>
        ) : null}
        {session.lastCommand?.output ? (
          <button
            type="button"
            className="terminal-pane-button"
            aria-label="Copy last output"
            title="Copy last command output"
            onClick={(event) => {
              event.stopPropagation();
              void navigator.clipboard?.writeText(session.lastCommand!.output);
            }}
          >
            Copy Out
          </button>
        ) : null}
        {session.shellPromptMarkerSeen ? (
          <button
            type="button"
            className="terminal-pane-button"
            aria-label="Jump to last prompt"
            title="Jump to last prompt"
            onClick={(event) => {
              event.stopPropagation();
              window.dispatchEvent(
                new CustomEvent("onibi:jump-last-prompt", {
                  detail: { ptyId: session.id },
                }),
              );
            }}
          >
            Prompt
          </button>
        ) : null}
        <button
          type="button"
          className="terminal-pane-button"
          aria-label={
            session.control?.owner === "user"
              ? "Hand session back to agent"
              : "Take over session"
          }
          title={
            session.control?.externalInputBlocked
              ? "External input is blocked"
              : "External input is allowed"
          }
          onClick={(event) => {
            event.stopPropagation();
            const userOwned = session.control?.owner === "user";
            setSessionControlState(session.id, {
              owner: userOwned ? "agent" : "user",
              externalInputBlocked: !userOwned,
              updatedAt: Date.now(),
              reason: userOwned ? "hand-back" : "takeover",
            });
          }}
        >
          {session.control?.owner === "user" ? "Hand Back" : "Take Over"}
        </button>
        <button
          type="button"
          className="terminal-pane-button"
          aria-label="Restart session"
          title="Restart session"
          onClick={(event) => {
            event.stopPropagation();
            onRestart(session);
          }}
        >
          Restart
        </button>
        <button
          type="button"
          className="terminal-pane-button"
          aria-label="Duplicate session"
          title="Duplicate session"
          onClick={(event) => {
            event.stopPropagation();
            onDuplicate(session, node.paneId);
          }}
        >
          Duplicate
        </button>
        <button
          type="button"
          className="terminal-pane-button"
          aria-label="Close session"
          title="Close session"
          onClick={(event) => {
            event.stopPropagation();
            onClose(session);
          }}
        >
          Close
        </button>
        <button
          type="button"
          className="terminal-pane-button"
          aria-label={maximizedPaneId === node.paneId ? "Restore pane" : "Maximize pane"}
          title={maximizedPaneId === node.paneId ? "Restore pane" : "Maximize pane"}
          onClick={(event) => {
            event.stopPropagation();
            onToggleMaximize(node.paneId);
          }}
        >
          {maximizedPaneId === node.paneId ? "Restore" : "Maximize"}
        </button>
      </div>
      {session.status === "stale" ? (
        <div className="terminal-stale-state">
          <strong>Session is stale</strong>
          <span>
            The process is no longer attached. Restart, duplicate, or close this
            saved session.
          </span>
        </div>
      ) : (
        <TerminalView
          ptyId={session.id}
          fontFamily={settings.terminalFontFamily}
          fontSize={settings.terminalFontSize}
          settings={settings}
          visible={terminalVisible && active}
          onExit={() => onTerminalExit(session)}
          onOpenLink={(url) => useSessionStore.getState().openWebUrl(url, session.id)}
          onShellUpdate={(update) => onShellUpdate(session, update)}
          onTrigger={(match) => onTerminalTrigger(session, match)}
        />
      )}
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
  const maximizedTerminalPaneId = useSessionStore(
    (state) => state.maximizedTerminalPaneId,
  );
  const terminalLayout = useSessionStore((state) => state.terminalLayout);
  const settings = useSessionStore((state) => state.settings);
  const replaceSession = useSessionStore((state) => state.replaceSession);
  const updateSession = useSessionStore((state) => state.updateSession);
  const appendSessionEvent = useSessionStore((state) => state.appendSessionEvent);
  const appendSessionTranscript = useSessionStore(
    (state) => state.appendSessionTranscript,
  );
  const startCommandBlock = useSessionStore((state) => state.startCommandBlock);
  const finishCommandBlock = useSessionStore((state) => state.finishCommandBlock);
  const toggleMaximizedTerminalPane = useSessionStore(
    (state) => state.toggleMaximizedTerminalPane,
  );
  const focusRelativeTerminalPane = useSessionStore(
    (state) => state.focusRelativeTerminalPane,
  );
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
        !primary ||
        event.altKey
      ) {
        return;
      }
      const target = event.target;
      if (target instanceof Element && target.closest(".modal-panel")) {
        return;
      }
      if (event.key.toLowerCase() === "d") {
        event.preventDefault();
        requestSplit(event.shiftKey ? "horizontal" : "vertical");
      } else if (event.key === "Enter" && event.shiftKey) {
        event.preventDefault();
        toggleMaximizedTerminalPane(activeTerminalPaneId);
      }
    }
    window.addEventListener("keydown", handleSplitShortcut);
    return () => window.removeEventListener("keydown", handleSplitShortcut);
  }, [activeTerminalPaneId, requestSplit, toggleMaximizedTerminalPane]);

  useEffect(() => {
    function handlePaneFocusShortcut(event: KeyboardEvent) {
      const primary = event.metaKey || event.ctrlKey;
      if (!primary || !event.altKey || event.defaultPrevented) {
        return;
      }
      if (event.key === "ArrowRight" || event.key === "ArrowDown") {
        event.preventDefault();
        focusRelativeTerminalPane(1);
      } else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
        event.preventDefault();
        focusRelativeTerminalPane(-1);
      }
    }
    window.addEventListener("keydown", handlePaneFocusShortcut);
    return () => window.removeEventListener("keydown", handlePaneFocusShortcut);
  }, [focusRelativeTerminalPane]);

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

  const handleShellUpdate = useCallback(
    (updatedSession: Session, update: TerminalShellUpdate) => {
      if (update.transcriptChunk) {
        appendSessionTranscript(updatedSession.id, update.transcriptChunk);
      }
      if (update.commandStarted) {
        startCommandBlock({
          id: newCommandBlockId(),
          sessionId: updatedSession.id,
          workspaceId: updatedSession.workspaceId,
          agent: updatedSession.agent,
          command: update.commandStarted.command,
          cwd: update.cwd ?? updatedSession.cwd ?? "",
          startedAt: update.commandStarted.startedAt,
          endedAt: null,
          exitCode: null,
          status: "running",
          outputPreview: "",
          previewUrl: update.preview?.url ?? updatedSession.preview?.url ?? null,
          changedFiles: [],
          attention: "running",
          source: "shell-integration",
        });
      }
      if (
        update.cwd !== undefined ||
        update.lastExitCode !== undefined ||
        update.preview ||
        update.promptMarkerSeen ||
        update.commandStarted ||
        update.lastCommand
      ) {
        updateSession(updatedSession.id, {
          cwd: update.cwd ?? updatedSession.cwd,
          lastExitCode:
            update.lastExitCode !== undefined
              ? update.lastExitCode
              : updatedSession.lastExitCode,
          preview: update.preview ?? updatedSession.preview ?? null,
          shellPromptMarkerSeen:
            update.promptMarkerSeen ?? updatedSession.shellPromptMarkerSeen,
          lastCommand:
            update.lastCommand ??
            (update.commandStarted
              ? {
                  command: update.commandStarted.command,
                  output: "",
                  startedAt: update.commandStarted.startedAt,
                  endedAt: null,
                  exitCode: null,
                }
              : updatedSession.lastCommand ?? null),
        });
      }
      if (update.lastCommand) {
        const draft =
          useSessionStore.getState().activeCommandBlocks[updatedSession.id] ?? null;
        const marker = update.lastCommand;
        void changedFilesForSession(updatedSession, workspaces).then((changedFiles) => {
          const block: CommandBlock = {
            id: draft?.id ?? newCommandBlockId(),
            sessionId: updatedSession.id,
            workspaceId: updatedSession.workspaceId,
            agent: updatedSession.agent,
            command: marker.command || draft?.command || "",
            cwd: update.cwd ?? draft?.cwd ?? updatedSession.cwd ?? "",
            startedAt: marker.startedAt || draft?.startedAt || Date.now(),
            endedAt: marker.endedAt ?? Date.now(),
            exitCode: marker.exitCode ?? update.lastExitCode ?? null,
            status: commandBlockStatus(marker.exitCode ?? update.lastExitCode),
            outputPreview: outputPreview(marker.output),
            previewUrl: update.preview?.url ?? updatedSession.preview?.url ?? null,
            changedFiles,
            attention:
              (marker.exitCode ?? update.lastExitCode ?? 0) === 0
                ? "idle"
                : "failed",
            source: "shell-integration",
          };
          finishCommandBlock(block);
          void persistCommandBlock(block).catch((error) => {
            console.warn("failed to persist command block", error);
          });
        });
      }
    },
    [
      appendSessionTranscript,
      finishCommandBlock,
      startCommandBlock,
      updateSession,
      workspaces,
    ],
  );

  const handleTerminalTrigger = useCallback(
    (triggeredSession: Session, match: TerminalTriggerMatch) => {
      updateSession(triggeredSession.id, { lastTrigger: match });
      appendSessionEvent({
        type: "terminal-trigger",
        workspaceId: triggeredSession.workspaceId,
        sessionId: triggeredSession.id,
        agent: triggeredSession.agent,
        summary: `${match.label}: ${match.line}`,
        metadata: { triggerId: match.id },
      });
      if ("Notification" in window && match.actions.includes("notify")) {
        if (Notification.permission === "granted") {
          new Notification(`Onibi: ${match.label}`, { body: match.line });
        } else if (Notification.permission === "default") {
          void Notification.requestPermission();
        }
      }
      if (match.actions.includes("open-preview") && triggeredSession.preview) {
        useSessionStore
          .getState()
          .openWebUrl(triggeredSession.preview.url, triggeredSession.id);
      }
      if (match.actions.includes("copy-line")) {
        void navigator.clipboard?.writeText(match.line);
      }
    },
    [appendSessionEvent, updateSession],
  );

  const handleRestartSession = useCallback(
    (target: Session) => {
      void restartSession(target.id).catch((error) => {
        console.warn("failed to restart session", error);
        updateSession(target.id, { status: "error" });
      });
    },
    [updateSession],
  );

  const handleDuplicateSession = useCallback((target: Session, paneId: string) => {
    void duplicateSession(target.id, {
      type: "split",
      targetPaneId: paneId,
      direction: "vertical",
    }).catch((error) => {
      console.warn("failed to duplicate session", error);
    });
  }, []);

  const handleCloseSession = useCallback((target: Session) => {
    void closeSession(target.id).catch((error) => {
      console.warn("failed to close session", error);
    });
  }, []);

  if (session) {
    const terminalVisible = selectedFile === null;
    const layout = layoutContainsSession(terminalLayout, session.id)
      ? terminalLayout!
      : fallbackLayout(session);
    const visibleLayout =
      maximizedTerminalPaneId && terminalPaneNodeForId(layout, maximizedTerminalPaneId)
        ? terminalPaneNodeForId(layout, maximizedTerminalPaneId)!
        : layout;
    const splitWorkspaceId = session.workspaceId;
    const splitDefaultCwd = session.cwd ?? null;
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
                node={visibleLayout}
                sessions={sessions}
                activeSessionId={activeSessionId}
                terminalVisible={terminalVisible}
                settings={settings}
                onTerminalExit={handleTerminalExit}
                onShellUpdate={handleShellUpdate}
                onTerminalTrigger={handleTerminalTrigger}
                maximizedPaneId={maximizedTerminalPaneId}
                onToggleMaximize={toggleMaximizedTerminalPane}
                onRestart={handleRestartSession}
                onDuplicate={handleDuplicateSession}
                onClose={handleCloseSession}
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
          defaultCwd={splitDefaultCwd}
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
