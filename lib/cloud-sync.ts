"use client";

import { createClient } from "@supabase/supabase-js";

export type CloudSnapshot = { payload: unknown; updated_at: string };

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

export async function pullCloudSnapshot() {
  const supabase = client();
  if (!supabase) throw new Error("Supabase is not configured for this deployment.");
  const user = await currentCloudUser();
  if (!user) throw new Error("Sign in before syncing.");
  const { data, error } = await supabase.from("app_state_snapshots").select("payload, updated_at").eq("user_id", user.id).maybeSingle<CloudSnapshot>();
  if (error) throw error;
  return data;
}

export async function pushCloudSnapshot(payload: unknown) {
  const supabase = client();
  if (!supabase) throw new Error("Supabase is not configured for this deployment.");
  const user = await currentCloudUser();
  if (!user) throw new Error("Sign in before syncing.");
  const { error } = await supabase.from("app_state_snapshots").upsert({ user_id: user.id, payload, updated_at: new Date().toISOString() });
  if (error) throw error;
}
