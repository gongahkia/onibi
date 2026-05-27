import type { Mock } from "vitest";

declare global {
  // eslint-disable-next-line no-var
  var __TAURI_MOCKS__: {
    invoke: Mock;
    listen: Mock;
    unlisten: Mock;
  };
}

export {};
