import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
} from "react";
import {
  AGENT_KINDS,
  APP_KEYBINDING_ACTION_LABELS,
  COLOR_SCHEME_OPTIONS,
  DEFAULT_AGENT_COMMANDS,
  agentDisplayLabel,
  type AppSettings,
  type AppKeybindingAction,
  type CustomCommandKeybinding,
  type Session,
  type Workspace,
  buildAgentHandoffPrompt,
  closeSession,
  displayKeyChord,
  duplicateSession,
  keyChordFromKeyboardEvent,
  launchAgentInWorkspacePath,
  normalizeAppKeyChord,
  openWorkspacePath,
  restartSession,
  restoreArrangement,
  sessionCanHandoff,
  sessionHasRestorableTerminal,
  sessionNeedsAttention,
  spawnAgentSession,
  useSessionStore,
} from "../lib/sessions";
import { listGitWorktrees, type GitWorktree } from "../lib/git";
import { ptyWrite } from "../lib/tauri-bridge";
import {
  copyTerminalRenderProfile,
  terminalRenderProfilingEnabled,
} from "../lib/terminal-render-profile";
import { chooseWorkspaceFolder } from "../lib/workspace-picker";
import { NewSessionDialog } from "./NewSessionDialog";
import { SettingsPane } from "./SettingsPane";

type CommandGroup =
  | "Session"
  | "Workspace"
  | "Settings"
  | "Layout"
  | "View"
  | "Agent"
  | "Editor"
  | "Terminal";

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

function appShortcut(
  settings: AppSettings,
  action: AppKeybindingAction,
  fallback?: string,
): string | undefined {
  const binding = settings.appKeybindings.find((item) => item.action === action);
  return binding ? displayKeyChord(binding.keys) : fallback;
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

async function copyActiveTerminalRenderProfile(ptyId?: string | null): Promise<void> {
  const report = await copyTerminalRenderProfile(ptyId);
  if (!report) {
    await navigator.clipboard?.writeText(
      'No terminal render profile is available. Enable it with localStorage.onibiTerminalDebug = "1" and run this command again.',
    );
  }
}

function indexedActionValue(
  action: AppKeybindingAction,
  prefix: "workspace.focusIndex" | "workspace.tab.focusIndex" | "terminal.focusPaneIndex",
): number | null {
  const raw = action.startsWith(prefix) ? action.slice(prefix.length) : "";
  const index = Number(raw);
  return Number.isInteger(index) && index >= 1 && index <= 9 ? index : null;
}

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [newSessionOpen, setNewSessionOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [workspaceNavigatorOpen, setWorkspaceNavigatorOpen] = useState(false);
  const [sessionNavigatorOpen, setSessionNavigatorOpen] = useState(false);
  const [keybindHelpOpen, setKeybindHelpOpen] = useState(false);
  const [activeWorktrees, setActiveWorktrees] = useState<GitWorktree[]>([]);
  const inputRef = useRef<HTMLInputElement>(null);

  const sessions = useSessionStore((state) => state.sessions);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const activeTerminalPaneId = useSessionStore((state) => state.activeTerminalPaneId);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeWorkspaceId = useSessionStore((state) => state.activeWorkspaceId);
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
  const setActiveWorkspace = useSessionStore((state) => state.setActiveWorkspace);
  const setActiveSidebarView = useSessionStore((state) => state.setActiveSidebarView);
  const focusRelativeAttentionSession = useSessionStore(
    (state) => state.focusRelativeAttentionSession,
  );
  const updateSettings = useSessionStore((state) => state.updateSettings);
  const toggleMaximizedTerminalPane = useSessionStore(
    (state) => state.toggleMaximizedTerminalPane,
  );
  const applyTerminalLayoutPreset = useSessionStore(
    (state) => state.applyTerminalLayoutPreset,
  );
  const focusRelativeTerminalPane = useSessionStore(
    (state) => state.focusRelativeTerminalPane,
  );
  const focusTerminalPaneIndex = useSessionStore(
    (state) => state.focusTerminalPaneIndex,
  );
  const focusRelativeWorkspace = useSessionStore(
    (state) => state.focusRelativeWorkspace,
  );
  const focusWorkspaceIndex = useSessionStore((state) => state.focusWorkspaceIndex);
  const focusRelativeWorkspaceTab = useSessionStore(
    (state) => state.focusRelativeWorkspaceTab,
  );
  const focusWorkspaceTabIndex = useSessionStore(
    (state) => state.focusWorkspaceTabIndex,
  );

  const paletteWorkspace =
    activeWorkspace(sessions, workspaces, activeSessionId) ??
    workspaces.find((workspace) => workspace.id === activeWorkspaceId) ??
    null;

  const prefixArmedRef = useRef(false);
  const prefixTimerRef = useRef<number | null>(null);
  const sendCustomCommand = useCallback((binding: CustomCommandKeybinding) => {
    const command = binding.command.trim();
    const sessionId = useSessionStore.getState().activeSessionId;
    if (!command || !sessionId) {
      return;
    }
    void ptyWrite(sessionId, new TextEncoder().encode(`${command}\r`)).catch((error) => {
      console.warn("custom command keybinding failed", error);
    });
    focusActiveTerminal();
  }, []);

  useEffect(() => {
    function handleGlobalKeyDown(event: KeyboardEvent) {
      if (event.defaultPrevented) {
        return;
      }
      const chord = keyChordFromKeyboardEvent(event);
      if (!chord) {
        return;
      }
      const target = event.target;
      const editable =
        target instanceof Element &&
        (target.closest("input, textarea, select") ||
          target.closest('[contenteditable="true"]'));
      const terminalTarget =
        target instanceof Element && Boolean(target.closest(".terminal-view"));
      const bindings = new Map(
        settings.appKeybindings.map((binding) => [binding.keys, binding.action]),
      );
      const customCommandBindings = new Map(
        settings.customCommandKeybindings.map((binding) => [binding.keys, binding]),
      );
      const prefix = normalizeAppKeyChord(settings.keybindingPrefix);

      function clearPrefix() {
        prefixArmedRef.current = false;
        if (prefixTimerRef.current !== null) {
          window.clearTimeout(prefixTimerRef.current);
          prefixTimerRef.current = null;
        }
      }

      function executeAction(action: AppKeybindingAction) {
        if (action === "commandPalette.open") {
          setOpen(true);
        } else if (action === "session.new") {
          setNewSessionOpen(true);
        } else if (action === "session.navigator.open") {
          setSessionNavigatorOpen(true);
        } else if (action === "workspace.navigator.open") {
          setWorkspaceNavigatorOpen(true);
        } else if (action === "keybindings.help.open") {
          setKeybindHelpOpen(true);
        } else if (action === "editor.reopenClosed") {
          reopenClosedBuffer();
        } else if (action === "terminal.splitRight") {
          window.dispatchEvent(
            new CustomEvent("onibi:terminal-split", {
              detail: { direction: "vertical" },
            }),
          );
        } else if (action === "terminal.splitDown") {
          window.dispatchEvent(
            new CustomEvent("onibi:terminal-split", {
              detail: { direction: "horizontal" },
            }),
          );
        } else if (action === "terminal.focusNextPane") {
          focusRelativeTerminalPane(1);
        } else if (action === "terminal.focusPreviousPane") {
          focusRelativeTerminalPane(-1);
        } else if (action === "terminal.toggleMaximize") {
          toggleMaximizedTerminalPane();
        } else if (action === "terminal.copyMode.enter") {
          const id = useSessionStore.getState().activeSessionId;
          if (id) {
            window.dispatchEvent(
              new CustomEvent("onibi:terminal-copy-mode", {
                detail: { ptyId: id },
              }),
            );
            focusActiveTerminal();
          }
        } else if (action === "session.closeActive") {
          const id = useSessionStore.getState().activeSessionId;
          if (id) {
            void closeSession(id).catch((error) => {
              console.warn("command failed", error);
            });
          }
        } else if (action === "workspace.next") {
          focusRelativeWorkspace(1);
        } else if (action === "workspace.previous") {
          focusRelativeWorkspace(-1);
        } else if (action === "workspace.tab.next") {
          focusRelativeWorkspaceTab(1);
        } else if (action === "workspace.tab.previous") {
          focusRelativeWorkspaceTab(-1);
        } else if (indexedActionValue(action, "workspace.focusIndex")) {
          focusWorkspaceIndex(indexedActionValue(action, "workspace.focusIndex")!);
        } else if (indexedActionValue(action, "workspace.tab.focusIndex")) {
          focusWorkspaceTabIndex(indexedActionValue(action, "workspace.tab.focusIndex")!);
        } else if (indexedActionValue(action, "terminal.focusPaneIndex")) {
          focusTerminalPaneIndex(indexedActionValue(action, "terminal.focusPaneIndex")!);
        } else {
          console.warn("unknown app keybinding action", action);
        }
      }

      if (prefixArmedRef.current) {
        const key = `prefix+${chord}`;
        const action = bindings.get(key);
        const command = customCommandBindings.get(key);
        clearPrefix();
        if (action) {
          event.preventDefault();
          executeAction(action);
        } else if (command) {
          event.preventDefault();
          sendCustomCommand(command);
        }
        return;
      }

      if (prefix && chord === prefix && (!editable || terminalTarget)) {
        event.preventDefault();
        prefixArmedRef.current = true;
        prefixTimerRef.current = window.setTimeout(clearPrefix, 1800);
        return;
      }

      const action = bindings.get(chord);
      const command = customCommandBindings.get(chord);
      if (
        (!action && !command) ||
        (editable &&
          !terminalTarget &&
          (!action || action !== "commandPalette.open"))
      ) {
        return;
      }
      event.preventDefault();
      if (action) {
        executeAction(action);
      } else if (command) {
        sendCustomCommand(command);
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
      if (prefixTimerRef.current !== null) {
        window.clearTimeout(prefixTimerRef.current);
        prefixTimerRef.current = null;
      }
    };
  }, [
    focusRelativeTerminalPane,
    focusRelativeWorkspace,
    focusRelativeWorkspaceTab,
    focusTerminalPaneIndex,
    focusWorkspaceIndex,
    focusWorkspaceTabIndex,
    reopenClosedBuffer,
    settings.appKeybindings,
    settings.customCommandKeybindings,
    settings.keybindingPrefix,
    sendCustomCommand,
    toggleMaximizedTerminalPane,
  ]);

  useEffect(() => {
    if (!open) {
      return;
    }
    setQuery("");
    setSelectedIndex(0);
    requestAnimationFrame(() => inputRef.current?.focus());
  }, [open]);

  useEffect(() => {
    if (!open || !paletteWorkspace) {
      setActiveWorktrees([]);
      return;
    }
    let cancelled = false;
    void listGitWorktrees(paletteWorkspace.path)
      .then((worktrees) => {
        if (!cancelled) {
          setActiveWorktrees(worktrees);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setActiveWorktrees([]);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [open, paletteWorkspace?.path]);

  const commands = useMemo<PaletteCommand[]>(() => {
    const activeSession = sessions.find((session) => session.id === activeSessionId);
    const currentWorkspace = paletteWorkspace;

    const nextCommands: PaletteCommand[] = [
      {
        id: "session.new",
        label: "New Session",
        group: "Session",
        description: "Choose an agent, workspace, and initial prompt",
        shortcut: appShortcut(settings, "session.new", primaryShortcut("N")),
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
        id: "workspace.navigator",
        label: "Open Workspace Navigator",
        group: "Workspace",
        description: `${workspaces.length} workspace${workspaces.length === 1 ? "" : "s"}`,
        shortcut: appShortcut(settings, "workspace.navigator.open", "Prefix+W"),
        keywords: ["switcher", "project", "workspace"],
        run: () => setWorkspaceNavigatorOpen(true),
      },
      {
        id: "session.navigator",
        label: "Open Session Navigator",
        group: "Session",
        description: `${sessions.length} session${sessions.length === 1 ? "" : "s"}`,
        shortcut: appShortcut(settings, "session.navigator.open", "Prefix+G"),
        keywords: ["switcher", "terminal", "agent"],
        run: () => setSessionNavigatorOpen(true),
      },
      {
        id: "keybindings.help",
        label: "Open Keybinding Help",
        group: "Settings",
        description: "Show configured app, terminal, and custom command bindings",
        shortcut: appShortcut(settings, "keybindings.help.open", "Prefix+?"),
        keywords: ["shortcuts", "keyboard", "keys"],
        run: () => setKeybindHelpOpen(true),
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
        shortcut: appShortcut(settings, "editor.reopenClosed", `${primaryShortcut("⇧")}T`),
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
        id: "terminal.copy-render-profile",
        label: "Copy Terminal Render Profile",
        group: "Terminal",
        description: terminalRenderProfilingEnabled()
          ? activeSession?.title ?? "Latest captured terminal profile"
          : "Requires localStorage.onibiTerminalDebug = \"1\"",
        keywords: ["render", "profile", "performance", "debug", "xterm"],
        run: async () => {
          await copyActiveTerminalRenderProfile(activeSession?.id ?? null);
        },
      },
      {
        id: "terminal.maximize",
        label: "Maximize or Restore Active Pane",
        group: "Layout",
        description: "Toggle the active terminal pane",
        shortcut: appShortcut(settings, "terminal.toggleMaximize", primaryShortcut("⇧↵")),
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
        id: "terminal.layout.even-horizontal",
        label: "Layout Panes Evenly Horizontal",
        group: "Layout",
        description: "Arrange panes side by side",
        keywords: ["pane", "split", "preset", "columns"],
        run: () => applyTerminalLayoutPreset("even-horizontal"),
      },
      {
        id: "terminal.layout.even-vertical",
        label: "Layout Panes Evenly Vertical",
        group: "Layout",
        description: "Arrange panes top to bottom",
        keywords: ["pane", "split", "preset", "rows"],
        run: () => applyTerminalLayoutPreset("even-vertical"),
      },
      {
        id: "terminal.layout.main-horizontal",
        label: "Layout Panes Main Horizontal",
        group: "Layout",
        description: "Keep the first pane as the left main pane",
        keywords: ["pane", "split", "preset", "main", "columns"],
        run: () => applyTerminalLayoutPreset("main-horizontal"),
      },
      {
        id: "terminal.layout.main-vertical",
        label: "Layout Panes Main Vertical",
        group: "Layout",
        description: "Keep the first pane as the top main pane",
        keywords: ["pane", "split", "preset", "main", "rows"],
        run: () => applyTerminalLayoutPreset("main-vertical"),
      },
      {
        id: "terminal.layout.tiled",
        label: "Layout Panes Tiled",
        group: "Layout",
        description: "Balance panes into a grid",
        keywords: ["pane", "split", "preset", "grid"],
        run: () => applyTerminalLayoutPreset("tiled"),
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

    settings.customCommandKeybindings.forEach((binding, index) => {
      nextCommands.push({
        id: `custom-command.${index}`,
        label: binding.description || binding.command,
        group: "Terminal",
        description: binding.description ? binding.command : undefined,
        shortcut: displayKeyChord(binding.keys),
        keywords: ["custom", "command", "keybinding", "shell"],
        run: () => sendCustomCommand(binding),
      });
    });

    for (const worktree of activeWorktrees) {
      const label =
        worktree.branch ??
        (worktree.detached
          ? "detached"
          : worktree.path.split("/").pop() ?? "worktree");
      nextCommands.push({
        id: `worktree.open.${worktree.path}`,
        label: `Open Worktree: ${label}`,
        group: "Workspace",
        description:
          worktree.path === currentWorkspace?.path ? "Current workspace" : worktree.path,
        keywords: ["git", "worktree", "open", label, worktree.path],
        run: async () => {
          await openWorkspacePath(worktree.path);
        },
      });
      nextCommands.push({
        id: `worktree.launch.${worktree.path}`,
        label: `Launch ${agentDisplayLabel(settings.defaultAgent, settings)} in ${label}`,
        group: "Agent",
        description: worktree.path,
        keywords: ["git", "worktree", "agent", "launch", settings.defaultAgent],
        run: async () => {
          await launchAgentInWorkspacePath(settings.defaultAgent, worktree.path);
        },
      });
    }

    for (const session of sessions) {
      nextCommands.push({
        id: `session.switch.${session.id}`,
        label: `Switch to ${session.title}`,
        group: "Session",
        description: `${agentDisplayLabel(session.agent, settings)} · ${workspaceNameForSession(
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
          shortcut: appShortcut(settings, "session.closeActive", primaryShortcut("W")),
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
            label: `Handoff to ${agentDisplayLabel(agent, settings)}`,
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
                activeTerminalPaneId
                  ? {
                      type: "tab",
                      targetPaneId: activeTerminalPaneId,
                    }
                  : null,
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
    activeWorktrees,
    addWorkspace,
    arrangements,
    clearSessionAttention,
    deleteArrangement,
    focusRelativeAttentionSession,
    paletteWorkspace,
    saveCurrentArrangement,
    selectFile,
    selectedFile,
    sessionEvents,
    sessions,
    setActiveSession,
    setActiveSidebarView,
    settings,
    sendCustomCommand,
    applyTerminalLayoutPreset,
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
              <kbd className="command-palette-trigger">
                {appShortcut(settings, "commandPalette.open", primaryShortcut("P"))}
              </kbd>
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
      {workspaceNavigatorOpen ? (
        <WorkspaceNavigatorModal
          workspaces={workspaces}
          sessions={sessions}
          activeWorkspaceId={activeWorkspaceId}
          onClose={() => setWorkspaceNavigatorOpen(false)}
          onFocus={(workspaceId) => {
            setActiveWorkspace(workspaceId);
            setWorkspaceNavigatorOpen(false);
            focusActiveTerminal();
          }}
        />
      ) : null}
      {sessionNavigatorOpen ? (
        <SessionNavigatorModal
          sessions={sessions}
          workspaces={workspaces}
          activeSessionId={activeSessionId}
          onClose={() => setSessionNavigatorOpen(false)}
          onFocus={(sessionId) => {
            setActiveSession(sessionId);
            setSessionNavigatorOpen(false);
            focusActiveTerminal();
          }}
        />
      ) : null}
      {keybindHelpOpen ? (
        <KeybindingHelpModal
          settings={settings}
          onClose={() => setKeybindHelpOpen(false)}
        />
      ) : null}
    </>
  );
}

function ModalShell({
  label,
  title,
  children,
  onClose,
}: {
  label: string;
  title: string;
  children: ReactNode;
  onClose: () => void;
}) {
  return (
    <div
      className="modal-backdrop navigator-modal"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <section
        className="modal-panel"
        role="dialog"
        aria-modal="true"
        aria-label={label}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            event.preventDefault();
            onClose();
          }
        }}
      >
        <div className="modal-header">
          <h2 className="modal-title">{title}</h2>
          <button
            type="button"
            className="icon-button"
            aria-label={`Close ${label}`}
            onClick={onClose}
          >
            <i className="codicon codicon-close" aria-hidden="true" />
          </button>
        </div>
        {children}
      </section>
    </div>
  );
}

function WorkspaceNavigatorModal({
  workspaces,
  sessions,
  activeWorkspaceId,
  onClose,
  onFocus,
}: {
  workspaces: Workspace[];
  sessions: Session[];
  activeWorkspaceId: string | null;
  onClose: () => void;
  onFocus: (workspaceId: string) => void;
}) {
  return (
    <ModalShell label="Workspace navigator" title="Workspace Navigator" onClose={onClose}>
      <div className="modal-body navigator-list">
        {workspaces.length === 0 ? (
          <div className="navigator-empty">No workspaces</div>
        ) : (
          workspaces.map((workspace) => {
            const workspaceSessions = sessions.filter(
              (session) => session.workspaceId === workspace.id,
            );
            const attentionCount = workspaceSessions.filter(sessionNeedsAttention).length;
            const active = workspace.id === activeWorkspaceId;
            return (
              <button
                key={workspace.id}
                type="button"
                className={`navigator-row ${active ? "active" : ""}`}
                onClick={() => onFocus(workspace.id)}
              >
                <span className="navigator-row-main">
                  <strong>{workspace.name}</strong>
                  <span>{workspace.path}</span>
                </span>
                <span className="navigator-row-meta">
                  {active ? <span className="navigator-pill">active</span> : null}
                  <span>{workspaceSessions.length} sessions</span>
                  {attentionCount > 0 ? (
                    <span className="navigator-pill attention">{attentionCount}</span>
                  ) : null}
                </span>
              </button>
            );
          })
        )}
      </div>
    </ModalShell>
  );
}

function sessionStatusLabel(session: Session): string {
  if (session.status === "awaiting-approval") {
    return "needs approval";
  }
  return session.status.replace(/-/g, " ");
}

function SessionNavigatorModal({
  sessions,
  workspaces,
  activeSessionId,
  onClose,
  onFocus,
}: {
  sessions: Session[];
  workspaces: Workspace[];
  activeSessionId: string | null;
  onClose: () => void;
  onFocus: (sessionId: string) => void;
}) {
  const agentLabelOverrides = useSessionStore(
    (state) => state.settings.agentLabelOverrides,
  );
  return (
    <ModalShell label="Session navigator" title="Session Navigator" onClose={onClose}>
      <div className="modal-body navigator-list">
        {sessions.length === 0 ? (
          <div className="navigator-empty">No sessions</div>
        ) : (
          sessions.map((session) => {
            const workspace =
              workspaces.find((item) => item.id === session.workspaceId)?.name ??
              "Workspace";
            const active = session.id === activeSessionId;
            const agentLabel = agentDisplayLabel(session.agent, agentLabelOverrides);
            return (
              <button
                key={session.id}
                type="button"
                className={`navigator-row ${active ? "active" : ""}`}
                onClick={() => onFocus(session.id)}
              >
                <span className="navigator-row-main">
                  <strong>{session.title || agentLabel}</strong>
                  <span>
                    {agentLabel} · {workspace}
                  </span>
                </span>
                <span className="navigator-row-meta">
                  {active ? <span className="navigator-pill">active</span> : null}
                  <span>{sessionStatusLabel(session)}</span>
                  {sessionNeedsAttention(session) ? (
                    <span className="navigator-pill attention">attention</span>
                  ) : null}
                </span>
              </button>
            );
          })
        )}
      </div>
    </ModalShell>
  );
}

function KeybindingHelpModal({
  settings,
  onClose,
}: {
  settings: AppSettings;
  onClose: () => void;
}) {
  const appBindings = settings.appKeybindings.filter((binding) =>
    normalizeAppKeyChord(binding.keys),
  );
  const terminalBindings = settings.terminalKeybindings.filter((binding) =>
    normalizeAppKeyChord(binding.keys),
  );
  const commandBindings = settings.customCommandKeybindings.filter(
    (binding) => normalizeAppKeyChord(binding.keys) && binding.command.trim(),
  );

  return (
    <ModalShell label="Keybinding help" title="Keybinding Help" onClose={onClose}>
      <div className="modal-body keybinding-help">
        <KeybindingHelpSection
          title="App"
          rows={appBindings.map((binding) => ({
            id: `${binding.action}:${binding.keys}`,
            keys: binding.keys,
            label: APP_KEYBINDING_ACTION_LABELS[binding.action],
          }))}
        />
        <KeybindingHelpSection
          title="Terminal"
          rows={terminalBindings.map((binding) => ({
            id: `${binding.action}:${binding.keys}`,
            keys: binding.keys,
            label: binding.action,
          }))}
          empty="No terminal keybindings configured."
        />
        <KeybindingHelpSection
          title="Custom Commands"
          rows={commandBindings.map((binding) => ({
            id: `${binding.command}:${binding.keys}`,
            keys: binding.keys,
            label: binding.description || binding.command,
          }))}
          empty="No custom command keybindings configured."
        />
      </div>
    </ModalShell>
  );
}

function KeybindingHelpSection({
  title,
  rows,
  empty = "No keybindings configured.",
}: {
  title: string;
  rows: Array<{ id: string; keys: string; label: string }>;
  empty?: string;
}) {
  return (
    <section className="keybinding-help-section" aria-label={`${title} keybindings`}>
      <h3>{title}</h3>
      {rows.length === 0 ? (
        <div className="settings-note">{empty}</div>
      ) : (
        <div className="keybinding-help-list">
          {rows.map((row) => (
            <div className="keybinding-help-row" key={row.id}>
              <span>{row.label}</span>
              <kbd>{displayKeyChord(row.keys)}</kbd>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
