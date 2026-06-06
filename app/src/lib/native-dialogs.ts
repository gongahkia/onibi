import {
  confirm as nativeConfirm,
  type ConfirmDialogOptions,
} from "@tauri-apps/plugin-dialog";

type ConfirmActionOptions = Pick<
  ConfirmDialogOptions,
  "cancelLabel" | "kind" | "okLabel" | "title"
>;

export async function confirmAction(
  message: string,
  options: ConfirmActionOptions = {},
): Promise<boolean> {
  try {
    return await nativeConfirm(message, {
      kind: "warning",
      title: "Confirm",
      ...options,
    });
  } catch {
    return typeof window !== "undefined" ? window.confirm(message) : false;
  }
}
