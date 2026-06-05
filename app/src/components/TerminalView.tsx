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
  type TerminalCopyFormat,
  type TerminalKeybindingAction,
  type TerminalTriggerMatch,
} from "../lib/sessions";
import {
  ptyReplay,
  ptyResize,
  ptyWrite,
  subscribePty,
  type PtyId,
  type PtyWireEvent,
} from "../lib/tauri-bridge";

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
  onUnavailable?: (error: unknown) => void;
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

interface CopyModePosition {
  row: number;
  col: number;
}

interface CopyModeState {
  cursor: CopyModePosition;
  anchor: CopyModePosition | null;
}

interface CopyPayload {
  plain: string;
  ansi?: string;
  html?: string;
}

interface RenderProfile {
  startedAt: number;
  chunks: number;
  bytes: number;
  batches: number;
  maxBatchBytes: number;
  totalFlushLatencyMs: number;
  maxFlushLatencyMs: number;
  replayBytes: number;
  replayDurationMs: number | null;
}

const OSC52_CLIPBOARD_MAX_BYTES = 1024 * 1024;

function terminalDebug(message: string, metadata?: Record<string, unknown>) {
  if (terminalDebugEnabled()) {
    console.debug(`[onibi:terminal] ${message}`, metadata ?? {});
  }
}

function isImeCompositionEvent(event: KeyboardEvent): boolean {
  return event.isComposing || event.key === "Process" || event.key === "Dead";
}

function terminalDebugEnabled(): boolean {
  try {
    return globalThis.localStorage?.getItem("onibiTerminalDebug") === "1";
  } catch {
    return false;
  }
}

function terminalThemeForTransparency(
  theme: ReturnType<typeof terminalThemeForSettings>,
  transparent: boolean,
): ReturnType<typeof terminalThemeForSettings> {
  return transparent ? { ...theme, background: "transparent" } : theme;
}

function textFromBytes(bytes: Uint8Array): string | null {
  try {
    return new TextDecoder().decode(bytes);
  } catch {
    return null;
  }
}

function decodeBase64Safe(data: string): Uint8Array | null {
  try {
    return decodeBase64(data);
  } catch {
    return null;
  }
}

function copyModeSerializeRange(state: CopyModeState): { start: number; end: number } {
  const rows = state.anchor
    ? [state.anchor.row, state.cursor.row]
    : [state.cursor.row, state.cursor.row];
  return { start: Math.min(...rows), end: Math.max(...rows) };
}

async function writeClipboardPayload(
  payload: CopyPayload,
  format: TerminalCopyFormat,
): Promise<void> {
  const plain = payload.plain;
  if (!plain) {
    return;
  }
  if (
    format === "html" &&
    payload.html &&
    typeof ClipboardItem !== "undefined" &&
    typeof navigator.clipboard?.write === "function"
  ) {
    try {
      await navigator.clipboard.write([
        new ClipboardItem({
          "text/plain": new Blob([plain], { type: "text/plain" }),
          "text/html": new Blob([payload.html], { type: "text/html" }),
        }),
      ]);
      return;
    } catch (error) {
      terminalDebug("html clipboard fallback", { error: String(error) });
    }
  }
  if (format === "ansi" && payload.ansi) {
    await navigator.clipboard?.writeText(payload.ansi);
    return;
  }
  await navigator.clipboard?.writeText(plain);
}

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

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function copyModeMaxRow(term: Terminal): number {
  return Math.max(term.buffer.active.length - 1, 0);
}

function copyModeLineText(
  term: Terminal,
  row: number,
  trimRight = true,
): string {
  return term.buffer.active.getLine(row)?.translateToString(trimRight) ?? "";
}

function copyModeMaxCol(term: Terminal, row: number): number {
  const maxTerminalCol = Math.max(term.cols - 1, 0);
  const text = copyModeLineText(term, row, true);
  return clamp(text.length > 0 ? text.length - 1 : 0, 0, maxTerminalCol);
}

function copyModeClampPosition(term: Terminal, position: CopyModePosition): CopyModePosition {
  const row = clamp(position.row, 0, copyModeMaxRow(term));
  return {
    row,
    col: clamp(position.col, 0, copyModeMaxCol(term, row)),
  };
}

function copyModeInitialPosition(term: Terminal): CopyModePosition {
  const buffer = term.buffer.active;
  return copyModeClampPosition(term, {
    row: buffer.baseY + buffer.cursorY,
    col: buffer.cursorX,
  });
}

function compareCopyModePositions(a: CopyModePosition, b: CopyModePosition): number {
  return a.row === b.row ? a.col - b.col : a.row - b.row;
}

function normalizedCopyModeRange(
  a: CopyModePosition,
  b: CopyModePosition,
): [CopyModePosition, CopyModePosition] {
  return compareCopyModePositions(a, b) <= 0 ? [a, b] : [b, a];
}

function copyModeSelectPosition(term: Terminal, cursor: CopyModePosition): void {
  term.select(cursor.col, cursor.row, 1);
}

function copyModeSelectRange(
  term: Terminal,
  anchor: CopyModePosition,
  cursor: CopyModePosition,
): void {
  const [start, end] = normalizedCopyModeRange(anchor, cursor);
  const length = Math.max((end.row - start.row) * term.cols - start.col + end.col + 1, 1);
  term.select(start.col, start.row, length);
}

function copyModeEnsureVisible(term: Terminal, position: CopyModePosition): void {
  const viewportTop = term.buffer.active.viewportY;
  const viewportBottom = viewportTop + Math.max(term.rows - 1, 0);
  if (position.row < viewportTop) {
    term.scrollToLine(position.row);
  } else if (position.row > viewportBottom) {
    term.scrollToLine(position.row - Math.max(term.rows - 1, 0));
  }
}

function copyModeApplySelection(term: Terminal, state: CopyModeState): void {
  if (state.anchor) {
    copyModeSelectRange(term, state.anchor, state.cursor);
  } else {
    copyModeSelectPosition(term, state.cursor);
  }
  copyModeEnsureVisible(term, state.cursor);
}

function copyModeMove(term: Terminal, position: CopyModePosition, direction: string): CopyModePosition {
  if (direction === "h") {
    if (position.col > 0) {
      return { ...position, col: position.col - 1 };
    }
    if (position.row > 0) {
      const row = position.row - 1;
      return { row, col: copyModeMaxCol(term, row) };
    }
    return position;
  }
  if (direction === "l") {
    const maxCol = copyModeMaxCol(term, position.row);
    if (position.col < maxCol) {
      return { ...position, col: position.col + 1 };
    }
    if (position.row < copyModeMaxRow(term)) {
      return { row: position.row + 1, col: 0 };
    }
    return position;
  }
  if (direction === "j") {
    return copyModeClampPosition(term, {
      row: position.row + 1,
      col: position.col,
    });
  }
  if (direction === "k") {
    return copyModeClampPosition(term, {
      row: position.row - 1,
      col: position.col,
    });
  }
  return position;
}

function copyModeIsWordChar(value: string): boolean {
  return /[A-Za-z0-9_]/.test(value);
}

function copyModeWordStartForward(term: Terminal, position: CopyModePosition): CopyModePosition {
  for (let row = position.row; row <= copyModeMaxRow(term); row += 1) {
    const text = copyModeLineText(term, row, true);
    let col = row === position.row ? position.col : 0;
    while (col < text.length && copyModeIsWordChar(text[col] ?? "")) {
      col += 1;
    }
    while (col < text.length && !copyModeIsWordChar(text[col] ?? "")) {
      col += 1;
    }
    if (col < text.length) {
      return copyModeClampPosition(term, { row, col });
    }
  }
  const row = copyModeMaxRow(term);
  return { row, col: copyModeMaxCol(term, row) };
}

function copyModeWordEndForward(term: Terminal, position: CopyModePosition): CopyModePosition {
  for (let row = position.row; row <= copyModeMaxRow(term); row += 1) {
    const text = copyModeLineText(term, row, true);
    let col = row === position.row ? position.col : 0;
    while (col < text.length && !copyModeIsWordChar(text[col] ?? "")) {
      col += 1;
    }
    if (col < text.length) {
      while (col + 1 < text.length && copyModeIsWordChar(text[col + 1] ?? "")) {
        col += 1;
      }
      return copyModeClampPosition(term, { row, col });
    }
  }
  const row = copyModeMaxRow(term);
  return { row, col: copyModeMaxCol(term, row) };
}

function copyModeWordStartBackward(term: Terminal, position: CopyModePosition): CopyModePosition {
  for (let row = position.row; row >= 0; row -= 1) {
    const text = copyModeLineText(term, row, true);
    let col = row === position.row ? position.col - 1 : text.length - 1;
    while (col >= 0 && !copyModeIsWordChar(text[col] ?? "")) {
      col -= 1;
    }
    if (col >= 0) {
      while (col > 0 && copyModeIsWordChar(text[col - 1] ?? "")) {
        col -= 1;
      }
      return copyModeClampPosition(term, { row, col });
    }
  }
  return { row: 0, col: 0 };
}

function copyModeIsBlankLine(term: Terminal, row: number): boolean {
  return copyModeLineText(term, row, true).trim() === "";
}

function copyModeParagraphForward(term: Terminal, position: CopyModePosition): CopyModePosition {
  const maxRow = copyModeMaxRow(term);
  let row = Math.min(position.row + 1, maxRow);
  while (row < maxRow && !copyModeIsBlankLine(term, row)) {
    row += 1;
  }
  while (row < maxRow && copyModeIsBlankLine(term, row)) {
    row += 1;
  }
  return copyModeClampPosition(term, { row, col: 0 });
}

function copyModeParagraphBackward(term: Terminal, position: CopyModePosition): CopyModePosition {
  let row = Math.max(position.row - 1, 0);
  while (row > 0 && !copyModeIsBlankLine(term, row)) {
    row -= 1;
  }
  while (row > 0 && copyModeIsBlankLine(term, row - 1)) {
    row -= 1;
  }
  return copyModeClampPosition(term, { row, col: 0 });
}

function copyModeTextFromRange(
  term: Terminal,
  anchor: CopyModePosition,
  cursor: CopyModePosition,
): string {
  const [start, end] = normalizedCopyModeRange(anchor, cursor);
  const lines: string[] = [];
  for (let row = start.row; row <= end.row; row += 1) {
    const from = row === start.row ? start.col : 0;
    const to = row === end.row ? end.col + 1 : term.cols;
    lines.push(copyModeLineText(term, row, false).slice(from, to).replace(/\s+$/g, ""));
  }
  return lines.join("\n");
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
  onUnavailable,
}: TerminalViewProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const searchAddonRef = useRef<SearchAddon | null>(null);
  const lastResizeRef = useRef<{ rows: number; cols: number } | null>(null);
  const textDecoderRef = useRef(new TextDecoder());
  const triggerLineBufferRef = useRef("");
  const activeCommandRef = useRef<SessionCommandMarker | null>(null);
  const visibleRef = useRef(visible);
  const layoutFramesRef = useRef<number[]>([]);
  const keybindingsRef = useRef<Map<string, TerminalKeybindingAction>>(new Map());
  const copyModeRef = useRef<CopyModeState | null>(null);
  const onShellUpdateRef = useRef(onShellUpdate);
  const onTriggerRef = useRef(onTrigger);
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [copyModeActive, setCopyModeActive] = useState(false);
  const terminalTheme = useMemo(() => terminalThemeForSettings(settings), [settings]);
  const resolvedTerminalTheme = useMemo(
    () =>
      terminalThemeForTransparency(
        terminalTheme,
        settings.terminalTransparentBackground,
      ),
    [settings.terminalTransparentBackground, terminalTheme],
  );
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
      const nextSize = { rows: term.rows, cols: term.cols };
      const lastSize = lastResizeRef.current;
      if (!lastSize || lastSize.rows !== nextSize.rows || lastSize.cols !== nextSize.cols) {
        lastResizeRef.current = nextSize;
        void ptyResize(ptyId, nextSize.rows, nextSize.cols);
      }
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
    lastResizeRef.current = null;

    const term = new Terminal({
      allowProposedApi: true,
      cursorBlink: true,
      convertEol: false,
      fontFamily: resolvedFontFamily,
      fontSize,
      letterSpacing: 0,
      lineHeight: 1,
      rightClickSelectsWord: true,
      scrollback: resolvedScrollback,
      screenReaderMode: settings.terminalScreenReaderMode,
      theme: resolvedTerminalTheme,
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
    term.parser.registerOscHandler(52, (data) => {
      if (!settings.terminalOsc52Clipboard) {
        return true;
      }
      const separator = data.indexOf(";");
      if (separator < 0) {
        return true;
      }
      const payload = data.slice(separator + 1).trim();
      if (!payload || payload === "?") {
        return true;
      }
      const bytes = decodeBase64Safe(payload);
      if (!bytes || bytes.length > OSC52_CLIPBOARD_MAX_BYTES) {
        terminalDebug("osc52 ignored", {
          ptyId,
          reason: bytes ? "oversized" : "malformed",
          bytes: bytes?.length ?? null,
        });
        return true;
      }
      const text = textFromBytes(bytes);
      if (text) {
        void navigator.clipboard?.writeText(text).catch((error) => {
          terminalDebug("osc52 clipboard failed", { ptyId, error: String(error) });
        });
      }
      return true;
    });

    term.open(container);
    let webglAddon: WebglAddon | null = null;
    let webglContextLossDisposable: { dispose: () => void } | null = null;
    try {
      webglAddon = new WebglAddon();
      webglContextLossDisposable =
        webglAddon.onContextLoss?.(() => {
          terminalDebug("webgl context lost", { ptyId });
          webglAddon?.dispose();
          webglAddon = null;
        }) ?? null;
      term.loadAddon(webglAddon);
      terminalDebug("renderer selected", { ptyId, renderer: "webgl" });
    } catch (error) {
      terminalDebug("renderer fallback", {
        ptyId,
        renderer: "dom",
        error: String(error),
      });
      webglAddon?.dispose();
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

    let selectionCopyFrame = 0;
    const writeTerminalClipboard = (payload: CopyPayload) => {
      void writeClipboardPayload(payload, settings.terminalCopyFormat).catch((error) => {
        terminalDebug("clipboard write failed", { ptyId, error: String(error) });
      });
    };
    const copyHtmlSelection = () => {
      try {
        return serializeAddon.serializeAsHTML({
          onlySelection: true,
          includeGlobalBackground: false,
        });
      } catch (error) {
        terminalDebug("selection html serialize failed", {
          ptyId,
          error: String(error),
        });
        return undefined;
      }
    };
    const copyPayloadForModeState = (state: CopyModeState): CopyPayload => {
      const plain = state.anchor
        ? term.getSelection() || copyModeTextFromRange(term, state.anchor, state.cursor)
        : copyModeLineText(term, state.cursor.row, true);
      const rowRange = copyModeSerializeRange(state);
      let ansi: string | undefined;
      let html: string | undefined;
      try {
        ansi = serializeAddon.serialize({
          range: rowRange,
          excludeModes: true,
          excludeAltBuffer: true,
        });
      } catch (error) {
        terminalDebug("ansi serialize failed", { ptyId, error: String(error) });
      }
      try {
        html = state.anchor
          ? copyHtmlSelection()
          : serializeAddon.serializeAsHTML({
              includeGlobalBackground: false,
              range: {
                startLine: state.cursor.row,
                endLine: state.cursor.row,
                startCol: 0,
              },
            });
      } catch (error) {
        terminalDebug("html serialize failed", { ptyId, error: String(error) });
      }
      return { plain, ansi, html };
    };
    const copyTerminalSelection = () => {
      const selection = term.getSelection();
      if (selection) {
        writeTerminalClipboard({
          plain: selection,
          html: copyHtmlSelection(),
        });
      }
    };
    const handleDoubleClick = () => {
      cancelAnimationFrame(selectionCopyFrame);
      selectionCopyFrame = requestAnimationFrame(copyTerminalSelection);
    };
    container.addEventListener("dblclick", handleDoubleClick);

    const encoder = new TextEncoder();
    const enterCopyMode = () => {
      const state: CopyModeState = {
        cursor: copyModeInitialPosition(term),
        anchor: null,
      };
      copyModeRef.current = state;
      setCopyModeActive(true);
      copyModeApplySelection(term, state);
      term.focus();
    };
    const exitCopyMode = () => {
      copyModeRef.current = null;
      setCopyModeActive(false);
      term.clearSelection();
      term.focus();
    };
    const copyModeCopyAndExit = () => {
      const state = copyModeRef.current;
      if (!state) {
        return;
      }
      writeTerminalClipboard(copyPayloadForModeState(state));
      exitCopyMode();
    };
    const updateCopyModeCursor = (cursor: CopyModePosition) => {
      const state = copyModeRef.current;
      if (!state) {
        return;
      }
      const nextState = {
        ...state,
        cursor: copyModeClampPosition(term, cursor),
      };
      copyModeRef.current = nextState;
      copyModeApplySelection(term, nextState);
    };
    const toggleCopyModeVisualSelection = () => {
      const state = copyModeRef.current;
      if (!state) {
        return;
      }
      const nextState = {
        ...state,
        anchor: state.anchor ? null : state.cursor,
      };
      copyModeRef.current = nextState;
      copyModeApplySelection(term, nextState);
    };
    const handleCopyModeKey = (event: KeyboardEvent) => {
      const state = copyModeRef.current;
      if (!state) {
        return false;
      }
      event.preventDefault();
      const key = keyFromKeyboardEvent(event);
      const rawKey = event.key === " " ? "space" : event.key.toLowerCase();
      if (key === "esc" || key === "q" || key === "ctrl+c") {
        exitCopyMode();
      } else if (["h", "j", "k", "l"].includes(rawKey)) {
        updateCopyModeCursor(copyModeMove(term, state.cursor, rawKey));
      } else if (rawKey === "w") {
        updateCopyModeCursor(copyModeWordStartForward(term, state.cursor));
      } else if (rawKey === "b") {
        updateCopyModeCursor(copyModeWordStartBackward(term, state.cursor));
      } else if (rawKey === "e") {
        updateCopyModeCursor(copyModeWordEndForward(term, state.cursor));
      } else if (rawKey === "{") {
        updateCopyModeCursor(copyModeParagraphBackward(term, state.cursor));
      } else if (rawKey === "}") {
        updateCopyModeCursor(copyModeParagraphForward(term, state.cursor));
      } else if (rawKey === "v" || rawKey === "space") {
        toggleCopyModeVisualSelection();
      } else if (rawKey === "y" || key === "enter") {
        copyModeCopyAndExit();
      }
      return true;
    };
    term.attachCustomKeyEventHandler((event) => {
      if (event.type !== "keydown") {
        return true;
      }
      if (isImeCompositionEvent(event)) {
        return true;
      }
      if (copyModeRef.current) {
        handleCopyModeKey(event);
        return false;
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
        copyTerminalSelection();
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
    let replayReady = false;
    let replayEndOffset = 0;
    const attachStartedAt = performance.now();
    const renderProfile: RenderProfile = {
      startedAt: attachStartedAt,
      chunks: 0,
      bytes: 0,
      batches: 0,
      maxBatchBytes: 0,
      totalFlushLatencyMs: 0,
      maxFlushLatencyMs: 0,
      replayBytes: 0,
      replayDurationMs: null,
    };
    const queuedEvents: PtyWireEvent[] = [];
    const pendingWriteChunks: Uint8Array[] = [];
    let pendingWriteBytes = 0;
    let pendingWriteFrame = 0;
    let pendingFirstQueuedAt: number | null = null;
    const flushPtyWrites = () => {
      pendingWriteFrame = 0;
      if (disposed || pendingWriteChunks.length === 0) {
        pendingWriteChunks.length = 0;
        pendingWriteBytes = 0;
        pendingFirstQueuedAt = null;
        return;
      }
      const chunks = pendingWriteChunks.splice(0);
      const byteCount = pendingWriteBytes;
      const queuedAt = pendingFirstQueuedAt;
      pendingWriteBytes = 0;
      pendingFirstQueuedAt = null;
      const text = chunks
        .map((chunk) => textDecoderRef.current.decode(chunk, { stream: true }))
        .join("");
      if (!text) {
        return;
      }
      applyOutputMetadata(text);
      applyTriggers(text);
      term.write(text);
      const flushLatency = queuedAt === null ? 0 : performance.now() - queuedAt;
      renderProfile.batches += 1;
      renderProfile.maxBatchBytes = Math.max(renderProfile.maxBatchBytes, byteCount);
      renderProfile.totalFlushLatencyMs += flushLatency;
      renderProfile.maxFlushLatencyMs = Math.max(
        renderProfile.maxFlushLatencyMs,
        flushLatency,
      );
      terminalDebug("write batch", {
        ptyId,
        chunks: chunks.length,
        bytes: byteCount,
        characters: text.length,
        flushLatencyMs: Math.round(flushLatency),
      });
    };
    const queuePtyBytes = (bytes: Uint8Array) => {
      if (bytes.length === 0) {
        return;
      }
      renderProfile.chunks += 1;
      renderProfile.bytes += bytes.length;
      pendingFirstQueuedAt ??= performance.now();
      pendingWriteChunks.push(bytes);
      pendingWriteBytes += bytes.length;
      if (pendingWriteBytes >= 64 * 1024) {
        if (pendingWriteFrame) {
          cancelAnimationFrame(pendingWriteFrame);
        }
        flushPtyWrites();
        return;
      }
      if (!pendingWriteFrame) {
        pendingWriteFrame = requestAnimationFrame(flushPtyWrites);
      }
    };
    const writePtyData = (data: string) => queuePtyBytes(decodeBase64(data));
    const writePtyEventData = (event: { data: string; offset: number }) => {
      const bytes = decodeBase64(event.data);
      if (event.offset < replayEndOffset) {
        const overlap = replayEndOffset - event.offset;
        if (overlap >= bytes.length) {
          return;
        }
        queuePtyBytes(bytes.subarray(overlap));
        return;
      }
      queuePtyBytes(bytes);
    };
    const handlePtyEvent = (event: PtyWireEvent) => {
      if (event.type === "data") {
        writePtyEventData(event);
      } else if (event.type === "notification") {
        window.dispatchEvent(
          new CustomEvent("onibi:pty-notification", {
            detail: {
              ptyId,
              source: event.source,
              title: event.title,
              body: event.body ?? null,
              urgency: event.urgency ?? null,
            },
          }),
        );
      } else {
        const suffix = event.signal ? ` (${event.signal})` : "";
        if (pendingWriteFrame) {
          cancelAnimationFrame(pendingWriteFrame);
        }
        flushPtyWrites();
        term.write(`\r\n[process exited: ${event.code}${suffix}]\r\n`);
        onExit?.({ code: event.code, signal: event.signal });
      }
    };
    const flushQueuedEvents = () => {
      for (const event of queuedEvents.splice(0)) {
        handlePtyEvent(event);
      }
    };

    void subscribePty(ptyId, (event) => {
      if (disposed) {
        return;
      }
      if (!replayReady) {
        queuedEvents.push(event);
        return;
      }
      handlePtyEvent(event);
    })
      .then((dispose) => {
        if (disposed) {
          dispose();
          return null;
        } else {
          unlisten = dispose;
        }
        return ptyReplay(ptyId);
      })
      .then((replay) => {
        if (disposed) {
          return;
        }
        if (replay) {
          replayEndOffset = replay.endOffset;
          renderProfile.replayBytes = Math.max(0, replay.endOffset - replay.startOffset);
          renderProfile.replayDurationMs = performance.now() - attachStartedAt;
          writePtyData(replay.data);
          terminalDebug("replay snapshot", {
            ptyId,
            startOffset: replay.startOffset,
            endOffset: replay.endOffset,
            encodedBytes: replay.data.length,
          });
        }
        replayReady = true;
        flushQueuedEvents();
      })
      .catch((error) => {
        if (!disposed) {
          const hasQueuedData = queuedEvents.some((event) => event.type === "data");
          replayReady = true;
          flushQueuedEvents();
          console.warn("failed to attach pty output", error);
          terminalDebug("attach failed", {
            ptyId,
            queuedEvents: queuedEvents.length,
            hasQueuedData,
            error: String(error),
          });
          if (!hasQueuedData) {
            onUnavailable?.(error);
          }
        }
      });

    function handleJumpToLastPrompt(event: Event) {
      if ((event as CustomEvent<{ ptyId?: string }>).detail?.ptyId === ptyId) {
        term.scrollToBottom();
        term.focus();
      }
    }
    function handleCopyModeRequest(event: Event) {
      if ((event as CustomEvent<{ ptyId?: string }>).detail?.ptyId === ptyId) {
        enterCopyMode();
      }
    }
    window.addEventListener("onibi:jump-last-prompt", handleJumpToLastPrompt);
    window.addEventListener("onibi:terminal-copy-mode", handleCopyModeRequest);

    return () => {
      disposed = true;
      copyModeRef.current = null;
      setCopyModeActive(false);
      cancelScheduledLayout();
      cancelAnimationFrame(frame);
      cancelAnimationFrame(selectionCopyFrame);
      if (pendingWriteFrame) {
        cancelAnimationFrame(pendingWriteFrame);
      }
      unlisten?.();
      resizeObserver.disconnect();
      container.removeEventListener("dblclick", handleDoubleClick);
      window.removeEventListener("onibi:jump-last-prompt", handleJumpToLastPrompt);
      window.removeEventListener("onibi:terminal-copy-mode", handleCopyModeRequest);
      inputDisposable.dispose();
      webLinksAddon.dispose();
      webglContextLossDisposable?.dispose();
      webglAddon?.dispose();
      terminalDebug("render profile", {
        ptyId,
        bytes: renderProfile.bytes,
        chunks: renderProfile.chunks,
        batches: renderProfile.batches,
        maxBatchBytes: renderProfile.maxBatchBytes,
        avgFlushLatencyMs:
          renderProfile.batches > 0
            ? Math.round(renderProfile.totalFlushLatencyMs / renderProfile.batches)
            : 0,
        maxFlushLatencyMs: Math.round(renderProfile.maxFlushLatencyMs),
        replayBytes: renderProfile.replayBytes,
        replayDurationMs:
          renderProfile.replayDurationMs === null
            ? null
            : Math.round(renderProfile.replayDurationMs),
        totalDurationMs: Math.round(performance.now() - renderProfile.startedAt),
      });
      searchAddon.dispose();
      fitAddon.dispose();
      unicode11Addon.dispose();
      serializeAddon.dispose();
      term.dispose();
      terminalRef.current = null;
      fitAddonRef.current = null;
      searchAddonRef.current = null;
    };
  }, [
    handleTerminalLink,
    onExit,
    onUnavailable,
    openSearch,
    ptyId,
    settings.terminalCopyFormat,
    settings.terminalOsc52Clipboard,
    settings.terminalScreenReaderMode,
    triggerMatchers,
  ]);

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
      screenReaderMode: settings.terminalScreenReaderMode,
      theme: resolvedTerminalTheme,
    };
    term.clearTextureAtlas();
    refreshTerminalLayout(visibleRef.current);
  }, [
    fontSize,
    ptyId,
    resolvedFontFamily,
    resolvedScrollback,
    settings.terminalScreenReaderMode,
    resolvedTerminalTheme.background,
    resolvedTerminalTheme.cursor,
    resolvedTerminalTheme.foreground,
    resolvedTerminalTheme.selectionBackground,
  ]);

  return (
    <div
      className={[
        "terminal-view-shell",
        copyModeActive ? "copy-mode" : "",
        settings.terminalTransparentBackground ? "transparent" : "",
      ]
        .filter(Boolean)
        .join(" ")}
      data-copy-mode={copyModeActive ? "true" : "false"}
      data-transparent={settings.terminalTransparentBackground ? "true" : "false"}
    >
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
