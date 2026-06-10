import { useEffect, useMemo, useState } from "react";
import {
  spawnRemoteSshSession,
  type RemoteKeybindingPolicy,
  type TerminalPanePlacement,
  useSessionStore,
} from "../lib/sessions";
import { chooseWorkspaceFolder } from "../lib/workspace-picker";

export interface RemoteSessionDialogProps {
  open: boolean;
  onClose: () => void;
  defaultWorkspaceId?: string | null;
  placement?: TerminalPanePlacement | null;
}

export function RemoteSessionDialog({
  open,
  onClose,
  defaultWorkspaceId,
  placement,
}: RemoteSessionDialogProps) {
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeWorkspaceId = useSessionStore((state) => state.activeWorkspaceId);
  const settings = useSessionStore((state) => state.settings);
  const addWorkspace = useSessionStore((state) => state.addWorkspace);
  const [target, setTarget] = useState("");
  const [workspaceId, setWorkspaceId] = useState(
    () => defaultWorkspaceId ?? activeWorkspaceId ?? workspaces[0]?.id ?? "",
  );
  const [remoteCwd, setRemoteCwd] = useState("");
  const [title, setTitle] = useState("");
  const [keybindingPolicy, setKeybindingPolicy] =
    useState<RemoteKeybindingPolicy>(() => settings.remoteKeybindingPolicy);
  const [choosingWorkspace, setChoosingWorkspace] = useState(false);
  const [spawning, setSpawning] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const selectedWorkspace = useMemo(
    () => workspaces.find((workspace) => workspace.id === workspaceId) ?? null,
    [workspaceId, workspaces],
  );

  useEffect(() => {
    if (!open) {
      return;
    }
    const preferredWorkspaceId = defaultWorkspaceId ?? activeWorkspaceId;
    if (preferredWorkspaceId && workspaces.some((item) => item.id === preferredWorkspaceId)) {
      setWorkspaceId(preferredWorkspaceId);
    } else if (!workspaces.some((item) => item.id === workspaceId)) {
      setWorkspaceId(workspaces[0]?.id ?? "");
    }
    setKeybindingPolicy(settings.remoteKeybindingPolicy);
  }, [
    activeWorkspaceId,
    defaultWorkspaceId,
    open,
    settings.remoteKeybindingPolicy,
    workspaceId,
    workspaces,
  ]);

  if (!open) {
    return null;
  }

  async function chooseWorkspace() {
    setError(null);
    setChoosingWorkspace(true);
    try {
      const workspace = await chooseWorkspaceFolder();
      if (!workspace) {
        return;
      }
      addWorkspace(workspace);
      setWorkspaceId(workspace.id);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setChoosingWorkspace(false);
    }
  }

  async function handleStart() {
    setError(null);
    setSpawning(true);
    try {
      if (!selectedWorkspace) {
        throw new Error("Choose a local workspace container.");
      }
      await spawnRemoteSshSession(
        selectedWorkspace,
        {
          target,
          remoteCwd: remoteCwd.trim() || null,
          title: title.trim() || null,
          keybindingPolicy,
        },
        placement,
      );
      setTarget("");
      setRemoteCwd("");
      setTitle("");
      onClose();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setSpawning(false);
    }
  }

  return (
    <div className="modal-backdrop" role="presentation">
      <section
        className="modal-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="remote-session-title"
      >
        <header className="modal-header">
          <h2 className="modal-title" id="remote-session-title">
            New Remote SSH Session
          </h2>
          <button
            type="button"
            className="icon-button"
            aria-label="Close remote SSH dialog"
            onClick={onClose}
          >
            x
          </button>
        </header>
        <div className="modal-body">
          <div className="form-grid">
            <label className="field-label">
              SSH target
              <input
                className="settings-input"
                value={target}
                placeholder="ssh://user@example.com:2222/workspace"
                autoFocus
                onChange={(event) => setTarget(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    void handleStart();
                  }
                }}
              />
            </label>
            <label className="field-label">
              Local workspace container
              <span className="workspace-picker-row">
                <select
                  className="settings-select"
                  value={workspaceId}
                  disabled={workspaces.length === 0}
                  onChange={(event) => setWorkspaceId(event.target.value)}
                >
                  {workspaces.length === 0 ? (
                    <option value="">No workspace selected</option>
                  ) : null}
                  {workspaces.map((workspace) => (
                    <option key={workspace.id} value={workspace.id}>
                      {workspace.name}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  className="text-button"
                  disabled={choosingWorkspace}
                  onClick={() => void chooseWorkspace()}
                >
                  {choosingWorkspace ? "Choosing" : "Choose Folder"}
                </button>
              </span>
            </label>
            {selectedWorkspace ? (
              <div className="settings-note">{selectedWorkspace.path}</div>
            ) : null}
            <label className="field-label">
              Remote directory
              <input
                className="settings-input"
                value={remoteCwd}
                placeholder="Optional, for example /srv/app"
                onChange={(event) => setRemoteCwd(event.target.value)}
              />
            </label>
            <label className="field-label">
              Title
              <input
                className="settings-input"
                value={title}
                placeholder="Optional"
                onChange={(event) => setTitle(event.target.value)}
              />
            </label>
            <label className="field-label">
              Keybindings
              <select
                className="settings-select"
                value={keybindingPolicy}
                onChange={(event) =>
                  setKeybindingPolicy(event.target.value as RemoteKeybindingPolicy)
                }
              >
                <option value="local">Onibi handles terminal shortcuts</option>
                <option value="remote">Remote terminal receives shortcuts</option>
              </select>
            </label>
            <div className="settings-note">SSH command: {settings.remoteSshCommand}</div>
            {error ? <div className="form-error">{error}</div> : null}
          </div>
        </div>
        <footer className="modal-footer">
          <button type="button" className="text-button" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className="text-button primary"
            disabled={spawning || !target.trim() || !selectedWorkspace}
            onClick={() => void handleStart()}
          >
            {spawning ? "Starting" : "Start"}
          </button>
        </footer>
      </section>
    </div>
  );
}
