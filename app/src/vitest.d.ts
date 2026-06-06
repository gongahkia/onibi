import type { Mock } from "vitest";

declare global {
  // eslint-disable-next-line no-var
  var __TAURI_MOCKS__: {
    dialogOpen: Mock;
    invoke: Mock;
    listen: Mock;
    openerRevealItemInDir: Mock;
    processRelaunch: Mock;
    updateCheck: Mock;
    unlisten: Mock;
  };
}

export {};
