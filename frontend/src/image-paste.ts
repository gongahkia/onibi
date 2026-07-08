export type ImageUploadRequest = {
  name: string;
  mime: string;
  data: string;
};

type ImagePasteOptions = {
  root: HTMLElement;
  uploadImage: (request: ImageUploadRequest) => Promise<string>;
  sendText: (text: string) => void;
  showToast?: (message: string) => void;
  focus?: () => void;
  readClipboard?: () => Promise<ClipboardItem[]>;
};

type ImagePasteController = {
  pasteFromClipboard: () => Promise<boolean>;
  dispose: () => void;
};

type DataTransferItemLike = {
  kind: string;
  type: string;
  getAsFile: () => File | null;
};

type DataTransferLike = {
  files?: ArrayLike<File>;
  items?: ArrayLike<DataTransferItemLike>;
};

const maxImageBytes = 2 * 1024 * 1024;
const supportedTypes = new Set(["image/png", "image/jpeg", "image/webp"]);

export function installImagePaste(options: ImagePasteOptions): ImagePasteController {
  const readClipboard = options.readClipboard ?? defaultReadClipboard();
  const pasteFromClipboard = async (): Promise<boolean> => {
    if (readClipboard === undefined) {
      return false;
    }
    let file: File | undefined;
    try {
      file = await imageFileFromClipboardItems(await readClipboard());
    } catch {
      return false;
    }
    if (file === undefined) {
      return false;
    }
    await uploadAndInsert(options, file);
    return true;
  };
  const handlePaste = (event: ClipboardEvent): void => {
    const file = imageFileFromDataTransfer(event.clipboardData);
    if (file === undefined) {
      return;
    }
    event.preventDefault();
    void uploadAndInsert(options, file);
  };
  options.root.addEventListener("paste", handlePaste);
  return {
    pasteFromClipboard,
    dispose() {
      options.root.removeEventListener("paste", handlePaste);
    }
  };
}

export async function imageFileToUploadRequest(file: File): Promise<ImageUploadRequest> {
  const mime = imageMimeForFile(file);
  if (!supportedTypes.has(mime)) {
    throw new Error("Image paste supports PNG, JPEG, or WebP.");
  }
  if (file.size > maxImageBytes) {
    throw new Error("Image paste limit is 2MB.");
  }
  const bytes = await fileBytes(file);
  if (bytes.byteLength > maxImageBytes) {
    throw new Error("Image paste limit is 2MB.");
  }
  return {
    name: file.name,
    mime,
    data: base64FromBytes(bytes)
  };
}

export function shellQuotePath(path: string): string {
  if (/^[A-Za-z0-9_@%+=:,./-]+$/.test(path)) {
    return path;
  }
  return `'${path.replaceAll("'", "'\\''")}'`;
}

async function uploadAndInsert(options: ImagePasteOptions, file: File): Promise<void> {
  try {
    options.showToast?.("Uploading image...");
    const path = await options.uploadImage(await imageFileToUploadRequest(file));
    options.sendText(shellQuotePath(path));
    options.showToast?.("Image path inserted.");
  } catch (err) {
    options.showToast?.(err instanceof Error ? err.message : "Image paste failed.");
  } finally {
    options.focus?.();
  }
}

function imageFileFromDataTransfer(data: DataTransferLike | null): File | undefined {
  if (data === null) {
    return undefined;
  }
  for (const file of Array.from(data.files ?? [])) {
    if (isImageType(file.type) || imageMimeForName(file.name) !== "") {
      return file;
    }
  }
  for (const item of Array.from(data.items ?? [])) {
    if (item.kind !== "file" || !isImageType(item.type)) {
      continue;
    }
    const file = item.getAsFile();
    if (file !== null) {
      return file;
    }
  }
  return undefined;
}

async function imageFileFromClipboardItems(items: ClipboardItem[]): Promise<File | undefined> {
  for (const item of items) {
    const imageType = item.types.find(isImageType);
    if (imageType === undefined) {
      continue;
    }
    const blob = await item.getType(imageType);
    return new File([blob], `clipboard.${extensionForMime(imageType)}`, { type: imageType });
  }
  return undefined;
}

function imageMimeForFile(file: File): string {
  const fromType = normalizeImageMime(file.type);
  if (fromType !== "") {
    return fromType;
  }
  return imageMimeForName(file.name);
}

function imageMimeForName(name: string): string {
  const lower = name.toLowerCase();
  if (lower.endsWith(".png")) {
    return "image/png";
  }
  if (lower.endsWith(".jpg") || lower.endsWith(".jpeg")) {
    return "image/jpeg";
  }
  if (lower.endsWith(".webp")) {
    return "image/webp";
  }
  if (lower.endsWith(".svg")) {
    return "image/svg+xml";
  }
  return "";
}

function normalizeImageMime(mime: string): string {
  const clean = mime.trim().toLowerCase().split(";")[0];
  return clean === "image/jpg" ? "image/jpeg" : clean;
}

function isImageType(mime: string): boolean {
  return normalizeImageMime(mime).startsWith("image/");
}

function extensionForMime(mime: string): string {
  switch (normalizeImageMime(mime)) {
    case "image/png":
      return "png";
    case "image/jpeg":
      return "jpg";
    case "image/webp":
      return "webp";
    default:
      return "img";
  }
}

function base64FromBytes(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 0x8000;
  for (let offset = 0; offset < bytes.byteLength; offset += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize));
  }
  return window.btoa(binary);
}

async function fileBytes(file: File): Promise<Uint8Array> {
  const withArrayBuffer = file as File & { arrayBuffer?: () => Promise<ArrayBuffer> };
  if (typeof withArrayBuffer.arrayBuffer === "function") {
    return new Uint8Array(await withArrayBuffer.arrayBuffer());
  }
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.addEventListener("load", () => {
      if (reader.result instanceof ArrayBuffer) {
        resolve(new Uint8Array(reader.result));
        return;
      }
      reject(new Error("image read failed"));
    });
    reader.addEventListener("error", () => reject(new Error("image read failed")));
    reader.readAsArrayBuffer(file);
  });
}

function defaultReadClipboard(): (() => Promise<ClipboardItem[]>) | undefined {
  return navigator.clipboard?.read?.bind(navigator.clipboard);
}
