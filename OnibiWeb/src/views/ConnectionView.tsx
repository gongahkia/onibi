import { FormEvent, useEffect, useMemo, useState } from "react";
import { ONIBI_WEB_VERSION, type ConnectionConfig } from "../types";
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

  const handlePaste = async () => {
    setPasteFeedback(null);
    try {
      const clip = await navigator.clipboard.readText();
      applyPayload(clip, "paste");
    } catch {
      setPasteFeedback("Clipboard read denied. Paste manually into the fields below.");
    }
  };

  const handleScanned = (raw: string) => {
    if (applyPayload(raw, "scan")) {
      setScannerOpen(false);
    }
  };

  return (
    <main className="mf-page mf-page-center">
      <section className="mf-card mf-card-connection">
        <header className="mf-title-row">
          <div className="mf-logo-dot" aria-hidden="true" />
          <div>
            <h1>Onibi Web</h1>
            <p>Connect to your Ghostty terminal host.</p>
          </div>
        </header>

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

        <p className="mf-mode-hint">
          {mode === "lan"
            ? "Same Wi-Fi as the Mac. Paste the LAN URL shown in Onibi → Settings → Network Binding."
            : "Anywhere. Paste the HTTPS URL from your tunnel (cloudflared / ngrok / tailscale funnel)."}
        </p>

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
                className="button-secondary mf-token-visibility-toggle"
                onClick={() => setTokenVisible((visible) => !visible)}
                disabled={connecting}
                aria-label={tokenVisible ? "Hide pairing token" : "Show pairing token"}
                aria-pressed={tokenVisible}
              >
                {tokenVisible ? "Hide" : "Show"}
              </button>
            </div>
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

        <section className="mf-extra-block" aria-label="Import pairing payload">
          <p>Scan the QR from the Mac's Settings, or paste the JSON / <code>onibi://</code> deep link.</p>
          <div className="mf-import-actions">
            <button type="button" className="button-secondary" disabled={connecting} onClick={() => setScannerOpen(true)}>
              Scan QR
            </button>
            <button type="button" className="button-secondary" disabled={connecting} onClick={handlePaste}>
              Paste Pairing Payload
            </button>
          </div>
          {pasteFeedback && <p className="mf-paste-feedback">{pasteFeedback}</p>}
        </section>

        {scannerOpen && (
          <QRScanner onDecoded={handleScanned} onClose={() => setScannerOpen(false)} />
        )}

        {recentHosts.length > 0 && (
          <section className="mf-extra-block mf-recent-hosts" aria-label="Recently used hosts">
            <p>Recent hosts (tokens are never stored in this list):</p>
            <ul>
              {recentHosts.map((entry) => (
                <li key={entry.baseURL}>
                  <button
                    type="button"
                    className="button-secondary mf-recent-host-use"
                    onClick={() => setBaseURL(entry.baseURL)}
                    disabled={connecting}
                  >
                    Use
                  </button>
                  <code>{entry.baseURL}</code>
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
          <section className="mf-extra-block" aria-label="Additional saved connection features">
            <p>Clear the saved host URL + token from this browser.</p>
            <button type="button" className="button-secondary" disabled={connecting} onClick={onClearSaved}>
              Clear Saved Connection
            </button>
          </section>
        )}

        <footer className="mf-version-footer" aria-label="Version">
          Onibi Web v{ONIBI_WEB_VERSION}
        </footer>
      </section>
    </main>
  );
}
