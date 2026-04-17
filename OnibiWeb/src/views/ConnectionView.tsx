import { FormEvent, useEffect, useMemo, useState } from "react";
import type { ConnectionConfig } from "../types";

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

/**
 * Accepts either:
 *   - JSON payload: {"type":"onibi_pairing","baseURL":"...","token":"..."}
 *   - Deep link:   onibi://pair?base=<urlencoded>&token=<urlencoded>
 */
function parsePastedPayload(raw: string): { baseURL: string; token: string } | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;

  try {
    const parsed = JSON.parse(trimmed);
    if (parsed && typeof parsed === "object" && typeof parsed.baseURL === "string" && typeof parsed.token === "string") {
      return { baseURL: parsed.baseURL, token: parsed.token };
    }
  } catch {
    // not JSON, fall through
  }

  if (trimmed.startsWith("onibi://")) {
    try {
      const url = new URL(trimmed);
      const base = url.searchParams.get("base");
      const token = url.searchParams.get("token");
      if (base && token) {
        return { baseURL: base, token };
      }
    } catch {
      return null;
    }
  }

  return null;
}

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

  const handlePaste = async () => {
    setPasteFeedback(null);
    try {
      const clip = await navigator.clipboard.readText();
      const parsed = parsePastedPayload(clip);
      if (!parsed) {
        setPasteFeedback("Clipboard does not contain an Onibi pairing payload.");
        return;
      }
      setBaseURL(parsed.baseURL);
      setToken(parsed.token);
      setMode(parsed.baseURL.startsWith("https://") ? "tunnel" : "lan");
      setPasteFeedback("Pairing payload imported.");
    } catch {
      setPasteFeedback("Clipboard read denied. Paste manually into the fields below.");
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

        <section className="mf-extra-block" aria-label="Paste pairing payload">
          <p>Onibi can give you a JSON payload or an <code>onibi://</code> deep link. Paste either to prefill both fields.</p>
          <button type="button" className="button-secondary" disabled={connecting} onClick={handlePaste}>
            Paste Pairing Payload
          </button>
          {pasteFeedback && <p className="mf-paste-feedback">{pasteFeedback}</p>}
        </section>

        {initialConnection && (
          <section className="mf-extra-block" aria-label="Additional saved connection features">
            <p>Clear the saved host URL + token from this browser.</p>
            <button type="button" className="button-secondary" disabled={connecting} onClick={onClearSaved}>
              Clear Saved Connection
            </button>
          </section>
        )}
      </section>
    </main>
  );
}
