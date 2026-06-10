export const COMMAND_PALETTE_USED_KEY = "onibi.commandPalette.used";
export const COMMAND_PALETTE_DISCOVERED_EVENT = "onibi:command-palette-discovered";

export function commandPaletteUsed(): boolean {
  try {
    return window.localStorage.getItem(COMMAND_PALETTE_USED_KEY) === "1";
  } catch {
    return false;
  }
}

export function markCommandPaletteUsed(): void {
  try {
    window.localStorage.setItem(COMMAND_PALETTE_USED_KEY, "1");
  } catch {
    // Discovery is cosmetic; storage failures should not block the palette.
  }
  window.dispatchEvent(new CustomEvent(COMMAND_PALETTE_DISCOVERED_EVENT));
}
