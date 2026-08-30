"use client";

export type ReceiptOcrResult = {
  text: string;
  amount?: number;
  date?: string;
};

function firstAmount(text: string) {
  const values = [...text.matchAll(/(?:S\$|SGD|\$)?\s*(\d{1,6}(?:[,.]\d{2})?)/gi)]
    .map((match) => Number(match[1].replace(",", "")))
    .filter((value) => Number.isFinite(value) && value > 0);
  return values.length ? Math.max(...values) : undefined;
}

function firstDate(text: string) {
  const match = text.match(/\b(\d{1,2})[/-](\d{1,2})[/-](\d{2,4})\b/);
  if (!match) return undefined;
  const year = match[3].length === 2 ? `20${match[3]}` : match[3];
  const month = Number(match[2]);
  const day = Number(match[1]);
  if (month > 12 || day > 31) return undefined;
  return `${year}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
}

export async function recognizeReceipt(file: File, onProgress?: (progress: number) => void): Promise<ReceiptOcrResult> {
  if (!file.type.startsWith("image/")) throw new Error("Choose an image receipt.");
  if (file.size > 8_000_000) throw new Error("Choose an image smaller than 8 MB.");
  const { createWorker, PSM } = await import("tesseract.js");
  const worker = await createWorker("eng", 1, {
    logger(message) { if (message.status === "recognizing text") onProgress?.(message.progress); }
  });
  try {
    await worker.setParameters({ tessedit_pageseg_mode: PSM.SPARSE_TEXT });
    const result = await worker.recognize(file);
    const text = result.data.text.replace(/\n{3,}/g, "\n\n").trim().slice(0, 12_000);
    return { text, amount: firstAmount(text), date: firstDate(text) };
  } finally {
    await worker.terminate();
  }
}
