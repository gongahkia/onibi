import { convertFileSrc } from "@tauri-apps/api/core";
import {
  useSessionStore,
  type AgentKind,
  type AppSettings,
  type NotificationDelivery,
} from "./sessions";

export type OnibiNotificationKind = "info" | "request" | "completion";
export type OnibiNotificationSource = "trigger" | "pty" | "session";

export interface OnibiNotificationIntent {
  title: string;
  body?: string | null;
  kind?: OnibiNotificationKind;
  source: OnibiNotificationSource;
  sessionId?: string | null;
  agent?: AgentKind | null;
  deliver?: boolean;
}

export interface OnibiNotificationToast {
  id: string;
  title: string;
  body: string | null;
  kind: OnibiNotificationKind;
  source: OnibiNotificationSource;
  sessionId: string | null;
  agent: AgentKind | null;
  timestamp: number;
}

export interface OnibiTerminalNotice {
  title: string;
  body: string | null;
  kind: OnibiNotificationKind;
  source: OnibiNotificationSource;
  sessionId: string | null;
  agent: AgentKind | null;
}

export interface OnibiNotificationResult {
  delivered: boolean;
  soundPlayed: boolean;
  suppressed: boolean;
  delivery: NotificationDelivery;
}

const TOAST_EVENT = "onibi:notification-toast";
const TERMINAL_NOTICE_EVENT = "onibi:terminal-notice";
let toastSequence = 0;

function currentSettings(): AppSettings {
  return useSessionStore.getState().settings;
}

function isForegroundSession(sessionId: string | null | undefined): boolean {
  if (!sessionId) {
    return false;
  }
  const state = useSessionStore.getState();
  return state.selectedFile === null && state.activeSessionId === sessionId;
}

function shouldSuppressForeground(intent: OnibiNotificationIntent): boolean {
  const settings = currentSettings();
  return (
    settings.suppressForegroundTabNotifications &&
    isForegroundSession(intent.sessionId)
  );
}

function soundAllowed(settings: AppSettings, agent: AgentKind | null | undefined): boolean {
  return !agent || settings.soundAgents[agent] !== false;
}

function emitToast(intent: OnibiNotificationIntent): OnibiNotificationToast {
  const toast: OnibiNotificationToast = {
    id: `notification-${Date.now()}-${++toastSequence}`,
    title: intent.title,
    body: intent.body ?? null,
    kind: intent.kind ?? "info",
    source: intent.source,
    sessionId: intent.sessionId ?? null,
    agent: intent.agent ?? null,
    timestamp: Date.now(),
  };
  window.dispatchEvent(new CustomEvent(TOAST_EVENT, { detail: toast }));
  return toast;
}

function emitTerminalNotice(intent: OnibiNotificationIntent): void {
  const notice: OnibiTerminalNotice = {
    title: intent.title,
    body: intent.body ?? null,
    kind: intent.kind ?? "info",
    source: intent.source,
    sessionId: intent.sessionId ?? null,
    agent: intent.agent ?? null,
  };
  window.dispatchEvent(new CustomEvent(TERMINAL_NOTICE_EVENT, { detail: notice }));
}

async function deliverSystemNotification(intent: OnibiNotificationIntent): Promise<boolean> {
  if (!("Notification" in window)) {
    emitToast(intent);
    return true;
  }
  let permission = Notification.permission;
  if (permission === "default") {
    permission = await Notification.requestPermission();
  }
  if (permission !== "granted") {
    emitToast(intent);
    return true;
  }
  new Notification(intent.title, {
    body: intent.body ?? undefined,
  });
  return true;
}

function audioUrlForPath(path: string): string {
  const trimmed = path.trim();
  if (/^(asset|blob|data|file|https?):/i.test(trimmed)) {
    return trimmed;
  }
  return convertFileSrc(trimmed);
}

async function playConfiguredAudio(path: string): Promise<boolean> {
  const trimmed = path.trim();
  if (!trimmed) {
    return false;
  }
  const audio = new Audio(audioUrlForPath(trimmed));
  await audio.play();
  return true;
}

async function playGeneratedChime(kind: "request" | "completion"): Promise<boolean> {
  const audioWindow = window as typeof window & {
    webkitAudioContext?: typeof AudioContext;
  };
  const AudioContextCtor = audioWindow.AudioContext ?? audioWindow.webkitAudioContext;
  if (!AudioContextCtor) {
    return false;
  }
  const context = new AudioContextCtor();
  if (context.state === "suspended") {
    await context.resume().catch(() => undefined);
  }
  const oscillator = context.createOscillator();
  const gain = context.createGain();
  oscillator.type = "sine";
  oscillator.frequency.value = kind === "request" ? 740 : 520;
  gain.gain.setValueAtTime(0.0001, context.currentTime);
  gain.gain.exponentialRampToValueAtTime(0.08, context.currentTime + 0.015);
  gain.gain.exponentialRampToValueAtTime(0.0001, context.currentTime + 0.18);
  oscillator.connect(gain);
  gain.connect(context.destination);
  oscillator.start();
  oscillator.stop(context.currentTime + 0.2);
  window.setTimeout(() => void context.close().catch(() => undefined), 240);
  return true;
}

async function playNotificationSound(intent: OnibiNotificationIntent): Promise<boolean> {
  const kind = intent.kind;
  if (kind !== "request" && kind !== "completion") {
    return false;
  }
  const settings = currentSettings();
  if (!settings.soundAlertsEnabled || !soundAllowed(settings, intent.agent)) {
    return false;
  }
  const customPath =
    kind === "request" ? settings.soundRequestPath : settings.soundCompletionPath;
  try {
    if (customPath.trim()) {
      return await playConfiguredAudio(customPath);
    }
    return await playGeneratedChime(kind);
  } catch (error) {
    console.warn("notification sound failed", error);
    return false;
  }
}

export async function dispatchOnibiNotification(
  intent: OnibiNotificationIntent,
): Promise<OnibiNotificationResult> {
  const settings = currentSettings();
  const delivery = settings.notificationDelivery;
  if (shouldSuppressForeground(intent)) {
    return { delivered: false, soundPlayed: false, suppressed: true, delivery };
  }

  let delivered = false;
  if (intent.deliver !== false) {
    if (delivery === "system") {
      delivered = await deliverSystemNotification(intent);
    } else if (delivery === "terminal") {
      emitTerminalNotice(intent);
      delivered = true;
    } else {
      emitToast(intent);
      delivered = true;
    }
  }

  const soundPlayed = await playNotificationSound(intent);
  return { delivered, soundPlayed, suppressed: false, delivery };
}

export const notificationEvents = {
  toast: TOAST_EVENT,
  terminalNotice: TERMINAL_NOTICE_EVENT,
};
