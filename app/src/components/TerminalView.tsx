import { useEffect, useMemo, useRef, type CSSProperties } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import {
  DEFAULT_SETTINGS,
  terminalThemeForSettings,
  type AppSettings,
} from "../lib/sessions";
import { ptyResize, ptyWrite, subscribePty, type PtyId } from "../lib/tauri-bridge";

export interface TerminalViewProps {
  ptyId: PtyId;
  fontFamily?: string;
  fontSize?: number;
  settings?: AppSettings;
}

const TERMINAL_FONT_FALLBACK =
  'Menlo, Monaco, Consolas, "Liberation Mono", monospace';

function quoteFontFamily(family: string): string {
  if (/^["'].*["']$/.test(family)) {
    return family;
  }
  if (/^[a-zA-Z-]+$/.test(family)) {
    return family;
  }
  return `"${family.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

function terminalFontStack(fontFamily: string | undefined): string {
  const trimmed = fontFamily?.trim();
  if (!trimmed) {
    return TERMINAL_FONT_FALLBACK;
  }
  if (trimmed.includes(",")) {
    return trimmed;
  }
  return `${quoteFontFamily(trimmed)}, ${TERMINAL_FONT_FALLBACK}`;
}

function decodeBase64(data: string): Uint8Array {
  const binary = atob(data);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

export function TerminalView({
  ptyId,
  fontFamily = DEFAULT_SETTINGS.terminalFontFamily,
  fontSize = 13,
  settings = DEFAULT_SETTINGS,
}: TerminalViewProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const terminalTheme = useMemo(() => terminalThemeForSettings(settings), [settings]);
  const resolvedFontFamily = useMemo(
    () => terminalFontStack(fontFamily),
    [fontFamily],
  );

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return undefined;
    }

    const term = new Terminal({
      cursorBlink: true,
      convertEol: false,
      fontFamily: resolvedFontFamily,
      fontSize,
      letterSpacing: 0,
      lineHeight: 1,
      scrollback: 10000,
      theme: terminalTheme,
    });
    const fitAddon = new FitAddon();
    const webLinksAddon = new WebLinksAddon();
    let webglAddon: WebglAddon | undefined;
    term.loadAddon(fitAddon);
    term.loadAddon(webLinksAddon);
    terminalRef.current = term;
    fitAddonRef.current = fitAddon;
    try {
      webglAddon = new WebglAddon();
      term.loadAddon(webglAddon);
    } catch (error) {
      console.debug("xterm webgl renderer unavailable", error);
    }

    term.open(container);
    fitAddon.fit();
    term.focus();
    void ptyResize(ptyId, term.rows, term.cols);

    let frame = 0;
    const fit = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        fitAddon.fit();
        void ptyResize(ptyId, term.rows, term.cols);
      });
    };
    const resizeObserver = new ResizeObserver(fit);
    resizeObserver.observe(container);

    const encoder = new TextEncoder();
    const inputDisposable = term.onData((data) => {
      void ptyWrite(ptyId, encoder.encode(data));
    });

    let disposed = false;
    let unlisten: (() => void) | undefined;
    void subscribePty(ptyId, (event) => {
      if (disposed) {
        return;
      }
      if (event.type === "data") {
        term.write(decodeBase64(event.data));
      } else {
        const suffix = event.signal ? ` (${event.signal})` : "";
        term.write(`\r\n[process exited: ${event.code}${suffix}]\r\n`);
      }
    }).then((dispose) => {
      if (disposed) {
        dispose();
      } else {
        unlisten = dispose;
      }
    });

    return () => {
      disposed = true;
      cancelAnimationFrame(frame);
      unlisten?.();
      resizeObserver.disconnect();
      inputDisposable.dispose();
      webglAddon?.dispose();
      webLinksAddon.dispose();
      fitAddon.dispose();
      term.dispose();
      terminalRef.current = null;
      fitAddonRef.current = null;
    };
  }, [ptyId]);

  useEffect(() => {
    const term = terminalRef.current;
    if (!term) {
      return;
    }
    term.options = {
      fontFamily: resolvedFontFamily,
      fontSize,
      letterSpacing: 0,
      lineHeight: 1,
      theme: terminalTheme,
    };
    term.clearTextureAtlas();
    fitAddonRef.current?.fit();
    void ptyResize(ptyId, term.rows, term.cols);
  }, [
    fontSize,
    ptyId,
    resolvedFontFamily,
    terminalTheme.background,
    terminalTheme.cursor,
    terminalTheme.foreground,
    terminalTheme.selectionBackground,
  ]);

  return (
    <div
      ref={containerRef}
      className="terminal-view"
      data-testid="terminal-view"
      style={
        {
          "--font-terminal": resolvedFontFamily,
          fontFamily: resolvedFontFamily,
        } as CSSProperties
      }
    />
  );
}
