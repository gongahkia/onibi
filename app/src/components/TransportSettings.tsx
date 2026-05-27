import { useCallback, useEffect, useMemo, useState, type CSSProperties } from "react";
import {
  disableTransport,
  enableTransport,
  fetchLanCertQr,
  fetchTransportStatus,
  type TransportSnapshot,
} from "../lib/transports";

interface TransportSettingsProps {
  pollIntervalMs?: number;
}

export function TransportSettings({ pollIntervalMs = 5000 }: TransportSettingsProps) {
  const [transports, setTransports] = useState<TransportSnapshot[]>([]);
  const [busyName, setBusyName] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [lanQrUrl, setLanQrUrl] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const next = await fetchTransportStatus();
      setTransports(next);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    void refresh();
    const id = window.setInterval(() => {
      void refresh();
    }, pollIntervalMs);
    return () => window.clearInterval(id);
  }, [pollIntervalMs, refresh]);

  useEffect(() => {
    return () => {
      if (lanQrUrl) {
        URL.revokeObjectURL(lanQrUrl);
      }
    };
  }, [lanQrUrl]);

  const sorted = useMemo(() => {
    const order = ["tailscale-funnel", "cloudflared", "lan"];
    return [...transports].sort((a, b) => order.indexOf(a.name) - order.indexOf(b.name));
  }, [transports]);

  async function toggle(snapshot: TransportSnapshot) {
    setBusyName(snapshot.name);
    try {
      if (snapshot.enabled) {
        await disableTransport(snapshot.name);
      } else {
        await enableTransport(snapshot.name);
      }
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyName(null);
    }
  }

  async function toggleLanQr() {
    if (lanQrUrl) {
      URL.revokeObjectURL(lanQrUrl);
      setLanQrUrl(null);
      return;
    }
    try {
      setLanQrUrl(await fetchLanCertQr());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  return (
    <section style={styles.panel} aria-label="Transport settings">
      <div style={styles.header}>
        <div>
          <h2 style={styles.title}>Transports</h2>
          <p style={styles.subtitle}>Expose this Onibi daemon through one or more reachable paths.</p>
        </div>
        <button type="button" style={styles.secondaryButton} onClick={() => void refresh()}>
          Refresh
        </button>
      </div>

      {error ? <div style={styles.error}>{error}</div> : null}

      <div style={styles.list}>
        {sorted.map((snapshot) => (
          <article key={snapshot.name} style={styles.row}>
            <div style={styles.rowMain}>
              <div style={styles.labelLine}>
                <strong>{snapshot.label}</strong>
                {snapshot.requiresExternalDep ? (
                  <span style={styles.dep}>requires {snapshot.requiresExternalDep}</span>
                ) : null}
              </div>
              <div style={styles.status}>{statusText(snapshot)}</div>
              {snapshot.url ? <div style={styles.url}>{snapshot.url}</div> : null}
              {snapshot.fingerprint ? <div style={styles.fingerprint}>{snapshot.fingerprint}</div> : null}
            </div>
            <div style={styles.actions}>
              {snapshot.name === "lan" ? (
                <button type="button" style={styles.secondaryButton} onClick={() => void toggleLanQr()}>
                  {lanQrUrl ? "Hide Cert QR" : "Show Cert QR"}
                </button>
              ) : null}
              <button
                type="button"
                aria-label={`${snapshot.enabled ? "Disable" : "Enable"} ${snapshot.label}`}
                aria-pressed={snapshot.enabled}
                disabled={busyName === snapshot.name}
                style={{
                  ...styles.toggle,
                  ...(snapshot.enabled ? styles.toggleOn : styles.toggleOff),
                }}
                onClick={() => void toggle(snapshot)}
              >
                {busyName === snapshot.name ? "Working" : snapshot.enabled ? "Enabled" : "Enable"}
              </button>
            </div>
          </article>
        ))}
      </div>

      {lanQrUrl ? (
        <div style={styles.qrPanel}>
          <img src={lanQrUrl} alt="LAN certificate install QR" style={styles.qrImage} />
        </div>
      ) : null}
    </section>
  );
}

function statusText(snapshot: TransportSnapshot): string {
  switch (snapshot.status.state) {
    case "running":
      return snapshot.status.url ? `Running at ${snapshot.status.url}` : "Running";
    case "starting":
      return "Starting";
    case "failed":
      return snapshot.status.message;
    case "stopped":
      return "Stopped";
  }
}

const styles: Record<string, CSSProperties> = {
  panel: {
    display: "grid",
    gap: 16,
    color: "#e6edf3",
    background: "#111827",
    border: "1px solid #273142",
    borderRadius: 8,
    padding: 18,
    maxWidth: 920,
  },
  header: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 16,
  },
  title: {
    margin: 0,
    fontSize: 20,
    lineHeight: 1.2,
    letterSpacing: 0,
  },
  subtitle: {
    margin: "6px 0 0",
    color: "#9ca3af",
    fontSize: 13,
    lineHeight: 1.45,
  },
  list: {
    display: "grid",
    gap: 10,
  },
  row: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 16,
    border: "1px solid #273142",
    borderRadius: 8,
    padding: 14,
    background: "#0f1624",
  },
  rowMain: {
    minWidth: 0,
    display: "grid",
    gap: 6,
  },
  labelLine: {
    display: "flex",
    alignItems: "center",
    gap: 10,
    flexWrap: "wrap",
    fontSize: 14,
  },
  dep: {
    color: "#9ca3af",
    fontSize: 12,
  },
  status: {
    color: "#cbd5e1",
    fontSize: 13,
  },
  url: {
    color: "#93c5fd",
    font: "12px/1.4 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
    overflowWrap: "anywhere",
  },
  fingerprint: {
    color: "#a7f3d0",
    font: "12px/1.4 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
    overflowWrap: "anywhere",
  },
  actions: {
    display: "flex",
    alignItems: "center",
    gap: 10,
    flexWrap: "wrap",
    justifyContent: "flex-end",
  },
  toggle: {
    border: 0,
    borderRadius: 8,
    minHeight: 44,
    minWidth: 96,
    padding: "0 16px",
    color: "#06111f",
    fontWeight: 700,
    cursor: "pointer",
  },
  toggleOn: {
    background: "#86efac",
  },
  toggleOff: {
    background: "#d1d5db",
  },
  secondaryButton: {
    border: "1px solid #384456",
    borderRadius: 8,
    minHeight: 40,
    padding: "0 12px",
    background: "#172033",
    color: "#dbeafe",
    cursor: "pointer",
  },
  error: {
    border: "1px solid #7f1d1d",
    borderRadius: 8,
    padding: 12,
    background: "#2b1115",
    color: "#fecaca",
    fontSize: 13,
  },
  qrPanel: {
    border: "1px solid #273142",
    borderRadius: 8,
    padding: 12,
    width: "fit-content",
    background: "#ffffff",
  },
  qrImage: {
    display: "block",
    width: 220,
    height: 220,
  },
};
