import { useMemo } from "react";
import {
  bufferKey,
  useSessionStore,
  type MainSelection,
} from "../lib/sessions";

const RECENT_LIMIT = 40;

function describeBuffer(buffer: MainSelection): { title: string; subtitle: string } {
  if (buffer.type === "git-diff") {
    return { title: buffer.name, subtitle: `git · ${buffer.stage} · ${buffer.path}` };
  }
  if (buffer.type === "agent-review") {
    return { title: buffer.name, subtitle: `review · ${buffer.path}` };
  }
  if (buffer.type === "web") {
    return { title: buffer.name, subtitle: buffer.url };
  }
  return { title: buffer.name, subtitle: buffer.path };
}

export function RecentFilesList() {
  const openBuffers = useSessionStore((state) => state.openBuffers);
  const closedBufferStack = useSessionStore((state) => state.closedBufferStack);
  const bufferAccessOrder = useSessionStore((state) => state.bufferAccessOrder);
  const selectFile = useSessionStore((state) => state.selectFile);
  const activeBufferKey = useSessionStore((state) => state.activeBufferKey);

  const ordered = useMemo(() => {
    const accessRank = new Map<string, number>();
    bufferAccessOrder.forEach((key, idx) => accessRank.set(key, idx));
    const open = [...openBuffers].sort((a, b) => {
      // most-recently-used last in bufferAccessOrder, so flip for display
      return (accessRank.get(bufferKey(b)) ?? -1) - (accessRank.get(bufferKey(a)) ?? -1);
    });
    const seen = new Set(open.map(bufferKey));
    const closed = closedBufferStack.filter((buffer) => !seen.has(bufferKey(buffer)));
    return [...open, ...closed].slice(0, RECENT_LIMIT);
  }, [openBuffers, closedBufferStack, bufferAccessOrder]);

  if (ordered.length === 0) {
    return (
      <div className="sidebar-empty">
        <i className="codicon codicon-files" aria-hidden="true" />
        <p>No recent files</p>
        <span>Files you open from terminals or agents appear here.</span>
      </div>
    );
  }

  return (
    <ul className="recent-files-list" role="list" aria-label="Recent files">
      {ordered.map((buffer) => {
        const key = bufferKey(buffer);
        const { title, subtitle } = describeBuffer(buffer);
        const isActive = key === activeBufferKey;
        const isOpen = openBuffers.some((entry) => bufferKey(entry) === key);
        return (
          <li key={key}>
            <button
              type="button"
              className={`recent-files-item${isActive ? " active" : ""}${isOpen ? " open" : ""}`}
              title={subtitle}
              onClick={() => selectFile(buffer)}
            >
              <span className="recent-files-title">{title}</span>
              <span className="recent-files-subtitle">{subtitle}</span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
