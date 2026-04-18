import { FormEvent, useEffect, useMemo, useState } from "react";
import type { ConnectionConfig } from "../types";
import { forgetHost, loadRecentHosts, type RecentHost } from "../store/recentHosts";
import { QRScanner } from "../components/QRScanner";
import { parsePairingPayload } from "../lib/pairingPayload";

interface ConnectionViewProps {
  initialConnection: ConnectionConfig | null;
  initialRememberToken: boolean;
  connecting: boolean;
  errorMessage: string | null;
  onConnect: (connection: ConnectionConfig, rememberToken: boolean) => void;
  onClearSaved: () => void;
}

type ConnectionMode = "lan" | "tunnel";

const LAN_PLACEHOLDER = "http://192.168.1.20:8787";
const TUNNEL_PLACEHOLDER = "https://your-tunnel.trycloudflare.com";


function detectMixedContent(pageProtocol: string, baseURL: string): boolean {
  if (pageProtocol !== "https:") return false;
  try {
    const parsed = new URL(baseURL);
    return parsed.protocol === "http:";
  } catch {
    return false;
  }
}

export function ConnectionView({
  initialConnection,
  initialRememberToken,
  connecting,
  errorMessage,
  onConnect,
  onClearSaved
}: ConnectionViewProps): JSX.Element {
  const initialMode: ConnectionMode = useMemo(() => {
    const candidate = initialConnection?.baseURL ?? "";
    return candidate.startsWith("https://") ? "tunnel" : "lan";
  }, [initialConnection]);

  const [mode, setMode] = useState<ConnectionMode>(initialMode);
  const [baseURL, setBaseURL] = useState(initialConnection?.baseURL ?? "http://127.0.0.1:8787");
  const [token, setToken] = useState(initialConnection?.token ?? "");
  const [rememberToken, setRememberToken] = useState(initialRememberToken);
  const [tokenVisible, setTokenVisible] = useState(false);
  const [pasteFeedback, setPasteFeedback] = useState<string | null>(null);
  const [recentHosts, setRecentHosts] = useState<RecentHost[]>(() => loadRecentHosts());
  const [scannerOpen, setScannerOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);

  useEffect(() => {
    setBaseURL(initialConnection?.baseURL ?? "http://127.0.0.1:8787");
    setToken(initialConnection?.token ?? "");
    setTokenVisible(false);
  }, [initialConnection]);

  useEffect(() => {
    setRememberToken(initialRememberToken);
  }, [initialRememberToken]);

  const mixedContent = detectMixedContent(window.location.protocol, baseURL);

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedURL = baseURL.trim();
    const trimmedToken = token.replace(/\s+/g, "");
    if (trimmedURL !== baseURL) setBaseURL(trimmedURL);
    if (trimmedToken !== token) setToken(trimmedToken);
    onConnect({ baseURL: trimmedURL, token: trimmedToken }, rememberToken);
  };

  const applyPayload = (raw: string, source: "paste" | "scan") => {
    const parsed = parsePairingPayload(raw);
    if (!parsed) {
      setPasteFeedback(
        source === "scan"
          ? "Scanned code is not an Onibi pairing payload."
          : "Clipboard does not contain an Onibi pairing payload."
      );
      return false;
    }
    setBaseURL(parsed.baseURL);
    setToken(parsed.token);
    setMode(parsed.baseURL.startsWith("https://") ? "tunnel" : "lan");
    setPasteFeedback(source === "scan" ? "Pairing payload scanned." : "Pairing payload imported.");
    return true;
  };

  const handleScanned = (raw: string) => {
    if (applyPayload(raw, "scan")) {
      setScannerOpen(false);
    }
  };

  return (
    <main className="mf-page mf-page-center">
      <button
        type="button"
        className="mf-help-pill"
        aria-label="Open help"
        aria-expanded={helpOpen}
        onClick={() => setHelpOpen(true)}
      >
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <circle cx="12" cy="12" r="9" />
          <path d="M9.5 9a2.5 2.5 0 1 1 3.5 2.3c-.9.4-1.5 1-1.5 2" />
          <path d="M12 17h.01" />
        </svg>
      </button>

      <section className="mf-card-connection">
        <header className="mf-title-row">
          <h1>Onibi Web</h1>
          <p>Connect to your Ghostty terminal host.</p>
        </header>

        <div className="mf-mode-block">
          <div className="mf-mode-switcher" role="tablist" aria-label="Connection mode">
            <button
              type="button"
              role="tab"
              aria-selected={mode === "lan"}
              className={mode === "lan" ? "mf-mode-active" : undefined}
              onClick={() => setMode("lan")}
              disabled={connecting}
            >
              LAN
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={mode === "tunnel"}
              className={mode === "tunnel" ? "mf-mode-active" : undefined}
              onClick={() => setMode("tunnel")}
              disabled={connecting}
            >
              Tunnel
            </button>
          </div>
        </div>

        <form className="mf-form" onSubmit={handleSubmit}>
          <label className="mf-field">
            <span>Host URL</span>
            <input
              type="url"
              value={baseURL}
              onChange={(event) => setBaseURL(event.target.value)}
              placeholder={mode === "lan" ? LAN_PLACEHOLDER : TUNNEL_PLACEHOLDER}
              autoCapitalize="off"
              autoCorrect="off"
              spellCheck={false}
              required
              disabled={connecting}
            />
          </label>

          <label className="mf-field">
            <span>Pairing Token</span>
            <div className="mf-token-input-row">
              <input
                type={tokenVisible ? "text" : "password"}
                value={token}
                onChange={(event) => setToken(event.target.value)}
                placeholder="Paste your latest token"
                autoCapitalize="off"
                autoCorrect="off"
                spellCheck={false}
                required
                disabled={connecting}
              />
              <button
                type="button"
                className="mf-token-icon-btn mf-token-scan-btn"
                onClick={() => { setPasteFeedback(null); setScannerOpen(true); }}
                disabled={connecting}
                aria-label="Scan pairing QR with camera"
              >
                <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="M4 8V6a2 2 0 0 1 2-2h2" />
                  <path d="M16 4h2a2 2 0 0 1 2 2v2" />
                  <path d="M20 16v2a2 2 0 0 1-2 2h-2" />
                  <path d="M8 20H6a2 2 0 0 1-2-2v-2" />
                  <circle cx="12" cy="12" r="3" />
                </svg>
              </button>
              <button
                type="button"
                className="mf-token-icon-btn mf-token-visibility-toggle"
                onClick={() => setTokenVisible((visible) => !visible)}
                disabled={connecting}
                aria-label={tokenVisible ? "Hide pairing token" : "Show pairing token"}
                aria-pressed={tokenVisible}
              >
                {tokenVisible ? (
                  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="M3 3l18 18" />
                    <path d="M10.58 10.58a2 2 0 0 0 2.83 2.83" />
                    <path d="M9.88 4.24A10.9 10.9 0 0 1 12 4c5 0 9.27 3.11 11 8-.53 1.5-1.37 2.86-2.44 4" />
                    <path d="M6.1 6.1C4.12 7.4 2.6 9.5 1.72 12c1.73 4.89 6 8 11 8a11 11 0 0 0 4.52-.96" />
                  </svg>
                ) : (
                  <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="M1.72 12C3.45 7.11 7.72 4 12 4s8.55 3.11 10.28 8c-1.73 4.89-6 8-10.28 8S3.45 16.89 1.72 12z" />
                    <circle cx="12" cy="12" r="3" />
                  </svg>
                )}
              </button>
            </div>
            {pasteFeedback && <p className="mf-paste-feedback">{pasteFeedback}</p>}
          </label>

          <label className="mf-checkbox-row">
            <input
              type="checkbox"
              checked={rememberToken}
              onChange={(event) => setRememberToken(event.target.checked)}
              disabled={connecting}
            />
            <span>Remember token on this device</span>
          </label>

          {mixedContent && (
            <p className="mf-alert mf-alert-warning" role="alert">
              This page is loaded over HTTPS but the Host URL uses HTTP. Browsers will block the request.
              Use the tunnel's HTTPS URL, or open this page over HTTP on LAN.
            </p>
          )}

          {errorMessage && (
            <p className="mf-alert mf-alert-error" role="alert">
              {errorMessage}
            </p>
          )}

          <button type="submit" disabled={connecting || baseURL.trim() === "" || token.trim() === ""}>
            {connecting ? "Connecting..." : "Connect"}
          </button>
        </form>

        {scannerOpen && (
          <QRScanner onDecoded={handleScanned} onClose={() => setScannerOpen(false)} />
        )}

        {helpOpen && (
          <div
            className="mf-help-overlay"
            role="dialog"
            aria-modal="true"
            aria-label="Help"
            onClick={() => setHelpOpen(false)}
          >
            <div className="mf-help-panel" onClick={(event) => event.stopPropagation()}>
              <header className="mf-help-header">
                <h2>Help</h2>
                <button
                  type="button"
                  className="mf-help-close"
                  onClick={() => setHelpOpen(false)}
                  aria-label="Close help"
                >
                  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="M18 6L6 18M6 6l12 12" />
                  </svg>
                </button>
              </header>
              <div className="mf-help-body">
                <section>
                  <h3>LAN</h3>
                  <p>Same Wi-Fi as the Mac. Paste the LAN URL from Onibi &rarr; Settings &rarr; Network Binding.</p>
                </section>
                <section>
                  <h3>Tunnel</h3>
                  <p>Anywhere. Paste the HTTPS URL from your tunnel (cloudflared / ngrok / tailscale).</p>
                </section>
                <section>
                  <h3>QR / Deep link</h3>
                  <p>
                    Tap the camera icon in the Pairing Token field to scan the QR shown in Onibi &rarr; Settings &rarr; Mobile Access.
                    Both Host URL and Token auto-fill from the payload.
                  </p>
                  <p>
                    Deep link format: <code>onibi://pair?...</code>. You can also paste a full payload directly into the Host URL or Token field.
                  </p>
                </section>
              </div>
            </div>
          </div>
        )}

        {recentHosts.length > 0 && (
          <section className="mf-extra-block mf-recent-hosts" aria-label="Recently used hosts">
            <p>Recent hosts</p>
            <ul>
              {recentHosts.map((entry) => (
                <li key={entry.baseURL}>
                  <code>{entry.baseURL}</code>
                  <button
                    type="button"
                    className="button-secondary mf-recent-host-use"
                    onClick={() => setBaseURL(entry.baseURL)}
                    disabled={connecting}
                  >
                    Use
                  </button>
                  <button
                    type="button"
                    className="mf-recent-host-forget"
                    onClick={() => setRecentHosts(forgetHost(entry.baseURL))}
                    aria-label={`Forget ${entry.baseURL}`}
                    disabled={connecting}
                  >
                    ×
                  </button>
                </li>
              ))}
            </ul>
          </section>
        )}

        {initialConnection && (
          <section className="mf-extra-block" aria-label="Saved connection">
            <button type="button" className="button-secondary" disabled={connecting} onClick={onClearSaved}>
              Clear Saved Connection
            </button>
          </section>
        )}

      </section>
    </main>
  );
}
