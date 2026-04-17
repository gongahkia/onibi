export interface RecentHost {
  baseURL: string;
  lastUsedAt: number;
}

const STORAGE_KEY = "onibi-web.recent-hosts";
const MAX_RECENT = 5;

function safeStorage(): Storage | null {
  try {
    const key = "__onibi_test__";
    window.localStorage.setItem(key, "x");
    window.localStorage.removeItem(key);
    return window.localStorage;
  } catch {
    return null;
  }
}

export function loadRecentHosts(): RecentHost[] {
  const store = safeStorage();
  if (!store) return [];
  const raw = store.getItem(STORAGE_KEY);
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter(
        (entry): entry is RecentHost =>
          typeof entry === "object" &&
          entry !== null &&
          typeof (entry as RecentHost).baseURL === "string" &&
          typeof (entry as RecentHost).lastUsedAt === "number"
      )
      .slice(0, MAX_RECENT);
  } catch {
    return [];
  }
}

export function rememberHost(baseURL: string, now: number = Date.now()): RecentHost[] {
  const trimmed = baseURL.trim();
  if (!trimmed) return loadRecentHosts();

  const current = loadRecentHosts().filter((entry) => entry.baseURL !== trimmed);
  const next: RecentHost[] = [{ baseURL: trimmed, lastUsedAt: now }, ...current].slice(0, MAX_RECENT);

  const store = safeStorage();
  if (store) {
    try {
      store.setItem(STORAGE_KEY, JSON.stringify(next));
    } catch {
      // ignore quota errors
    }
  }
  return next;
}

export function forgetHost(baseURL: string): RecentHost[] {
  const next = loadRecentHosts().filter((entry) => entry.baseURL !== baseURL);
  const store = safeStorage();
  if (store) {
    try {
      store.setItem(STORAGE_KEY, JSON.stringify(next));
    } catch {
      // ignore
    }
  }
  return next;
}
