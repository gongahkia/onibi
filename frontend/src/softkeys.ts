import { terminalThemeLabel, terminalThemeNames } from "./terminal";
import type { TerminalThemeName } from "./terminal";

type SoftKeyBarOptions = {
  root: HTMLElement;
  sendBytes: (data: Uint8Array) => void;
  sendText: (data: string) => void;
  pageUp: () => Promise<void> | void;
  pageDown: () => Promise<void> | void;
  focus: () => void;
  getTheme: () => TerminalThemeName;
  setTheme: (theme: TerminalThemeName) => void;
  decreaseFontSize: () => void;
  increaseFontSize: () => void;
  readOnly?: boolean;
};

type Modifier = "ctrl" | "alt";

type KeyDef = {
  label: string;
  base: Uint8Array;
  repeat?: boolean;
  ctrl?: Uint8Array;
  alt?: Uint8Array;
};

const esc = 0x1b;
const keys: KeyDef[] = [
  { label: "Esc", base: bytes(esc) },
  { label: "Tab", base: bytes(0x09), ctrl: bytes(0x09), alt: bytes(esc, 0x09) },
  {
    label: "↑",
    base: bytes(esc, 0x5b, 0x41),
    repeat: true,
    ctrl: seq("\x1b[1;5A"),
    alt: seq("\x1b[1;3A")
  },
  {
    label: "↓",
    base: bytes(esc, 0x5b, 0x42),
    repeat: true,
    ctrl: seq("\x1b[1;5B"),
    alt: seq("\x1b[1;3B")
  },
  {
    label: "←",
    base: bytes(esc, 0x5b, 0x44),
    repeat: true,
    ctrl: seq("\x1b[1;5D"),
    alt: seq("\x1b[1;3D")
  },
  {
    label: "→",
    base: bytes(esc, 0x5b, 0x43),
    repeat: true,
    ctrl: seq("\x1b[1;5C"),
    alt: seq("\x1b[1;3C")
  },
  { label: "^C", base: bytes(0x03) },
  { label: "^D", base: bytes(0x04) },
  { label: "^Z", base: bytes(0x1a) }
];

export class SoftKeyBar {
  private modifier: Modifier | undefined;
  private readonly buttons = new Map<Modifier, HTMLButtonElement>();
  private readonly themePicker: HTMLSelectElement;

  constructor(private readonly options: SoftKeyBarOptions) {
    const frag = document.createDocumentFragment();
    if (options.readOnly === true) {
      const banner = document.createElement("div");
      banner.className = "softkey-view-only";
      banner.textContent = "VIEW ONLY";
      frag.append(banner);
      frag.append(this.actionButton("A-", options.decreaseFontSize));
      frag.append(this.actionButton("A+", options.increaseFontSize));
      this.themePicker = this.themeSelect(options.getTheme());
      frag.append(this.themePicker);
      options.root.replaceChildren(frag);
      return;
    }
    frag.append(this.modifierButton("Ctrl", "ctrl"));
    frag.append(this.modifierButton("Alt", "alt"));
    for (const key of keys) {
      frag.append(this.keyButton(key));
    }
    frag.append(this.pageButton("PgUp", options.pageUp));
    frag.append(this.pageButton("PgDn", options.pageDown));
    frag.append(this.actionButton("A-", options.decreaseFontSize));
    frag.append(this.actionButton("A+", options.increaseFontSize));
    frag.append(this.pasteButton());
    this.themePicker = this.themeSelect(options.getTheme());
    frag.append(this.themePicker);
    options.root.replaceChildren(frag);
  }

  private modifierButton(label: string, modifier: Modifier): HTMLButtonElement {
    const el = this.button(label);
    this.buttons.set(modifier, el);
    el.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      this.modifier = this.modifier === modifier ? undefined : modifier;
      this.renderModifier();
      this.options.focus();
    });
    return el;
  }

  private keyButton(key: KeyDef): HTMLButtonElement {
    const el = this.button(key.label);
    let holdTimer = 0;
    let repeatTimer = 0;
    const stop = () => {
      window.clearTimeout(holdTimer);
      window.clearInterval(repeatTimer);
    };
    el.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      el.setPointerCapture(event.pointerId);
      this.sendKey(key);
      if (key.repeat === true) {
        holdTimer = window.setTimeout(() => {
          repeatTimer = window.setInterval(() => this.sendKey(key), 100);
        }, 500);
      }
    });
    el.addEventListener("pointerup", stop);
    el.addEventListener("pointercancel", stop);
    el.addEventListener("lostpointercapture", stop);
    return el;
  }

  private pasteButton(): HTMLButtonElement {
    const el = this.button("Paste");
    el.classList.add("softkey-paste");
    el.hidden = navigator.clipboard?.readText === undefined;
    el.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      void this.paste();
    });
    return el;
  }

  private pageButton(label: string, action: () => void): HTMLButtonElement {
    const el = this.button(label);
    el.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      void action();
      if (document.activeElement instanceof HTMLElement) {
        document.activeElement.blur();
      }
    });
    return el;
  }

  private actionButton(label: string, action: () => void): HTMLButtonElement {
    const el = this.button(label);
    el.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      action();
      this.options.focus();
    });
    return el;
  }

  private async paste(): Promise<void> {
    try {
      const text = await navigator.clipboard.readText();
      if (text.length > 0) {
        this.options.sendText(text);
      }
    } finally {
      this.options.focus();
    }
  }

  private sendKey(key: KeyDef): void {
    const modifier = this.modifier;
    const data =
      modifier === "ctrl"
        ? (key.ctrl ?? key.base)
        : modifier === "alt"
          ? (key.alt ?? prefixAlt(key.base))
          : key.base;
    this.options.sendBytes(data);
    if (modifier !== undefined) {
      this.modifier = undefined;
      this.renderModifier();
    }
    this.options.focus();
  }

  private renderModifier(): void {
    for (const [modifier, button] of this.buttons) {
      button.classList.toggle("active", this.modifier === modifier);
      button.setAttribute("aria-pressed", String(this.modifier === modifier));
    }
  }

  private button(label: string): HTMLButtonElement {
    const el = document.createElement("button");
    el.type = "button";
    el.className = "softkey-button";
    el.textContent = label;
    el.tabIndex = -1;
    return el;
  }

  private themeSelect(current: TerminalThemeName): HTMLSelectElement {
    const el = document.createElement("select");
    el.className = "softkey-button softkey-theme";
    el.tabIndex = -1;
    for (const theme of terminalThemeNames) {
      const option = document.createElement("option");
      option.value = theme;
      option.textContent = terminalThemeLabel(theme);
      el.append(option);
    }
    el.value = current;
    el.addEventListener("change", () => {
      const next = el.value as TerminalThemeName;
      this.options.setTheme(next);
      this.options.focus();
    });
    return el;
  }
}

function bytes(...values: number[]): Uint8Array {
  return new Uint8Array(values);
}

function seq(value: string): Uint8Array {
  return bytes(...Array.from(value, (char) => char.charCodeAt(0)));
}

function prefixAlt(data: Uint8Array): Uint8Array {
  const out = new Uint8Array(data.length + 1);
  out[0] = esc;
  out.set(data, 1);
  return out;
}
