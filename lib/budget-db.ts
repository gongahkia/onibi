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
export const legacyBudgetStorageKey = "together-budget-demo";
const databaseName = "old-pants";
const legacyDatabaseName = "together-budget";
const retiredSeedTransactionIds = new Set(["t1", "t2", "t3", "t4", "t5", "t6", "t7"]);
const retiredSeedCategoryIds = new Set([
  "category-expense-groceries",
  "category-expense-dining",
  "category-expense-transport",
  "category-expense-utilities",
  "category-expense-rent",
  "category-expense-health",
  "category-expense-shopping",
  "category-expense-entertainment",
  "category-expense-other",
  "category-income-salary",
  "category-income-freelance",
  "category-income-interest",
  "category-income-refund",
  "category-income-other"
]);

class BudgetDatabase extends Dexie {
  transactions!: Table<Transaction, string>;
  sheets!: Table<Sheet, string>;
  categories!: Table<Category, string>;
  meta!: Table<MetaRecord, string>;

  constructor(name: string) {
    super(name);
    this.version(1).stores({
      transactions: "id, sheetId, date, [sheetId+date], updatedAt, deletedAt, category",
      sheets: "id, updatedAt, deletedAt",
      categories: "id, kind, updatedAt, deletedAt",
      meta: "&key"
    });
  }
}

const database = new BudgetDatabase(databaseName);
const legacyDatabase = new BudgetDatabase(legacyDatabaseName);
let lastSaved: BudgetCacheState | null = null;

function withoutReceiptFiles(transaction: Transaction): Transaction {
  const { attachments: _attachments, hasAttachment: _hasAttachment, ...record } = transaction;
  return record;
}

function removeRetiredSeedRecords(state: BudgetCacheState): BudgetCacheState {
  const transactions = state.transactions.filter((item) => !retiredSeedTransactionIds.has(item.id));
  const categories = state.categories.filter((item) => !retiredSeedCategoryIds.has(item.id));
  const activeSheetIds = new Set(transactions.map((item) => item.sheetId));
  const sheets = state.sheets.filter((item) => item.id !== "shared-expenses" || activeSheetIds.has(item.id));
  if (transactions.length === state.transactions.length && categories.length === state.categories.length && sheets.length === state.sheets.length) return state;
  return { ...state, transactions, sheets, categories };
}

async function writeChanged<T extends { id: string }>(table: Table<T, string>, previous: T[] | undefined, next: T[]) {
  const prior = new Map(previous?.map((item) => [item.id, item]));
  const upcoming = new Set(next.map((item) => item.id));
  const changed = next.filter((item) => prior.get(item.id) !== item);
  const removed = previous?.filter((item) => !upcoming.has(item.id)).map((item) => item.id) || [];
  if (changed.length) await table.bulkPut(changed);
  if (removed.length) await table.bulkDelete(removed);
}

async function readBudgetCacheFrom(source: BudgetDatabase): Promise<BudgetCacheState | null> {
  const [transactions, sheets, categories, preferences] = await Promise.all([
    source.transactions.toArray(),
    source.sheets.toArray(),
    source.categories.toArray(),
    source.meta.get("preferences")
  ]);
  if (!sheets.length || !preferences) return null;
  return {
    transactions: transactions.map(withoutReceiptFiles),
    sheets,
    categories,
    preferences: preferences.value as AppPreferences
  };
}

export async function readBudgetCache(): Promise<BudgetCacheState | null> { return readBudgetCacheFrom(database); }

export async function migrateLegacyBudgetCache(fallback: BudgetCacheState) {
  const cached = await readBudgetCache();
  if (cached) {
    const cleanedCache = removeRetiredSeedRecords(cached);
    if (cleanedCache !== cached) await saveBudgetCache(cleanedCache, true);
    return cleanedCache;
  }
  const legacyCache = await readBudgetCacheFrom(legacyDatabase);
  let legacy: Partial<BudgetCacheState> | null = null;
  try {
    const raw = window.localStorage.getItem(legacyBudgetStorageKey);
    legacy = raw ? JSON.parse(raw) as Partial<BudgetCacheState> : null;
  } catch { /* Invalid legacy data should not prevent an empty install. */ }
  const state = removeRetiredSeedRecords({
    transactions: (legacyCache?.transactions || legacy?.transactions || fallback.transactions).map(withoutReceiptFiles),
    sheets: legacyCache?.sheets || legacy?.sheets || fallback.sheets,
    categories: legacyCache?.categories || legacy?.categories || fallback.categories,
    preferences: { ...fallback.preferences, ...legacy?.preferences, ...legacyCache?.preferences }
  });
  await saveBudgetCache(state, true);
  window.localStorage.removeItem(legacyBudgetStorageKey);
  return state;
}

export async function saveBudgetCache(state: BudgetCacheState, force = false) {
  const normalized: BudgetCacheState = { ...state, transactions: state.transactions.map(withoutReceiptFiles) };
  const previous = force ? null : lastSaved;
  await database.transaction("rw", database.transactions, database.sheets, database.categories, database.meta, async () => {
    if (force) {
      await Promise.all([database.transactions.clear(), database.sheets.clear(), database.categories.clear()]);
      if (normalized.transactions.length) await database.transactions.bulkPut(normalized.transactions);
      if (normalized.sheets.length) await database.sheets.bulkPut(normalized.sheets);
      if (normalized.categories.length) await database.categories.bulkPut(normalized.categories);
      await database.meta.put({ key: "preferences", value: normalized.preferences });
    } else {
      await writeChanged(database.transactions, previous?.transactions, normalized.transactions);
      await writeChanged(database.sheets, previous?.sheets, normalized.sheets);
      await writeChanged(database.categories, previous?.categories, normalized.categories);
      if (previous?.preferences !== normalized.preferences) await database.meta.put({ key: "preferences", value: normalized.preferences });
    }
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
