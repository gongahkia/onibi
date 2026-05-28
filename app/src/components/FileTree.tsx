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
  deleteWorkspacePath,
  listWorkspaceDir,
  moveWorkspacePath,
  renameWorkspacePath,
  selectedFileFromEntry,
  type FsEntry,
  type Workspace,
  useSessionStore,
} from "../lib/sessions";
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

function isDescendantPath(parent: string, path: string): boolean {
  const normalizedParent = parent.replace(/\/+$/, "");
  return path === normalizedParent || path.startsWith(`${normalizedParent}/`);
}

interface TreeIcon {
  label: string;
  tone: string;
  title: string;
}

const SPECIAL_FILE_ICONS: Record<string, TreeIcon> = {
  ".env": { label: "ENV", tone: "config", title: "Environment file" },
  ".gitignore": { label: "GIT", tone: "config", title: "Git ignore file" },
  Dockerfile: { label: "DKR", tone: "config", title: "Docker file" },
  Makefile: { label: "MK", tone: "config", title: "Makefile" },
  "package.json": { label: "NPM", tone: "data", title: "Package manifest" },
  "pnpm-lock.yaml": { label: "LCK", tone: "lock", title: "Lockfile" },
  "package-lock.json": { label: "LCK", tone: "lock", title: "Lockfile" },
  "README.md": { label: "MD", tone: "docs", title: "Markdown file" },
};

const EXTENSION_FILE_ICONS: Record<string, TreeIcon> = {
  css: { label: "CSS", tone: "style", title: "CSS file" },
  gif: { label: "IMG", tone: "image", title: "Image file" },
  html: { label: "HTM", tone: "markup", title: "HTML file" },
  ico: { label: "IMG", tone: "image", title: "Image file" },
  jpeg: { label: "IMG", tone: "image", title: "Image file" },
  jpg: { label: "IMG", tone: "image", title: "Image file" },
  js: { label: "JS", tone: "code", title: "JavaScript file" },
  json: { label: "JSN", tone: "data", title: "JSON file" },
  jsx: { label: "JSX", tone: "code", title: "JavaScript React file" },
  lock: { label: "LCK", tone: "lock", title: "Lockfile" },
  md: { label: "MD", tone: "docs", title: "Markdown file" },
  png: { label: "IMG", tone: "image", title: "Image file" },
  rs: { label: "RS", tone: "code", title: "Rust file" },
  svg: { label: "SVG", tone: "image", title: "SVG file" },
  toml: { label: "TOM", tone: "config", title: "TOML file" },
  ts: { label: "TS", tone: "code", title: "TypeScript file" },
  tsx: { label: "TSX", tone: "code", title: "TypeScript React file" },
  txt: { label: "TXT", tone: "docs", title: "Text file" },
  yaml: { label: "YML", tone: "config", title: "YAML file" },
  yml: { label: "YML", tone: "config", title: "YAML file" },
};

function iconForEntry(entry: FsEntry): TreeIcon {
  if (entry.kind === "dir") {
    return { label: "DIR", tone: "folder", title: "Folder" };
  }

  const specialIcon = SPECIAL_FILE_ICONS[entry.name];
  if (specialIcon) {
    return specialIcon;
  }

  const extension = entry.name.includes(".")
    ? entry.name.split(".").pop()?.toLowerCase()
    : "";
  return (
    (extension ? EXTENSION_FILE_ICONS[extension] : undefined) ?? {
      label: "TXT",
      tone: "file",
      title: "File",
    }
  );
}

function TreeFileIcon({ entry }: { entry: FsEntry }) {
  const icon = iconForEntry(entry);
  return (
    <span
      className={`tree-file-icon ${icon.tone}`}
      title={icon.title}
      aria-hidden="true"
    >
      {icon.label}
    </span>
  );
}

export function FileTree() {
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
              draggedPath={dragged?.path ?? null}
              dropTargetKey={dropTargetKey}
              onToggle={(path) => void toggleDir(workspace, path)}
              onContextMenu={(event, item) => openContextMenu(event, workspace, item)}
              onSelect={(file) => selectFile(selectedFileFromEntry(workspace, file))}
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
          placeholder="Filter files"
          aria-label="Filter files"
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
  isDropTarget,
  onToggle,
  onSelectRoot,
  onContextMenu,
  onDragOver,
  onDragLeave,
  onDrop,
  renderNode,
}: WorkspaceRootProps) {
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
        <span className="tree-row-name">{entry.name}</span>
        <span className="tree-meta">{loading[key] ? "..." : ""}</span>
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
