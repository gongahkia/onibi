import { imageFileToUploadRequest, installImagePaste, shellQuotePath } from "../image-paste";
import type { ImageUploadRequest } from "../image-paste";

const pngBytes = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);

test("uploads pasted image and inserts returned path", async () => {
  const root = document.createElement("div");
  const uploaded: ImageUploadRequest[] = [];
  const sent: string[] = [];
  const controller = installImagePaste({
    root,
    uploadImage: async (request) => {
      uploaded.push(request);
      return "/tmp/onibi-clip.png";
    },
    sendText: (text) => sent.push(text)
  });
  const event = pasteEvent(new File([pngBytes], "clip.png", { type: "image/png" }));
  root.dispatchEvent(event);
  await waitFor(() => expect(sent).toEqual(["/tmp/onibi-clip.png"]));
  expect(event.defaultPrevented).toBe(true);
  expect(uploaded).toHaveLength(1);
  expect(uploaded[0].mime).toBe("image/png");
  expect(uploaded[0].data).toBe("iVBORw0KGgo=");
  controller.dispose();
});

test("rejects pasted svg before upload", async () => {
  const root = document.createElement("div");
  const toast: string[] = [];
  const uploaded: ImageUploadRequest[] = [];
  const sent: string[] = [];
  installImagePaste({
    root,
    uploadImage: async (request) => {
      uploaded.push(request);
      return "/tmp/clip.svg";
    },
    sendText: (text) => sent.push(text),
    showToast: (message) => toast.push(message)
  });
  root.dispatchEvent(pasteEvent(new File(["<svg></svg>"], "clip.svg", { type: "image/svg+xml" })));
  await waitFor(() => expect(toast).toContain("Image paste supports PNG, JPEG, or WebP."));
  expect(uploaded).toEqual([]);
  expect(sent).toEqual([]);
});

test("caps image upload request at 2mb", async () => {
  const tooLarge = new File([new Uint8Array(2 * 1024 * 1024 + 1)], "clip.png", {
    type: "image/png"
  });
  await expect(imageFileToUploadRequest(tooLarge)).rejects.toThrow("2MB");
});

test("quotes paths with shell-special characters", () => {
  expect(shellQuotePath("/tmp/onibi.png")).toBe("/tmp/onibi.png");
  expect(shellQuotePath("/Users/me/Application Support/onibi/uploads/a'b.png")).toBe(
    "'/Users/me/Application Support/onibi/uploads/a'\\''b.png'"
  );
});

function pasteEvent(file: File): ClipboardEvent {
  const event = new window.Event("paste", { bubbles: true, cancelable: true }) as ClipboardEvent;
  Object.defineProperty(event, "clipboardData", {
    value: {
      files: [file],
      items: []
    }
  });
  return event;
}

async function waitFor(assertion: () => void): Promise<void> {
  let lastError: unknown;
  for (let i = 0; i < 100; i += 1) {
    try {
      assertion();
      return;
    } catch (error) {
      lastError = error;
      await new Promise((resolve) => setTimeout(resolve, 10));
    }
  }
  throw lastError;
}
