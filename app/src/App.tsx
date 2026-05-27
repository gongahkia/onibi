import { useEffect, useState } from "react";
import "./App.css";
import { TerminalView } from "./components/TerminalView";
import { ptyKill, ptySpawn, shellPath, type PtyId } from "./lib/tauri-bridge";

function App() {
  const [ptyId, setPtyId] = useState<PtyId | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    let spawnedId: PtyId | null = null;
    void ptySpawn({
      command: shellPath(),
      args: [],
      cwd: null,
      env: [],
      rows: 30,
      cols: 100,
    })
      .then((id) => {
        spawnedId = id;
        if (active) {
          setPtyId(id);
        } else {
          void ptyKill(id);
        }
      })
      .catch((err: unknown) => {
        if (active) {
          setError(err instanceof Error ? err.message : String(err));
        }
      });

    return () => {
      active = false;
      if (spawnedId) {
        void ptyKill(spawnedId);
      }
    };
  }, []);

  return (
    <main className="app-shell">
      {ptyId ? <TerminalView ptyId={ptyId} /> : <div className="terminal-loading">Starting shell...</div>}
      {error ? <div className="terminal-error">{error}</div> : null}
    </main>
  );
}

export default App;
