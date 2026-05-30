import { SessionHistoryView } from "./SessionHistoryView";

export interface ActivityCenterProps {
  open: boolean;
  onClose: () => void;
  workspaceId?: string | null;
}

export function ActivityCenter({ open, onClose, workspaceId = null }: ActivityCenterProps) {
  if (!open) {
    return null;
  }

  return (
    <div className="modal-backdrop activity-center" role="presentation">
      <section
        className="modal-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="activity-center-title"
      >
        <header className="modal-header">
          <h2 className="modal-title" id="activity-center-title">
            Activity
          </h2>
          <button
            type="button"
            className="icon-button"
            aria-label="Close activity"
            onClick={onClose}
          >
            x
          </button>
        </header>
        <div className="activity-center-body">
          <SessionHistoryView workspaceId={workspaceId} />
        </div>
      </section>
    </div>
  );
}
