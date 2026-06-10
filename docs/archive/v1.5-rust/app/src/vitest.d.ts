import type { Mock } from "vitest";

declare global {
  // eslint-disable-next-line no-var
  var __TAURI_MOCKS__: {
    dialogConfirm: Mock;
    dialogOpen: Mock;
    invoke: Mock;
    listen: Mock;
    openerRevealItemInDir: Mock;
    processRelaunch: Mock;
    requestUserAttention: Mock;
    updateCheck: Mock;
    unlisten: Mock;
  };
}

export {};
