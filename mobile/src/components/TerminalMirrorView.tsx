import { useEffect, useRef } from "react";
import { decodeBase64Text } from "../utils";

export function TerminalMirrorView({
  sessionId,
  sessions,
  chunks,
  onSelect,
}: {
  sessionId: string;
  sessions: string[];
  chunks: string[];
  onSelect: (sessionId: string) => void;
}) {
  const terminalRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<{ write(data: string): void; dispose(): void } | null>(null);
  const fitRef = useRef<{ fit(): void; dispose(): void } | null>(null);
  const writtenRef = useRef(0);
  const plainText = chunks.map(decodeBase64Text).join("");

  useEffect(() => {
    const container = terminalRef.current;
    if (!container || !sessionId) {
      return undefined;
    }
    let disposed = false;
    writtenRef.current = 0;
    void Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")]).then(
      ([xterm, fit]) => {
        if (disposed) {
          return;
        }
        const term = new xterm.Terminal({
          convertEol: true,
          disableStdin: true,
          fontFamily: "Menlo, Monaco, monospace",
          fontSize: 12,
          scrollback: 4000,
          theme: { background: "#080b10", foreground: "#dbe5ed" },
        });
        const fitAddon = new fit.FitAddon();
        term.loadAddon(fitAddon);
        term.open(container);
        fitAddon.fit();
        termRef.current = term;
        fitRef.current = fitAddon;
      },
    );
    return () => {
      disposed = true;
      fitRef.current?.dispose();
      termRef.current?.dispose();
      fitRef.current = null;
      termRef.current = null;
    };
  }, [sessionId]);

  useEffect(() => {
    const term = termRef.current;
    if (!term) {
      return;
    }
    for (let index = writtenRef.current; index < chunks.length; index += 1) {
      term.write(decodeBase64Text(chunks[index]));
    }
    writtenRef.current = chunks.length;
    fitRef.current?.fit();
  }, [chunks]);

  return (
    <section className="terminal-pane">
      <div className="terminal-toolbar">
        <select
          value={sessionId}
          onChange={(event) => onSelect(event.target.value)}
          aria-label="Terminal session"
        >
          {sessions.length === 0 ? <option value="">No sessions</option> : null}
          {sessions.map((session) => (
            <option key={session} value={session}>
              {session}
            </option>
          ))}
        </select>
      </div>
      <div ref={terminalRef} className="terminal-mount" />
      <pre className="terminal-fallback" aria-label="Terminal mirror fallback">
        {plainText || "Waiting for terminal output..."}
      </pre>
    </section>
  );
}
