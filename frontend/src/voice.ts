export type VoiceRecognitionConstructor = new () => VoiceRecognition;

export type VoiceRecognition = {
  continuous: boolean;
  interimResults: boolean;
  lang: string;
  maxAlternatives: number;
  onend: ((event: Event) => void) | null;
  onerror: ((event: VoiceRecognitionErrorEvent) => void) | null;
  onresult: ((event: VoiceRecognitionResultEvent) => void) | null;
  onstart: ((event: Event) => void) | null;
  abort: () => void;
  start: () => void;
  stop: () => void;
};

export type VoiceRecognitionResultEvent = Event & {
  resultIndex: number;
  results: VoiceRecognitionResultList;
};

export type VoiceRecognitionResultList = {
  length: number;
  item?: (index: number) => VoiceRecognitionResult;
  [index: number]: VoiceRecognitionResult;
};

export type VoiceRecognitionResult = {
  isFinal: boolean;
  length: number;
  [index: number]: { transcript: string; confidence?: number };
};

export type VoiceRecognitionErrorEvent = Event & {
  error?: string;
  message?: string;
};

type VoiceInputOptions = {
  root: HTMLElement;
  sendText: (text: string) => void;
  showToast?: (message: string) => void;
  focus?: () => void;
  recognitionConstructor?: () => VoiceRecognitionConstructor | undefined;
  permissionState?: () => Promise<PermissionState | "unknown">;
  storage?: Pick<Storage, "getItem" | "setItem">;
};

const languageKey = "onibi-voice-lang";
const languages = [
  ["", "auto"],
  ["en-US", "en-US"],
  ["en-GB", "en-GB"],
  ["es-ES", "es-ES"],
  ["fr-FR", "fr-FR"],
  ["de-DE", "de-DE"],
  ["ja-JP", "ja-JP"],
  ["zh-CN", "zh-CN"]
] as const;

export class VoiceInputController {
  private readonly doc: Document;
  private readonly overlay: HTMLElement;
  private readonly status: HTMLElement;
  private readonly transcript: HTMLElement;
  private readonly language: HTMLSelectElement;
  private recognition: VoiceRecognition | undefined;
  private listening = false;
  private finalTranscript = "";
  private sentFinal = false;

  constructor(private readonly options: VoiceInputOptions) {
    this.doc = options.root.ownerDocument;
    this.overlay = this.doc.createElement("div");
    this.overlay.className = "voice-overlay";
    this.overlay.hidden = true;
    this.overlay.setAttribute("aria-live", "polite");
    const header = this.doc.createElement("div");
    header.className = "voice-header";
    const title = this.doc.createElement("div");
    title.className = "voice-title";
    title.textContent = "Voice";
    this.status = this.doc.createElement("div");
    this.status.className = "voice-status";
    this.language = this.languageSelect();
    const stop = this.button("Stop");
    stop.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      this.stop();
    });
    stop.addEventListener("click", () => this.stop());
    header.append(title, this.status, this.language, stop);
    this.transcript = this.doc.createElement("div");
    this.transcript.className = "voice-transcript";
    this.overlay.append(header, this.transcript);
    options.root.append(this.overlay);
  }

  isSupported(): boolean {
    return this.recognitionConstructor() !== undefined;
  }

  async toggle(): Promise<void> {
    if (this.listening) {
      this.stop();
      return;
    }
    await this.start();
  }

  stop(): void {
    if (this.recognition === undefined) {
      return;
    }
    this.setStatus("Stopping");
    this.recognition.stop();
  }

  dispose(): void {
    this.recognition?.abort();
    this.overlay.remove();
  }

  private async start(): Promise<void> {
    const ctor = this.recognitionConstructor();
    if (ctor === undefined) {
      this.showMessage("Unavailable", "Voice input unavailable in this browser.");
      return;
    }
    if ((await this.permissionState()) === "denied") {
      this.showMessage("Blocked", "Microphone permission blocked.");
      return;
    }
    const recognition = new ctor();
    this.recognition = recognition;
    this.finalTranscript = "";
    this.sentFinal = false;
    recognition.continuous = false;
    recognition.interimResults = true;
    recognition.maxAlternatives = 1;
    recognition.lang = this.selectedLanguage();
    recognition.onstart = () => {
      this.listening = true;
      this.setStatus("Listening");
    };
    recognition.onresult = (event) => this.handleResult(event);
    recognition.onerror = (event) => {
      const message = errorMessage(event);
      this.setStatus(message);
      this.options.showToast?.(message);
    };
    recognition.onend = () => {
      this.listening = false;
      this.setStatus("Stopped");
      this.options.focus?.();
    };
    this.transcript.textContent = "";
    this.show();
    this.setStatus("Starting");
    try {
      recognition.start();
    } catch {
      this.recognition = undefined;
      this.listening = false;
      this.showMessage("Unavailable", "Voice input unavailable.");
    }
  }

  private handleResult(event: VoiceRecognitionResultEvent): void {
    let finalText = "";
    let interimText = "";
    for (let i = event.resultIndex; i < event.results.length; i += 1) {
      const result = resultAt(event.results, i);
      const transcript = result?.[0]?.transcript ?? "";
      if (result?.isFinal === true) {
        finalText = appendText(finalText, transcript);
      } else {
        interimText = appendText(interimText, transcript);
      }
    }
    const normalizedFinal = normalizeTranscript(finalText);
    if (normalizedFinal !== "") {
      this.options.sendText(this.sentFinal ? ` ${normalizedFinal}` : normalizedFinal);
      this.sentFinal = true;
      this.finalTranscript = appendText(this.finalTranscript, normalizedFinal);
    }
    const displayText = appendText(this.finalTranscript, normalizeTranscript(interimText));
    this.transcript.textContent = displayText;
  }

  private showMessage(status: string, message: string): void {
    this.show();
    this.setStatus(status);
    this.transcript.textContent = message;
    this.options.showToast?.(message);
    this.options.focus?.();
  }

  private show(): void {
    this.overlay.hidden = false;
  }

  private setStatus(value: string): void {
    this.status.textContent = value;
  }

  private selectedLanguage(): string {
    const lang = this.language.value.trim();
    return lang === "" ? this.doc.documentElement.lang || navigator.language || "en-US" : lang;
  }

  private languageSelect(): HTMLSelectElement {
    const select = this.doc.createElement("select");
    select.className = "voice-language";
    select.tabIndex = -1;
    for (const [value, label] of languages) {
      const option = this.doc.createElement("option");
      option.value = value;
      option.textContent = label;
      select.append(option);
    }
    select.value = this.storedLanguage();
    select.addEventListener("change", () => {
      this.storeLanguage(select.value);
      this.options.focus?.();
    });
    return select;
  }

  private storedLanguage(): string {
    try {
      return this.options.storage?.getItem(languageKey) ?? "";
    } catch {
      return "";
    }
  }

  private storeLanguage(value: string): void {
    try {
      this.options.storage?.setItem(languageKey, value);
    } catch {
      return;
    }
  }

  private recognitionConstructor(): VoiceRecognitionConstructor | undefined {
    if (this.options.recognitionConstructor !== undefined) {
      return this.options.recognitionConstructor();
    }
    const win = window as Window & {
      SpeechRecognition?: VoiceRecognitionConstructor;
      webkitSpeechRecognition?: VoiceRecognitionConstructor;
    };
    return win.SpeechRecognition ?? win.webkitSpeechRecognition;
  }

  private async permissionState(): Promise<PermissionState | "unknown"> {
    if (this.options.permissionState !== undefined) {
      return this.options.permissionState();
    }
    try {
      const permissions = navigator.permissions;
      if (permissions?.query === undefined) {
        return "unknown";
      }
      return (await permissions.query({ name: "microphone" as PermissionName })).state;
    } catch {
      return "unknown";
    }
  }

  private button(label: string): HTMLButtonElement {
    const el = this.doc.createElement("button");
    el.type = "button";
    el.className = "voice-button";
    el.textContent = label;
    el.tabIndex = -1;
    return el;
  }
}

function resultAt(
  results: VoiceRecognitionResultList,
  index: number
): VoiceRecognitionResult | undefined {
  return results[index] ?? results.item?.(index);
}

function normalizeTranscript(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

function appendText(current: string, next: string): string {
  const clean = normalizeTranscript(next);
  if (clean === "") {
    return current;
  }
  return current === "" ? clean : `${current} ${clean}`;
}

function errorMessage(event: VoiceRecognitionErrorEvent): string {
  switch (event.error) {
    case "not-allowed":
    case "service-not-allowed":
      return "Microphone permission blocked.";
    case "audio-capture":
      return "No microphone found.";
    case "no-speech":
      return "No speech detected.";
    case "language-not-supported":
    case "language-unavailable":
      return "Voice language unavailable.";
    default:
      return event.message?.trim() || "Voice input failed.";
  }
}
