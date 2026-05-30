import { Fragment, useCallback, useEffect, useState } from "react";
import {
  Group as PanelGroup,
  Panel,
  Separator as PanelResizeHandle,
} from "react-resizable-panels";
import { AgentReviewBuffer, GitDiffBuffer } from "./DiffBuffer";
import { EditorTabBar } from "./EditorTabBar";
import { EmptyState } from "./EmptyState";
import { EditorBuffer } from "./EditorBuffer";
import { NewSessionDialog } from "./NewSessionDialog";
import { TerminalTabStrip } from "./TerminalTabStrip";
import { TerminalView, type TerminalShellUpdate } from "./TerminalView";
import { stopAgentReview } from "../lib/agent-review";
import {
  AGENT_KINDS,
  AGENT_LABELS,
  buildAgentHandoffPrompt,
  closeSession,
  duplicateSession,
  bufferKey,
  leafSessionIds,
  newCommandBlockId,
  pruneTerminalLayout,
  restartSession,
  sessionAttentionState,
  sessionCanHandoff,
  sessionHasRestorableTerminal,
  sessionNeedsAttention,
  sessionTitle,
  spawnAgentSession,
  terminalPaneNodeForId,
  useSessionStore,
  type AgentKind,
  type CommandBlock,
  type Session,
  type SessionEvent,
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
  workspaces,
  selectedPath,
  sessionEvents,
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
  onAddTab,
  onClose,
}: {
  node: TerminalPaneNode;
  sessions: Session[];
  workspaces: Workspace[];
  selectedPath: string | null;
  sessionEvents: SessionEvent[];
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
  onAddTab: (paneId: string) => void;
  onClose: (session: Session) => void;
}) {
  const setActiveTerminalPane = useSessionStore((state) => state.setActiveTerminalPane);
  const [handoffOpen, setHandoffOpen] = useState(false);
  if (node.type === "split") {
    return (
      <PanelGroup orientation={panelOrientation(node.direction)}>
        {node.children.map((child, index) => (
          <Fragment key={child.paneId}>
            <Panel minSize={12}>
              <TerminalPaneTree
                node={child}
                sessions={sessions}
                workspaces={workspaces}
                selectedPath={selectedPath}
                sessionEvents={sessionEvents}
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
                onAddTab={onAddTab}
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
  const leafSessions = leafSessionIds(node)
    .map((id) => sessions.find((s) => s.id === id))
    .filter((s): s is Session => Boolean(s));

  const session = sessions.find((item) => item.id === node.sessionId) ?? null;
  if (!session) {
    return <div className="editor-message">Terminal session is no longer available.</div>;
  }
  const active = activeSessionId === session.id;
  const workspace = workspaces.find((item) => item.id === session.workspaceId) ?? null;
  const canHandoff = sessionCanHandoff(session) && Boolean(workspace);
  const canRestart = Boolean(session.restart);
  const canMaximize = sessionHasRestorableTerminal(session);
  const attentionState = sessionAttentionState(session);
  const needsAttention = sessionNeedsAttention(session);
  return (
    <section
      className={`terminal-pane ${active ? "active" : ""} ${needsAttention ? "attention" : ""}`}
      data-attention={attentionState}
      onPointerDown={() => setActiveTerminalPane(node.paneId)}
    >
      <div className="terminal-pane-toolbar">
        <span className="terminal-pane-cwd" title={session.cwd ?? session.title}>
          <i className="codicon codicon-folder" aria-hidden="true" />
          <span className="terminal-pane-cwd-text">{session.cwd ?? session.title}</span>
          {session.lastExitCode !== null && session.lastExitCode !== undefined ? (
            <span
              className={`terminal-pane-exit ${session.lastExitCode === 0 ? "ok" : "bad"}`}
            >
              <i
                className={`codicon ${session.lastExitCode === 0 ? "codicon-pass" : "codicon-error"}`}
                aria-hidden="true"
              />
              {session.lastExitCode}
            </span>
          ) : null}
        </span>
        {session.lastTrigger ? (
          <span className="terminal-trigger-pill" title={session.lastTrigger.line}>
            {session.lastTrigger.label}
          </span>
        ) : null}
        <div className="terminal-pane-actions">
          {session.preview ? (
            <button
              type="button"
              className="terminal-pane-icon-button"
              aria-label="Open session preview"
              title={session.preview.url}
              onClick={(event) => {
                event.stopPropagation();
                useSessionStore.getState().openWebUrl(session.preview!.url, session.id);
              }}
            >
              <i className="codicon codicon-link-external" aria-hidden="true" />
            </button>
          ) : null}
          {session.lastCommand?.command ? (
            <button
              type="button"
              className="terminal-pane-icon-button"
              aria-label="Copy last command"
              title={`Copy: ${session.lastCommand.command}`}
              onClick={(event) => {
                event.stopPropagation();
                void navigator.clipboard?.writeText(session.lastCommand!.command);
              }}
            >
              <i className="codicon codicon-copy" aria-hidden="true" />
            </button>
          ) : null}
          {session.lastCommand?.output ? (
            <button
              type="button"
              className="terminal-pane-icon-button"
              aria-label="Copy last output"
              title="Copy last command output"
              onClick={(event) => {
                event.stopPropagation();
                void navigator.clipboard?.writeText(session.lastCommand!.output);
              }}
            >
              <i className="codicon codicon-output" aria-hidden="true" />
            </button>
          ) : null}
          {session.shellPromptMarkerSeen ? (
            <button
              type="button"
              className="terminal-pane-icon-button"
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
              <i className="codicon codicon-arrow-up" aria-hidden="true" />
            </button>
          ) : null}
          {canHandoff ? (
            <button
              type="button"
              className="terminal-pane-icon-button"
              aria-label="Handoff session to another agent"
              title="Handoff to another agent"
              onClick={(event) => {
                event.stopPropagation();
                setHandoffOpen(true);
              }}
            >
              <i className="codicon codicon-arrow-swap" aria-hidden="true" />
            </button>
          ) : null}
          <button
            type="button"
            className="terminal-pane-icon-button"
            aria-label="Restart session"
            title="Restart session"
            disabled={!canRestart}
            onClick={(event) => {
              event.stopPropagation();
              onRestart(session);
            }}
          >
            <i className="codicon codicon-debug-restart" aria-hidden="true" />
          </button>
          <button
            type="button"
            className="terminal-pane-icon-button"
            aria-label="Split right"
            title="Split right (duplicate session)"
            disabled={!canRestart}
            onClick={(event) => {
              event.stopPropagation();
              onDuplicate(session, node.paneId);
            }}
          >
            <i className="codicon codicon-split-horizontal" aria-hidden="true" />
          </button>
          {canMaximize ? (
            <button
              type="button"
              className="terminal-pane-icon-button"
              aria-label={maximizedPaneId === node.paneId ? "Restore pane" : "Maximize pane"}
              title={maximizedPaneId === node.paneId ? "Restore pane" : "Maximize pane"}
              onClick={(event) => {
                event.stopPropagation();
                onToggleMaximize(node.paneId);
              }}
            >
              <i
                className={`codicon ${maximizedPaneId === node.paneId ? "codicon-screen-normal" : "codicon-screen-full"}`}
                aria-hidden="true"
              />
            </button>
          ) : null}
          <button
            type="button"
            className="terminal-pane-icon-button danger"
            aria-label="Close session"
            title="Close session"
            onClick={(event) => {
              event.stopPropagation();
              onClose(session);
            }}
          >
            <i className="codicon codicon-trash" aria-hidden="true" />
          </button>
        </div>
      </div>
      <TerminalTabStrip
        leaf={node}
        sessions={leafSessions}
        onAddSession={onAddTab}
        onCloseSession={onClose}
      />
      {session.status === "stale" ? (
        <div className="terminal-stale-state">
          <strong>Session is stale</strong>
          <span>
            The process is no longer attached. Restart, duplicate, or close this
            saved session.
          </span>
        </div>
      ) : (
        <div className="terminal-pane-bodies">
          {leafSessions.map((tabSession) => (
            <div
              key={tabSession.id}
              className={`terminal-pane-body ${tabSession.id === node.sessionId ? "active" : ""}`}
              data-active={tabSession.id === node.sessionId}
            >
              <TerminalView
                ptyId={tabSession.id}
                fontFamily={settings.terminalFontFamily}
                fontSize={settings.terminalFontSize}
                settings={settings}
                visible={terminalVisible && active && tabSession.id === node.sessionId}
                onExit={() => onTerminalExit(tabSession)}
                onOpenLink={(url) =>
                  useSessionStore.getState().openWebUrl(url, tabSession.id)
                }
                onShellUpdate={(update) => onShellUpdate(tabSession, update)}
                onTrigger={(match) => onTerminalTrigger(tabSession, match)}
              />
            </div>
          ))}
        </div>
      )}
      {handoffOpen && canHandoff ? (
        <AgentHandoffDialog
          session={session}
          workspace={workspace}
          selectedPath={selectedPath}
          sessionEvents={sessionEvents}
          paneId={node.paneId}
          onClose={() => setHandoffOpen(false)}
        />
      ) : null}
    </section>
  );
}

function defaultHandoffAgent(source: AgentKind): AgentKind {
  return (
    AGENT_KINDS.find((agent) => agent !== "shell" && agent !== source) ??
    "claude-code"
  );
}

function AgentHandoffDialog({
  session,
  workspace,
  selectedPath,
  sessionEvents,
  paneId,
  onClose,
}: {
  session: Session;
  workspace: Workspace | null;
  selectedPath: string | null;
  sessionEvents: SessionEvent[];
  paneId: string;
  onClose: () => void;
}) {
  const [agent, setAgent] = useState<AgentKind>(() => defaultHandoffAgent(session.agent));
  const [handoffError, setHandoffError] = useState<string | null>(null);
  const [spawning, setSpawning] = useState(false);

  async function startHandoff() {
    if (!workspace) {
      setHandoffError("This session has no workspace.");
      return;
    }
    setSpawning(true);
    setHandoffError(null);
    try {
      await spawnAgentSession(
        agent,
        workspace,
        buildAgentHandoffPrompt(session, workspace, selectedPath, sessionEvents),
        {
          type: "split",
          targetPaneId: paneId,
          direction: "vertical",
        },
        {
          cwd:
            session.cwd && session.cwd.startsWith(workspace.path)
              ? session.cwd
              : workspace.path,
        },
      );
      onClose();
    } catch (caught) {
      setHandoffError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setSpawning(false);
    }
  }

  return (
    <div className="modal-backdrop handoff-backdrop" role="presentation">
      <section
        className="modal-panel handoff-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="handoff-title"
      >
        <header className="modal-header">
          <h2 className="modal-title" id="handoff-title">
            Handoff Session
          </h2>
          <button
            type="button"
            className="icon-button"
            aria-label="Close handoff dialog"
            onClick={onClose}
          >
            x
          </button>
        </header>
        <div className="modal-body">
          <div className="form-grid">
            <label className="field-label">
              Next agent
              <select
                className="settings-select"
                aria-label="Next agent"
                value={agent}
                onChange={(event) => setAgent(event.target.value as AgentKind)}
              >
                {AGENT_KINDS.filter(
                  (candidate) => candidate !== "shell" && candidate !== session.agent,
                ).map((candidate) => (
                  <option key={candidate} value={candidate}>
                    {AGENT_LABELS[candidate]}
                  </option>
                ))}
              </select>
            </label>
            <div className="settings-note">
              Workspace: {workspace?.path ?? "No workspace"}
            </div>
            <div className="settings-note">
              Source: {AGENT_LABELS[session.agent]} · {session.title}
            </div>
          </div>
          {handoffError ? <div className="editor-error">{handoffError}</div> : null}
          <footer className="dialog-actions">
            <button type="button" className="text-button" onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className="text-button primary"
              disabled={spawning || !workspace}
              onClick={() => void startHandoff()}
            >
              {spawning ? "Starting" : "Start Handoff"}
            </button>
          </footer>
        </div>
      </section>
    </div>
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
  const openBuffers = useSessionStore((state) => state.openBuffers);
  const activeBufferKey = useSessionStore((state) => state.activeBufferKey);
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const sessionEvents = useSessionStore((state) => state.sessionEvents);
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
  const activeTerminalSession =
    session && sessionHasRestorableTerminal(session) ? session : null;
  const renderableSessionIds = new Set(
    sessions
      .filter((item) => sessionHasRestorableTerminal(item))
      .map((item) => item.id),
  );
  const renderableTerminalLayout = pruneTerminalLayout(
    terminalLayout,
    renderableSessionIds,
  );
  const [splitPlacement, setSplitPlacement] = useState<TerminalPanePlacement | null>(
    null,
  );

  const requestSplit = useCallback(
    (direction: TerminalSplitDirection) => {
      if (!activeTerminalSession || !activeTerminalPaneId) {
        return;
      }
      setSplitPlacement({
        type: "split",
        targetPaneId: activeTerminalPaneId,
        direction,
      });
    },
    [activeTerminalPaneId, activeTerminalSession],
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

  const handleAddTab = useCallback(
    (paneId: string) => {
      setSplitPlacement({ type: "tab", targetPaneId: paneId });
    },
    [],
  );

  const handleCloseSession = useCallback((target: Session) => {
    void closeSession(target.id).catch((error) => {
      console.warn("failed to close session", error);
    });
  }, []);

  if (activeTerminalSession) {
    const terminalVisible = selectedFile === null;
    const layout = layoutContainsSession(
      renderableTerminalLayout,
      activeTerminalSession.id,
    )
      ? renderableTerminalLayout!
      : fallbackLayout(activeTerminalSession);
    const visibleLayout =
      maximizedTerminalPaneId && terminalPaneNodeForId(layout, maximizedTerminalPaneId)
        ? terminalPaneNodeForId(layout, maximizedTerminalPaneId)!
        : layout;
    const splitWorkspaceId = activeTerminalSession.workspaceId;
    const splitDefaultCwd = activeTerminalSession.cwd ?? null;
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
                workspaces={workspaces}
                selectedPath={selectedFile?.path ?? null}
                sessionEvents={sessionEvents}
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
                onAddTab={handleAddTab}
                onClose={handleCloseSession}
              />
          </section>
          {selectedFile ? (
            <section className="main-pane-surface editor-surface">
              <EditorTabBar />
              <div className="editor-surface-body">
                <BufferStack
                  openBuffers={openBuffers}
                  activeBufferKey={activeBufferKey}
                  settings={settings}
                />
              </div>
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
        <EditorTabBar />
        <div className="editor-surface-body">
          <BufferStack
            openBuffers={openBuffers}
            activeBufferKey={activeBufferKey}
            settings={settings}
          />
        </div>
      </main>
    );
  }

  return (
    <main className="main-pane" data-testid="main-pane-empty">
      <EmptyState />
    </main>
  );
}

interface BufferStackProps {
  openBuffers: import("../lib/sessions").MainSelection[];
  activeBufferKey: string | null;
  settings: ReturnType<typeof useSessionStore.getState>["settings"];
}

function BufferStack({ openBuffers, activeBufferKey, settings }: BufferStackProps) {
  return (
    <>
      {openBuffers.map((buffer) => {
        const key = bufferKey(buffer);
        const isActive = key === activeBufferKey;
        return (
          <div
            key={key}
            className={`buffer-mount ${isActive ? "active" : ""}`}
            data-active={isActive}
            aria-hidden={!isActive}
          >
            {buffer.type === "web" ? (
              <WebBuffer url={buffer.url} />
            ) : buffer.type === "git-diff" ? (
              <GitDiffBuffer selection={buffer} mode={settings.diffViewMode} />
            ) : buffer.type === "agent-review" ? (
              <AgentReviewBuffer selection={buffer} mode={settings.diffViewMode} />
            ) : (
              <EditorBuffer
                path={buffer.path}
                workspaceRoot={buffer.workspaceRoot}
                fontFamily={settings.editorFontFamily}
                keybindingMode={settings.editorKeybindingMode}
                bufferKey={key}
              />
            )}
          </div>
        );
      })}
    </>
  );
}
