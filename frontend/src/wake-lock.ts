type WakeLockSentinelLike = EventTarget & {
  release: () => Promise<void>;
};

type WakeLockNavigatorLike = Navigator & {
  wakeLock?: {
    request: (type: "screen") => Promise<WakeLockSentinelLike>;
  };
};

type WakeLockOptions = {
  documentRef?: Document;
  navigatorRef?: WakeLockNavigatorLike;
  storage?: Pick<Storage, "getItem">;
  storageKey?: string;
};

const defaultStorageKey = "onibi:wake-lock";

export class ApprovalWakeLock {
  private readonly documentRef: Document;
  private readonly navigatorRef: WakeLockNavigatorLike;
  private readonly storage?: Pick<Storage, "getItem">;
  private readonly storageKey: string;
  private sentinel: WakeLockSentinelLike | undefined;
  private pendingCount = 0;
  private requestInFlight: Promise<void> | undefined;
  private disposed = false;

  constructor(options: WakeLockOptions = {}) {
    this.documentRef = options.documentRef ?? document;
    this.navigatorRef = options.navigatorRef ?? (navigator as WakeLockNavigatorLike);
    this.storage = options.storage ?? safeLocalStorage();
    this.storageKey = options.storageKey ?? defaultStorageKey;
    this.documentRef.addEventListener("visibilitychange", this.handleVisibilityChange);
  }

  setPendingCount(count: number): void {
    this.pendingCount = Math.max(0, count);
    void this.sync();
  }

  dispose(): void {
    this.disposed = true;
    this.documentRef.removeEventListener("visibilitychange", this.handleVisibilityChange);
    void this.release();
  }

  private readonly handleVisibilityChange = (): void => {
    void this.sync();
  };

  private async sync(): Promise<void> {
    if (
      this.disposed ||
      this.pendingCount === 0 ||
      this.disabled() ||
      this.documentRef.visibilityState !== "visible"
    ) {
      await this.release();
      return;
    }
    await this.acquire();
  }

  private async acquire(): Promise<void> {
    if (this.sentinel !== undefined || this.requestInFlight !== undefined) {
      return this.requestInFlight;
    }
    const wakeLock = this.navigatorRef.wakeLock;
    if (wakeLock === undefined) {
      return;
    }
    this.requestInFlight = wakeLock
      .request("screen")
      .then((sentinel) => {
        this.sentinel = sentinel;
        sentinel.addEventListener("release", this.handleSentinelRelease, { once: true });
      })
      .catch(() => {})
      .finally(() => {
        this.requestInFlight = undefined;
      });
    return this.requestInFlight;
  }

  private readonly handleSentinelRelease = (): void => {
    this.sentinel = undefined;
    void this.sync();
  };

  private async release(): Promise<void> {
    const sentinel = this.sentinel;
    this.sentinel = undefined;
    if (sentinel === undefined) {
      return;
    }
    await sentinel.release().catch(() => {});
  }

  private disabled(): boolean {
    const value = this.storage?.getItem(this.storageKey)?.trim().toLowerCase();
    return value === "0" || value === "false" || value === "off";
  }
}

function safeLocalStorage(): Pick<Storage, "getItem"> | undefined {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}
