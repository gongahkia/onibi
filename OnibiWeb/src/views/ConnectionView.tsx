import { FormEvent, useEffect, useState } from "react";
import type { ConnectionConfig } from "../types";

interface ConnectionViewProps {
  initialConnection: ConnectionConfig | null;
  connecting: boolean;
  errorMessage: string | null;
  onConnect: (connection: ConnectionConfig) => void;
  onClearSaved: () => void;
}

export function ConnectionView({
  initialConnection,
  connecting,
  errorMessage,
  onConnect,
  onClearSaved
}: ConnectionViewProps): JSX.Element {
  const [baseURL, setBaseURL] = useState(initialConnection?.baseURL ?? "http://127.0.0.1:8787");
  const [token, setToken] = useState(initialConnection?.token ?? "");

  useEffect(() => {
    setBaseURL(initialConnection?.baseURL ?? "http://127.0.0.1:8787");
    setToken(initialConnection?.token ?? "");
  }, [initialConnection]);

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onConnect({
      baseURL,
      token
    });
  };

  return (
    <main className="screen screen-connect">
      <header className="screen-header">
        <h1>Onibi Web</h1>
        <p>Connect to your Mac host and pairing token.</p>
      </header>

      <form className="connection-form" onSubmit={handleSubmit}>
        <label>
          Host URL
          <input
            type="url"
            value={baseURL}
            onChange={(event) => setBaseURL(event.target.value)}
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            required
            disabled={connecting}
          />
        </label>

        <label>
          Pairing Token
          <input
            type="password"
            value={token}
            onChange={(event) => setToken(event.target.value)}
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            required
            disabled={connecting}
          />
        </label>

        {errorMessage && <p className="error-text">{errorMessage}</p>}

        <div className="connection-actions">
          <button type="submit" disabled={connecting || baseURL.trim() === "" || token.trim() === ""}>
            {connecting ? "Connecting..." : "Connect"}
          </button>
          {initialConnection && (
            <button type="button" className="button-secondary" disabled={connecting} onClick={onClearSaved}>
              Clear Saved Connection
            </button>
          )}
        </div>
      </form>
    </main>
  );
}
