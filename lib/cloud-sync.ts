"use client";

import { createClient } from "@supabase/supabase-js";
import type { AppPreferences, Category, Sheet, SyncRecordType, SyncTombstone, Transaction } from "@/lib/types";

type CloudRecordType = SyncRecordType | "preferences";
type SyncRecord = { record_type: CloudRecordType; record_id: string; payload: unknown; updated_at: string; deleted_at: string | null };
export type IncrementalCloudState = { sheets: Sheet[]; categories: Category[]; transactions: Transaction[]; preferences: AppPreferences };
type SyncResult = { state: IncrementalCloudState; cursor?: string; pulled: boolean; settledTombstones: string[] };

const localPreferenceKeys = new Set(["syncEnabled", "lastSyncedAt", "lastGoogleBackupAt", "syncTombstones", "syncReconciliationVersion"]);
const earliestSyncTimestamp = new Date(0).toISOString();

function client() {
  const url = process.env.NEXT_PUBLIC_SUPABASE_URL;
  const key = process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY;
  return url && key ? createClient(url, key) : null;
}

export function isCloudSyncConfigured() { return Boolean(client()); }

export async function currentCloudUser() {
  const supabase = client();
  if (!supabase) return null;
  const { data } = await supabase.auth.getUser();
  return data.user;
}

export async function sendCloudMagicLink(email: string) {
  const supabase = client();
  if (!supabase) throw new Error("Supabase is not configured for this deployment.");
  const { error } = await supabase.auth.signInWithOtp({ email, options: { emailRedirectTo: window.location.origin } });
  if (error) throw error;
}

function recordKey(record: Pick<SyncRecord, "record_type" | "record_id">) { return `${record.record_type}:${record.record_id}`; }
function compareTimestamps(left: string, right: string) { return left.localeCompare(right); }
function isTombstone(record: SyncRecord) { return Boolean(record.deleted_at); }

function cloudPreferences(preferences: AppPreferences) {
  return Object.fromEntries(Object.entries(preferences).filter(([key]) => !localPreferenceKeys.has(key))) as Omit<AppPreferences, "syncEnabled" | "lastSyncedAt" | "lastGoogleBackupAt" | "syncTombstones">;
}

function addNewest(records: Map<string, SyncRecord>, record: SyncRecord) {
  const key = recordKey(record);
  const existing = records.get(key);
  if (!existing || compareTimestamps(record.updated_at, existing.updated_at) > 0 || (record.updated_at === existing.updated_at && isTombstone(record) && !isTombstone(existing))) records.set(key, record);
}

function recordsFor(state: IncrementalCloudState): SyncRecord[] {
  const records = new Map<string, SyncRecord>();
  const add = <T extends { id: string; updatedAt?: string; deletedAt?: string }>(recordType: SyncRecordType, payload: T) => {
    const updatedAt = payload.deletedAt && (!payload.updatedAt || compareTimestamps(payload.deletedAt, payload.updatedAt) > 0) ? payload.deletedAt : payload.updatedAt || earliestSyncTimestamp;
    addNewest(records, { record_type: recordType, record_id: payload.id, payload, updated_at: updatedAt, deleted_at: payload.deletedAt || null });
  };
  state.sheets.forEach((sheet) => add("sheet", sheet));
  state.categories.forEach((category) => add("category", category));
  state.transactions.forEach((transaction) => add("transaction", transaction));
  addNewest(records, { record_type: "preferences", record_id: "preferences", payload: cloudPreferences(state.preferences), updated_at: state.preferences.updatedAt || earliestSyncTimestamp, deleted_at: null });
  (state.preferences.syncTombstones || []).forEach((tombstone) => addNewest(records, { record_type: tombstone.recordType, record_id: tombstone.recordId, payload: { id: tombstone.recordId }, updated_at: tombstone.deletedAt, deleted_at: tombstone.deletedAt }));
  return [...records.values()];
}

function mergeRecords(localRecords: SyncRecord[], remoteRecords: SyncRecord[]) {
  const merged = new Map(localRecords.map((record) => [recordKey(record), record]));
  remoteRecords.forEach((remote) => {
    const local = merged.get(recordKey(remote));
    if (!local || compareTimestamps(remote.updated_at, local.updated_at) >= 0) merged.set(recordKey(remote), remote);
  });
  return merged;
}

function hasRecordShape(record: SyncRecord, key: string) { return typeof record.payload === "object" && record.payload !== null && key in record.payload; }

function stateFromRecords(records: Iterable<SyncRecord>, fallback: IncrementalCloudState): IncrementalCloudState {
  const sheets: Sheet[] = [];
  const categories: Category[] = [];
  const transactions: Transaction[] = [];
  let preferences = fallback.preferences;
  for (const record of records) {
    if (record.record_type === "preferences" && !record.deleted_at) {
      preferences = { ...fallback.preferences, ...cloudPreferences(record.payload as AppPreferences) };
      continue;
    }
    if (record.record_type === "sheet" && hasRecordShape(record, "name")) {
      sheets.push({ ...(record.payload as Sheet), deletedAt: record.deleted_at || undefined });
      continue;
    }
    if (record.record_type === "category" && hasRecordShape(record, "name")) {
      categories.push({ ...(record.payload as Category), deletedAt: record.deleted_at || undefined });
      continue;
    }
    if (record.record_type === "transaction" && !record.deleted_at && hasRecordShape(record, "amount")) transactions.push(record.payload as Transaction);
  }
  return { sheets, categories, transactions, preferences };
}

function recordsToPush(localRecords: SyncRecord[], remoteRecords: SyncRecord[], cursor: string | undefined, tombstones: SyncTombstone[]) {
  const remoteByKey = new Map(remoteRecords.map((record) => [recordKey(record), record]));
  const tombstoneKeys = new Set(tombstones.map((tombstone) => `${tombstone.recordType}:${tombstone.recordId}`));
  return localRecords.filter((local) => {
    const remote = remoteByKey.get(recordKey(local));
    if (remote && compareTimestamps(local.updated_at, remote.updated_at) <= 0) return false;
    if (tombstoneKeys.has(recordKey(local))) return true;
    return cursor ? compareTimestamps(local.updated_at, cursor) > 0 : true;
  });
}

/**
 * Reconciles changed remote records with the complete local state. Records use a
 * last-write-wins timestamp, so an incremental pull cannot replace untouched
 * local collections. Deleted records are transmitted as tombstones.
 */
export async function syncIncrementalState(state: IncrementalCloudState, cursor?: string): Promise<SyncResult> {
  const supabase = client();
  if (!supabase) throw new Error("Supabase is not configured for this deployment.");
  const user = await currentCloudUser();
  if (!user) throw new Error("Sign in before syncing.");
  const remoteRequest = supabase.from("app_sync_records").select("record_type, record_id, payload, updated_at, deleted_at").eq("user_id", user.id).order("updated_at", { ascending: true });
  const { data: remote, error: pullError } = cursor ? await remoteRequest.gt("updated_at", cursor) : await remoteRequest;
  if (pullError) throw pullError;
  const remoteRecords = (remote || []) as SyncRecord[];
  const localRecords = recordsFor(state);
  const merged = mergeRecords(localRecords, remoteRecords);
  const recordsToUpload = recordsToPush(localRecords, remoteRecords, cursor, state.preferences.syncTombstones || []);
  if (recordsToUpload.length) {
    const { error: pushError } = await supabase.from("app_sync_records").upsert(recordsToUpload.map((record) => ({ ...record, user_id: user.id })), { onConflict: "user_id,record_type,record_id" });
    if (pushError) throw pushError;
  }
  const { data: latest, error: latestError } = await supabase.from("app_sync_records").select("updated_at").eq("user_id", user.id).order("updated_at", { ascending: false }).limit(1).maybeSingle();
  if (latestError) throw latestError;
  const uploadedKeys = new Set(recordsToUpload.map(recordKey));
  const remoteByKey = new Map(remoteRecords.map((record) => [recordKey(record), record]));
  const settledTombstones = (state.preferences.syncTombstones || []).filter((tombstone) => {
    const key = `${tombstone.recordType}:${tombstone.recordId}`;
    const remote = remoteByKey.get(key);
    return uploadedKeys.has(key) || Boolean(remote && compareTimestamps(remote.updated_at, tombstone.deletedAt) >= 0);
  }).map((tombstone) => `${tombstone.recordType}:${tombstone.recordId}`);
  return { state: stateFromRecords(merged.values(), state), cursor: latest?.updated_at || cursor, pulled: Boolean(remoteRecords.length), settledTombstones };
}
