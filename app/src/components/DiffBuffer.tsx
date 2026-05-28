import { useEffect, useState } from "react";
import {
  acceptAgentReview,
  getAgentReviewDiff,
  rejectAgentReview,
} from "../lib/agent-review";
import { getGitFileDiff } from "../lib/git";
import {
  useSessionStore,
  type DiffViewMode,
  type SelectedAgentReview,
  type SelectedGitDiff,
} from "../lib/sessions";
import { DiffViewer, type DiffContent } from "./DiffViewer";

type DiffState =
  | { status: "loading" }
  | { status: "ready"; diff: DiffContent }
  | { status: "error"; error: string };

export function GitDiffBuffer({
  selection,
  mode,
}: {
  selection: SelectedGitDiff;
  mode: DiffViewMode;
}) {
  const selectFile = useSessionStore((state) => state.selectFile);
  const [state, setState] = useState<DiffState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    void getGitFileDiff(selection.workspaceRoot, selection.path, selection.stage)
      .then((diff) => {
        if (!cancelled) {
          setState({ status: "ready", diff });
        }
      })
      .catch((caught) => {
        if (!cancelled) {
          setState({
            status: "error",
            error: caught instanceof Error ? caught.message : String(caught),
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [selection.path, selection.stage, selection.workspaceRoot]);

  if (state.status === "loading") {
    return <div className="editor-message">Loading diff</div>;
  }
  if (state.status === "error") {
    return <div className="editor-error">{state.error}</div>;
  }
  return (
    <DiffViewer
      diff={state.diff}
      mode={mode}
      actions={
        <button type="button" className="text-button" onClick={() => selectFile(null)}>
          Close
        </button>
      }
    />
  );
}

export function AgentReviewBuffer({
  selection,
  mode,
}: {
  selection: SelectedAgentReview;
  mode: DiffViewMode;
}) {
  const selectFile = useSessionStore((state) => state.selectFile);
  const [state, setState] = useState<DiffState>({ status: "loading" });
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    setActionError("");
    void getAgentReviewDiff(selection.workspaceRoot, selection.path)
      .then((diff) => {
        if (!cancelled) {
          setState({ status: "ready", diff });
        }
      })
      .catch((caught) => {
        if (!cancelled) {
          setState({
            status: "error",
            error: caught instanceof Error ? caught.message : String(caught),
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [selection.path, selection.workspaceRoot]);

  async function run(action: () => Promise<void>) {
    setBusy(true);
    setActionError("");
    try {
      await action();
      selectFile(null);
    } catch (caught) {
      setActionError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setBusy(false);
    }
  }

  if (state.status === "loading") {
    return <div className="editor-message">Loading agent diff</div>;
  }
  if (state.status === "error") {
    return <div className="editor-error">{state.error}</div>;
  }
  return (
    <>
      {actionError ? <div className="editor-error">{actionError}</div> : null}
      <DiffViewer
        diff={state.diff}
        mode={mode}
        actions={
          <>
            <button
              type="button"
              className="text-button"
              disabled={busy}
              onClick={() =>
                void run(() =>
                  acceptAgentReview(selection.workspaceRoot, selection.path),
                )
              }
            >
              Accept
            </button>
            <button
              type="button"
              className="text-button danger"
              disabled={busy}
              onClick={() =>
                void run(() =>
                  rejectAgentReview(selection.workspaceRoot, selection.path),
                )
              }
            >
              Reject
            </button>
            <button type="button" className="text-button" onClick={() => selectFile(null)}>
              Close
            </button>
          </>
        }
      />
    </>
  );
}
