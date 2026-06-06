import { useState } from "react";
import { ApprovalAuditView } from "./ApprovalAuditView";
import { SessionHistoryView } from "./SessionHistoryView";

export interface ActivityCenterProps {
  open: boolean;
  onClose: () => void;
  workspaceId?: string | null;
}

export function ActivityCenter({ open, onClose, workspaceId = null }: ActivityCenterProps) {
  const [tab, setTab] = useState<"activity" | "approvals">("activity");
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
        <div className="activity-filter-row" role="tablist" aria-label="Activity center view">
          <button
            type="button"
            className={tab === "activity" ? "active" : ""}
            aria-selected={tab === "activity"}
            role="tab"
            onClick={() => setTab("activity")}
          >
            Activity
          </button>
          <button
            type="button"
            className={tab === "approvals" ? "active" : ""}
            aria-selected={tab === "approvals"}
            role="tab"
            onClick={() => setTab("approvals")}
          >
            Approvals
          </button>
        </div>
        <div className="activity-center-body">
          {tab === "activity" ? (
            <SessionHistoryView workspaceId={workspaceId} />
          ) : (
            <ApprovalAuditView />
          )}
        </div>
      </section>
    </div>
  );
}
