import { useEffect, useMemo, useState } from "react";
import {
  commitGit,
  createGitWorktree,
  discardGitPaths,
  gitStateForEntry,
  hasStagedChange,
  hasWorkingTreeChange,
  listGitWorktrees,
  removeGitWorktree,
  stageGitPaths,
  type GitStatus,
  type GitStatusEntry,
  type GitWorktree,
  unstageGitPaths,
} from "../lib/git";
import {
  launchAgentInWorkspacePath,
  openWorkspacePath,
  useSessionStore,
  type Workspace,
} from "../lib/sessions";
import { confirmAction } from "../lib/native-dialogs";

interface SourceControlViewProps {
  workspace: Workspace | null;
  status: GitStatus | null;
  loading: boolean;
  error: string;
  onRefresh: () => Promise<void>;
}

function relativePathLabel(entry: GitStatusEntry): string {
  return entry.originalPath ? `${entry.originalPath} -> ${entry.path}` : entry.path;
}

function changeDescription(entry: GitStatusEntry): string {
  const state = gitStateForEntry(entry);
  if (hasStagedChange(entry) && hasWorkingTreeChange(entry)) {
    return `Staged and ${state.label.toLowerCase()}`;
  }
  if (hasStagedChange(entry)) {
    return `Staged ${state.label.toLowerCase()}`;
  }
  return state.label;
}

function SourceControlEntry({
  entry,
  busy,
  onStage,
  onUnstage,
  onDiscard,
  onOpen,
}: {
  entry: GitStatusEntry;
  busy: boolean;
  onStage: (entry: GitStatusEntry) => void;
  onUnstage: (entry: GitStatusEntry) => void;
  onDiscard: (entry: GitStatusEntry) => void;
  onOpen: (entry: GitStatusEntry, stage: "staged" | "working") => void;
}) {
  const state = gitStateForEntry(entry);
  const staged = hasStagedChange(entry);
  const working = hasWorkingTreeChange(entry);
  return (
    <div
      className="source-control-row"
      role="button"
      tabIndex={0}
      onClick={() => onOpen(entry, staged && !working ? "staged" : "working")}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          onOpen(entry, staged && !working ? "staged" : "working");
        }
      }}
    >
      <span className={`source-control-state tone-${state.tone}`} title={state.label}>
        {state.badge}
      </span>
      <span className="source-control-path" title={relativePathLabel(entry)}>
        {relativePathLabel(entry)}
      </span>
      <span className="source-control-description">{changeDescription(entry)}</span>
      {working ? (
        <button
          type="button"
          className="tree-action-button"
          disabled={busy}
          onClick={(event) => {
            event.stopPropagation();
            onStage(entry);
          }}
          title="Stage"
          aria-label={`Stage ${entry.path}`}
        >
          +
        </button>
      ) : null}
      {staged ? (
        <button
          type="button"
          className="tree-action-button"
          disabled={busy}
          onClick={(event) => {
            event.stopPropagation();
            onUnstage(entry);
          }}
          title="Unstage"
          aria-label={`Unstage ${entry.path}`}
        >
          -
        </button>
      ) : null}
      <button
        type="button"
        className="tree-action-button danger"
        disabled={busy}
        onClick={(event) => {
          event.stopPropagation();
          onDiscard(entry);
        }}
        title="Discard"
        aria-label={`Discard ${entry.path}`}
      >
        x
      </button>
    </div>
  );
}

export function SourceControlView({
  workspace,
  status,
  loading,
  error,
  onRefresh,
}: SourceControlViewProps) {
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const [worktrees, setWorktrees] = useState<GitWorktree[]>([]);
  const [worktreesLoading, setWorktreesLoading] = useState(false);
  const selectFile = useSessionStore((state) => state.selectFile);
  const settings = useSessionStore((state) => state.settings);

  const entries = status?.entries ?? [];
  const stagedEntries = useMemo(
    () => entries.filter((entry) => hasStagedChange(entry)),
    [entries],
  );
  const changedEntries = useMemo(
    () => entries.filter((entry) => hasWorkingTreeChange(entry)),
    [entries],
  );
  const allPaths = useMemo(() => entries.map((entry) => entry.path), [entries]);
  const canCommit = Boolean(workspace && message.trim() && stagedEntries.length > 0);
  async function refreshWorktrees() {
    if (!workspace) {
      setWorktrees([]);
      return;
    }
    setWorktreesLoading(true);
    try {
      setWorktrees(await listGitWorktrees(workspace.path));
    } catch {
      setWorktrees([]);
    } finally {
      setWorktreesLoading(false);
    }
  }

  useEffect(() => {
    void refreshWorktrees();
  }, [workspace?.path]);

  async function runAction(action: () => Promise<unknown>) {
    if (!workspace) {
      return;
    }
    setBusy(true);
    setActionError("");
    try {
      await action();
      await onRefresh();
      await refreshWorktrees();
    } catch (caught) {
      setActionError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setBusy(false);
    }
  }

  function stageEntry(entry: GitStatusEntry) {
    void runAction(() => stageGitPaths(workspace!.path, [entry.path]));
  }

  function unstageEntry(entry: GitStatusEntry) {
    void runAction(() => unstageGitPaths(workspace!.path, [entry.path]));
  }

  async function discardEntry(entry: GitStatusEntry) {
    if (!(await confirmAction(`Discard changes in ${entry.path}?`, {
      okLabel: "Discard",
      title: "Discard Changes",
    }))) {
      return;
    }
    void runAction(() => discardGitPaths(workspace!.path, [entry.path]));
  }

  function openEntry(entry: GitStatusEntry, stage: "staged" | "working") {
    if (!workspace) {
      return;
    }
    selectFile({
      type: "git-diff",
      workspaceId: workspace.id,
      workspaceRoot: workspace.path,
      path: entry.path,
      name: entry.path.split("/").pop() ?? entry.path,
      stage,
    });
  }

  function siblingWorktreePath(branch: string): string {
    const root = status?.repoRoot ?? workspace?.path ?? "";
    const trimmed = root.replace(/\/+$/, "");
    const slash = trimmed.lastIndexOf("/");
    const parent = slash > 0 ? trimmed.slice(0, slash) : ".";
    const name = slash > 0 ? trimmed.slice(slash + 1) : "repo";
    const slug = branch
      .trim()
      .replace(/[^A-Za-z0-9._-]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .toLowerCase();
    return `${parent}/${name}-${slug || "worktree"}`;
  }

  async function openWorktree(path: string) {
    await openWorkspacePath(path);
  }

  function createWorktree() {
    if (!workspace) {
      return;
    }
    const branch = window.prompt("New worktree branch");
    if (!branch?.trim()) {
      return;
    }
    const path = window.prompt("Worktree path", siblingWorktreePath(branch));
    if (!path?.trim()) {
      return;
    }
    void runAction(async () => {
      const worktree = await createGitWorktree(
        workspace.path,
        branch,
        path,
        status?.branch ?? undefined,
      );
      await openWorktree(worktree.path);
    });
  }

  async function deleteWorktree(worktree: GitWorktree) {
    if (!workspace) {
      return;
    }
    if (!(await confirmAction(`Remove worktree ${worktree.path}?`, {
      okLabel: "Remove",
      title: "Remove Worktree",
    }))) {
      return;
    }
    void runAction(() => removeGitWorktree(workspace.path, worktree.path, false));
  }

  function launchDefaultAgent(worktree: GitWorktree) {
    if (!workspace) {
      return;
    }
    void runAction(async () => {
      await launchAgentInWorkspacePath(settings.defaultAgent, worktree.path);
    });
  }

  if (!workspace) {
    return (
      <aside className="source-control-view" aria-label="Source Control">
        <div className="source-control-header">Source Control</div>
        <div className="source-control-empty">Open a workspace session to view Git.</div>
      </aside>
    );
  }

  return (
    <aside className="source-control-view" aria-label="Source Control">
      <div className="source-control-header">
        <span>Source Control</span>
        <button
          type="button"
          className="tree-action-button"
          onClick={() => void onRefresh()}
          disabled={loading || busy}
          title="Refresh"
          aria-label="Refresh source control"
        >
          R
        </button>
      </div>

      {!status?.isRepo ? (
        <>
          {error || actionError ? (
            <div className="tree-error">{error || actionError}</div>
          ) : null}
          <div className="source-control-empty">
            {loading ? "Loading repository..." : "No Git repository in this workspace."}
          </div>
        </>
      ) : (
        <>
          <div className="source-control-repo">
            <span className="source-control-repo-name">{workspace.name}</span>
            <span className="source-control-branch">{status.branch ?? "detached"}</span>
            {status.upstream ? (
              <span className="source-control-upstream">{status.upstream}</span>
            ) : null}
          </div>
          <div className="source-control-worktrees">
            <div className="source-control-worktree-header">
              <span>Worktrees</span>
              <button
                type="button"
                className="tree-action-button"
                disabled={busy || worktreesLoading}
                onClick={createWorktree}
                title="Create worktree"
                aria-label="Create worktree"
              >
                +
              </button>
              <button
                type="button"
                className="tree-action-button"
                disabled={worktreesLoading}
                onClick={() => void refreshWorktrees()}
                title="Refresh worktrees"
                aria-label="Refresh worktrees"
              >
                R
              </button>
            </div>
            {worktrees.length === 0 ? (
              <div className="source-control-empty">
                {worktreesLoading ? "Loading worktrees..." : "No worktrees found."}
              </div>
            ) : (
              worktrees.map((worktree) => {
                const isPrimary = worktree.path === status.repoRoot;
                return (
                <div
                  className={`source-control-worktree-row${isPrimary ? " primary" : ""}`}
                  key={worktree.path}
                >
                  <span className="source-control-worktree-label" title={worktree.path}>
                    <i
                      className={`codicon ${isPrimary ? "codicon-repo" : "codicon-repo-forked"}`}
                      aria-hidden="true"
                    />
                    <span>{worktree.branch ?? (worktree.detached ? "detached" : "worktree")}</span>
                    {isPrimary ? <span className="source-control-worktree-tag">primary</span> : null}
                  </span>
                  <button
                    type="button"
                    className="tree-action-button"
                    disabled={busy}
                    onClick={() => void openWorktree(worktree.path)}
                    title="Open as workspace"
                    aria-label={`Open ${worktree.path} as workspace`}
                  >
                    O
                  </button>
                  <button
                    type="button"
                    className="tree-action-button"
                    disabled={busy}
                    onClick={() => launchDefaultAgent(worktree)}
                    title="Launch default agent"
                    aria-label={`Launch default agent in ${worktree.path}`}
                  >
                    ▶
                  </button>
                  <button
                    type="button"
                    className="tree-action-button danger"
                    disabled={busy || worktree.path === status.repoRoot}
                    onClick={() => deleteWorktree(worktree)}
                    title="Remove worktree"
                    aria-label={`Remove worktree ${worktree.path}`}
                  >
                    x
                  </button>
                </div>
                );
              })
            )}
          </div>
          <input
            className="source-control-message"
            value={message}
            placeholder={`Message (${status.branch ?? "HEAD"})`}
            onChange={(event) => setMessage(event.target.value)}
          />
          <div className="source-control-actions">
            <button
              type="button"
              className="source-control-primary"
              disabled={busy || !canCommit}
              onClick={() =>
                void runAction(async () => {
                  await commitGit(workspace.path, message);
                  setMessage("");
                })
              }
            >
              Commit
            </button>
          </div>
          <div className="source-control-actions">
            <button
              type="button"
              className="source-control-secondary"
              disabled={busy || allPaths.length === 0}
              onClick={() => void runAction(() => stageGitPaths(workspace.path, allPaths))}
            >
              Stage All
            </button>
          </div>

          {error || actionError ? (
            <div className="tree-error">{error || actionError}</div>
          ) : null}

          {stagedEntries.length > 0 ? (
            <section className="source-control-section">
              <div className="source-control-section-title">
                <span>Staged Changes</span>
                <span>{stagedEntries.length}</span>
              </div>
              {stagedEntries.map((entry) => (
                <SourceControlEntry
                  key={`staged:${entry.path}`}
                  entry={entry}
                  busy={busy}
                  onStage={stageEntry}
                  onUnstage={unstageEntry}
                  onDiscard={discardEntry}
                  onOpen={(entry) => openEntry(entry, "staged")}
                />
              ))}
            </section>
          ) : null}

          <section className="source-control-section">
            <div className="source-control-section-title">
              <span>Changes</span>
              <span>{changedEntries.length}</span>
            </div>
            {changedEntries.length === 0 ? (
              <div className="source-control-empty compact">
                {loading ? "Refreshing..." : "No working tree changes."}
              </div>
            ) : (
              changedEntries.map((entry) => (
                <SourceControlEntry
                  key={`changed:${entry.path}`}
                  entry={entry}
                  busy={busy}
                  onStage={stageEntry}
                  onUnstage={unstageEntry}
                  onDiscard={discardEntry}
                  onOpen={(entry) => openEntry(entry, "working")}
                />
              ))
            )}
          </section>
        </>
      )}
    </aside>
  );
}
