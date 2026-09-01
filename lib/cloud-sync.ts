"use client";

import { createClient } from "@supabase/supabase-js";
import type { AppPreferences, Category, Sheet, SheetAccessLevel, SyncRecordType, SyncTombstone, Transaction } from "@/lib/types";

type CloudRecordType = SyncRecordType | "preferences";
type SyncRecord = { user_id: string; record_type: CloudRecordType; record_id: string; payload: unknown; updated_at: string; deleted_at: string | null };
export type SheetShare = { id: string; owner_id: string; sheet_id: string; recipient_email: string; access_level: SheetAccessLevel; created_at: string; updated_at: string };
export type IncrementalCloudState = { sheets: Sheet[]; categories: Category[]; transactions: Transaction[]; preferences: AppPreferences };
type SyncResult = { state: IncrementalCloudState; cursor?: string; pulled: boolean; settledTombstones: string[]; sharedCursors: Record<string, string> };

const localPreferenceKeys = new Set(["syncEnabled", "lastSyncedAt", "lastGoogleBackupAt", "syncTombstones", "syncReconciliationVersion", "sharedSyncCursors"]);
const earliestSyncTimestamp = new Date(0).toISOString();
const syncPageSize = 500;

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

function requireClient() {
  const supabase = client();
  if (!supabase) throw new Error("Supabase is not configured for this deployment.");
  return supabase;
}

async function requireUser() {
  const user = await currentCloudUser();
  if (!user) throw new Error("Sign in before managing sharing.");
  return user;
}

function normalizedEmail(email: string) {
  const value = email.trim().toLocaleLowerCase();
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) throw new Error("Enter a valid email address.");
  return value;
}

export async function listSheetShares(sheetId: string): Promise<SheetShare[]> {
  const supabase = requireClient();
  const user = await requireUser();
  const { data, error } = await supabase.from("sheet_shares").select("id, owner_id, sheet_id, recipient_email, access_level, created_at, updated_at").eq("owner_id", user.id).eq("sheet_id", sheetId).order("created_at", { ascending: true });
  if (error) throw error;
  return (data || []) as SheetShare[];
}

export async function createSheetShare({ sheetId, email, accessLevel }: { sheetId: string; email: string; accessLevel: SheetAccessLevel }) {
  const supabase = requireClient();
  const user = await requireUser();
  const recipientEmail = normalizedEmail(email);
  const { data, error } = await supabase.from("sheet_shares").upsert({ owner_id: user.id, sheet_id: sheetId, recipient_email: recipientEmail, access_level: accessLevel, updated_at: new Date().toISOString() }, { onConflict: "owner_id,sheet_id,recipient_email" }).select("id, owner_id, sheet_id, recipient_email, access_level, created_at, updated_at").single();
  if (error) throw error;
  return data as SheetShare;
}

export async function removeSheetShare(shareId: string) {
  const supabase = requireClient();
  await requireUser();
  const { error } = await supabase.from("sheet_shares").delete().eq("id", shareId);
  if (error) throw error;
}

function recordKey(record: Pick<SyncRecord, "user_id" | "record_type" | "record_id">) { return `${record.user_id}:${record.record_type}:${record.record_id}`; }
function tombstoneKey(tombstone: SyncTombstone, userId: string) { return `${tombstone.sharedOwnerId || userId}:${tombstone.recordType}:${tombstone.recordId}`; }
function compareTimestamps(left: string, right: string) { return left.localeCompare(right); }
function isTombstone(record: SyncRecord) { return Boolean(record.deleted_at); }

function cloudPreferences(preferences: AppPreferences) {
  return Object.fromEntries(Object.entries(preferences).filter(([key]) => !localPreferenceKeys.has(key))) as Omit<AppPreferences, "syncEnabled" | "lastSyncedAt" | "lastGoogleBackupAt" | "syncTombstones" | "sharedSyncCursors">;
}

function addNewest(records: Map<string, SyncRecord>, record: SyncRecord) {
  const key = recordKey(record);
  const existing = records.get(key);
  if (!existing || compareTimestamps(record.updated_at, existing.updated_at) > 0 || (record.updated_at === existing.updated_at && isTombstone(record) && !isTombstone(existing))) records.set(key, record);
}

function withoutSharedMetadata<T extends { sharedOwnerId?: string; accessLevel?: SheetAccessLevel }>(record: T) {
  const { sharedOwnerId: _sharedOwnerId, accessLevel: _accessLevel, ...payload } = record;
  return payload;
}

function ownerForSheet(sheetId: string | undefined, sheets: Sheet[], fallbackOwnerId: string) {
  return sheets.find((sheet) => sheet.id === sheetId)?.sharedOwnerId || fallbackOwnerId;
}

function recordsFor(state: IncrementalCloudState, userId: string): SyncRecord[] {
  const records = new Map<string, SyncRecord>();
  const add = <T extends { id: string; updatedAt?: string; deletedAt?: string }>(ownerId: string, recordType: SyncRecordType, payload: T) => {
    const updatedAt = payload.deletedAt && (!payload.updatedAt || compareTimestamps(payload.deletedAt, payload.updatedAt) > 0) ? payload.deletedAt : payload.updatedAt || earliestSyncTimestamp;
    addNewest(records, { user_id: ownerId, record_type: recordType, record_id: payload.id, payload, updated_at: updatedAt, deleted_at: payload.deletedAt || null });
  };
  state.sheets.forEach((sheet) => add(sheet.sharedOwnerId || userId, "sheet", withoutSharedMetadata(sheet)));
  state.categories.forEach((category) => add(userId, "category", category));
  state.transactions.forEach((transaction) => add(transaction.sharedOwnerId || ownerForSheet(transaction.sheetId, state.sheets, userId), "transaction", withoutSharedMetadata(transaction)));
  addNewest(records, { user_id: userId, record_type: "preferences", record_id: "preferences", payload: cloudPreferences(state.preferences), updated_at: state.preferences.updatedAt || earliestSyncTimestamp, deleted_at: null });
  (state.preferences.syncTombstones || []).forEach((tombstone) => {
    const ownerId = tombstone.sharedOwnerId || ownerForSheet(tombstone.sheetId, state.sheets, userId);
    addNewest(records, { user_id: ownerId, record_type: tombstone.recordType, record_id: tombstone.recordId, payload: { id: tombstone.recordId, sheetId: tombstone.sheetId }, updated_at: tombstone.deletedAt, deleted_at: tombstone.deletedAt });
  });
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
function shareKey(ownerId: string, sheetId: string) { return `${ownerId}:${sheetId}`; }

function stateFromRecords(records: Iterable<SyncRecord>, fallback: IncrementalCloudState, userId: string, incomingShares: SheetShare[]): IncrementalCloudState {
  const sheets: Sheet[] = [];
  const categories: Category[] = [];
  const transactions: Transaction[] = [];
  const shareBySheet = new Map(incomingShares.map((share) => [shareKey(share.owner_id, share.sheet_id), share]));
  let preferences = fallback.preferences;
  for (const record of records) {
    if (record.record_type === "preferences" && !record.deleted_at && record.user_id === userId) {
      preferences = { ...fallback.preferences, ...cloudPreferences(record.payload as AppPreferences) };
      continue;
    }
    if (record.record_type === "sheet" && hasRecordShape(record, "name")) {
      const share = record.user_id === userId ? undefined : shareBySheet.get(shareKey(record.user_id, record.record_id));
      if (record.user_id !== userId && !share) continue;
      sheets.push({ ...(record.payload as Sheet), deletedAt: record.deleted_at || undefined, ...(share ? { sharedOwnerId: record.user_id, accessLevel: share.access_level } : {}) });
      continue;
    }
    if (record.record_type === "category" && record.user_id === userId && hasRecordShape(record, "name")) {
      categories.push({ ...(record.payload as Category), deletedAt: record.deleted_at || undefined });
      continue;
    }
    if (record.record_type === "transaction" && !record.deleted_at && hasRecordShape(record, "amount")) {
      const payload = record.payload as Transaction;
      if (record.user_id !== userId && !shareBySheet.has(shareKey(record.user_id, payload.sheetId || ""))) continue;
      transactions.push({ ...payload, ...(record.user_id === userId ? {} : { sharedOwnerId: record.user_id }) });
    }
  }
  return { sheets, categories, transactions, preferences };
}

function recordsToPush(localRecords: SyncRecord[], remoteRecords: SyncRecord[], cursor: string | undefined, tombstones: SyncTombstone[], userId: string) {
  const remoteByKey = new Map(remoteRecords.map((record) => [recordKey(record), record]));
  const tombstoneKeys = new Set(tombstones.map((tombstone) => tombstoneKey(tombstone, userId)));
  return localRecords.filter((local) => {
    const remote = remoteByKey.get(recordKey(local));
    if (remote && compareTimestamps(local.updated_at, remote.updated_at) <= 0) return false;
    if (tombstoneKeys.has(recordKey(local))) return true;
    return cursor ? compareTimestamps(local.updated_at, cursor) > 0 : true;
  });
}

async function pullRemoteRecords(supabase: NonNullable<ReturnType<typeof client>>, ownerId: string, cursor?: string) {
  const records: SyncRecord[] = [];
  for (let from = 0; ; from += syncPageSize) {
    const query = supabase.from("app_sync_records").select("user_id, record_type, record_id, payload, updated_at, deleted_at").eq("user_id", ownerId).order("updated_at", { ascending: true }).order("record_type", { ascending: true }).order("record_id", { ascending: true }).range(from, from + syncPageSize - 1);
    const { data, error } = cursor ? await query.gt("updated_at", cursor) : await query;
    if (error) throw error;
    const page = (data || []) as SyncRecord[];
    records.push(...page);
    if (page.length < syncPageSize) return records;
  }
}

async function pullIncomingShares(supabase: NonNullable<ReturnType<typeof client>>, userId: string) {
  const { data, error } = await supabase.from("sheet_shares").select("id, owner_id, sheet_id, recipient_email, access_level, created_at, updated_at");
  if (error) throw error;
  return ((data || []) as SheetShare[]).filter((share) => share.owner_id !== userId);
}

async function pushRecords(supabase: NonNullable<ReturnType<typeof client>>, records: SyncRecord[]) {
  for (let from = 0; from < records.length; from += syncPageSize) {
    const { error } = await supabase.from("app_sync_records").upsert(records.slice(from, from + syncPageSize), { onConflict: "user_id,record_type,record_id" });
    if (error) throw error;
  }
}

function stateWithoutRevokedShares(state: IncrementalCloudState, incomingShares: SheetShare[]) {
  const activeShares = new Set(incomingShares.map((share) => shareKey(share.owner_id, share.sheet_id)));
  const sheets = state.sheets.filter((sheet) => !sheet.sharedOwnerId || activeShares.has(shareKey(sheet.sharedOwnerId, sheet.id)));
  const sheetIds = new Set(sheets.map((sheet) => sheet.id));
  return { ...state, sheets, transactions: state.transactions.filter((transaction) => !transaction.sharedOwnerId || sheetIds.has(transaction.sheetId || "")) };
}

/** Reconciles complete local state with own and shared record deltas. */
export async function syncIncrementalState(state: IncrementalCloudState, cursor?: string): Promise<SyncResult> {
  const supabase = requireClient();
  const user = await requireUser();
  const incomingShares = await pullIncomingShares(supabase, user.id);
  const syncState = stateWithoutRevokedShares(state, incomingShares);
  const ownRecords = await pullRemoteRecords(supabase, user.id, cursor);
  const sharedCursors = { ...(state.preferences.sharedSyncCursors || {}) };
  const sharedRecords: SyncRecord[] = [];
  for (const share of incomingShares) {
    const key = shareKey(share.owner_id, share.sheet_id);
    const records = await pullRemoteRecords(supabase, share.owner_id, sharedCursors[key]);
    sharedRecords.push(...records);
    const latest = records.reduce<string | undefined>((timestamp, record) => !timestamp || compareTimestamps(record.updated_at, timestamp) > 0 ? record.updated_at : timestamp, undefined);
    if (latest) sharedCursors[key] = latest;
  }
  const remoteRecords = [...ownRecords, ...sharedRecords];
  const localRecords = recordsFor(syncState, user.id);
  const merged = mergeRecords(localRecords, remoteRecords);
  const recordsToUpload = recordsToPush(localRecords, remoteRecords, cursor, syncState.preferences.syncTombstones || [], user.id);
  if (recordsToUpload.length) await pushRecords(supabase, recordsToUpload);
  const { data: latest, error: latestError } = await supabase.from("app_sync_records").select("updated_at").eq("user_id", user.id).order("updated_at", { ascending: false }).limit(1).maybeSingle();
  if (latestError) throw latestError;
  const uploadedKeys = new Set(recordsToUpload.map(recordKey));
  const remoteByKey = new Map(remoteRecords.map((record) => [recordKey(record), record]));
  const settledTombstones = (syncState.preferences.syncTombstones || []).filter((tombstone) => {
    const remote = remoteByKey.get(tombstoneKey(tombstone, user.id));
    return uploadedKeys.has(tombstoneKey(tombstone, user.id)) || Boolean(remote && compareTimestamps(remote.updated_at, tombstone.deletedAt) >= 0);
  }).map((tombstone) => `${tombstone.recordType}:${tombstone.recordId}`);
  return { state: stateFromRecords(merged.values(), syncState, user.id, incomingShares), cursor: latest?.updated_at || cursor, pulled: Boolean(remoteRecords.length), settledTombstones, sharedCursors };
}
