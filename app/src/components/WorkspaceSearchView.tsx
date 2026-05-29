import { useEffect, useMemo, useState } from "react";
import { searchWorkspace, type WorkspaceSearchResult } from "../lib/workspace-search";
import { useSessionStore, type Session, type Workspace } from "../lib/sessions";

function activeWorkspaceFor(
  sessions: Session[],
  workspaces: Workspace[],
  activeSessionId: string | null,
): Workspace | null {
  const activeSession = sessions.find((session) => session.id === activeSessionId);
  return workspaces.find((workspace) => workspace.id === activeSession?.workspaceId) ?? null;
}

function relativePath(root: string, path: string): string {
  return path.startsWith(`${root}/`) ? path.slice(root.length + 1) : path;
}

function basename(path: string): string {
  return path.split("/").pop() ?? path;
}

export function WorkspaceSearchView() {
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const selectFile = useSessionStore((state) => state.selectFile);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<WorkspaceSearchResult[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const workspace = useMemo(
    () => activeWorkspaceFor(sessions, workspaces, activeSessionId),
    [activeSessionId, sessions, workspaces],
  );

  useEffect(() => {
    if (!workspace || query.trim().length < 2) {
      setResults([]);
      setError("");
      setLoading(false);
      return;
    }
    let cancelled = false;
    const timer = window.setTimeout(() => {
      setLoading(true);
      setError("");
      void searchWorkspace(workspace.path, query)
        .then((nextResults) => {
          if (!cancelled) {
            setResults(nextResults);
          }
        })
        .catch((caught) => {
          if (!cancelled) {
            setResults([]);
            setError(caught instanceof Error ? caught.message : String(caught));
          }
        })
        .finally(() => {
          if (!cancelled) {
            setLoading(false);
          }
        });
    }, 180);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [query, workspace]);

  if (!workspace) {
    return (
      <section className="workspace-search-view">
        <div className="source-control-empty">Start a session to search a workspace.</div>
      </section>
    );
  }

  return (
    <section className="workspace-search-view" aria-label="Workspace search">
      <div className="workspace-search-header">
        <input
          className="filter-input"
          value={query}
          placeholder="Search files..."
          aria-label="Search workspace text"
          onChange={(event) => setQuery(event.target.value)}
        />
        <span className="settings-note">
          {loading ? "Searching" : `${results.length} results`}
        </span>
      </div>
      {error ? <div className="context-menu-error">{error}</div> : null}
      <div className="workspace-search-results">
        {query.trim().length < 2 ? (
          <div className="source-control-empty">Enter at least 2 characters.</div>
        ) : results.length === 0 && !loading ? (
          <div className="source-control-empty">No matches.</div>
        ) : (
          results.map((result) => (
            <button
              key={`${result.path}:${result.line}:${result.column}`}
              type="button"
              className="workspace-search-result"
              onClick={() =>
                selectFile({
                  workspaceId: workspace.id,
                  workspaceRoot: workspace.path,
                  path: result.path,
                  name: basename(result.path),
                  size: 0,
                })
              }
            >
              <span className="workspace-search-path">
                {relativePath(workspace.path, result.path)}
              </span>
              <span className="workspace-search-location">
                {result.line}:{result.column}
              </span>
              <span className="workspace-search-preview">{result.preview}</span>
            </button>
          ))
        )}
      </div>
    </section>
  );
}
