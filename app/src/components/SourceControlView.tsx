import { useMemo, useState } from "react";
import {
  commitGit,
  discardGitPaths,
  gitStateForEntry,
  hasStagedChange,
  hasWorkingTreeChange,
  stageGitPaths,
  syncGit,
  type GitStatus,
  type GitStatusEntry,
  unstageGitPaths,
} from "../lib/git";
import type { Workspace } from "../lib/sessions";

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
}: {
  entry: GitStatusEntry;
  busy: boolean;
  onStage: (entry: GitStatusEntry) => void;
  onUnstage: (entry: GitStatusEntry) => void;
  onDiscard: (entry: GitStatusEntry) => void;
}) {
  const state = gitStateForEntry(entry);
  const staged = hasStagedChange(entry);
  const working = hasWorkingTreeChange(entry);
  return (
    <div className="source-control-row">
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
          onClick={() => onStage(entry)}
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
          onClick={() => onUnstage(entry)}
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
        onClick={() => onDiscard(entry)}
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
  const syncLabel =
    status && (status.ahead > 0 || status.behind > 0)
      ? `Sync ${status.behind}↓ ${status.ahead}↑`
      : "Sync";

  async function runAction(action: () => Promise<unknown>) {
    if (!workspace) {
      return;
    }
    setBusy(true);
    setActionError("");
    try {
      await action();
      await onRefresh();
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

  function discardEntry(entry: GitStatusEntry) {
    if (!window.confirm(`Discard changes in ${entry.path}?`)) {
      return;
    }
    void runAction(() => discardGitPaths(workspace!.path, [entry.path]));
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
            <button
              type="button"
              className="source-control-secondary"
              disabled={busy || !status.upstream}
              onClick={() => void runAction(() => syncGit(workspace.path))}
              title={status.upstream ? "Pull with fast-forward, then push" : "No upstream configured"}
            >
              {syncLabel}
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
                />
              ))
            )}
          </section>
        </>
      )}
    </aside>
  );
}
