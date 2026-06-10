import { useEffect, useState } from "react";
import {
  ONIBI_RELEASES_URL,
  UPDATE_CHECK_EVENT,
  checkForAppUpdate,
  installPendingAppUpdate,
  recordUpdateCheck,
  shouldAutoCheckForUpdates,
  type AppUpdateCheckResult,
  type AppUpdateProgress,
} from "../lib/app-updater";

type UpdateState =
  | { status: "idle"; result: AppUpdateCheckResult | null }
  | { status: "checking"; result: AppUpdateCheckResult | null }
  | { status: "ready"; result: AppUpdateCheckResult }
  | { status: "installing"; result: AppUpdateCheckResult; progress: AppUpdateProgress | null }
  | { status: "error"; result: AppUpdateCheckResult | null; error: string };

function messageForError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function UpdateDialog() {
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<UpdateState>({
    status: "idle",
    result: null,
  });

  async function runCheck(showWhenCurrent: boolean) {
    setState((current) => ({ status: "checking", result: current.result }));
    try {
      const result = await checkForAppUpdate();
      recordUpdateCheck();
      if (result.available || showWhenCurrent) {
        setOpen(true);
      }
      setState({ status: "ready", result });
    } catch (error) {
      if (showWhenCurrent) {
        setOpen(true);
      }
      setState({
        status: "error",
        result: null,
        error: messageForError(error),
      });
    }
  }

  useEffect(() => {
    function handleCheckEvent() {
      setOpen(true);
      void runCheck(true);
    }
    window.addEventListener(UPDATE_CHECK_EVENT, handleCheckEvent);
    return () => window.removeEventListener(UPDATE_CHECK_EVENT, handleCheckEvent);
  }, []);

  useEffect(() => {
    if (shouldAutoCheckForUpdates()) {
      void runCheck(false);
    }
  }, []);

  async function install() {
    if (state.status !== "ready" || !state.result.available) {
      return;
    }
    setState({ status: "installing", result: state.result, progress: null });
    try {
      await installPendingAppUpdate((progress) => {
        setState({
          status: "installing",
          result: state.result,
          progress,
        });
      });
    } catch (error) {
      setState({
        status: "error",
        result: state.result,
        error: messageForError(error),
      });
    }
  }

  if (!open) {
    return null;
  }

  const result = state.result;
  const updateAvailable = result?.available === true;
  const progress =
    state.status === "installing" && state.progress
      ? formatProgress(state.progress)
      : null;

  return (
    <div className="modal-backdrop" role="presentation">
      <section
        className="modal-panel update-dialog"
        role="dialog"
        aria-modal="true"
        aria-label="Software update"
      >
        <header className="modal-header">
          <h2 className="modal-title">Software Update</h2>
          <button
            type="button"
            className="icon-button"
            aria-label="Close update dialog"
            onClick={() => setOpen(false)}
            disabled={state.status === "installing"}
          >
            x
          </button>
        </header>
        <div className="update-dialog-body">
          {state.status === "checking" ? <p>Checking for updates...</p> : null}
          {state.status === "error" ? (
            <div className="editor-error">{state.error}</div>
          ) : null}
          {state.status === "ready" && !updateAvailable ? (
            <p>Onibi is up to date ({state.result.currentVersion}).</p>
          ) : null}
          {result?.available ? (
            <>
              <p>
                Onibi {result.version} is available. You have{" "}
                {result.currentVersion}.
              </p>
              {result.date ? <p>Published {result.date}</p> : null}
              {result.body ? (
                <pre className="update-notes">{result.body}</pre>
              ) : null}
            </>
          ) : null}
          {progress ? <p>{progress}</p> : null}
        </div>
        <footer className="modal-footer">
          <button
            type="button"
            className="text-button"
            onClick={() => window.open(result?.releaseUrl ?? ONIBI_RELEASES_URL, "_blank")}
          >
            Open Release
          </button>
          <button
            type="button"
            className="text-button"
            onClick={() => setOpen(false)}
            disabled={state.status === "installing"}
          >
            Later
          </button>
          <button
            type="button"
            className="text-button primary"
            disabled={!updateAvailable || state.status === "installing"}
            onClick={() => void install()}
          >
            {state.status === "installing" ? "Installing..." : "Install and Relaunch"}
          </button>
        </footer>
      </section>
    </div>
  );
}

function formatProgress(progress: AppUpdateProgress): string {
  if (progress.phase === "finished") {
    return "Download complete.";
  }
  if (progress.totalBytes && progress.totalBytes > 0) {
    const percent = Math.min(
      100,
      Math.round((progress.downloadedBytes / progress.totalBytes) * 100),
    );
    return `Downloading ${percent}%`;
  }
  return "Downloading update...";
}
