import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import type { OutputEntry } from "../store/sessionStore";

interface SessionOutputPaneProps {
  entries: OutputEntry[];
  onTerminalInput: (data: string) => void;
}

export function SessionOutputPane({
  entries,
  onTerminalInput
}: SessionOutputPaneProps): JSX.Element {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const lastRenderedChunkIdRef = useRef<string | null>(null);
  const onTerminalInputRef = useRef(onTerminalInput);

  useEffect(() => {
    onTerminalInputRef.current = onTerminalInput;
  }, [onTerminalInput]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }

    const terminal = new Terminal({
      allowProposedApi: false,
      convertEol: false,
      cursorBlink: true,
      fontFamily:
        'ui-monospace, SFMono-Regular, Menlo, Monaco, "Cascadia Mono", "Segoe UI Mono", "Roboto Mono", monospace',
      fontSize: 14,
      theme: {
        background: "#0d1117",
        foreground: "#f3f4f6",
        cursor: "#f3f4f6"
      }
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(container);
    fitAddon.fit();
    terminal.focus();

    const onResize = () => {
      fitAddon.fit();
    };
    const inputDisposable = terminal.onData((data) => {
      onTerminalInputRef.current(data);
    });
    window.addEventListener("resize", onResize);

    terminalRef.current = terminal;
    fitAddonRef.current = fitAddon;

    return () => {
      window.removeEventListener("resize", onResize);
      inputDisposable.dispose();
      terminal.dispose();
      terminalRef.current = null;
      fitAddonRef.current = null;
      lastRenderedChunkIdRef.current = null;
    };
  }, []);

  useEffect(() => {
    const terminal = terminalRef.current;
    if (!terminal) {
      return;
    }

    if (entries.length === 0) {
      terminal.reset();
      lastRenderedChunkIdRef.current = null;
      return;
    }

    const lastRenderedChunkID = lastRenderedChunkIdRef.current;
    let startIndex = 0;

    if (lastRenderedChunkID) {
      const lastIndex = entries.findIndex((entry) => entry.id === lastRenderedChunkID);
      if (lastIndex >= 0) {
        startIndex = lastIndex + 1;
      } else {
        terminal.reset();
      }
    } else {
      terminal.reset();
    }

    for (let index = startIndex; index < entries.length; index += 1) {
      terminal.write(entries[index].text);
    }

    lastRenderedChunkIdRef.current = entries[entries.length - 1].id;
    fitAddonRef.current?.fit();
  }, [entries]);

  return (
    <section className="output-pane">
      <div className="xterm-container" ref={containerRef} />
    </section>
  );
}
