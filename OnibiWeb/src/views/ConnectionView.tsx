import { FormEvent, useEffect, useState } from "react";
import type { ConnectionConfig } from "../types";

interface ConnectionViewProps {
  initialConnection: ConnectionConfig | null;
  initialRememberToken: boolean;
  connecting: boolean;
  errorMessage: string | null;
  onConnect: (connection: ConnectionConfig, rememberToken: boolean) => void;
  onClearSaved: () => void;
}

export function ConnectionView({
  initialConnection,
  initialRememberToken,
  connecting,
  errorMessage,
  onConnect,
  onClearSaved
}: ConnectionViewProps): JSX.Element {
  const [baseURL, setBaseURL] = useState(initialConnection?.baseURL ?? "http://127.0.0.1:8787");
  const [token, setToken] = useState(initialConnection?.token ?? "");
  const [rememberToken, setRememberToken] = useState(initialRememberToken);
  const [tokenVisible, setTokenVisible] = useState(false);

  useEffect(() => {
    setBaseURL(initialConnection?.baseURL ?? "http://127.0.0.1:8787");
    setToken(initialConnection?.token ?? "");
    setTokenVisible(false);
  }, [initialConnection]);

  useEffect(() => {
    setRememberToken(initialRememberToken);
  }, [initialRememberToken]);

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onConnect(
      {
        baseURL,
        token
      },
      rememberToken
    );
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

        <form className="mf-form" onSubmit={handleSubmit}>
          <label className="mf-field">
            <span>Host URL</span>
            <input
              type="url"
              value={baseURL}
              onChange={(event) => setBaseURL(event.target.value)}
              placeholder="http://127.0.0.1:8787"
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

          {errorMessage && (
            <p className="mf-alert mf-alert-error" role="alert">
              {errorMessage}
            </p>
          )}

          <button type="submit" disabled={connecting || baseURL.trim() === "" || token.trim() === ""}>
            {connecting ? "Connecting..." : "Connect"}
          </button>
        </form>

        {initialConnection && (
          <section className="mf-extra-block" aria-label="Additional saved connection features">
            <p>Additional from existing frontend: clear saved connection.</p>
            <button type="button" className="button-secondary" disabled={connecting} onClick={onClearSaved}>
              Clear Saved Connection
            </button>
          </section>
        )}
      </section>
    </main>
  );
}
