import { JSDOM } from "jsdom";
import type {
  VoiceRecognitionConstructor,
  VoiceRecognitionErrorEvent,
  VoiceRecognitionResultEvent,
  VoiceRecognitionResultList
} from "../voice";

test("types final voice transcripts and renders interim text", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { VoiceInputController } = await import("../voice");
  const root = requireRoot();
  const sent: string[] = [];
  const controller = new VoiceInputController({
    root,
    sendText: (text) => sent.push(text),
    recognitionConstructor: fakeRecognitionConstructor,
    permissionState: async () => "prompt"
  });
  await controller.toggle();
  const recognition = requireRecognition();
  recognition.emitResult([{ transcript: "hello", isFinal: false }]);
  expect(root.querySelector(".voice-status")?.textContent).toBe("Listening");
  expect(root.querySelector(".voice-transcript")?.textContent).toContain("hello");
  expect(sent).toEqual([]);
  recognition.emitResult([{ transcript: "hello world", isFinal: true }]);
  expect(sent).toEqual(["hello world"]);
  expect(root.querySelector(".voice-transcript")?.textContent).toContain("hello world");
  recognition.stop();
  expect(root.querySelector(".voice-status")?.textContent).toBe("Stopped");
  controller.dispose();
  dom.window.close();
});

test("handles denied microphone permission without starting recognition", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { VoiceInputController } = await import("../voice");
  const root = requireRoot();
  FakeRecognition.last = undefined;
  const toasts: string[] = [];
  const controller = new VoiceInputController({
    root,
    sendText: () => {},
    showToast: (message) => toasts.push(message),
    recognitionConstructor: fakeRecognitionConstructor,
    permissionState: async () => "denied"
  });
  await controller.toggle();
  expect(FakeRecognition.last).toBeUndefined();
  expect(root.textContent).toContain("Microphone permission blocked.");
  expect(toasts).toContain("Microphone permission blocked.");
  controller.dispose();
  dom.window.close();
});

test("shows unsupported fallback when speech recognition is missing", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { VoiceInputController } = await import("../voice");
  const root = requireRoot();
  const controller = new VoiceInputController({
    root,
    sendText: () => {},
    recognitionConstructor: () => undefined
  });
  expect(controller.isSupported()).toBe(false);
  await controller.toggle();
  expect(root.textContent).toContain("Voice input unavailable in this browser.");
  controller.dispose();
  dom.window.close();
});

class FakeRecognition {
  static last: FakeRecognition | undefined;
  continuous = false;
  interimResults = false;
  lang = "";
  maxAlternatives = 0;
  onend: ((event: Event) => void) | null = null;
  onerror: ((event: VoiceRecognitionErrorEvent) => void) | null = null;
  onresult: ((event: VoiceRecognitionResultEvent) => void) | null = null;
  onstart: ((event: Event) => void) | null = null;

  constructor() {
    FakeRecognition.last = this;
  }

  start(): void {
    this.onstart?.(new Event("start"));
  }

  stop(): void {
    this.onend?.(new Event("end"));
  }

  abort(): void {
    this.onend?.(new Event("end"));
  }

  emitResult(items: Array<{ transcript: string; isFinal: boolean }>): void {
    this.onresult?.(
      Object.assign(new Event("result"), {
        resultIndex: 0,
        results: resultList(items)
      })
    );
  }
}

function resultList(
  items: Array<{ transcript: string; isFinal: boolean }>
): VoiceRecognitionResultList {
  const results = items.map((item) => ({
    0: { transcript: item.transcript },
    isFinal: item.isFinal,
    length: 1
  }));
  return Object.assign(results, {
    item: (index: number) => results[index]
  }) as VoiceRecognitionResultList;
}

function fakeRecognitionConstructor(): VoiceRecognitionConstructor {
  return FakeRecognition as unknown as VoiceRecognitionConstructor;
}

function requireRecognition(): FakeRecognition {
  if (FakeRecognition.last === undefined) {
    throw new Error("missing fake recognition");
  }
  return FakeRecognition.last;
}

function requireRoot(): HTMLElement {
  const root = document.getElementById("root");
  if (root === null) {
    throw new Error("missing root");
  }
  return root;
}

function installDOM(markup: string): JSDOM {
  const dom = new JSDOM(markup, { url: "https://onibi.test/" });
  const win = dom.window;
  Object.defineProperty(globalThis, "window", { value: win, configurable: true });
  Object.defineProperty(globalThis, "document", { value: win.document, configurable: true });
  Object.defineProperty(globalThis, "navigator", { value: win.navigator, configurable: true });
  Object.defineProperty(globalThis, "Event", { value: win.Event, configurable: true });
  Object.defineProperty(globalThis, "HTMLElement", { value: win.HTMLElement, configurable: true });
  Object.defineProperty(globalThis, "HTMLButtonElement", {
    value: win.HTMLButtonElement,
    configurable: true
  });
  return dom;
}
