import type { Connection } from "../types";
import { shortMachineId } from "../utils";

export function ConnectionSwitcher({
  connections,
  activeId,
  onSelect,
  onForget,
}: {
  connections: Connection[];
  activeId: string;
  onSelect: (id: string) => void;
  onForget: () => void;
}) {
  if (connections.length <= 1) {
    return null;
  }
  return (
    <div className="machine-switcher">
      <select
        aria-label="Paired machine"
        value={activeId}
        onChange={(event) => onSelect(event.target.value)}
      >
        {connections.map((connection) => (
          <option key={connection.id} value={connection.id}>
            {shortMachineId(connection.machineId)}
            {connection.needsRePair ? " - re-pair" : ""}
          </option>
        ))}
      </select>
      <button type="button" className="ghost-button" onClick={onForget}>
        Remove
      </button>
    </div>
  );
}
