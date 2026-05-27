import { useCallback, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import {
  listWorkspaceDir,
  selectedFileFromEntry,
  type FsEntry,
  type Workspace,
  useSessionStore,
  workspaceFromPath,
} from "../lib/sessions";

type ChildrenByPath = Record<string, FsEntry[]>;
type ErrorByPath = Record<string, string>;

function nodeKey(workspace: Workspace, path: string): string {
  return `${workspace.id}:${path}`;
}

function isHidden(entry: FsEntry): boolean {
  return entry.name.startsWith(".");
}

export function FileTree() {
  const workspaces = useSessionStore((state) => state.workspaces);
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

  const normalizedFilter = filter.trim().toLowerCase();

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

  async function loadChildren(workspace: Workspace, path: string) {
    const key = nodeKey(workspace, path);
    if (children[key] || loading[key]) {
      return;
    }
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

  async function toggleDir(workspace: Workspace, path: string) {
    const key = nodeKey(workspace, path);
    const nextExpanded = !expanded[key];
    setExpanded((state) => ({ ...state, [key]: nextExpanded }));
    if (nextExpanded) {
      await loadChildren(workspace, path);
    }
  }

  async function addWorkspaceFromPrompt() {
    const path = window.prompt("Workspace path");
    if (!path) {
      return;
    }
    try {
      const workspace = await workspaceFromPath(path);
      addWorkspace(workspace);
      await loadChildren(workspace, workspace.path);
      setExpanded((state) => ({ ...state, [nodeKey(workspace, workspace.path)]: true }));
    } catch (caught) {
      setErrors((state) => ({
        ...state,
        global: caught instanceof Error ? caught.message : String(caught),
      }));
    }
  }

  const workspaceRows = useMemo(
    () =>
      workspaces.map((workspace) => (
        <WorkspaceRoot
          key={workspace.id}
          workspace={workspace}
          expanded={Boolean(expanded[nodeKey(workspace, workspace.path)])}
          loading={Boolean(loading[nodeKey(workspace, workspace.path)])}
          error={errors[nodeKey(workspace, workspace.path)]}
          entries={visibleEntries(children[nodeKey(workspace, workspace.path)] ?? [])}
          selectedPath={selectedFile?.path ?? null}
          onToggle={() => void toggleDir(workspace, workspace.path)}
          onSelectRoot={() => selectFile(null)}
          onRemove={() => removeWorkspace(workspace.id)}
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
              onToggle={(path) => void toggleDir(workspace, path)}
              onSelect={(file) => selectFile(selectedFileFromEntry(workspace, file))}
            />
          )}
        />
      )),
    [
      children,
      errors,
      expanded,
      loading,
      removeWorkspace,
      selectFile,
      selectedFile,
      visibleEntries,
      workspaces,
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
          onClick={() => void addWorkspaceFromPrompt()}
        >
          +
        </button>
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
    </aside>
  );
}

interface WorkspaceRootProps {
  workspace: Workspace;
  expanded: boolean;
  loading: boolean;
  error?: string;
  entries: FsEntry[];
  selectedPath: string | null;
  onToggle: () => void;
  onSelectRoot: () => void;
  onRemove: () => void;
  renderNode: (entry: FsEntry, depth: number) => ReactNode;
}

function WorkspaceRoot({
  workspace,
  expanded,
  loading,
  error,
  entries,
  onToggle,
  onSelectRoot,
  onRemove,
  renderNode,
}: WorkspaceRootProps) {
  return (
    <section>
      <button
        type="button"
        className="workspace-row"
        onClick={onSelectRoot}
        onDoubleClick={onToggle}
        onContextMenu={(event) => {
          event.preventDefault();
          if (window.confirm(`Remove ${workspace.name}?`)) {
            onRemove();
          }
        }}
      >
        <span className="tree-glyph" onClick={onToggle}>
          {expanded ? "v" : ">"}
        </span>
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
  onToggle: (path: string) => void;
  onSelect: (entry: FsEntry) => void;
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
  onToggle,
  onSelect,
}: TreeNodeProps) {
  const key = nodeKey(workspace, entry.path);
  const isDir = entry.kind === "dir";
  const isExpanded = Boolean(expanded[key]);
  const childEntries = visibleEntries(childrenByPath[key] ?? []);
  return (
    <div>
      <button
        type="button"
        className={`tree-row ${selectedPath === entry.path ? "selected" : ""}`}
        style={{ "--depth": depth } as CSSProperties}
        onClick={() => (isDir ? onToggle(entry.path) : onSelect(entry))}
      >
        <span className="tree-depth" />
        <span className="tree-glyph">{isDir ? (isExpanded ? "v" : ">") : ""}</span>
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
              onToggle={onToggle}
              onSelect={onSelect}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}
