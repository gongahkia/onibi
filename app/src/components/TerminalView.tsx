import { useCallback, useEffect, useMemo, useRef, type CSSProperties } from "react";
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
  visible?: boolean;
  onExit?: (event: { code: number; signal: string | null }) => void;
  onOpenLink?: (url: string, event: MouseEvent) => void;
}

const TERMINAL_SYMBOL_FONT_FALLBACKS = [
  '"Symbols Nerd Font Mono"',
  '"Symbols Nerd Font"',
  '"MesloLGS NF"',
  '"MesloLGS Nerd Font Mono"',
  '"JetBrainsMono Nerd Font Mono"',
  '"JetBrainsMono Nerd Font"',
  '"FiraCode Nerd Font Mono"',
  '"FiraCode Nerd Font"',
  '"Hack Nerd Font Mono"',
  '"Hack Nerd Font"',
  '"CaskaydiaCove Nerd Font Mono"',
  '"CaskaydiaCove Nerd Font"',
  '"CaskaydiaMono Nerd Font Mono"',
  '"CaskaydiaMono Nerd Font"',
];

const TERMINAL_STANDARD_FONT_FALLBACKS = [
  "Menlo",
  "Monaco",
  "Consolas",
  '"Liberation Mono"',
  "monospace",
];

const TERMINAL_STANDARD_FONT_KEYS = new Set(
  TERMINAL_STANDARD_FONT_FALLBACKS.map((family) => fontFamilyKey(family)),
);

function quoteFontFamily(family: string): string {
  if (/^["'].*["']$/.test(family)) {
    return family;
  }
  if (/^[a-zA-Z-]+$/.test(family)) {
    return family;
  }
  return `"${family.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

function fontFamilyKey(family: string): string {
  let normalized = family.trim();
  if (
    (normalized.startsWith('"') && normalized.endsWith('"')) ||
    (normalized.startsWith("'") && normalized.endsWith("'"))
  ) {
    normalized = normalized.slice(1, -1);
  }
  return normalized.replace(/\\(["'])/g, "$1").toLowerCase();
}

function splitFontStack(fontFamily: string | undefined): string[] {
  return (
    fontFamily
      ?.split(",")
      .map((family) => family.trim())
      .filter(Boolean) ?? []
  );
}

function mergeFontStacks(families: string[]): string {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const family of families) {
    const key = fontFamilyKey(family);
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    result.push(family);
  }
  return result.join(", ");
}

function isStandardTerminalFallback(family: string): boolean {
  return TERMINAL_STANDARD_FONT_KEYS.has(fontFamilyKey(family));
}

function terminalFontStack(fontFamily: string | undefined): string {
  const selectedFamilies = splitFontStack(fontFamily).map(quoteFontFamily);
  const [primaryFont = "Menlo", ...secondaryFonts] = selectedFamilies;
  const preferredSecondaryFonts = secondaryFonts.filter(
    (family) => !isStandardTerminalFallback(family),
  );

  return mergeFontStacks([
    primaryFont,
    ...preferredSecondaryFonts,
    ...TERMINAL_SYMBOL_FONT_FALLBACKS,
    ...TERMINAL_STANDARD_FONT_FALLBACKS,
  ]);
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
  fontSize = DEFAULT_SETTINGS.terminalFontSize,
  settings = DEFAULT_SETTINGS,
  visible = true,
  onExit,
  onOpenLink,
}: TerminalViewProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const visibleRef = useRef(visible);
  const layoutFramesRef = useRef<number[]>([]);
  const terminalTheme = useMemo(() => terminalThemeForSettings(settings), [settings]);
  const resolvedFontFamily = useMemo(
    () => terminalFontStack(fontFamily),
    [fontFamily],
  );
  const handleTerminalLink = useCallback(
    (event: MouseEvent, uri: string) => {
      if ((event.metaKey || event.ctrlKey) && onOpenLink) {
        event.preventDefault();
        onOpenLink(uri, event);
        return;
      }
      window.open(uri, "_blank", "noopener,noreferrer");
    },
    [onOpenLink],
  );

  useEffect(() => {
    visibleRef.current = visible;
  }, [visible]);

  const cancelScheduledLayout = () => {
    layoutFramesRef.current.forEach((frame) => cancelAnimationFrame(frame));
    layoutFramesRef.current = [];
  };

  const refreshTerminalLayout = (focusTerminal = false) => {
    cancelScheduledLayout();

    const run = (shouldFocus: boolean) => {
      const term = terminalRef.current;
      const fitAddon = fitAddonRef.current;
      if (!term || !fitAddon) {
        return;
      }
      fitAddon.fit();
      term.refresh(0, Math.max(term.rows - 1, 0));
      if (shouldFocus) {
        term.focus();
      }
      void ptyResize(ptyId, term.rows, term.cols);
    };

    run(focusTerminal);
    const firstFrame = requestAnimationFrame(() => {
      run(focusTerminal);
      const secondFrame = requestAnimationFrame(() => {
        run(false);
        layoutFramesRef.current = [];
      });
      layoutFramesRef.current = [secondFrame];
    });
    layoutFramesRef.current = [firstFrame];
  };

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
    const webLinksAddon = new WebLinksAddon(handleTerminalLink);
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
    refreshTerminalLayout(visibleRef.current);

    let frame = 0;
    const fit = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        refreshTerminalLayout(false);
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
        onExit?.({ code: event.code, signal: event.signal });
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
      cancelScheduledLayout();
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
  }, [handleTerminalLink, onExit, ptyId]);

  useEffect(() => {
    if (visible) {
      refreshTerminalLayout(true);
    }
  }, [visible]);

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
    refreshTerminalLayout(visibleRef.current);
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
      data-visible={visible ? "true" : "false"}
      style={
        {
          "--font-terminal": resolvedFontFamily,
          fontFamily: resolvedFontFamily,
        } as CSSProperties
      }
    />
  );
}
