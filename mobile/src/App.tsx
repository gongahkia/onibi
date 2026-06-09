import { useStoredConnections } from "./connections";
import { shortMachineId } from "./utils";
import { ConnectionSwitcher } from "./components/ConnectionSwitcher";
import { InboxView } from "./components/InboxView";
import { PairingView } from "./components/PairingView";

// re-exports kept stable for smoke.test.ts and any external consumer
export {
  approvalSupportsUpdatedInput,
  buildDecisionBody,
  commandText,
  swipeDecision,
} from "./approvals";
export { mergeConnection } from "./connections";
export {
  candidateBaseUrls,
  chooseBaseUrl,
  parsePairingInput,
} from "./pairing";
export {
  buildPaneTargetOptions,
  emergencyStopRequest,
  needsRemoteConfirmation,
  resolveRemoteTarget,
  sendRemotePresetRequest,
  sendRemoteTextRequest,
} from "./remote-pane";
export { connectionStateMessage, reconnectDelay } from "./realtime";
export { installStateBody, installStateTitle } from "./install-prompt";
export type {
  Approval,
  Connection,
  PairingPayload,
  PaneTarget,
  RunEvent,
  TransportEndpoint,
  WsState,
} from "./types";

function App() {
  const connections = useStoredConnections();
  const activeConnection = connections.activeConnection;

  if (!activeConnection) {
    return <PairingView onPaired={connections.addConnection} />;
  }

  if (activeConnection.needsRePair) {
    return (
      <main className="app-shell">
        <ConnectionSwitcher
          connections={connections.connections}
          activeId={activeConnection.id}
          onSelect={connections.selectConnection}
          onForget={() => connections.removeConnection(activeConnection.id)}
        />
        <section className="repair-panel" aria-label="Re-pair machine">
          <p className="eyebrow">Token expired</p>
          <h1>{shortMachineId(activeConnection.machineId)}</h1>
          <p className="error-line">
            {activeConnection.authError ?? "Pair this machine again to continue."}
          </p>
          <PairingView
            embedded
            onPaired={(next) => connections.replaceConnection(activeConnection.id, next)}
          />
        </section>
      </main>
    );
  }

  return (
    <InboxView
      connection={activeConnection}
      connections={connections.connections}
      onPaired={connections.addConnection}
      onForget={() => connections.removeConnection(activeConnection.id)}
      onSelectConnection={connections.selectConnection}
      onAuthInvalid={(message) => connections.markNeedsRePair(activeConnection.id, message)}
    />
  );
}

export default App;
