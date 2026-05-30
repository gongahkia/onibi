import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import {
  AGENT_KINDS,
  AGENT_LABELS,
  COLOR_SCHEME_OPTIONS,
  DEFAULT_AGENT_COMMANDS,
  type AppSettings,
  type Session,
  type Workspace,
  buildAgentHandoffPrompt,
  closeSession,
  duplicateSession,
  restartSession,
  restoreArrangement,
  sessionCanHandoff,
  sessionHasRestorableTerminal,
  sessionNeedsAttention,
  spawnAgentSession,
  useSessionStore,
} from "../lib/sessions";
import { chooseWorkspaceFolder } from "../lib/workspace-picker";
import { NewSessionDialog } from "./NewSessionDialog";
import { SettingsPane } from "./SettingsPane";

type CommandGroup = "Session" | "Workspace" | "Settings" | "Layout" | "View" | "Agent" | "Editor";

interface PaletteCommand {
  id: string;
  label: string;
  group: CommandGroup;
  description?: string;
  shortcut?: string;
  keywords?: string[];
  run: () => void | Promise<void>;
}

function isApplePlatform(): boolean {
  return /Mac|iPhone|iPad|iPod/.test(navigator.platform);
}

function primaryShortcut(key: string): string {
  return isApplePlatform() ? `⌘${key}` : `Ctrl ${key}`;
}

function commandText(command: PaletteCommand): string {
  return [
    command.label,
    command.group,
    command.description,
    command.shortcut,
    ...(command.keywords ?? []),
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function matchesQuery(command: PaletteCommand, query: string): boolean {
  const terms = query
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean);
  if (terms.length === 0) {
    return true;
  }
  const haystack = commandText(command);
  return terms.every((term) => haystack.includes(term));
}

function workspaceNameForSession(
  session: Session,
  workspaces: Workspace[],
): string {
  return (
    workspaces.find((workspace) => workspace.id === session.workspaceId)?.name ??
    "Workspace"
  );
}

function activeWorkspace(
  sessions: Session[],
  workspaces: Workspace[],
  activeSessionId: string | null,
): Workspace | null {
  const session = sessions.find((item) => item.id === activeSessionId);
  return workspaces.find((workspace) => workspace.id === session?.workspaceId) ?? null;
}

function setTabBarPosition(
  updateSettings: (patch: Partial<AppSettings>) => void,
  position: "left" | "right" | "top" | "bottom",
): void {
  updateSettings({
    tabBarOrientation: position === "top" || position === "bottom" ? "horizontal" : "vertical",
    tabBarPosition: position,
  });
}

function focusActiveTerminal(): void {
  const textarea = document.querySelector<HTMLTextAreaElement>(
    ".terminal-view .xterm-helper-textarea",
  );
  if (textarea) {
    textarea.focus();
    return;
  }
  document.querySelector<HTMLElement>(".terminal-view")?.focus();
}

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [newSessionOpen, setNewSessionOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const sessions = useSessionStore((state) => state.sessions);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const activeTerminalPaneId = useSessionStore((state) => state.activeTerminalPaneId);
  const workspaces = useSessionStore((state) => state.workspaces);
  const arrangements = useSessionStore((state) => state.arrangements);
  const selectedFile = useSessionStore((state) => state.selectedFile);
  const closedBufferStack = useSessionStore((state) => state.closedBufferStack);
  const reopenClosedBuffer = useSessionStore((state) => state.reopenClosedBuffer);
  const settings = useSessionStore((state) => state.settings);
  const sessionEvents = useSessionStore((state) => state.sessionEvents);
  const addWorkspace = useSessionStore((state) => state.addWorkspace);
  const saveCurrentArrangement = useSessionStore((state) => state.saveCurrentArrangement);
  const deleteArrangement = useSessionStore((state) => state.deleteArrangement);
  const clearSessionAttention = useSessionStore((state) => state.clearSessionAttention);
  const selectFile = useSessionStore((state) => state.selectFile);
  const setActiveSession = useSessionStore((state) => state.setActiveSession);
  const setActiveSidebarView = useSessionStore((state) => state.setActiveSidebarView);
  const focusRelativeAttentionSession = useSessionStore(
    (state) => state.focusRelativeAttentionSession,
  );
  const updateSettings = useSessionStore((state) => state.updateSettings);
  const toggleMaximizedTerminalPane = useSessionStore(
    (state) => state.toggleMaximizedTerminalPane,
  );
  const focusRelativeTerminalPane = useSessionStore(
    (state) => state.focusRelativeTerminalPane,
  );

  useEffect(() => {
    function handleGlobalKeyDown(event: KeyboardEvent) {
      if (
        event.key.toLowerCase() === "p" &&
        (event.metaKey || event.ctrlKey) &&
        !event.altKey &&
        !event.shiftKey
      ) {
        event.preventDefault();
        setOpen(true);
        return;
      }
      // Cmd/Ctrl + Shift + T → reopen closed tab (VSCode parity)
      if (
        event.key.toLowerCase() === "t" &&
        (event.metaKey || event.ctrlKey) &&
        event.shiftKey &&
        !event.altKey
      ) {
        event.preventDefault();
        reopenClosedBuffer();
      }
    }

    function handleOpenEvent() {
      setOpen(true);
    }

    window.addEventListener("keydown", handleGlobalKeyDown);
    window.addEventListener("onibi:open-command-palette", handleOpenEvent as EventListener);
    return () => {
      window.removeEventListener("keydown", handleGlobalKeyDown);
      window.removeEventListener(
        "onibi:open-command-palette",
        handleOpenEvent as EventListener,
      );
    };
  }, [reopenClosedBuffer]);

  useEffect(() => {
    if (!open) {
      return;
    }
    setQuery("");
    setSelectedIndex(0);
    requestAnimationFrame(() => inputRef.current?.focus());
  }, [open]);

  const commands = useMemo<PaletteCommand[]>(() => {
    const activeSession = sessions.find((session) => session.id === activeSessionId);
    const currentWorkspace = activeWorkspace(sessions, workspaces, activeSessionId);

    const nextCommands: PaletteCommand[] = [
      {
        id: "session.new",
        label: "New Session",
        group: "Session",
        description: "Choose an agent, workspace, and initial prompt",
        shortcut: primaryShortcut("N"),
        keywords: ["agent", "terminal", "spawn", "start"],
        run: () => setNewSessionOpen(true),
      },
      {
        id: "workspace.quick-launch",
        label: "Quick Workspace Launcher",
        group: "Workspace",
        description: "Choose an agent, workspace, prompt, or split",
        keywords: ["quick", "launcher", "agent", "worktree", "arrangement"],
        run: () => setNewSessionOpen(true),
      },
      {
        id: "settings.open",
        label: "Open Settings",
        group: "Settings",
        description: "Edit appearance, layout, agents, and workspaces",
        shortcut: primaryShortcut(","),
        keywords: ["preferences", "agents", "workspace"],
        run: () => setSettingsOpen(true),
      },
      {
        id: "editor.reopen-closed",
        label: "Reopen Closed Editor",
        group: "Editor",
        description:
          closedBufferStack.length > 0
            ? `Reopen ${closedBufferStack[0].path}`
            : "No recently closed editors",
        shortcut: `${primaryShortcut("⇧")}T`,
        keywords: ["restore", "undo", "close", "tab", "file"],
        run: () => {
          reopenClosedBuffer();
        },
      },
      {
        id: "workspace.open",
        label: "Add Workspace",
        group: "Workspace",
        description: "Choose a folder from this computer",
        shortcut: primaryShortcut("O"),
        keywords: ["folder", "project", "open"],
        run: async () => {
          const workspace = await chooseWorkspaceFolder();
          if (workspace) {
            addWorkspace(workspace);
          }
        },
      },
      {
        id: "workspace.search",
        label: "Open Workspace Search",
        group: "Workspace",
        description: currentWorkspace?.name ?? "No active workspace",
        keywords: ["find", "ripgrep", "text"],
        run: () => setActiveSidebarView("search"),
      },
      {
        id: "attention.next",
        label: "Focus Next Attention Item",
        group: "Session",
        description: `${sessions.filter((session) => sessionNeedsAttention(session)).length} sessions need attention`,
        keywords: ["approval", "failed", "trigger", "needs attention"],
        run: () => focusRelativeAttentionSession(1),
      },
      {
        id: "attention.previous",
        label: "Focus Previous Attention Item",
        group: "Session",
        description: "Move backward through sessions that need attention",
        keywords: ["approval", "failed", "trigger", "needs attention"],
        run: () => focusRelativeAttentionSession(-1),
      },
      {
        id: "arrangement.save",
        label: "Save Current Arrangement",
        group: "Layout",
        description: currentWorkspace?.name ?? "Current terminal layout",
        keywords: ["layout", "workspace", "restore"],
        run: () => {
          const name = window.prompt("Arrangement name");
          saveCurrentArrangement(name ?? undefined);
        },
      },
      {
        id: "mobile.open",
        label: "Open Mobile PWA",
        group: "View",
        description: "Open the paired-device browser surface",
        keywords: ["phone", "pwa", "approval", "qr"],
        run: () => {
          window.open("/m/", "_blank", "noopener,noreferrer");
        },
      },
      {
        id: "terminal.focus",
        label: "Focus Active Terminal",
        group: "View",
        description: activeSession
          ? `${activeSession.title} in ${currentWorkspace?.name ?? "workspace"}`
          : "No active terminal session",
        keywords: ["shell", "pty", "agent"],
        run: focusActiveTerminal,
      },
      {
        id: "terminal.maximize",
        label: "Maximize or Restore Active Pane",
        group: "Layout",
        description: "Toggle the active terminal pane",
        shortcut: primaryShortcut("⇧↵"),
        keywords: ["pane", "split", "zoom"],
        run: () => toggleMaximizedTerminalPane(),
      },
      {
        id: "terminal.focus-next-pane",
        label: "Focus Next Pane",
        group: "Layout",
        description: "Move focus through terminal splits",
        keywords: ["pane", "split", "next"],
        run: () => focusRelativeTerminalPane(1),
      },
      {
        id: "terminal.focus-previous-pane",
        label: "Focus Previous Pane",
        group: "Layout",
        description: "Move focus backward through terminal splits",
        keywords: ["pane", "split", "previous"],
        run: () => focusRelativeTerminalPane(-1),
      },
      {
        id: "file.clear-selection",
        label: "Clear File Selection",
        group: "Workspace",
        description: selectedFile ? selectedFile.path : "Return to the active terminal",
        keywords: ["editor", "terminal", "main pane"],
        run: () => selectFile(null),
      },
      {
        id: "settings.hidden-files",
        label: settings.showHiddenFiles ? "Hide Hidden Files" : "Show Hidden Files",
        group: "Settings",
        description: "Toggle dotfiles in the workspace tree",
        keywords: ["dotfiles", "files", "tree"],
        run: () => updateSettings({ showHiddenFiles: !settings.showHiddenFiles }),
      },
      {
        id: "settings.file-icons",
        label: settings.showFileIcons ? "Hide File Icons" : "Show File Icons",
        group: "Settings",
        description: "Toggle icons in the workspace tree",
        keywords: ["files", "tree", "sidebar", "icons"],
        run: () => updateSettings({ showFileIcons: !settings.showFileIcons }),
      },
      ...COLOR_SCHEME_OPTIONS.map<PaletteCommand>((option) => ({
        id: `settings.theme.${option.id}`,
        label: `Theme: ${option.label}`,
        group: "Settings",
        description: settings.theme === option.id ? "Current theme" : undefined,
        keywords: ["appearance", "color", "scheme"],
        run: () => updateSettings({ theme: option.id }),
      })),
      {
        id: "settings.font.increase",
        label: "Increase Terminal Font Size",
        group: "Settings",
        description: `${settings.terminalFontSize}px`,
        shortcut: primaryShortcut("+"),
        keywords: ["text", "terminal", "zoom"],
        run: () =>
          updateSettings({
            terminalFontSize: Math.min(settings.terminalFontSize + 1, 28),
          }),
      },
      {
        id: "settings.font.decrease",
        label: "Decrease Terminal Font Size",
        group: "Settings",
        description: `${settings.terminalFontSize}px`,
        shortcut: primaryShortcut("-"),
        keywords: ["text", "terminal", "zoom"],
        run: () =>
          updateSettings({
            terminalFontSize: Math.max(settings.terminalFontSize - 1, 10),
          }),
      },
      {
        id: "layout.left",
        label: "Move Tab Bar Left",
        group: "Layout",
        description: settings.tabBarPosition === "left" ? "Current layout" : undefined,
        keywords: ["sidebar", "vertical"],
        run: () => setTabBarPosition(updateSettings, "left"),
      },
      {
        id: "layout.right",
        label: "Move Tab Bar Right",
        group: "Layout",
        description: settings.tabBarPosition === "right" ? "Current layout" : undefined,
        keywords: ["sidebar", "vertical"],
        run: () => setTabBarPosition(updateSettings, "right"),
      },
      {
        id: "layout.top",
        label: "Move Tab Bar Top",
        group: "Layout",
        description: settings.tabBarPosition === "top" ? "Current layout" : undefined,
        keywords: ["tabs", "horizontal"],
        run: () => setTabBarPosition(updateSettings, "top"),
      },
      {
        id: "layout.bottom",
        label: "Move Tab Bar Bottom",
        group: "Layout",
        description: settings.tabBarPosition === "bottom" ? "Current layout" : undefined,
        keywords: ["tabs", "horizontal"],
        run: () => setTabBarPosition(updateSettings, "bottom"),
      },
      {
        id: "agents.reset-commands",
        label: "Reset Agent Commands",
        group: "Settings",
        description: "Restore default launch commands",
        keywords: ["claude", "codex", "gemini", "aider", "opencode"],
        run: () => updateSettings({ agentCommands: DEFAULT_AGENT_COMMANDS }),
      },
    ];

    for (const session of sessions) {
      nextCommands.push({
        id: `session.switch.${session.id}`,
        label: `Switch to ${session.title}`,
        group: "Session",
        description: `${AGENT_LABELS[session.agent]} · ${workspaceNameForSession(
          session,
          workspaces,
        )}`,
        shortcut: session.id === activeSessionId ? "Active" : undefined,
        keywords: ["tab", "terminal", "agent", session.agent],
        run: () => setActiveSession(session.id),
      });
    }

    for (const arrangement of arrangements) {
      nextCommands.push(
        {
          id: `arrangement.restore.${arrangement.id}`,
          label: `Restore Arrangement: ${arrangement.name}`,
          group: "Layout",
          description: `${arrangement.sessions.length} sessions`,
          keywords: ["layout", "arrangement", "restore"],
          run: async () => {
            await restoreArrangement(arrangement.id);
          },
        },
        {
          id: `arrangement.delete.${arrangement.id}`,
          label: `Delete Arrangement: ${arrangement.name}`,
          group: "Layout",
          description: "Remove saved arrangement",
          keywords: ["layout", "arrangement", "delete"],
          run: () => {
            if (window.confirm(`Delete ${arrangement.name}?`)) {
              deleteArrangement(arrangement.id);
            }
          },
        },
      );
    }

    if (activeSession) {
      const canUseTerminalActions = sessionHasRestorableTerminal(activeSession);
      nextCommands.push(
        {
          id: "session.copy-id",
          label: "Copy Active Session ID",
          group: "Session",
          description: activeSession.id,
          keywords: ["pty", "debug", "clipboard"],
          run: () => void navigator.clipboard?.writeText(activeSession.id),
        },
        {
          id: "session.clear-attention",
          label: "Clear Active Session Attention",
          group: "Session",
          description: activeSession.title,
          keywords: ["attention", "badge", "trigger", "dismiss"],
          run: () => clearSessionAttention(activeSession.id),
        },
        ...(activeSession.preview
          ? [
              {
                id: "session.open-preview",
                label: "Open Active Session Preview",
                group: "View" as const,
                description: activeSession.preview.url,
                keywords: ["preview", "port", "server", "localhost"],
                run: () =>
                  useSessionStore
                    .getState()
                    .openWebUrl(activeSession.preview!.url, activeSession.id),
              },
              {
                id: "session.copy-preview",
                label: "Copy Active Session Preview URL",
                group: "View" as const,
                description: activeSession.preview.url,
                keywords: ["preview", "port", "server", "localhost"],
                run: () => void navigator.clipboard?.writeText(activeSession.preview!.url),
              },
            ]
          : []),
        ...(canUseTerminalActions && activeSession.restart
          ? [
              {
                id: "session.restart-active",
                label: "Restart Active Session",
                group: "Session" as const,
                description: activeSession.title,
                keywords: ["reload", "respawn", "terminal"],
                run: async () => {
                  await restartSession(activeSession.id);
                },
              },
              {
                id: "session.duplicate-active",
                label: "Duplicate Active Session",
                group: "Session" as const,
                description: activeSession.cwd ?? activeSession.title,
                keywords: ["copy", "split", "terminal"],
                run: async () => {
                  await duplicateSession(
                    activeSession.id,
                    activeTerminalPaneId
                      ? {
                          type: "split",
                          targetPaneId: activeTerminalPaneId,
                          direction: "vertical",
                        }
                      : null,
                  );
                },
              },
            ]
          : []),
        {
          id: "session.close-active",
          label: "Close Active Session",
          group: "Session",
          description: activeSession.title,
          shortcut: primaryShortcut("W"),
          keywords: ["remove", "tab"],
          run: async () => {
            await closeSession(activeSession.id);
          },
        },
      );
      if (currentWorkspace && sessionCanHandoff(activeSession)) {
        for (const agent of AGENT_KINDS.filter(
          (candidate) => candidate !== "shell" && candidate !== activeSession.agent,
        )) {
          nextCommands.push({
            id: `agent.handoff.${agent}`,
            label: `Handoff to ${AGENT_LABELS[agent]}`,
            group: "Agent",
            description: currentWorkspace.name,
            keywords: ["handoff", "transfer", "context", agent],
            run: async () => {
              await spawnAgentSession(
                agent,
                currentWorkspace,
                buildAgentHandoffPrompt(
                  activeSession,
                  currentWorkspace,
                  selectedFile?.path ?? null,
                  sessionEvents,
                ),
              );
            },
          });
        }
      }
    }

    return nextCommands;
  }, [
    activeSessionId,
    activeTerminalPaneId,
    addWorkspace,
    arrangements,
    clearSessionAttention,
    deleteArrangement,
    focusRelativeAttentionSession,
    saveCurrentArrangement,
    selectFile,
    selectedFile,
    sessionEvents,
    sessions,
    setActiveSession,
    setActiveSidebarView,
    settings,
    toggleMaximizedTerminalPane,
    focusRelativeTerminalPane,
    updateSettings,
    workspaces,
  ]);

  const filteredCommands = useMemo(
    () => commands.filter((command) => matchesQuery(command, query)).slice(0, 14),
    [commands, query],
  );

  useEffect(() => {
    setSelectedIndex((index) =>
      filteredCommands.length === 0
        ? 0
        : Math.min(index, filteredCommands.length - 1),
    );
  }, [filteredCommands.length]);

  async function execute(command: PaletteCommand | undefined) {
    if (!command) {
      return;
    }
    setOpen(false);
    try {
      await command.run();
    } catch (error) {
      console.warn("command failed", error);
    }
  }

  function handleInputKeyDown(event: ReactKeyboardEvent<HTMLInputElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      setOpen(false);
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      setSelectedIndex((index) =>
        filteredCommands.length === 0 ? 0 : (index + 1) % filteredCommands.length,
      );
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setSelectedIndex((index) =>
        filteredCommands.length === 0
          ? 0
          : (index - 1 + filteredCommands.length) % filteredCommands.length,
      );
    } else if (event.key === "Home") {
      event.preventDefault();
      setSelectedIndex(0);
    } else if (event.key === "End") {
      event.preventDefault();
      setSelectedIndex(Math.max(filteredCommands.length - 1, 0));
    } else if (event.key === "Enter") {
      event.preventDefault();
      void execute(filteredCommands[selectedIndex]);
    }
  }

  return (
    <>
      {open ? (
        <div
          className="command-palette-backdrop"
          role="presentation"
          onMouseDown={(event) => {
            if (event.target === event.currentTarget) {
              setOpen(false);
            }
          }}
        >
          <section
            className="command-palette"
            role="dialog"
            aria-modal="true"
            aria-label="Command palette"
          >
            <div className="command-palette-input-wrap">
              <input
                ref={inputRef}
                className="command-palette-input"
                value={query}
                placeholder="Execute a command..."
                aria-label="Search commands"
                aria-activedescendant={
                  filteredCommands[selectedIndex]
                    ? `command-${filteredCommands[selectedIndex].id}`
                    : undefined
                }
                onChange={(event) => {
                  setQuery(event.target.value);
                  setSelectedIndex(0);
                }}
                onKeyDown={handleInputKeyDown}
              />
              <kbd className="command-palette-trigger">{primaryShortcut("P")}</kbd>
            </div>
            <div className="command-list" role="listbox" aria-label="Commands">
              {filteredCommands.length === 0 ? (
                <div className="command-empty">No commands</div>
              ) : (
                filteredCommands.map((command, index) => (
                  <button
                    id={`command-${command.id}`}
                    key={command.id}
                    type="button"
                    role="option"
                    aria-selected={index === selectedIndex}
                    className={`command-row ${index === selectedIndex ? "selected" : ""}`}
                    onMouseEnter={() => setSelectedIndex(index)}
                    onClick={() => void execute(command)}
                  >
                    <span className="command-row-main">
                      <span className="command-label">{command.label}</span>
                      {command.description ? (
                        <span className="command-description">
                          {command.description}
                        </span>
                      ) : null}
                    </span>
                    <span className="command-meta">
                      <span className="command-group">{command.group}</span>
                      {command.shortcut ? (
                        <kbd className="command-shortcut">{command.shortcut}</kbd>
                      ) : null}
                    </span>
                  </button>
                ))
              )}
            </div>
          </section>
        </div>
      ) : null}
      <NewSessionDialog
        open={newSessionOpen}
        onClose={() => setNewSessionOpen(false)}
      />
      <SettingsPane open={settingsOpen} onClose={() => setSettingsOpen(false)} />
    </>
  );
}
