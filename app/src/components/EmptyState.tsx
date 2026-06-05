import { useMemo, useRef, useState, type FormEvent } from "react";
import { createPortal } from "react-dom";
import { open } from "@tauri-apps/plugin-dialog";
import { cloneGitRepository } from "../lib/git";
import { useSessionStore, workspaceIdForPath } from "../lib/sessions";
import { chooseWorkspaceFolder } from "../lib/workspace-picker";

function openCommandPalette() {
  window.dispatchEvent(new CustomEvent("onibi:open-command-palette"));
}

export function EmptyState() {
  const [cloneOpen, setCloneOpen] = useState(false);
  const workspaces = useSessionStore((state) => state.workspaces);
  const addWorkspace = useSessionStore((state) => state.addWorkspace);
  const setActiveWorkspace = useSessionStore((state) => state.setActiveWorkspace);
  const setActiveSidebarView = useSessionStore((state) => state.setActiveSidebarView);
  const recentWorkspaces = useMemo(() => workspaces.slice(0, 5), [workspaces]);

  async function handleOpenFolder() {
    const workspace = await chooseWorkspaceFolder();
    if (workspace) {
      addWorkspace(workspace);
      setActiveWorkspace(workspace.id);
      setActiveSidebarView("files");
    }
  }

  return (
    <div className="empty-state welcome" data-testid="empty-state">
      <div className="welcome-content">
        <div className="welcome-grid">
          <section className="welcome-section">
            <h2>Start</h2>
            <button type="button" className="welcome-link" onClick={openCommandPalette}>
              <i className="codicon codicon-add" aria-hidden="true" />
              <span>New Session...</span>
            </button>
            <button type="button" className="welcome-link" onClick={handleOpenFolder}>
              <i className="codicon codicon-folder-opened" aria-hidden="true" />
              <span>Open Folder...</span>
            </button>
            <button
              type="button"
              className="welcome-link"
              onClick={() => setCloneOpen(true)}
            >
              <i className="codicon codicon-source-control" aria-hidden="true" />
              <span>Clone Git Repository...</span>
            </button>
          </section>
          <section className="welcome-section">
            <h2>Recent</h2>
            {recentWorkspaces.length === 0 ? (
              <p className="welcome-muted">No recent folders</p>
            ) : (
              recentWorkspaces.map((workspace) => (
                <button
                  key={workspace.id}
                  type="button"
                  className="welcome-link"
                  onClick={() => {
                    setActiveWorkspace(workspace.id);
                    setActiveSidebarView("files");
                  }}
                  title={workspace.path}
                >
                  <i className="codicon codicon-folder" aria-hidden="true" />
                  <span>{workspace.name}</span>
                  <span className="welcome-muted welcome-path">{workspace.path}</span>
                </button>
              ))
            )}
          </section>
        </div>
      </div>
      <CloneRepositoryDialog
        open={cloneOpen}
        onClose={() => setCloneOpen(false)}
        onCloned={(workspace) => {
          addWorkspace(workspace);
          setActiveWorkspace(workspace.id);
          setActiveSidebarView("files");
          setCloneOpen(false);
        }}
      />
    </div>
  );
}

interface CloneRepositoryDialogProps {
  open: boolean;
  onClose: () => void;
  onCloned: (workspace: { id: string; path: string; name: string }) => void;
}

function defaultRepoName(remote: string): string {
  const trimmed = remote.trim().replace(/\/+$/, "").replace(/\.git$/i, "");
  const separator = Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf(":"));
  return trimmed.slice(separator + 1).replace(/[^A-Za-z0-9._-]+/g, "-");
}

function CloneRepositoryDialog({
  open: dialogOpen,
  onClose,
  onCloned,
}: CloneRepositoryDialogProps) {
  const [remote, setRemote] = useState("");
  const [destinationParent, setDestinationParent] = useState("");
  const [name, setName] = useState("");
  const [choosing, setChoosing] = useState(false);
  const [cloning, setCloning] = useState(false);
  const [error, setError] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  if (!dialogOpen) {
    return null;
  }

  const resolvedName = name.trim() || defaultRepoName(remote);

  async function chooseDestination() {
    setChoosing(true);
    setError("");
    try {
      const selected = await open({
        directory: true,
        multiple: false,
        title: "Choose clone destination",
      });
      if (selected && !Array.isArray(selected)) {
        setDestinationParent(selected);
      }
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setChoosing(false);
    }
  }

  async function clone(event: FormEvent) {
    event.preventDefault();
    if (!remote.trim()) {
      setError("Repository URL is required.");
      return;
    }
    if (!destinationParent.trim()) {
      setError("Choose a destination folder.");
      return;
    }
    setCloning(true);
    setError("");
    try {
      const result = await cloneGitRepository(
        remote.trim(),
        destinationParent.trim(),
        resolvedName,
      );
      onCloned({
        id: workspaceIdForPath(result.path),
        path: result.path,
        name: result.name,
      });
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setCloning(false);
    }
  }

  return createPortal(
    <div className="modal-backdrop" role="presentation">
      <section
        className="modal-panel clone-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="clone-dialog-title"
      >
        <header className="modal-header">
          <h2 className="modal-title" id="clone-dialog-title">
            Clone Repository
          </h2>
          <button
            type="button"
            className="icon-button"
            aria-label="Close clone dialog"
            onClick={onClose}
          >
            x
          </button>
        </header>
        <form className="modal-body" onSubmit={(event) => void clone(event)}>
          <div className="form-grid">
            <label className="field-label">
              Repository URL
              <input
                ref={inputRef}
                className="settings-input"
                value={remote}
                placeholder="https://github.com/owner/repository.git"
                onChange={(event) => setRemote(event.target.value)}
              />
            </label>
            <button
              type="button"
              className="clone-source-button"
              onClick={() => {
                if (!remote) {
                  setRemote("https://github.com/");
                }
                inputRef.current?.focus();
              }}
            >
              <span>
                <i className="codicon codicon-github" aria-hidden="true" />
                Clone from GitHub
              </span>
              <span>remote sources</span>
            </button>
            <label className="field-label">
              Destination
              <span className="workspace-picker-row">
                <input
                  className="settings-input"
                  value={destinationParent}
                  placeholder="Choose parent folder"
                  onChange={(event) => setDestinationParent(event.target.value)}
                />
                <button
                  type="button"
                  className="text-button"
                  disabled={choosing || cloning}
                  onClick={() => void chooseDestination()}
                >
                  {choosing ? "Choosing" : "Browse"}
                </button>
              </span>
            </label>
            <label className="field-label">
              Folder name
              <input
                className="settings-input"
                value={name}
                placeholder={defaultRepoName(remote) || "repository"}
                onChange={(event) => setName(event.target.value)}
              />
            </label>
            {destinationParent && resolvedName ? (
              <div className="settings-note">
                Target: {destinationParent.replace(/\/+$/, "")}/{resolvedName}
              </div>
            ) : null}
          </div>
          {error ? <div className="editor-error">{error}</div> : null}
          <footer className="dialog-actions">
            <button type="button" className="text-button" onClick={onClose}>
              Cancel
            </button>
            <button
              type="submit"
              className="text-button primary"
              disabled={cloning || !remote.trim() || !destinationParent.trim()}
            >
              {cloning ? "Cloning" : "Clone"}
            </button>
          </footer>
        </form>
      </section>
    </div>,
    document.body,
  );
}
