"use client";

import Dexie, { type Table } from "dexie";
import type { AppPreferences, Category, Sheet, Transaction } from "@/lib/types";

export type BudgetCacheState = {
  transactions: Transaction[];
  sheets: Sheet[];
  categories: Category[];
  preferences: AppPreferences;
};

type MetaRecord = { key: string; value: unknown };

class BudgetDatabase extends Dexie {
  transactions!: Table<Transaction, string>;
  sheets!: Table<Sheet, string>;
  categories!: Table<Category, string>;
  meta!: Table<MetaRecord, string>;

  constructor() {
    super("together-budget");
    this.version(1).stores({
      transactions: "id, sheetId, date, [sheetId+date], updatedAt, deletedAt, category",
      sheets: "id, updatedAt, deletedAt",
      categories: "id, kind, updatedAt, deletedAt",
      meta: "&key"
    });
  }
}

const database = new BudgetDatabase();
let lastSaved: BudgetCacheState | null = null;

function withoutReceiptFiles(transaction: Transaction): Transaction {
  const { attachments: _attachments, hasAttachment: _hasAttachment, ...record } = transaction;
  return record;
}

async function writeChanged<T extends { id: string }>(table: Table<T, string>, previous: T[] | undefined, next: T[]) {
  const prior = new Map(previous?.map((item) => [item.id, item]));
  const upcoming = new Set(next.map((item) => item.id));
  const changed = next.filter((item) => prior.get(item.id) !== item);
  const removed = previous?.filter((item) => !upcoming.has(item.id)).map((item) => item.id) || [];
  if (changed.length) await table.bulkPut(changed);
  if (removed.length) await table.bulkDelete(removed);
}

export async function readBudgetCache(): Promise<BudgetCacheState | null> {
  const [transactions, sheets, categories, preferences] = await Promise.all([
    database.transactions.toArray(),
    database.sheets.toArray(),
    database.categories.toArray(),
    database.meta.get("preferences")
  ]);
  if (!sheets.length || !preferences) return null;
  return {
    transactions: transactions.map(withoutReceiptFiles),
    sheets,
    categories,
    preferences: preferences.value as AppPreferences
  };
}

export async function migrateLegacyBudgetCache(fallback: BudgetCacheState) {
  const cached = await readBudgetCache();
  if (cached) return cached;
  let legacy: Partial<BudgetCacheState> | null = null;
  try {
    const raw = window.localStorage.getItem("together-budget-demo");
    legacy = raw ? JSON.parse(raw) as Partial<BudgetCacheState> : null;
  } catch { /* Invalid legacy data should not prevent an empty install. */ }
  const state: BudgetCacheState = {
    transactions: (legacy?.transactions || fallback.transactions).map(withoutReceiptFiles),
    sheets: legacy?.sheets || fallback.sheets,
    categories: legacy?.categories || fallback.categories,
    preferences: { ...fallback.preferences, ...legacy?.preferences }
  };
  await saveBudgetCache(state, true);
  window.localStorage.removeItem("together-budget-demo");
  return state;
}

export async function saveBudgetCache(state: BudgetCacheState, force = false) {
  const normalized: BudgetCacheState = { ...state, transactions: state.transactions.map(withoutReceiptFiles) };
  const previous = force ? null : lastSaved;
  await database.transaction("rw", database.transactions, database.sheets, database.categories, database.meta, async () => {
    await writeChanged(database.transactions, previous?.transactions, normalized.transactions);
    await writeChanged(database.sheets, previous?.sheets, normalized.sheets);
    await writeChanged(database.categories, previous?.categories, normalized.categories);
    if (force || previous?.preferences !== normalized.preferences) await database.meta.put({ key: "preferences", value: normalized.preferences });
  });
  lastSaved = normalized;
}

export async function storageStatus() {
  if (!navigator.storage?.estimate) return null;
  return navigator.storage.estimate();
}

export async function requestPersistentStorage() {
  if (!navigator.storage?.persist) return false;
  return navigator.storage.persist();
}
