"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { Transaction } from "@/lib/types";

export function useTransactionSearch(items: Transaction[], query: string) {
  const worker = useRef<Worker | null>(null);
  const [result, setResult] = useState<{ query: string; ids: Set<string> } | null>(null);
  const entries = useMemo(() => items.map((item) => ({ id: item.id, text: `${item.title} ${item.category} ${item.merchant || ""} ${item.notes || ""} ${item.ocrText || ""} ${item.sheet || ""}`.toLocaleLowerCase() })), [items]);

  useEffect(() => {
    if (items.length < 2_000) return;
    const next = new Worker(new URL("./transaction-search.worker.ts", import.meta.url));
    worker.current = next;
    next.onmessage = (event: MessageEvent<{ query: string; ids: string[] }>) => setResult({ query: event.data.query, ids: new Set(event.data.ids) });
    next.postMessage({ type: "index", entries });
    return () => { next.terminate(); if (worker.current === next) worker.current = null; };
  }, [entries, items.length]);

  useEffect(() => {
    if (items.length < 2_000 || !query.trim()) return;
    worker.current?.postMessage({ type: "query", query });
  }, [items.length, query]);

  if (items.length < 2_000 || !query.trim() || result?.query !== query) return null;
  return result.ids;
}
