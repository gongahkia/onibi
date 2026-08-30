"use client";

import { createClient } from "@supabase/supabase-js";
import type { AppPreferences, Category, Sheet, Transaction } from "@/lib/types";

type SyncRecord = { record_type: "sheet" | "category" | "transaction" | "preferences"; record_id: string; payload: unknown; updated_at: string; deleted_at: string | null };
export type IncrementalCloudState = { sheets: Sheet[]; categories: Category[]; transactions: Transaction[]; preferences: AppPreferences };

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

function recordsFor(state: IncrementalCloudState): SyncRecord[] {
  const now = new Date().toISOString();
  return [
    ...state.sheets.map((payload) => ({ record_type: "sheet" as const, record_id: payload.id, payload, updated_at: payload.updatedAt || now, deleted_at: payload.deletedAt || null })),
    ...state.categories.map((payload) => ({ record_type: "category" as const, record_id: payload.id, payload, updated_at: payload.updatedAt || now, deleted_at: payload.deletedAt || null })),
    ...state.transactions.map((payload) => ({ record_type: "transaction" as const, record_id: payload.id, payload, updated_at: payload.updatedAt || now, deleted_at: payload.deletedAt || null })),
    { record_type: "preferences" as const, record_id: "preferences", payload: state.preferences, updated_at: state.preferences.updatedAt || now, deleted_at: null }
  ];
}

function stateFromRecords(records: SyncRecord[], fallback: IncrementalCloudState): IncrementalCloudState {
  const sheets = records.filter((record) => record.record_type === "sheet" && !record.deleted_at).map((record) => record.payload as Sheet);
  const categories = records.filter((record) => record.record_type === "category" && !record.deleted_at).map((record) => record.payload as Category);
  const transactions = records.filter((record) => record.record_type === "transaction" && !record.deleted_at).map((record) => record.payload as Transaction);
  const preferences = records.find((record) => record.record_type === "preferences")?.payload as AppPreferences | undefined;
  return { sheets: sheets.length ? sheets : fallback.sheets, categories: categories.length ? categories : fallback.categories, transactions: transactions.length ? transactions : fallback.transactions, preferences: preferences || fallback.preferences };
}

/** Pull first on a fresh install; later syncs send only locally changed records. */
export async function syncIncrementalState(state: IncrementalCloudState, cursor?: string) {
  const supabase = client();
  if (!supabase) throw new Error("Supabase is not configured for this deployment.");
  const user = await currentCloudUser();
  if (!user) throw new Error("Sign in before syncing.");
  const remoteRequest = supabase.from("app_sync_records").select("record_type, record_id, payload, updated_at, deleted_at").eq("user_id", user.id).order("updated_at", { ascending: true });
  const { data: remote, error: pullError } = cursor ? await remoteRequest.gt("updated_at", cursor) : await remoteRequest;
  if (pullError) throw pullError;
  const remoteRecords = (remote || []) as SyncRecord[];
  if (!cursor && remoteRecords.length) return { state: stateFromRecords(remoteRecords, state), cursor: remoteRecords.at(-1)?.updated_at, pulled: true };
  const localRecords = recordsFor(state).filter((record) => !cursor || record.updated_at > cursor);
  if (localRecords.length) {
    const { error: pushError } = await supabase.from("app_sync_records").upsert(localRecords.map((record) => ({ ...record, user_id: user.id })), { onConflict: "user_id,record_type,record_id" });
    if (pushError) throw pushError;
  }
  const { data: latest, error: latestError } = await supabase.from("app_sync_records").select("updated_at").eq("user_id", user.id).order("updated_at", { ascending: false }).limit(1).maybeSingle();
  if (latestError) throw latestError;
  return { state: stateFromRecords(remoteRecords, state), cursor: latest?.updated_at || cursor, pulled: Boolean(remoteRecords.length) };
}
