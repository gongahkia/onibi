import {
  Fragment,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent as ReactDragEvent,
} from "react";
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
  resolveNewPaneCwd,
  sessionAttentionState,
  sessionCanHandoff,
  sessionHasRestorableTerminal,
  sessionNeedsAttention,
  sessionTitle,
  spawnAgentSession,
  terminalPaneNodeForId,
  useSessionStore,
  isAbsoluteCwdPath,
  type AgentKind,
  type CommandBlock,
  type Session,
  type SessionEvent,
  type TerminalTriggerMatch,
  type TerminalPaneNode,
  type TerminalPanePlacement,
  type TerminalSplitDirection,
  type Workspace,
  type WorkspaceTab,
} from "../lib/sessions";
import { getGitStatus } from "../lib/git";
import { persistCommandBlock } from "../lib/command-blocks";
import { ptySpawn, shellPath } from "../lib/tauri-bridge";

const WORKSPACE_TAB_DRAG_MIME = "application/x-onibi-workspace-terminal-tab";
const TERMINAL_PANE_DRAG_MIME = "application/x-onibi-terminal-pane";

async function spawnShellReplacement(
  session: Session,
  workspace: Workspace,
): Promise<Session> {
  const shellMode = useSessionStore.getState().settings.terminalShellMode;
  const restart = {
    command: shellPath(),
    args: [],
    cwd: workspace.path,
    env: [],
    shellMode,
  };
  const id = await ptySpawn({
    command: restart.command,
    args: restart.args,
    cwd: restart.cwd,
    env: restart.env,
    shellMode: restart.shellMode,
    rows: 30,
    cols: 100,
    agent: "shell",
    workspaceId: workspace.id,
    title: sessionTitle("shell", workspace),
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
    restart,
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
    return leafSessionIds(node).includes(sessionId);
  }
  return node.children.some((child) => layoutContainsSession(child, sessionId));
}

function sessionIdsForLayout(node: TerminalPaneNode | null): string[] {
  if (!node) {
    return [];
  }
  if (node.type === "leaf") {
    return leafSessionIds(node);
  }
  return node.children.flatMap(sessionIdsForLayout);
}

function WorkspaceTerminalTabStrip({
  workspace,
  tabs,
  sessions,
  activeTabId,
  onSelect,
  onCreate,
  onReorder,
}: {
  workspace: Workspace;
  tabs: WorkspaceTab[];
  sessions: Session[];
  activeTabId: string | null;
  onSelect: (workspaceId: string, tabId: string) => void;
  onCreate: (workspaceId: string) => void;
  onReorder: (
    workspaceId: string,
    fromTabId: string,
    toTabId: string,
    before: boolean,
  ) => void;
}) {
  const [draggingTabId, setDraggingTabId] = useState<string | null>(null);
  const [dropIndicator, setDropIndicator] = useState<{
    tabId: string;
    before: boolean;
  } | null>(null);

  function draggedTabId(event: ReactDragEvent): string | null {
    return draggingTabId || event.dataTransfer.getData(WORKSPACE_TAB_DRAG_MIME) || null;
  }

  function dropBefore(event: ReactDragEvent<HTMLButtonElement>): boolean {
    const rect = event.currentTarget.getBoundingClientRect();
    return event.clientX < rect.left + rect.width / 2;
  }

  return (
    <div
      className="workspace-terminal-tab-strip"
      role="tablist"
      aria-label={`${workspace.name} terminal tabs`}
    >
      {tabs.map((tab) => {
        const tabSessionIds = sessionIdsForLayout(tab.terminalLayout);
        const firstSession = sessions.find((session) =>
          tabSessionIds.includes(session.id),
        );
        const label =
          tab.title ||
          firstSession?.title ||
          (tabSessionIds.length > 0 ? "Terminal" : "Empty");
        const active = tab.id === activeTabId;
        return (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={active}
            draggable
            className={`workspace-terminal-tab ${active ? "active" : ""} ${
              draggingTabId === tab.id ? "dragging" : ""
            } ${
              dropIndicator?.tabId === tab.id
                ? dropIndicator.before
                  ? "drop-before"
                  : "drop-after"
                : ""
            }`}
            title={`${workspace.name}: ${label}`}
            onClick={() => onSelect(workspace.id, tab.id)}
            onDragStart={(event) => {
              setDraggingTabId(tab.id);
              event.dataTransfer.effectAllowed = "move";
              event.dataTransfer.setData(WORKSPACE_TAB_DRAG_MIME, tab.id);
              event.dataTransfer.setData("text/plain", label);
            }}
            onDragOver={(event) => {
              const fromId = draggedTabId(event);
              if (!fromId || fromId === tab.id) {
                return;
              }
              event.preventDefault();
              event.dataTransfer.dropEffect = "move";
              setDropIndicator({ tabId: tab.id, before: dropBefore(event) });
            }}
            onDragLeave={(event) => {
              if (
                event.currentTarget instanceof Node &&
                event.relatedTarget instanceof Node &&
                event.currentTarget.contains(event.relatedTarget)
              ) {
                return;
              }
              setDropIndicator((current) =>
                current?.tabId === tab.id ? null : current,
              );
            }}
            onDrop={(event) => {
              const fromId = draggedTabId(event);
              setDraggingTabId(null);
              setDropIndicator(null);
              if (!fromId || fromId === tab.id) {
                return;
              }
              event.preventDefault();
              onReorder(workspace.id, fromId, tab.id, dropBefore(event));
            }}
            onDragEnd={() => {
              setDraggingTabId(null);
              setDropIndicator(null);
            }}
          >
            <i className="codicon codicon-terminal" aria-hidden="true" />
            <span className="workspace-terminal-tab-label">{label}</span>
            {tabSessionIds.length > 1 ? (
              <span className="workspace-terminal-tab-count">
                {tabSessionIds.length}
              </span>
            ) : null}
            {tabSessionIds.length === 0 ? (
              <span className="workspace-terminal-tab-empty">empty</span>
            ) : null}
          </button>
        );
      })}
      <button
        type="button"
        className="workspace-terminal-tab-add"
        aria-label="New workspace terminal tab"
        title="New terminal tab"
        onClick={() => onCreate(workspace.id)}
      >
        <i className="codicon codicon-add" aria-hidden="true" />
      </button>
    </div>
  );
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
  onTerminalUnavailable,
  onShellUpdate,
  onTerminalTrigger,
  maximizedPaneId,
  autoRestartingSessionIds,
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
  onTerminalUnavailable: (session: Session) => void;
  onShellUpdate: (session: Session, update: TerminalShellUpdate) => void;
  onTerminalTrigger: (session: Session, match: TerminalTriggerMatch) => void;
  maximizedPaneId: string | null;
  autoRestartingSessionIds: Set<string>;
  onToggleMaximize: (paneId: string) => void;
  onRestart: (session: Session) => void;
  onDuplicate: (session: Session, paneId: string) => void;
  onAddTab: (paneId: string) => void;
  onClose: (session: Session) => void;
}) {
  const setActiveTerminalPane = useSessionStore((state) => state.setActiveTerminalPane);
  const updateTerminalSplitSizes = useSessionStore(
    (state) => state.updateTerminalSplitSizes,
  );
  const moveTerminalPane = useSessionStore((state) => state.moveTerminalPane);
  const [handoffOpen, setHandoffOpen] = useState(false);
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number } | null>(
    null,
  );
  const [paneDropPosition, setPaneDropPosition] = useState<"before" | "after" | null>(
    null,
  );
  useEffect(() => {
    if (!contextMenu) {
      return undefined;
    }
    const close = () => setContextMenu(null);
    window.addEventListener("click", close);
    window.addEventListener("keydown", close);
    return () => {
      window.removeEventListener("click", close);
      window.removeEventListener("keydown", close);
    };
  }, [contextMenu]);
  if (node.type === "split") {
    const sizes =
      node.sizes && node.sizes.length === node.children.length ? node.sizes : undefined;
    return (
      <PanelGroup
        orientation={panelOrientation(node.direction)}
        onLayoutChanged={(layout) =>
          updateTerminalSplitSizes(
            node.paneId,
            node.children.map((child) => layout[child.paneId] ?? 0),
          )
        }
      >
        {node.children.map((child, index) => (
          <Fragment key={child.paneId}>
            <Panel id={child.paneId} minSize={12} defaultSize={sizes?.[index]}>
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
                onTerminalUnavailable={onTerminalUnavailable}
                onShellUpdate={onShellUpdate}
                onTerminalTrigger={onTerminalTrigger}
                maximizedPaneId={maximizedPaneId}
                autoRestartingSessionIds={autoRestartingSessionIds}
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
  const autoRestarting = autoRestartingSessionIds.has(session.id);
  function dragPaneId(event: ReactDragEvent): string | null {
    const value = event.dataTransfer.getData(TERMINAL_PANE_DRAG_MIME);
    return value.trim() || null;
  }

  function paneDropPositionFromEvent(event: ReactDragEvent<HTMLElement>) {
    const rect = event.currentTarget.getBoundingClientRect();
    const useHorizontalAxis = rect.width >= rect.height;
    const midpoint = useHorizontalAxis
      ? rect.left + rect.width / 2
      : rect.top + rect.height / 2;
    const pointer = useHorizontalAxis ? event.clientX : event.clientY;
    return pointer < midpoint ? "before" : "after";
  }

  return (
    <section
      className={`terminal-pane ${active ? "active" : ""} ${needsAttention ? "attention" : ""} ${paneDropPosition ? `drop-${paneDropPosition}` : ""}`}
      data-attention={attentionState}
      onPointerDown={() => setActiveTerminalPane(node.paneId)}
      onContextMenu={(event) => {
        event.preventDefault();
        setActiveTerminalPane(node.paneId);
        setContextMenu({ x: event.clientX, y: event.clientY });
      }}
      onDragOver={(event) => {
        if (dragPaneId(event) && dragPaneId(event) !== node.paneId) {
          event.preventDefault();
          event.dataTransfer.dropEffect = "move";
          setPaneDropPosition(paneDropPositionFromEvent(event));
        }
      }}
      onDragLeave={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
          setPaneDropPosition(null);
        }
      }}
      onDrop={(event) => {
        const sourcePaneId = dragPaneId(event);
        setPaneDropPosition(null);
        if (!sourcePaneId || sourcePaneId === node.paneId) {
          return;
        }
        event.preventDefault();
        moveTerminalPane(sourcePaneId, node.paneId, paneDropPositionFromEvent(event));
      }}
    >
      <div className="terminal-pane-toolbar">
        {settings.showTerminalPaneAgentLabels ? (
          <span
            className="terminal-pane-agent-label"
            title={`${AGENT_LABELS[session.agent]} · ${session.title}`}
          >
            {AGENT_LABELS[session.agent]}
          </span>
        ) : null}
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
          <button
            type="button"
            className="terminal-pane-icon-button drag-handle"
            aria-label="Drag pane"
            title="Drag pane"
            draggable
            onDragStart={(event) => {
              event.stopPropagation();
              event.dataTransfer.effectAllowed = "move";
              event.dataTransfer.setData(TERMINAL_PANE_DRAG_MIME, node.paneId);
            }}
            onDragEnd={() => setPaneDropPosition(null)}
          >
            <i className="codicon codicon-gripper" aria-hidden="true" />
          </button>
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
      {contextMenu ? (
        <TerminalPaneContextMenu
          x={contextMenu.x}
          y={contextMenu.y}
          paneId={node.paneId}
          session={session}
          workspace={workspace}
          canRestart={canRestart}
          canMaximize={canMaximize}
          isMaximized={maximizedPaneId === node.paneId}
          canHandoff={canHandoff}
          onClose={() => setContextMenu(null)}
          onFocus={() => setActiveTerminalPane(node.paneId)}
          onToggleMaximize={() => onToggleMaximize(node.paneId)}
          onRestart={() => onRestart(session)}
          onDuplicate={() => onDuplicate(session, node.paneId)}
          onAddTab={() => onAddTab(node.paneId)}
          onHandoff={() => setHandoffOpen(true)}
          onCloseSession={() => onClose(session)}
        />
      ) : null}
      <TerminalTabStrip
        leaf={node}
        sessions={leafSessions}
        onAddSession={onAddTab}
        onCloseSession={onClose}
      />
      {session.status === "stale" ? (
        <div className="terminal-stale-state">
          <strong>{autoRestarting ? "Restarting session" : "Session is stale"}</strong>
          <span>
            {autoRestarting
              ? "Reattaching the saved terminal process."
              : "The process is no longer attached. Restart, duplicate, or close this saved session."}
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
                onUnavailable={() => onTerminalUnavailable(tabSession)}
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

function TerminalPaneContextMenu({
  x,
  y,
  paneId,
  session,
  workspace,
  canRestart,
  canMaximize,
  isMaximized,
  canHandoff,
  onClose,
  onFocus,
  onToggleMaximize,
  onRestart,
  onDuplicate,
  onAddTab,
  onHandoff,
  onCloseSession,
}: {
  x: number;
  y: number;
  paneId: string;
  session: Session;
  workspace: Workspace | null;
  canRestart: boolean;
  canMaximize: boolean;
  isMaximized: boolean;
  canHandoff: boolean;
  onClose: () => void;
  onFocus: () => void;
  onToggleMaximize: () => void;
  onRestart: () => void;
  onDuplicate: () => void;
  onAddTab: () => void;
  onHandoff: () => void;
  onCloseSession: () => void;
}) {
  const left =
    typeof window === "undefined" ? x : Math.min(x, Math.max(window.innerWidth - 272, 8));
  const top =
    typeof window === "undefined" ? y : Math.min(y, Math.max(window.innerHeight - 360, 8));

  function run(action: () => void) {
    action();
    onClose();
  }

  function copy(value: string | null | undefined) {
    if (value) {
      void navigator.clipboard?.writeText(value);
    }
  }

  return (
    <div
      className="file-context-menu terminal-pane-context-menu"
      role="menu"
      style={{ left, top }}
      onClick={(event) => event.stopPropagation()}
      onContextMenu={(event) => event.preventDefault()}
    >
      <div className="context-menu-title">
        {AGENT_LABELS[session.agent]} · {session.title}
        <span>{workspace?.name ?? "Workspace"}</span>
      </div>
      <button type="button" role="menuitem" onClick={() => run(onFocus)}>
        Focus Pane
      </button>
      <button
        type="button"
        role="menuitem"
        disabled={!canMaximize}
        onClick={() => run(onToggleMaximize)}
      >
        {isMaximized ? "Restore Pane" : "Maximize Pane"}
      </button>
      <div className="context-separator" role="separator" />
      <button type="button" role="menuitem" onClick={() => run(onAddTab)}>
        New Terminal Tab Here
      </button>
      <button
        type="button"
        role="menuitem"
        disabled={!canRestart}
        onClick={() => run(onDuplicate)}
      >
        Split Right with Duplicate
      </button>
      <button
        type="button"
        role="menuitem"
        disabled={!canRestart}
        onClick={() => run(onRestart)}
      >
        Restart Session
      </button>
      <button
        type="button"
        role="menuitem"
        disabled={!canHandoff}
        onClick={() => run(onHandoff)}
      >
        Handoff to Another Agent
      </button>
      <div className="context-separator" role="separator" />
      <button type="button" role="menuitem" onClick={() => run(() => copy(paneId))}>
        Copy Pane ID
      </button>
      <button
        type="button"
        role="menuitem"
        onClick={() => run(() => copy(session.id))}
      >
        Copy Session ID
      </button>
      <button
        type="button"
        role="menuitem"
        disabled={!workspace}
        onClick={() => run(() => copy(workspace?.path))}
      >
        Copy Workspace Path
      </button>
      <div className="context-separator" role="separator" />
      <button type="button" role="menuitem" className="danger" onClick={() => run(onCloseSession)}>
        Close Session
      </button>
    </div>
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
          type: "tab",
          targetPaneId: paneId,
        },
        {
          cwd: isAbsoluteCwdPath(session.cwd) ? session.cwd : workspace.path,
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
  const workspaceTabs = useSessionStore((state) => state.workspaceTabs);
  const activeWorkspaceId = useSessionStore((state) => state.activeWorkspaceId);
  const activeWorkspaceTabId = useSessionStore(
    (state) => state.activeWorkspaceTabId,
  );
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
  const setActiveWorkspaceTab = useSessionStore(
    (state) => state.setActiveWorkspaceTab,
  );
  const createWorkspaceTab = useSessionStore(
    (state) => state.createWorkspaceTab,
  );
  const reorderWorkspaceTab = useSessionStore(
    (state) => state.reorderWorkspaceTab,
  );
  const session = sessions.find((item) => item.id === activeSessionId) ?? null;
  const activeTerminalSession = session;
  const activeWorkspace = useMemo(() => {
    return (
      workspaces.find((workspace) => workspace.id === activeWorkspaceId) ??
      (activeTerminalSession
        ? workspaces.find(
            (workspace) => workspace.id === activeTerminalSession.workspaceId,
          )
        : null) ??
      workspaces[0] ??
      null
    );
  }, [activeTerminalSession, activeWorkspaceId, workspaces]);
  const activeWorkspaceTabs = useMemo(
    () =>
      activeWorkspace
        ? workspaceTabs.filter((tab) => tab.workspaceId === activeWorkspace.id)
        : [],
    [activeWorkspace, workspaceTabs],
  );
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
  const attemptedAutoRestarts = useRef<Set<string>>(new Set());
  const [autoRestartingSessionIds, setAutoRestartingSessionIds] = useState<
    Set<string>
  >(() => new Set());

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
    function handleSplitEvent(event: Event) {
      const direction = (event as CustomEvent<{ direction?: TerminalSplitDirection }>)
        .detail?.direction;
      requestSplit(direction === "horizontal" ? "horizontal" : "vertical");
    }

    window.addEventListener("onibi:terminal-split", handleSplitEvent);
    return () => window.removeEventListener("onibi:terminal-split", handleSplitEvent);
  }, [requestSplit]);

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

  const handleTerminalUnavailable = useCallback(
    (unavailableSession: Session) => {
      updateSession(unavailableSession.id, { status: "stale" });
      appendSessionEvent({
        type: "session-stopped",
        workspaceId: unavailableSession.workspaceId,
        sessionId: unavailableSession.id,
        agent: unavailableSession.agent,
        summary: `${unavailableSession.title} detached`,
      });
      if (unavailableSession.agent !== "shell") {
        void stopAgentReview(unavailableSession.id).catch(() => undefined);
      }
    },
    [appendSessionEvent, updateSession],
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

  useEffect(() => {
    const target = activeTerminalSession;
    if (!target || target.status !== "stale" || !target.restart) {
      return;
    }
    if (attemptedAutoRestarts.current.has(target.id)) {
      return;
    }
    attemptedAutoRestarts.current.add(target.id);
    setAutoRestartingSessionIds((current) => {
      const next = new Set(current);
      next.add(target.id);
      return next;
    });
    void restartSession(target.id)
      .catch((error) => {
        console.warn("failed to auto-restart stale session", error);
        updateSession(target.id, { status: "stale" });
      })
      .finally(() => {
        setAutoRestartingSessionIds((current) => {
          const next = new Set(current);
          next.delete(target.id);
          return next;
        });
      });
  }, [activeTerminalSession, updateSession]);

  if (activeTerminalSession || activeWorkspace) {
    const terminalVisible = selectedFile === null;
    const layout = activeTerminalSession
      ? layoutContainsSession(renderableTerminalLayout, activeTerminalSession.id)
        ? renderableTerminalLayout!
        : fallbackLayout(activeTerminalSession)
      : null;
    const visibleLayout =
      layout && maximizedTerminalPaneId && terminalPaneNodeForId(layout, maximizedTerminalPaneId)
        ? terminalPaneNodeForId(layout, maximizedTerminalPaneId)!
        : layout;
    const splitWorkspaceId =
      activeTerminalSession?.workspaceId ?? activeWorkspace?.id ?? null;
    const splitDefaultCwd = resolveNewPaneCwd(
      settings,
      activeTerminalSession,
      activeWorkspace,
    );
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
            {activeWorkspace ? (
              <WorkspaceTerminalTabStrip
                workspace={activeWorkspace}
                tabs={activeWorkspaceTabs}
                sessions={sessions}
                activeTabId={activeWorkspaceTabId}
                onSelect={setActiveWorkspaceTab}
                onCreate={createWorkspaceTab}
                onReorder={reorderWorkspaceTab}
              />
            ) : null}
            <div className="workspace-terminal-body">
              {activeTerminalSession && visibleLayout ? (
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
                  onTerminalUnavailable={handleTerminalUnavailable}
                  onShellUpdate={handleShellUpdate}
                  onTerminalTrigger={handleTerminalTrigger}
                  maximizedPaneId={maximizedTerminalPaneId}
                  autoRestartingSessionIds={autoRestartingSessionIds}
                  onToggleMaximize={toggleMaximizedTerminalPane}
                  onRestart={handleRestartSession}
                  onDuplicate={handleDuplicateSession}
                  onAddTab={handleAddTab}
                  onClose={handleCloseSession}
                />
              ) : (
                <div className="workspace-tab-empty">
                  <EmptyState />
                </div>
              )}
            </div>
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
                vimRelativeLineNumbers={settings.editorVimRelativeLineNumbers}
                vimSystemClipboard={settings.editorVimSystemClipboard}
                emacsSystemClipboard={settings.editorEmacsSystemClipboard}
                bufferKey={key}
              />
            )}
          </div>
        );
      })}
    </>
  );
}
