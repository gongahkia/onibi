import { vi } from "vitest";

vi.mock("@xterm/addon-fit", () => ({ FitAddon: class {} }));
vi.mock("@xterm/addon-image", () => ({ ImageAddon: class {} }));
vi.mock("@xterm/xterm", () => ({ Terminal: class {} }));

import { defaultTerminalTheme, loadStoredTerminalTheme, terminalThemeNames } from "../terminal";

test("retains only dark and light themes and migrates legacy storage to dark", () => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value)
  } as Storage;

  values.set("onibi-theme", "ghostty-default");
  expect(terminalThemeNames).toEqual(["dark", "light"]);
  expect(loadStoredTerminalTheme(storage)).toBe(defaultTerminalTheme);
  expect(values.get("onibi-theme")).toBe("dark");

  values.set("onibi-theme", "light");
  expect(loadStoredTerminalTheme(storage)).toBe("light");
});
