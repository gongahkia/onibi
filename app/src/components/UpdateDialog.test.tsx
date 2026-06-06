import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import { UpdateDialog } from "./UpdateDialog";
import { UPDATE_CHECK_EVENT, UPDATE_LAST_CHECK_KEY } from "../lib/app-updater";

function resetUpdaterMocks() {
  globalThis.__TAURI_MOCKS__.updateCheck.mockReset();
  globalThis.__TAURI_MOCKS__.updateCheck.mockResolvedValue(null);
  globalThis.__TAURI_MOCKS__.processRelaunch.mockReset();
  globalThis.__TAURI_MOCKS__.processRelaunch.mockResolvedValue(undefined);
  localStorage.setItem(UPDATE_LAST_CHECK_KEY, String(Date.now()));
}

describe("UpdateDialog", () => {
  beforeEach(() => {
    resetUpdaterMocks();
    vi.clearAllMocks();
  });

  test("shows current status when no update is available", async () => {
    render(<UpdateDialog />);

    window.dispatchEvent(new CustomEvent(UPDATE_CHECK_EVENT));

    expect(await screen.findByRole("dialog", { name: "Software update" })).toBeTruthy();
    expect(await screen.findByText(/Onibi is up to date/)).toBeTruthy();
    expect(globalThis.__TAURI_MOCKS__.updateCheck).toHaveBeenCalledTimes(1);
  });

  test("installs an available update and relaunches", async () => {
    const downloadAndInstall = vi.fn(async (onProgress?: (event: unknown) => void) => {
      onProgress?.({ event: "Started", data: { contentLength: 10 } });
      onProgress?.({ event: "Progress", data: { chunkLength: 10 } });
      onProgress?.({ event: "Finished" });
    });
    globalThis.__TAURI_MOCKS__.updateCheck.mockResolvedValue({
      version: "1.5.1",
      date: "2026-06-06T00:00:00Z",
      body: "Release notes",
      downloadAndInstall,
    });

    render(<UpdateDialog />);
    window.dispatchEvent(new CustomEvent(UPDATE_CHECK_EVENT));

    expect(await screen.findByText(/Onibi 1.5.1 is available/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Install and Relaunch" }));

    await waitFor(() => {
      expect(downloadAndInstall).toHaveBeenCalled();
      expect(globalThis.__TAURI_MOCKS__.processRelaunch).toHaveBeenCalled();
    });
  });

  test("auto-checks only after the throttle interval", async () => {
    localStorage.setItem(UPDATE_LAST_CHECK_KEY, String(Date.now()));
    render(<UpdateDialog />);

    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(globalThis.__TAURI_MOCKS__.updateCheck).not.toHaveBeenCalled();
  });
});
