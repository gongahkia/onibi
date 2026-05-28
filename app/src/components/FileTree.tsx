import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import {
  listWorkspaceDir,
  selectedFileFromEntry,
  type FsEntry,
  type Workspace,
  useSessionStore,
} from "../lib/sessions";
import { chooseWorkspaceFolder } from "../lib/workspace-picker";

type ChildrenByPath = Record<string, FsEntry[]>;
type ErrorByPath = Record<string, string>;

function nodeKey(workspace: Workspace, path: string): string {
  return `${workspace.id}:${path}`;
}

function isHidden(entry: FsEntry): boolean {
  return entry.name.startsWith(".");
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

  const normalizedFilter = filter.trim().toLowerCase();
  const activeWorkspace = useMemo(() => {
    const activeSession = sessions.find((session) => session.id === activeSessionId);
    const scopedWorkspaceId = activeSession?.workspaceId ?? selectedFile?.workspaceId;
    return workspaces.find((workspace) => workspace.id === scopedWorkspaceId) ?? null;
  }, [activeSessionId, selectedFile?.workspaceId, sessions, workspaces]);
  const visibleWorkspaces = activeWorkspace ? [activeWorkspace] : workspaces;

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

  async function addWorkspaceFromPicker() {
    setChoosingWorkspace(true);
    setErrors((state) => ({ ...state, global: "" }));
    try {
      const workspace = await chooseWorkspaceFolder();
      if (!workspace) {
        return;
      }
      addWorkspace(workspace);
      await loadChildren(workspace, workspace.path);
      setExpanded((state) => ({ ...state, [nodeKey(workspace, workspace.path)]: true }));
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
              showFileIcons={settings.showFileIcons}
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
  showFileIcons: boolean;
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
  showFileIcons,
  onToggle,
  onSelectRoot,
  onRemove,
  renderNode,
}: WorkspaceRootProps) {
  return (
    <section>
      <button
        type="button"
        className={`workspace-row ${showFileIcons ? "with-icons" : ""}`}
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
  showFileIcons,
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
        className={`tree-row ${showFileIcons ? "with-icons" : ""} ${
          selectedPath === entry.path ? "selected" : ""
        }`}
        style={{ "--depth": depth } as CSSProperties}
        onClick={() => (isDir ? onToggle(entry.path) : onSelect(entry))}
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
              onToggle={onToggle}
              onSelect={onSelect}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}
