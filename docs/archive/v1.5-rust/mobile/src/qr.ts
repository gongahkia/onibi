import type { BarcodeDetectorCtor } from "./types";

export async function scanQrImage(file: File): Promise<string> {
  if (typeof window === "undefined" || !("BarcodeDetector" in window)) {
    throw new Error("QR image scanning is not available in this browser.");
  }
  const detector = new ((window as unknown as { BarcodeDetector: BarcodeDetectorCtor })
    .BarcodeDetector)({ formats: ["qr_code"] });
  const bitmap = await createImageBitmap(file);
  const codes = await detector.detect(bitmap);
  const raw = codes[0]?.rawValue;
  if (!raw) {
    throw new Error("No QR payload found in image.");
  }
  return raw;
}
