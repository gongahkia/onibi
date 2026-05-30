import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type FormEvent,
} from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { SerializeAddon } from "@xterm/addon-serialize";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { WebglAddon } from "@xterm/addon-webgl";
import "@xterm/xterm/css/xterm.css";
import {
  DEFAULT_SETTINGS,
  detectSessionPreview,
  terminalScrollbackLinesForSettings,
  terminalThemeForSettings,
  type AppSettings,
  type SessionCommandMarker,
  type SessionPreview,
  type TerminalKeybindingAction,
  type TerminalTriggerMatch,
} from "../lib/sessions";
import { ptyResize, ptyWrite, subscribePty, type PtyId } from "../lib/tauri-bridge";

export interface TerminalShellUpdate {
  cwd?: string;
  lastExitCode?: number;
  promptMarkerSeen?: boolean;
  commandStarted?: { command: string; startedAt: number };
  lastCommand?: SessionCommandMarker;
  preview?: SessionPreview;
  transcriptChunk?: string;
}

export interface TerminalViewProps {
  ptyId: PtyId;
  fontFamily?: string;
  fontSize?: number;
  settings?: AppSettings;
  visible?: boolean;
  onExit?: (event: { code: number; signal: string | null }) => void;
  onOpenLink?: (url: string, event: MouseEvent) => void;
  onShellUpdate?: (update: TerminalShellUpdate) => void;
  onTrigger?: (match: TerminalTriggerMatch) => void;
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

function keyFromKeyboardEvent(event: KeyboardEvent): string {
  const modifiers = [
    event.metaKey ? "cmd" : "",
    event.ctrlKey ? "ctrl" : "",
    event.altKey ? "alt" : "",
    event.shiftKey ? "shift" : "",
  ].filter(Boolean);
  let key = event.key.toLowerCase();
  if (key === " ") {
    key = "space";
  } else if (key === "escape") {
    key = "esc";
  } else if (key === "return") {
    key = "enter";
  }
  return [...modifiers, key].join("+");
}

function terminalKeybindingMap(
  settings: AppSettings,
): Map<string, TerminalKeybindingAction> {
  return new Map(
    settings.terminalKeybindings.map((binding) => [binding.keys, binding.action]),
  );
}

function decodeFileUriPath(uri: string): string | null {
  if (!uri.startsWith("file://")) {
    return null;
  }
  const withoutScheme = uri.slice("file://".length);
  const slash = withoutScheme.indexOf("/");
  if (slash < 0) {
    return null;
  }
  const path = withoutScheme.slice(slash);
  try {
    return decodeURI(path);
  } catch {
    return path;
  }
}

function decodeOscPayload(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function parseShellUpdate(text: string): TerminalShellUpdate | null {
  const update: TerminalShellUpdate = {};
  const matcher = /\x1b\](7;.*?|133;.*?)(?:\x07|\x1b\\)/g;
  for (const match of text.matchAll(matcher)) {
    const payload = match[1] ?? "";
    if (payload.startsWith("7;")) {
      const cwd = decodeFileUriPath(payload.slice(2));
      if (cwd) {
        update.cwd = cwd;
      }
    } else if (payload.startsWith("133;D")) {
      const value = payload.match(/^133;D;?(-?\d+)?/)?.[1];
      if (value !== undefined) {
        const parsed = Number(value);
        if (Number.isFinite(parsed)) {
          update.lastExitCode = parsed;
        }
      }
      update.promptMarkerSeen = true;
    } else if (payload.startsWith("133;C")) {
      const command = payload.startsWith("133;C;")
        ? decodeOscPayload(payload.slice("133;C;".length)).trim()
        : "";
      update.commandStarted = { command, startedAt: Date.now() };
      update.promptMarkerSeen = true;
    } else if (payload.startsWith("133;A")) {
      update.promptMarkerSeen = true;
    }
  }
  return update.cwd !== undefined ||
    update.lastExitCode !== undefined ||
    update.promptMarkerSeen ||
    update.commandStarted
    ? update
    : null;
}

function stripTerminalControls(text: string): string {
  return text
    .replace(/\x1b\].*?(?:\x07|\x1b\\)/g, "")
    .replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "")
    .replace(/\x1b[@-Z\\-_]/g, "");
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
  onShellUpdate,
  onTrigger,
}: TerminalViewProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const searchAddonRef = useRef<SearchAddon | null>(null);
  const textDecoderRef = useRef(new TextDecoder());
  const triggerLineBufferRef = useRef("");
  const activeCommandRef = useRef<SessionCommandMarker | null>(null);
  const visibleRef = useRef(visible);
  const layoutFramesRef = useRef<number[]>([]);
  const keybindingsRef = useRef<Map<string, TerminalKeybindingAction>>(new Map());
  const onShellUpdateRef = useRef(onShellUpdate);
  const onTriggerRef = useRef(onTrigger);
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const terminalTheme = useMemo(() => terminalThemeForSettings(settings), [settings]);
  const resolvedScrollback = terminalScrollbackLinesForSettings(settings);
  const resolvedFontFamily = useMemo(
    () => terminalFontStack(fontFamily),
    [fontFamily],
  );
  const triggerMatchers = useMemo(
    () =>
      settings.terminalTriggers
        .filter((trigger) => trigger.enabled)
        .map((trigger) => {
          try {
            return { trigger, regex: new RegExp(trigger.pattern, "i") };
          } catch {
            return null;
          }
        })
        .filter(
          (item): item is NonNullable<typeof item> => item !== null,
        ),
    [settings.terminalTriggers],
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

  const openSearch = useCallback(() => {
    setSearchOpen(true);
    requestAnimationFrame(() => searchInputRef.current?.focus());
  }, []);

  const runSearch = useCallback(
    (direction: "next" | "previous" = "next") => {
      const query = searchQuery.trim();
      if (!query) {
        return;
      }
      if (direction === "previous") {
        searchAddonRef.current?.findPrevious(query);
      } else {
        searchAddonRef.current?.findNext(query);
      }
    },
    [searchQuery],
  );

  useEffect(() => {
    visibleRef.current = visible;
  }, [visible]);

  useEffect(() => {
    keybindingsRef.current = terminalKeybindingMap(settings);
  }, [settings]);

  useEffect(() => {
    onShellUpdateRef.current = onShellUpdate;
  }, [onShellUpdate]);

  useEffect(() => {
    onTriggerRef.current = onTrigger;
  }, [onTrigger]);

  useEffect(() => {
    if (searchOpen) {
      requestAnimationFrame(() => searchInputRef.current?.focus());
    }
  }, [searchOpen]);

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
      scrollback: resolvedScrollback,
      theme: terminalTheme,
    });
    const fitAddon = new FitAddon();
    const searchAddon = new SearchAddon();
    const webLinksAddon = new WebLinksAddon(handleTerminalLink);
    const unicode11Addon = new Unicode11Addon();
    const serializeAddon = new SerializeAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(searchAddon);
    term.loadAddon(webLinksAddon);
    term.loadAddon(unicode11Addon);
    term.loadAddon(serializeAddon);
    term.unicode.activeVersion = "11";
    terminalRef.current = term;
    fitAddonRef.current = fitAddon;
    searchAddonRef.current = searchAddon;

    // OSC 8 hyperlinks (anchor sequences emitted by tools like `ls --hyperlink`).
    term.parser.registerOscHandler(8, (data) => {
      const parts = data.split(";");
      const url = parts[parts.length - 1] ?? "";
      if (url) {
        ;(term as unknown as { _onibiActiveLink?: string })._onibiActiveLink = url;
      } else {
        ;(term as unknown as { _onibiActiveLink?: string })._onibiActiveLink = undefined;
      }
      return false; // allow xterm to keep parsing
    });

    term.open(container);

    let webglAddon: WebglAddon | null = null;
    try {
      webglAddon = new WebglAddon();
      webglAddon.onContextLoss(() => {
        webglAddon?.dispose();
        webglAddon = null;
      });
      term.loadAddon(webglAddon);
    } catch {
      // WebGL2 unavailable; xterm falls back to canvas/DOM renderer.
      webglAddon = null;
    }
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
    term.attachCustomKeyEventHandler((event) => {
      if (event.type !== "keydown") {
        return true;
      }
      if ((event.metaKey || event.ctrlKey) && !event.altKey && event.key.toLowerCase() === "f") {
        event.preventDefault();
        openSearch();
        return false;
      }
      const action = keybindingsRef.current.get(keyFromKeyboardEvent(event));
      if (!action) {
        return true;
      }
      event.preventDefault();
      if (action === "copy") {
        const selection = term.getSelection();
        if (selection) {
          void navigator.clipboard?.writeText(selection);
        }
      } else if (action === "paste") {
        void navigator.clipboard?.readText?.().then((text) => {
          if (text) {
            void ptyWrite(ptyId, encoder.encode(text));
          }
        });
      } else if (action === "clear") {
        term.clear();
      } else if (action === "select-all") {
        term.selectAll();
      } else if (action === "find") {
        openSearch();
      }
      return false;
    });
    const inputDisposable = term.onData((data) => {
      void ptyWrite(ptyId, encoder.encode(data));
    });

    const applyTriggers = (text: string) => {
      if (triggerMatchers.length === 0) {
        return;
      }
      const plain = stripTerminalControls(text);
      const combined = `${triggerLineBufferRef.current}${plain}`;
      const lines = combined.split(/\r?\n|\r/g);
      triggerLineBufferRef.current = (lines.pop() ?? "").slice(-2000);
      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed) {
          continue;
        }
        for (const { trigger, regex } of triggerMatchers) {
          if (!regex.test(trimmed)) {
            continue;
          }
          onTriggerRef.current?.({
            id: trigger.id,
            label: trigger.label,
            pattern: trigger.pattern,
            line: trimmed.slice(0, 500),
            actions: trigger.actions,
            timestamp: Date.now(),
          });
        }
      }
    };

    const applyOutputMetadata = (text: string) => {
      const shellUpdate = parseShellUpdate(text) ?? {};
      const plain = stripTerminalControls(text);
      if (plain) {
        shellUpdate.transcriptChunk = plain;
      }
      const preview = detectSessionPreview(plain);
      const startedCommand = shellUpdate.commandStarted;
      if (startedCommand) {
        activeCommandRef.current = {
          command: startedCommand.command,
          output: "",
          startedAt: startedCommand.startedAt,
          endedAt: null,
          exitCode: null,
        };
      }
      if (activeCommandRef.current && plain) {
        activeCommandRef.current = {
          ...activeCommandRef.current,
          output: `${activeCommandRef.current.output}${plain}`.slice(-20000),
        };
      }
      if (shellUpdate.lastExitCode !== undefined && activeCommandRef.current) {
        shellUpdate.lastCommand = {
          ...activeCommandRef.current,
          endedAt: Date.now(),
          exitCode: shellUpdate.lastExitCode,
        };
        activeCommandRef.current = null;
      }
      if (preview) {
        shellUpdate.preview = preview;
      }
      if (
        shellUpdate.cwd !== undefined ||
        shellUpdate.lastExitCode !== undefined ||
        shellUpdate.promptMarkerSeen ||
        shellUpdate.commandStarted ||
        shellUpdate.lastCommand ||
        shellUpdate.preview ||
        shellUpdate.transcriptChunk
      ) {
        onShellUpdateRef.current?.(shellUpdate);
      }
    };

    let disposed = false;
    let unlisten: (() => void) | undefined;
    void subscribePty(ptyId, (event) => {
      if (disposed) {
        return;
      }
      if (event.type === "data") {
        const bytes = decodeBase64(event.data);
        const text = textDecoderRef.current.decode(bytes, { stream: true });
        applyOutputMetadata(text);
        applyTriggers(text);
        term.write(bytes);
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

    function handleJumpToLastPrompt(event: Event) {
      if ((event as CustomEvent<{ ptyId?: string }>).detail?.ptyId === ptyId) {
        term.scrollToBottom();
        term.focus();
      }
    }
    window.addEventListener("onibi:jump-last-prompt", handleJumpToLastPrompt);

    return () => {
      disposed = true;
      cancelScheduledLayout();
      cancelAnimationFrame(frame);
      unlisten?.();
      resizeObserver.disconnect();
      window.removeEventListener("onibi:jump-last-prompt", handleJumpToLastPrompt);
      inputDisposable.dispose();
      webLinksAddon.dispose();
      searchAddon.dispose();
      fitAddon.dispose();
      unicode11Addon.dispose();
      serializeAddon.dispose();
      webglAddon?.dispose();
      term.dispose();
      terminalRef.current = null;
      fitAddonRef.current = null;
      searchAddonRef.current = null;
    };
  }, [handleTerminalLink, onExit, openSearch, ptyId, triggerMatchers]);

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
      scrollback: resolvedScrollback,
      theme: terminalTheme,
    };
    term.clearTextureAtlas();
    refreshTerminalLayout(visibleRef.current);
  }, [
    fontSize,
    ptyId,
    resolvedFontFamily,
    resolvedScrollback,
    terminalTheme.background,
    terminalTheme.cursor,
    terminalTheme.foreground,
    terminalTheme.selectionBackground,
  ]);

  return (
    <div className="terminal-view-shell">
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
      {searchOpen ? (
        <form
          className="terminal-search"
          role="search"
          onSubmit={(event: FormEvent) => {
            event.preventDefault();
            runSearch("next");
          }}
        >
          <input
            ref={searchInputRef}
            value={searchQuery}
            aria-label="Find in terminal"
            placeholder="Find in terminal"
            onChange={(event) => setSearchQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                event.preventDefault();
                setSearchOpen(false);
              }
            }}
          />
          <button type="button" onClick={() => runSearch("previous")}>
            Previous
          </button>
          <button type="submit">Next</button>
          <button
            type="button"
            aria-label="Close search"
            onClick={() => setSearchOpen(false)}
          >
            x
          </button>
        </form>
      ) : null}
    </div>
  );
}
