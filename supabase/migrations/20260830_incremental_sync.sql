alter table public.transactions add column if not exists ocr_text text;
alter table public.transactions add column if not exists deleted_at timestamptz;
create index if not exists transactions_sheet_occurred_at_idx on public.transactions (sheet_id, occurred_at desc) where deleted_at is null;
create index if not exists transactions_updated_at_idx on public.transactions (updated_at desc);

create table if not exists public.app_sync_records (
  user_id uuid not null references auth.users on delete cascade,
  record_type text not null check (record_type in ('sheet', 'category', 'transaction', 'preferences')),
  record_id text not null,
  payload jsonb not null,
  version bigint not null default 1,
  deleted_at timestamptz,
  updated_at timestamptz not null default now(),
  primary key (user_id, record_type, record_id)
);
alter table public.app_sync_records enable row level security;
drop policy if exists "read own sync records" on public.app_sync_records;
drop policy if exists "insert own sync records" on public.app_sync_records;
drop policy if exists "update own sync records" on public.app_sync_records;
create policy "read own sync records" on public.app_sync_records for select using (user_id = auth.uid());
create policy "insert own sync records" on public.app_sync_records for insert with check (user_id = auth.uid());
create policy "update own sync records" on public.app_sync_records for update using (user_id = auth.uid()) with check (user_id = auth.uid());
