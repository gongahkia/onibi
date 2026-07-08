import assert from "node:assert/strict";
import test from "node:test";
import { ApprovalWakeLock } from "./wake-lock.js";

class FakeDocument extends EventTarget {
  visibilityState: DocumentVisibilityState = "visible";

  setVisibility(state: DocumentVisibilityState): void {
    this.visibilityState = state;
    this.dispatchEvent(new Event("visibilitychange"));
  }
}

class FakeSentinel extends EventTarget {
  releases = 0;

  async release(): Promise<void> {
    this.releases += 1;
    this.dispatchEvent(new Event("release"));
  }
}

class FakeStorage {
  constructor(private readonly values = new Map<string, string>()) {}

  getItem(key: string): string | null {
    return this.values.get(key) ?? null;
  }
}

type WakeLockNavigatorLike = Navigator & {
  wakeLock?: {
    request: (type: "screen") => Promise<FakeSentinel>;
  };
};

function fakeNavigator(sentinels: FakeSentinel[]): WakeLockNavigatorLike {
  return {
    wakeLock: {
      async request(type: "screen"): Promise<FakeSentinel> {
        assert.equal(type, "screen");
        const sentinel = new FakeSentinel();
        sentinels.push(sentinel);
        return sentinel;
      }
    }
  } as WakeLockNavigatorLike;
}

async function flush(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

test("no-ops when wake lock API is unavailable", async () => {
  const doc = new FakeDocument();
  const controller = new ApprovalWakeLock({
    documentRef: doc as unknown as Document,
    navigatorRef: {} as WakeLockNavigatorLike,
    storage: new FakeStorage()
  });
  controller.setPendingCount(1);
  await flush();
  controller.dispose();
});

test("acquires while approval is pending and releases after it clears", async () => {
  const doc = new FakeDocument();
  const sentinels: FakeSentinel[] = [];
  const controller = new ApprovalWakeLock({
    documentRef: doc as unknown as Document,
    navigatorRef: fakeNavigator(sentinels),
    storage: new FakeStorage()
  });
  controller.setPendingCount(1);
  await flush();
  assert.equal(sentinels.length, 1);
  controller.setPendingCount(0);
  await flush();
  assert.equal(sentinels[0].releases, 1);
  controller.dispose();
});

test("reacquires on visibility return while approvals remain pending", async () => {
  const doc = new FakeDocument();
  const sentinels: FakeSentinel[] = [];
  const controller = new ApprovalWakeLock({
    documentRef: doc as unknown as Document,
    navigatorRef: fakeNavigator(sentinels),
    storage: new FakeStorage()
  });
  controller.setPendingCount(1);
  await flush();
  assert.equal(sentinels.length, 1);
  doc.setVisibility("hidden");
  await flush();
  assert.equal(sentinels[0].releases, 1);
  doc.setVisibility("visible");
  await flush();
  assert.equal(sentinels.length, 2);
  controller.dispose();
});

test("respects local storage opt-out", async () => {
  const doc = new FakeDocument();
  const sentinels: FakeSentinel[] = [];
  const controller = new ApprovalWakeLock({
    documentRef: doc as unknown as Document,
    navigatorRef: fakeNavigator(sentinels),
    storage: new FakeStorage(new Map([["onibi:wake-lock", "off"]]))
  });
  controller.setPendingCount(1);
  await flush();
  assert.equal(sentinels.length, 0);
  controller.dispose();
});
