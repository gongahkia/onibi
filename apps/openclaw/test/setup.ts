import "@testing-library/jest-dom/vitest";

class TestStorage implements Storage {
  private readonly entries = new Map<string, string>();

  get length(): number {
    return this.entries.size;
  }

  clear(): void {
    this.entries.clear();
  }

  getItem(key: string): string | null {
    return this.entries.get(key) ?? null;
  }

  key(index: number): string | null {
    return [...this.entries.keys()][index] ?? null;
  }

  removeItem(key: string): void {
    this.entries.delete(key);
  }

  setItem(key: string, value: string): void {
    this.entries.set(key, value);
  }
}

class TestResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}

const testStorage = new TestStorage();

Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: testStorage
});

Object.defineProperty(window, "localStorage", {
  configurable: true,
  value: testStorage
});

Object.defineProperty(globalThis, "ResizeObserver", {
  configurable: true,
  value: TestResizeObserver
});
