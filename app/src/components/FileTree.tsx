import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type CSSProperties,
  type DragEvent,
  type MouseEvent,
  type ReactNode,
} from "react";
import { revealItemInDir } from "@tauri-apps/plugin-opener";
import {
  createWorkspaceDir,
  createWorkspaceFile,
  createWorkspaceFileWithContents,
  deleteWorkspacePath,
  listWorkspaceDir,
  moveWorkspacePath,
  readWorkspaceFile,
  renameWorkspacePath,
  selectedFileFromEntry,
  type FsEntry,
  type Workspace,
  useSessionStore,
} from "../lib/sessions";
import type { AgentReviewRecord } from "../lib/agent-review";
import type { GitTreeState } from "../lib/git";
import { chooseWorkspaceFolder } from "../lib/workspace-picker";

type ChildrenByPath = Record<string, FsEntry[]>;
type ErrorByPath = Record<string, string>;
const FILE_TREE_DRAG_MIME = "application/x-onibi-file-tree-entry";

interface ContextTarget {
  workspace: Workspace;
  entry: FsEntry;
  isWorkspaceRoot: boolean;
}

interface ContextMenuState {
  x: number;
  y: number;
  target: ContextTarget;
}

interface DragPayload {
  workspaceId: string;
  path: string;
  kind: FsEntry["kind"];
  name: string;
}

function nodeKey(workspace: Workspace, path: string): string {
  return `${workspace.id}:${path}`;
}

function isHidden(entry: FsEntry): boolean {
  return entry.name.startsWith(".");
}

function rootEntry(workspace: Workspace): FsEntry {
  return {
    name: workspace.name,
    path: workspace.path,
    kind: "dir",
    size: 0,
  };
}

function parentPath(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  const index = trimmed.lastIndexOf("/");
  return index <= 0 ? "/" : trimmed.slice(0, index);
}

function relativeWorkspacePath(workspace: Workspace, path: string): string {
  return path === workspace.path
    ? "."
    : path.replace(`${workspace.path.replace(/\/+$/, "")}/`, "");
}

function envTargetName(name: string): string | null {
  if (name === ".env") {
    return ".env.example";
  }
  if (name === ".env.example") {
    return ".env";
  }
  return null;
}

function redactEnvContents(contents: string): string {
  return contents.replace(
    /^(\s*(?:export\s+)?[A-Za-z_][A-Za-z0-9_]*\s*=\s*).*$/gm,
    "$1",
  );
}

function isDescendantPath(parent: string, path: string): boolean {
  const normalizedParent = parent.replace(/\/+$/, "");
  return path === normalizedParent || path.startsWith(`${normalizedParent}/`);
}

interface TreeIcon {
  shape:
    | "braces"
    | "code"
    | "config"
    | "docker"
    | "file"
    | "folder"
    | "git"
    | "image"
    | "lock"
    | "markdown"
    | "package"
    | "style"
    | "text";
  glyph?: string;
  tone: string;
  title: string;
}

const SPECIAL_FILE_ICONS: Record<string, TreeIcon> = {
  ".env": { shape: "config", tone: "config", title: "Environment file" },
  ".gitignore": { shape: "git", tone: "git", title: "Git ignore file" },
  "agents.md": { shape: "markdown", glyph: "A", tone: "agent", title: "Markdown file" },
  "claude.md": { shape: "markdown", glyph: "C", tone: "agent", title: "Markdown file" },
  makefile: { shape: "code", glyph: "M", tone: "make", title: "Makefile" },
  "package-lock.json": { shape: "lock", tone: "lock", title: "Lockfile" },
  "package.json": { shape: "package", glyph: "npm", tone: "package", title: "Package manifest" },
  "pnpm-lock.yaml": { shape: "lock", tone: "lock", title: "Lockfile" },
  "pnpm-workspace.yaml": { shape: "package", glyph: "pn", tone: "package", title: "Package workspace" },
  "readme.md": { shape: "markdown", glyph: "i", tone: "docs", title: "Markdown file" },
  "tsconfig.json": { shape: "braces", glyph: "TS", tone: "data", title: "TypeScript config" },
};

const EXTENSION_FILE_ICONS: Record<string, TreeIcon> = {
  css: { shape: "style", glyph: "#", tone: "style", title: "CSS file" },
  gif: { shape: "image", tone: "image", title: "Image file" },
  html: { shape: "code", glyph: "<>", tone: "html", title: "HTML file" },
  ico: { shape: "image", tone: "image", title: "Image file" },
  jpeg: { shape: "image", tone: "image", title: "Image file" },
  jpg: { shape: "image", tone: "image", title: "Image file" },
  js: { shape: "code", glyph: "JS", tone: "javascript", title: "JavaScript file" },
  json: { shape: "braces", glyph: "{}", tone: "data", title: "JSON file" },
  jsx: { shape: "code", glyph: "JSX", tone: "javascript", title: "JavaScript React file" },
  lock: { shape: "lock", tone: "lock", title: "Lockfile" },
  md: { shape: "markdown", glyph: "MD", tone: "docs", title: "Markdown file" },
  mjs: { shape: "code", glyph: "JS", tone: "javascript", title: "JavaScript file" },
  png: { shape: "image", tone: "image", title: "Image file" },
  py: { shape: "code", glyph: "Py", tone: "python", title: "Python file" },
  rs: { shape: "code", glyph: "RS", tone: "rust", title: "Rust file" },
  svg: { shape: "image", tone: "image", title: "SVG file" },
  toml: { shape: "config", tone: "config", title: "TOML file" },
  ts: { shape: "code", glyph: "TS", tone: "typescript", title: "TypeScript file" },
  tsx: { shape: "code", glyph: "TSX", tone: "typescript", title: "TypeScript React file" },
  txt: { shape: "text", tone: "text", title: "Text file" },
  yaml: { shape: "config", glyph: "!", tone: "yaml", title: "YAML file" },
  yml: { shape: "config", glyph: "!", tone: "yaml", title: "YAML file" },
};

function iconForEntry(entry: FsEntry): TreeIcon {
  if (entry.kind === "dir") {
    return { shape: "folder", tone: "folder", title: "Folder" };
  }

  const lowerName = entry.name.toLowerCase();
  if (
    lowerName === "dockerfile" ||
    lowerName.startsWith("dockerfile.") ||
    lowerName === "docker-compose.yml" ||
    lowerName === "docker-compose.yaml"
  ) {
    return { shape: "docker", tone: "docker", title: "Docker file" };
  }

  const specialIcon = SPECIAL_FILE_ICONS[lowerName];
  if (specialIcon) {
    return specialIcon;
  }

  const extension = entry.name.includes(".")
    ? entry.name.split(".").pop()?.toLowerCase()
    : "";
  return (
    (extension ? EXTENSION_FILE_ICONS[extension] : undefined) ?? {
      shape: "file",
      tone: "file",
      title: "File",
    }
  );
}

interface FileTreeProps {
  gitStatusByPath?: Record<string, GitTreeState>;
  agentReviewsByPath?: Record<string, AgentReviewRecord>;
}

function TreeFileIcon({ entry }: { entry: FsEntry }) {
  const icon = iconForEntry(entry);
  return (
    <span
      className={`tree-file-icon tree-file-icon--${icon.shape} tone-${icon.tone}`}
      data-glyph={icon.glyph ?? ""}
      title={icon.title}
      aria-hidden="true"
    />
  );
}

function gitStateForPath(
  gitStatusByPath: Record<string, GitTreeState> | undefined,
  path: string,
  isDir: boolean,
): GitTreeState | null {
  if (!gitStatusByPath) {
    return null;
  }
  const exactState = gitStatusByPath[path];
  if (exactState || !isDir) {
    return exactState ?? null;
  }
  const prefix = `${path.replace(/\/+$/, "")}/`;
  return Object.keys(gitStatusByPath).some((changedPath) => changedPath.startsWith(prefix))
    ? { badge: "•", label: "Contains git changes", tone: "modified" }
    : null;
}

function GitStateBadge({ state }: { state: GitTreeState | null }) {
  if (!state) {
    return null;
  }
  return (
    <span
      className={`tree-git-state tone-${state.tone}`}
      title={state.label}
      aria-label={state.label}
    >
      {state.badge}
    </span>
  );
}

export function FileTree({ gitStatusByPath, agentReviewsByPath }: FileTreeProps = {}) {
  const workspaces = useSessionStore((state) => state.workspaces);
  const sessions = useSessionStore((state) => state.sessions);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const selectedFile = useSessionStore((state) => state.selectedFile);
  const selectFile = useSessionStore((state) => state.selectFile);
  const addWorkspace = useSessionStore((state) => state.addWorkspace);
  const removeWorkspace = useSessionStore((state) => state.removeWorkspace);
  const settings = useSessionStore((state) => state.settings);
  const updateSettings = useSessionStore((state) => state.updateSettings);
  const [filter, setFilter] = useState("");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [children, setChildren] = useState<ChildrenByPath>({});
  const [loading, setLoading] = useState<Record<string, boolean>>({});
  const [errors, setErrors] = useState<ErrorByPath>({});
  const [choosingWorkspace, setChoosingWorkspace] = useState(false);
  const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
  const [dragged, setDragged] = useState<DragPayload | null>(null);
  const [dropTargetKey, setDropTargetKey] = useState<string | null>(null);

  const normalizedFilter = filter.trim().toLowerCase();
  const activeSession = useMemo(
    () => sessions.find((session) => session.id === activeSessionId) ?? null,
    [activeSessionId, sessions],
  );
  const activeWorkspace = useMemo(() => {
    return (
      workspaces.find((workspace) => workspace.id === activeSession?.workspaceId) ??
      null
    );
  }, [activeSession?.workspaceId, workspaces]);
  const visibleWorkspaces = activeWorkspace ? [activeWorkspace] : [];
  const actionWorkspace = activeWorkspace;

  const visibleEntries = useCallback(
    (entries: FsEntry[]) =>
      entries.filter((entry) => {
        if (!settings.showHiddenFiles && isHidden(entry)) {
          return false;
        }
        return !normalizedFilter || entry.name.toLowerCase().includes(normalizedFilter);
      }),
    [normalizedFilter, settings.showHiddenFiles],
  );

  async function refreshChildren(workspace: Workspace, path: string) {
    const key = nodeKey(workspace, path);
    setLoading((state) => ({ ...state, [key]: true }));
    setErrors((state) => ({ ...state, [key]: "" }));
    try {
      const nextChildren = await listWorkspaceDir(workspace.path, path);
      setChildren((state) => ({ ...state, [key]: nextChildren }));
    } catch (caught) {
      setErrors((state) => ({
        ...state,
        [key]: caught instanceof Error ? caught.message : String(caught),
      }));
    } finally {
      setLoading((state) => ({ ...state, [key]: false }));
    }
  }

  async function loadChildren(workspace: Workspace, path: string) {
    const key = nodeKey(workspace, path);
    if (children[key] || loading[key]) {
      return;
    }
    await refreshChildren(workspace, path);
  }

  async function toggleDir(workspace: Workspace, path: string) {
    const key = nodeKey(workspace, path);
    const nextExpanded = !expanded[key];
    setExpanded((state) => ({ ...state, [key]: nextExpanded }));
    if (nextExpanded) {
      await loadChildren(workspace, path);
    }
  }

  function clearCachedPath(workspace: Workspace, path: string) {
    const prefix = nodeKey(workspace, path);
    setChildren((state) =>
      Object.fromEntries(
        Object.entries(state).filter(([key]) => !key.startsWith(prefix)),
      ),
    );
    setExpanded((state) =>
      Object.fromEntries(
        Object.entries(state).filter(([key]) => !key.startsWith(prefix)),
      ),
    );
  }

  function contextTargetFor(workspace: Workspace, entry?: FsEntry): ContextTarget {
    return {
      workspace,
      entry: entry ?? rootEntry(workspace),
      isWorkspaceRoot: !entry,
    };
  }

  function dragPayloadFor(workspace: Workspace, entry: FsEntry): DragPayload {
    return {
      workspaceId: workspace.id,
      path: entry.path,
      kind: entry.kind,
      name: entry.name,
    };
  }

  function dragPayloadFromEvent(event: DragEvent): DragPayload | null {
    if (dragged) {
      return dragged;
    }
    const raw = event.dataTransfer.getData(FILE_TREE_DRAG_MIME);
    if (!raw) {
      return null;
    }
    try {
      const parsed = JSON.parse(raw) as Partial<DragPayload>;
      if (
        typeof parsed.workspaceId === "string" &&
        typeof parsed.path === "string" &&
        (parsed.kind === "file" || parsed.kind === "dir") &&
        typeof parsed.name === "string"
      ) {
        return parsed as DragPayload;
      }
    } catch {
      return null;
    }
    return null;
  }

  function canDropOnTarget(payload: DragPayload | null, target: ContextTarget): boolean {
    if (!payload || target.entry.kind !== "dir") {
      return false;
    }
    if (payload.workspaceId !== target.workspace.id) {
      return false;
    }
    if (parentPath(payload.path) === target.entry.path) {
      return false;
    }
    if (payload.kind === "dir" && isDescendantPath(payload.path, target.entry.path)) {
      return false;
    }
    return true;
  }

  function handleDragStart(
    event: DragEvent,
    workspace: Workspace,
    entry: FsEntry,
  ) {
    event.stopPropagation();
    const payload = dragPayloadFor(workspace, entry);
    setDragged(payload);
    setContextMenu(null);
    event.dataTransfer.effectAllowed = "move";
    event.dataTransfer.setData(FILE_TREE_DRAG_MIME, JSON.stringify(payload));
    event.dataTransfer.setData("text/plain", entry.path);
  }

  function handleDragEnd() {
    setDragged(null);
    setDropTargetKey(null);
  }

  function handleDragOver(event: DragEvent, target: ContextTarget) {
    const payload = dragPayloadFromEvent(event);
    if (!canDropOnTarget(payload, target)) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = "move";
    setDropTargetKey(nodeKey(target.workspace, target.entry.path));
  }

  function handleDragLeave(event: DragEvent, target: ContextTarget) {
    if (
      event.currentTarget instanceof Node &&
      event.relatedTarget instanceof Node &&
      event.currentTarget.contains(event.relatedTarget)
    ) {
      return;
    }
    const key = nodeKey(target.workspace, target.entry.path);
    setDropTargetKey((current) => (current === key ? null : current));
  }

  async function handleDrop(event: DragEvent, target: ContextTarget) {
    const payload = dragPayloadFromEvent(event);
    setDragged(null);
    setDropTargetKey(null);
    if (!canDropOnTarget(payload, target) || !payload) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    await moveTarget(payload, target);
  }

  function openContextMenu(
    event: MouseEvent,
    workspace: Workspace,
    entry?: FsEntry,
  ) {
    event.preventDefault();
    event.stopPropagation();
    setContextMenu({
      x: event.clientX,
      y: event.clientY,
      target: contextTargetFor(workspace, entry),
    });
  }

  function targetParentPath(target: ContextTarget): string {
    return target.entry.kind === "dir" ? target.entry.path : parentPath(target.entry.path);
  }

  async function createChild(target: ContextTarget, kind: "file" | "dir") {
    const name = window.prompt(kind === "file" ? "New file name" : "New folder name");
    if (!name?.trim()) {
      return;
    }
    setErrors((state) => ({ ...state, global: "" }));
    const parent = targetParentPath(target);
    try {
      const entry =
        kind === "file"
          ? await createWorkspaceFile(target.workspace.path, parent, name)
          : await createWorkspaceDir(target.workspace.path, parent, name);
      setExpanded((state) => ({
        ...state,
        [nodeKey(target.workspace, parent)]: true,
      }));
      await refreshChildren(target.workspace, parent);
      if (entry.kind === "file") {
        selectFile(selectedFileFromEntry(target.workspace, entry));
      }
    } catch (caught) {
      setErrors((state) => ({
        ...state,
        global: caught instanceof Error ? caught.message : String(caught),
      }));
    }
  }

  async function createEnvSibling(target: ContextTarget) {
    const targetName = envTargetName(target.entry.name);
    if (!targetName || target.entry.kind !== "file") {
      return;
    }
    setErrors((state) => ({ ...state, global: "" }));
    const parent = parentPath(target.entry.path);
    try {
      const sourceBytes = await readWorkspaceFile(
        target.workspace.path,
        target.entry.path,
      );
      const text = new TextDecoder().decode(new Uint8Array(sourceBytes));
      const nextText =
        target.entry.name === ".env" ? redactEnvContents(text) : text;
      const created = await createWorkspaceFileWithContents(
        target.workspace.path,
        parent,
        targetName,
        new TextEncoder().encode(nextText),
      );
      await refreshChildren(target.workspace, parent);
      selectFile(selectedFileFromEntry(target.workspace, created));
    } catch (caught) {
      setErrors((state) => ({
        ...state,
        global: caught instanceof Error ? caught.message : String(caught),
      }));
    }
  }

  async function renameTarget(target: ContextTarget) {
    if (target.isWorkspaceRoot) {
      return;
    }
    const name = window.prompt("Rename", target.entry.name);
    if (!name?.trim() || name === target.entry.name) {
      return;
    }
    setErrors((state) => ({ ...state, global: "" }));
    const parent = parentPath(target.entry.path);
    try {
      const renamed = await renameWorkspacePath(
        target.workspace.path,
        target.entry.path,
        name,
      );
      clearCachedPath(target.workspace, target.entry.path);
      await refreshChildren(target.workspace, parent);
      if (selectedFile?.path === target.entry.path && renamed.kind === "file") {
        selectFile(selectedFileFromEntry(target.workspace, renamed));
      }
    } catch (caught) {
      setErrors((state) => ({
        ...state,
        global: caught instanceof Error ? caught.message : String(caught),
      }));
    }
  }

  async function deleteTarget(target: ContextTarget) {
    if (target.isWorkspaceRoot) {
      return;
    }
    if (!window.confirm(`Delete ${target.entry.name}?`)) {
      return;
    }
    setErrors((state) => ({ ...state, global: "" }));
    const parent = parentPath(target.entry.path);
    try {
      await deleteWorkspacePath(target.workspace.path, target.entry.path);
      clearCachedPath(target.workspace, target.entry.path);
      await refreshChildren(target.workspace, parent);
      if (selectedFile?.path.startsWith(target.entry.path)) {
        selectFile(null);
      }
    } catch (caught) {
      setErrors((state) => ({
        ...state,
        global: caught instanceof Error ? caught.message : String(caught),
      }));
    }
  }

  async function moveTarget(source: DragPayload, target: ContextTarget) {
    setErrors((state) => ({ ...state, global: "" }));
    const sourceParent = parentPath(source.path);
    const destination = target.entry.path;
    try {
      const moved = await moveWorkspacePath(
        target.workspace.path,
        source.path,
        destination,
      );
      clearCachedPath(target.workspace, source.path);
      setExpanded((state) => ({
        ...state,
        [nodeKey(target.workspace, destination)]: true,
      }));
      await Promise.all([
        refreshChildren(target.workspace, sourceParent),
        refreshChildren(target.workspace, destination),
      ]);
      if (selectedFile?.path === source.path && moved.kind === "file") {
        selectFile(selectedFileFromEntry(target.workspace, moved));
      } else if (
        source.kind === "dir" &&
        selectedFile?.path &&
        isDescendantPath(source.path, selectedFile.path)
      ) {
        selectFile({
          ...selectedFile,
          workspaceId: target.workspace.id,
          workspaceRoot: target.workspace.path,
          path: selectedFile.path.replace(source.path, moved.path),
        });
      }
    } catch (caught) {
      setErrors((state) => ({
        ...state,
        global: caught instanceof Error ? caught.message : String(caught),
      }));
    }
  }

  async function refreshTarget(target: ContextTarget) {
    const path = target.entry.kind === "dir" ? target.entry.path : parentPath(target.entry.path);
    await refreshChildren(target.workspace, path);
  }

  async function revealTarget(target: ContextTarget) {
    await revealItemInDir(target.entry.path);
  }

  async function copyText(value: string) {
    await navigator.clipboard?.writeText(value);
  }

  function openTarget(target: ContextTarget) {
    if (target.entry.kind === "dir") {
      void toggleDir(target.workspace, target.entry.path);
      return;
    }
    selectFile(selectedFileFromEntry(target.workspace, target.entry));
  }

  function collapseAll() {
    setExpanded({});
  }

  async function refreshVisibleWorkspaces() {
    await Promise.all(
      visibleWorkspaces.map((workspace) => refreshChildren(workspace, workspace.path)),
    );
  }

  async function addWorkspaceFromPicker() {
    setChoosingWorkspace(true);
    setErrors((state) => ({ ...state, global: "" }));
    try {
      const workspace = await chooseWorkspaceFolder();
      if (!workspace) {
        return;
      }
      addWorkspace(workspace);
      if (activeWorkspace?.id === workspace.id) {
        await loadChildren(workspace, workspace.path);
        setExpanded((state) => ({
          ...state,
          [nodeKey(workspace, workspace.path)]: true,
        }));
      }
    } catch (caught) {
      setErrors((state) => ({
        ...state,
        global: caught instanceof Error ? caught.message : String(caught),
      }));
    } finally {
      setChoosingWorkspace(false);
    }
  }

  useEffect(() => {
    if (!activeWorkspace) {
      return;
    }
    const key = nodeKey(activeWorkspace, activeWorkspace.path);
    setExpanded((state) => (state[key] ? state : { ...state, [key]: true }));
    void loadChildren(activeWorkspace, activeWorkspace.path);
  }, [activeWorkspace?.id, activeWorkspace?.path]);

  useEffect(() => {
    setContextMenu(null);
  }, [activeWorkspace?.id]);

  useEffect(() => {
    function closeContextMenu() {
      setContextMenu(null);
    }

    function closeWithEscape(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setContextMenu(null);
      }
    }

    window.addEventListener("click", closeContextMenu);
    window.addEventListener("keydown", closeWithEscape);
    return () => {
      window.removeEventListener("click", closeContextMenu);
      window.removeEventListener("keydown", closeWithEscape);
    };
  }, []);

  const workspaceRows = useMemo(
    () =>
      visibleWorkspaces.map((workspace) => (
        <WorkspaceRoot
          key={workspace.id}
          workspace={workspace}
          expanded={Boolean(expanded[nodeKey(workspace, workspace.path)])}
          loading={Boolean(loading[nodeKey(workspace, workspace.path)])}
          error={errors[nodeKey(workspace, workspace.path)]}
          entries={visibleEntries(children[nodeKey(workspace, workspace.path)] ?? [])}
          showFileIcons={settings.showFileIcons}
          gitStatusByPath={gitStatusByPath}
          isDropTarget={dropTargetKey === nodeKey(workspace, workspace.path)}
          onToggle={() => void toggleDir(workspace, workspace.path)}
          onSelectRoot={() => selectFile(null)}
          onContextMenu={(event) => openContextMenu(event, workspace)}
          onDragOver={(event) =>
            handleDragOver(event, contextTargetFor(workspace))
          }
          onDragLeave={(event) =>
            handleDragLeave(event, contextTargetFor(workspace))
          }
          onDrop={(event) => void handleDrop(event, contextTargetFor(workspace))}
          renderNode={(entry, depth) => (
            <TreeNode
              key={entry.path}
              workspace={workspace}
              entry={entry}
              depth={depth}
              expanded={expanded}
              loading={loading}
              errors={errors}
              childrenByPath={children}
              visibleEntries={visibleEntries}
              selectedPath={selectedFile?.path ?? null}
              showFileIcons={settings.showFileIcons}
              gitStatusByPath={gitStatusByPath}
              agentReviewsByPath={agentReviewsByPath}
              draggedPath={dragged?.path ?? null}
              dropTargetKey={dropTargetKey}
              onToggle={(path) => void toggleDir(workspace, path)}
              onContextMenu={(event, item) => openContextMenu(event, workspace, item)}
              onSelect={(file) => {
                const review = agentReviewsByPath?.[file.path];
                if (review) {
                  selectFile({
                    type: "agent-review",
                    workspaceId: workspace.id,
                    workspaceRoot: workspace.path,
                    path: file.path,
                    name: file.name,
                    reviewId: review.id,
                  });
                } else {
                  selectFile(selectedFileFromEntry(workspace, file));
                }
              }}
              onDragStart={(event, item) => handleDragStart(event, workspace, item)}
              onDragEnd={handleDragEnd}
              onDragOver={(event, item) =>
                handleDragOver(event, contextTargetFor(workspace, item))
              }
              onDragLeave={(event, item) =>
                handleDragLeave(event, contextTargetFor(workspace, item))
              }
              onDrop={(event, item) =>
                void handleDrop(event, contextTargetFor(workspace, item))
              }
            />
          )}
        />
      )),
    [
      children,
      errors,
      expanded,
      dragged,
      dropTargetKey,
      loading,
      selectFile,
      selectedFile,
      settings.showFileIcons,
      gitStatusByPath,
      agentReviewsByPath,
      visibleEntries,
      visibleWorkspaces,
    ],
  );

  return (
    <aside className="file-tree" aria-label="Workspaces">
      <div className="file-tree-toolbar">
        <input
          className="filter-input"
          value={filter}
          placeholder="Filter file names"
          aria-label="Filter file names"
          onChange={(event) => setFilter(event.target.value)}
        />
        <button
          type="button"
          className="icon-button"
          aria-label="Open Folder"
          title="Open Folder"
          disabled={choosingWorkspace}
          onClick={() => void addWorkspaceFromPicker()}
        >
          +
        </button>
        <div className="tree-toolbar-actions" aria-label="File tree actions">
          <button
            type="button"
            className="tree-action-button"
            aria-label="New file"
            title="New file"
            disabled={!actionWorkspace}
            onClick={() =>
              actionWorkspace
                ? void createChild(contextTargetFor(actionWorkspace), "file")
                : undefined
            }
          >
            +F
          </button>
          <button
            type="button"
            className="tree-action-button"
            aria-label="New folder"
            title="New folder"
            disabled={!actionWorkspace}
            onClick={() =>
              actionWorkspace
                ? void createChild(contextTargetFor(actionWorkspace), "dir")
                : undefined
            }
          >
            +D
          </button>
          <button
            type="button"
            className="tree-action-button"
            aria-label="Refresh file tree"
            title="Refresh"
            disabled={visibleWorkspaces.length === 0}
            onClick={() => void refreshVisibleWorkspaces()}
          >
            R
          </button>
          <button
            type="button"
            className="tree-action-button"
            aria-label="Collapse all folders"
            title="Collapse all"
            onClick={collapseAll}
          >
            []
          </button>
        </div>
        <label className="tree-options">
          <input
            type="checkbox"
            checked={settings.showHiddenFiles}
            onChange={(event) =>
              updateSettings({ showHiddenFiles: event.target.checked })
            }
          />
          Hidden files
        </label>
      </div>
      {errors.global ? <div className="tree-error">{errors.global}</div> : null}
      <div className="workspace-list">{workspaceRows}</div>
      {contextMenu ? (
        <FileTreeContextMenu
          menu={contextMenu}
          onClose={() => setContextMenu(null)}
          onOpen={openTarget}
          onReveal={(target) => void revealTarget(target)}
          onCopyPath={(target) => void copyText(target.entry.path)}
          onCopyRelativePath={(target) =>
            void copyText(relativeWorkspacePath(target.workspace, target.entry.path))
          }
          onNewFile={(target) => void createChild(target, "file")}
          onNewFolder={(target) => void createChild(target, "dir")}
          onCreateEnvSibling={(target) => void createEnvSibling(target)}
          onRefresh={(target) => void refreshTarget(target)}
          onRename={(target) => void renameTarget(target)}
          onDelete={(target) => void deleteTarget(target)}
          onRemoveWorkspace={(target) => removeWorkspace(target.workspace.id)}
        />
      ) : null}
    </aside>
  );
}

interface FileTreeContextMenuProps {
  menu: ContextMenuState;
  onClose: () => void;
  onOpen: (target: ContextTarget) => void;
  onReveal: (target: ContextTarget) => void;
  onCopyPath: (target: ContextTarget) => void;
  onCopyRelativePath: (target: ContextTarget) => void;
  onNewFile: (target: ContextTarget) => void;
  onNewFolder: (target: ContextTarget) => void;
  onCreateEnvSibling: (target: ContextTarget) => void;
  onRefresh: (target: ContextTarget) => void;
  onRename: (target: ContextTarget) => void;
  onDelete: (target: ContextTarget) => void;
  onRemoveWorkspace: (target: ContextTarget) => void;
}

function FileTreeContextMenu({
  menu,
  onClose,
  onOpen,
  onReveal,
  onCopyPath,
  onCopyRelativePath,
  onNewFile,
  onNewFolder,
  onCreateEnvSibling,
  onRefresh,
  onRename,
  onDelete,
  onRemoveWorkspace,
}: FileTreeContextMenuProps) {
  const { target } = menu;

  function run(action: (target: ContextTarget) => void) {
    action(target);
    onClose();
  }

  return (
    <div
      className="file-context-menu"
      role="menu"
      style={{ left: menu.x, top: menu.y }}
      onClick={(event) => event.stopPropagation()}
      onContextMenu={(event) => event.preventDefault()}
    >
      <button type="button" role="menuitem" onClick={() => run(onOpen)}>
        Open
      </button>
      <button type="button" role="menuitem" onClick={() => run(onReveal)}>
        Reveal in Finder
      </button>
      <div className="context-separator" />
      {target.entry.kind === "dir" ? (
        <>
          <button type="button" role="menuitem" onClick={() => run(onNewFile)}>
            New File
          </button>
          <button type="button" role="menuitem" onClick={() => run(onNewFolder)}>
            New Folder
          </button>
          <button type="button" role="menuitem" onClick={() => run(onRefresh)}>
            Refresh
          </button>
          <div className="context-separator" />
        </>
      ) : null}
      {target.entry.kind === "file" && envTargetName(target.entry.name) ? (
        <>
          <button
            type="button"
            role="menuitem"
            onClick={() => run(onCreateEnvSibling)}
          >
            {target.entry.name === ".env"
              ? "Create .env.example from .env"
              : "Create .env from .env.example"}
          </button>
          <div className="context-separator" />
        </>
      ) : null}
      <button type="button" role="menuitem" onClick={() => run(onCopyPath)}>
        Copy Path
      </button>
      <button type="button" role="menuitem" onClick={() => run(onCopyRelativePath)}>
        Copy Relative Path
      </button>
      <div className="context-separator" />
      {target.isWorkspaceRoot ? (
        <button
          type="button"
          role="menuitem"
          className="danger"
          onClick={() => run(onRemoveWorkspace)}
        >
          Remove Workspace
        </button>
      ) : (
        <>
          <button type="button" role="menuitem" onClick={() => run(onRename)}>
            Rename...
          </button>
          <button
            type="button"
            role="menuitem"
            className="danger"
            onClick={() => run(onDelete)}
          >
            Delete
          </button>
        </>
      )}
    </div>
  );
}

interface WorkspaceRootProps {
  workspace: Workspace;
  expanded: boolean;
  loading: boolean;
  error?: string;
  entries: FsEntry[];
  showFileIcons: boolean;
  gitStatusByPath?: Record<string, GitTreeState>;
  isDropTarget: boolean;
  onToggle: () => void;
  onSelectRoot: () => void;
  onContextMenu: (event: MouseEvent) => void;
  onDragOver: (event: DragEvent) => void;
  onDragLeave: (event: DragEvent) => void;
  onDrop: (event: DragEvent) => void;
  renderNode: (entry: FsEntry, depth: number) => ReactNode;
}

function WorkspaceRoot({
  workspace,
  expanded,
  loading,
  error,
  entries,
  showFileIcons,
  gitStatusByPath,
  isDropTarget,
  onToggle,
  onSelectRoot,
  onContextMenu,
  onDragOver,
  onDragLeave,
  onDrop,
  renderNode,
}: WorkspaceRootProps) {
  const gitState = gitStateForPath(gitStatusByPath, workspace.path, true);
  return (
    <section>
      <button
        type="button"
        className={`workspace-row ${showFileIcons ? "with-icons" : ""} ${
          isDropTarget ? "drop-target" : ""
        }`}
        onClick={onSelectRoot}
        onDoubleClick={onToggle}
        onContextMenu={onContextMenu}
        onDragOver={onDragOver}
        onDragLeave={onDragLeave}
        onDrop={onDrop}
      >
        <span className="tree-glyph" onClick={onToggle}>
          {expanded ? "v" : ">"}
        </span>
        {showFileIcons ? (
          <TreeFileIcon
            entry={{ name: workspace.name, path: workspace.path, kind: "dir", size: 0 }}
          />
        ) : null}
        <span className="workspace-name">{workspace.name}</span>
        <span className="workspace-path">{loading ? "loading" : ""}</span>
        <GitStateBadge state={gitState} />
      </button>
      {error ? <div className="tree-error">{error}</div> : null}
      {expanded ? <div className="tree-children">{entries.map((entry) => renderNode(entry, 1))}</div> : null}
    </section>
  );
}

interface TreeNodeProps {
  workspace: Workspace;
  entry: FsEntry;
  depth: number;
  expanded: Record<string, boolean>;
  loading: Record<string, boolean>;
  errors: ErrorByPath;
  childrenByPath: ChildrenByPath;
  visibleEntries: (entries: FsEntry[]) => FsEntry[];
  selectedPath: string | null;
  showFileIcons: boolean;
  gitStatusByPath?: Record<string, GitTreeState>;
  agentReviewsByPath?: Record<string, AgentReviewRecord>;
  draggedPath: string | null;
  dropTargetKey: string | null;
  onToggle: (path: string) => void;
  onContextMenu: (event: MouseEvent, entry: FsEntry) => void;
  onSelect: (entry: FsEntry) => void;
  onDragStart: (event: DragEvent, entry: FsEntry) => void;
  onDragEnd: () => void;
  onDragOver: (event: DragEvent, entry: FsEntry) => void;
  onDragLeave: (event: DragEvent, entry: FsEntry) => void;
  onDrop: (event: DragEvent, entry: FsEntry) => void;
}

function TreeNode({
  workspace,
  entry,
  depth,
  expanded,
  loading,
  errors,
  childrenByPath,
  visibleEntries,
  selectedPath,
  showFileIcons,
  gitStatusByPath,
  agentReviewsByPath,
  draggedPath,
  dropTargetKey,
  onToggle,
  onContextMenu,
  onSelect,
  onDragStart,
  onDragEnd,
  onDragOver,
  onDragLeave,
  onDrop,
}: TreeNodeProps) {
  const key = nodeKey(workspace, entry.path);
  const isDir = entry.kind === "dir";
  const isExpanded = Boolean(expanded[key]);
  const childEntries = visibleEntries(childrenByPath[key] ?? []);
  const gitState = gitStateForPath(gitStatusByPath, entry.path, isDir);
  const agentReview = agentReviewsByPath?.[entry.path] ?? null;
  return (
    <div>
      <button
        type="button"
        className={`tree-row ${showFileIcons ? "with-icons" : ""} ${
          selectedPath === entry.path ? "selected" : ""
        } ${draggedPath === entry.path ? "dragging" : ""} ${
          isDir && dropTargetKey === key ? "drop-target" : ""
        }`}
        style={{ "--depth": depth } as CSSProperties}
        draggable
        onClick={() => (isDir ? onToggle(entry.path) : onSelect(entry))}
        onContextMenu={(event) => onContextMenu(event, entry)}
        onDragStart={(event) => onDragStart(event, entry)}
        onDragEnd={onDragEnd}
        onDragOver={(event) => {
          if (isDir) {
            onDragOver(event, entry);
          }
        }}
        onDragLeave={(event) => {
          if (isDir) {
            onDragLeave(event, entry);
          }
        }}
        onDrop={(event) => {
          if (isDir) {
            onDrop(event, entry);
          }
        }}
      >
        <span className="tree-depth" />
        <span className="tree-glyph">{isDir ? (isExpanded ? "v" : ">") : ""}</span>
        {showFileIcons ? <TreeFileIcon entry={entry} /> : null}
        <span className={`tree-row-name ${gitState ? `git-${gitState.tone}` : ""}`}>
          {entry.name}
        </span>
        <span className="tree-meta">{loading[key] ? "..." : ""}</span>
        {agentReview ? (
          <span className="tree-agent-state" title="Agent edit pending review">
            A
          </span>
        ) : null}
        <GitStateBadge state={gitState} />
      </button>
      {errors[key] ? <div className="tree-error">{errors[key]}</div> : null}
      {isDir && isExpanded ? (
        <div className="tree-children">
          {childEntries.map((child) => (
            <TreeNode
              key={child.path}
              workspace={workspace}
              entry={child}
              depth={depth + 1}
              expanded={expanded}
              loading={loading}
              errors={errors}
              childrenByPath={childrenByPath}
              visibleEntries={visibleEntries}
              selectedPath={selectedPath}
              showFileIcons={showFileIcons}
              gitStatusByPath={gitStatusByPath}
              agentReviewsByPath={agentReviewsByPath}
              draggedPath={draggedPath}
              dropTargetKey={dropTargetKey}
              onToggle={onToggle}
              onContextMenu={onContextMenu}
              onSelect={onSelect}
              onDragStart={onDragStart}
              onDragEnd={onDragEnd}
              onDragOver={onDragOver}
              onDragLeave={onDragLeave}
              onDrop={onDrop}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}
