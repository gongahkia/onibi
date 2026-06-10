import { relaunch } from "@tauri-apps/plugin-process";
import {
  check,
  type DownloadEvent,
  type Update,
} from "@tauri-apps/plugin-updater";
import { APP_VERSION } from "./app-version";

export const UPDATE_CHECK_EVENT = "onibi:check-updates";
export const UPDATE_LAST_CHECK_KEY = "onibiUpdateLastCheckAt";
export const UPDATE_CHECK_INTERVAL_MS = 24 * 60 * 60 * 1000;
export const ONIBI_RELEASES_URL = "https://github.com/gongahkia/onibi/releases/latest";

let pendingUpdate: Update | null = null;

export interface AppUpdateCheckResult {
  available: boolean;
  currentVersion: string;
  version?: string;
  date?: string;
  body?: string;
  releaseUrl: string;
}

export interface AppUpdateProgress {
  phase: "started" | "progress" | "finished";
  downloadedBytes: number;
  totalBytes: number | null;
}

export function shouldAutoCheckForUpdates(now = Date.now()): boolean {
  const raw = localStorage.getItem(UPDATE_LAST_CHECK_KEY);
  const last = raw ? Number(raw) : 0;
  return !Number.isFinite(last) || last <= 0 || now - last >= UPDATE_CHECK_INTERVAL_MS;
}

export function recordUpdateCheck(now = Date.now()): void {
  localStorage.setItem(UPDATE_LAST_CHECK_KEY, String(now));
}

export async function checkForAppUpdate(): Promise<AppUpdateCheckResult> {
  const update = await check();
  pendingUpdate = update;
  if (!update) {
    return {
      available: false,
      currentVersion: APP_VERSION,
      releaseUrl: ONIBI_RELEASES_URL,
    };
  }
  return {
    available: true,
    currentVersion: APP_VERSION,
    version: update.version,
    date: update.date,
    body: update.body,
    releaseUrl: ONIBI_RELEASES_URL,
  };
}

export async function installPendingAppUpdate(
  onProgress?: (progress: AppUpdateProgress) => void,
): Promise<void> {
  const update = pendingUpdate ?? (await check());
  if (!update) {
    throw new Error("No app update is available.");
  }
  pendingUpdate = update;
  let downloadedBytes = 0;
  let totalBytes: number | null = null;
  await update.downloadAndInstall((event: DownloadEvent) => {
    if (event.event === "Started") {
      totalBytes = event.data.contentLength ?? null;
      downloadedBytes = 0;
      onProgress?.({ phase: "started", downloadedBytes, totalBytes });
    } else if (event.event === "Progress") {
      downloadedBytes += event.data.chunkLength;
      onProgress?.({ phase: "progress", downloadedBytes, totalBytes });
    } else if (event.event === "Finished") {
      onProgress?.({ phase: "finished", downloadedBytes, totalBytes });
    }
  });
  await relaunch();
}
