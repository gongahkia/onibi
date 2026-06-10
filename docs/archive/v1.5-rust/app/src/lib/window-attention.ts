import { getCurrentWindow, UserAttentionType } from "@tauri-apps/api/window";

export async function requestInformationalAttention(): Promise<void> {
  try {
    await getCurrentWindow().requestUserAttention(UserAttentionType.Informational);
  } catch {
    // Browser previews and test runners do not expose the Tauri window runtime.
  }
}
