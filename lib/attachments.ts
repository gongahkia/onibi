"use client";

import { createClient } from "@supabase/supabase-js";
import type { Attachment } from "@/lib/types";

const DATABASE_NAME = "together-budget-attachments";
const STORE_NAME = "files";
const BUCKET_NAME = "attachments";

type LocalAttachment = { path: string; file: Blob };

function safeName(value: string) { return value.replace(/[^a-zA-Z0-9._-]/g, "-"); }
function makePath(transactionId: string, filename: string) { return `receipts/${transactionId}/${crypto.randomUUID()}-${safeName(filename)}`; }

function openDatabase() {
  return new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open(DATABASE_NAME, 1);
    request.onupgradeneeded = () => request.result.createObjectStore(STORE_NAME, { keyPath: "path" });
    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);
  });
}

async function putLocal(path: string, file: File) {
  const database = await openDatabase();
  await new Promise<void>((resolve, reject) => {
    const transaction = database.transaction(STORE_NAME, "readwrite");
    transaction.objectStore(STORE_NAME).put({ path, file } satisfies LocalAttachment);
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => reject(transaction.error);
    transaction.onabort = () => reject(transaction.error);
  });
  database.close();
}

async function getLocal(path: string) {
  const database = await openDatabase();
  const result = await new Promise<Blob | null>((resolve, reject) => {
    const request = database.transaction(STORE_NAME, "readonly").objectStore(STORE_NAME).get(path);
    request.onsuccess = () => resolve((request.result as LocalAttachment | undefined)?.file || null);
    request.onerror = () => reject(request.error);
  });
  database.close();
  return result;
}

function supabaseClient() {
  const url = process.env.NEXT_PUBLIC_SUPABASE_URL;
  const key = process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY;
  return url && key ? createClient(url, key) : null;
}

export async function storeAttachment(transactionId: string, file: File): Promise<Attachment> {
  const storagePath = makePath(transactionId, file.name || "attachment");
  const client = supabaseClient();
  if (client) {
    try {
      const { error } = await client.storage.from(BUCKET_NAME).upload(storagePath, file, { contentType: file.type || "application/octet-stream", upsert: false });
      if (!error) return { filename: file.name || "attachment", mimeType: file.type || "application/octet-stream", size: file.size, storage: "supabase", storagePath };
    } catch { /* fall through to the local demo store */ }
  }
  await putLocal(storagePath, file);
  return { filename: file.name || "attachment", mimeType: file.type || "application/octet-stream", size: file.size, storage: "indexeddb", storagePath };
}

export async function readAttachment(attachment: Attachment) {
  if (attachment.storage === "supabase") {
    const client = supabaseClient();
    if (client) {
      try {
        const { data, error } = await client.storage.from(BUCKET_NAME).download(attachment.storagePath);
        if (!error && data) return data;
      } catch { /* return null when the remote file is unavailable */ }
    }
    return null;
  }
  return getLocal(attachment.storagePath);
}
