create extension if not exists pgcrypto;

create type public.sheet_scope as enum ('personal', 'shared');
create type public.transaction_kind as enum ('expense', 'income', 'transfer', 'settlement');
create type public.member_role as enum ('owner', 'member');

create table public.profiles (
  id uuid primary key references auth.users on delete cascade,
  display_name text not null check (char_length(display_name) between 1 and 80),
  timezone text not null default 'Asia/Singapore', created_at timestamptz not null default now()
);
create table public.households (
  id uuid primary key default gen_random_uuid(), name text not null, base_currency char(3) not null default 'SGD', created_at timestamptz not null default now()
);
create table public.household_members (
  household_id uuid references public.households on delete cascade,
  user_id uuid references public.profiles on delete cascade,
  role public.member_role not null default 'member', created_at timestamptz not null default now(), primary key (household_id, user_id)
);
create table public.invites (
  id uuid primary key default gen_random_uuid(), household_id uuid not null references public.households on delete cascade,
  email text not null, role public.member_role not null default 'member', token_hash text not null unique, expires_at timestamptz not null, accepted_at timestamptz, created_by uuid not null references public.profiles on delete cascade, created_at timestamptz not null default now()
);
create table public.sheets (
  id uuid primary key default gen_random_uuid(), household_id uuid references public.households on delete cascade,
  owner_id uuid not null references public.profiles on delete cascade, name text not null, scope public.sheet_scope not null,
  base_currency char(3) not null default 'SGD', archived_at timestamptz, created_at timestamptz not null default now(),
  check ((scope = 'shared' and household_id is not null) or (scope = 'personal' and household_id is null))
);
create table public.categories (
  id uuid primary key default gen_random_uuid(), owner_id uuid not null references public.profiles on delete cascade, sheet_id uuid references public.sheets on delete cascade,
  name text not null, kind public.transaction_kind not null default 'expense', icon text, color text, sort_order integer not null default 0, archived_at timestamptz
);
create table public.transactions (
  id uuid primary key default gen_random_uuid(), sheet_id uuid not null references public.sheets on delete cascade, kind public.transaction_kind not null,
  amount numeric(20,8) not null check (amount >= 0), currency char(3) not null, converted_amount numeric(20,8), exchange_rate numeric(20,10), rate_mode text check (rate_mode in ('default','custom','auto_refresh')),
  title text not null, notes text not null default '', merchant text, category_id uuid references public.categories on delete set null,
  occurred_at timestamptz not null, local_timezone text not null, pending boolean not null default false, paid_by uuid references public.profiles on delete set null,
  source text not null default 'manual' check (source in ('manual','bank','import')), provider_name text, provider_transaction_id text,
  recurring_rule jsonb, transfer_group_id uuid, transfer_direction text check (transfer_direction in ('in','out')), ocr_text text, deleted_at timestamptz, version integer not null default 1, created_by uuid not null references public.profiles, created_at timestamptz not null default now(), updated_at timestamptz not null default now(),
  unique nulls not distinct (provider_name, provider_transaction_id)
);
create index transactions_sheet_occurred_at_idx on public.transactions (sheet_id, occurred_at desc) where deleted_at is null;
create index transactions_updated_at_idx on public.transactions (updated_at desc);
create table public.transaction_splits (
  id uuid primary key default gen_random_uuid(), transaction_id uuid not null references public.transactions on delete cascade,
  member_id uuid not null references public.profiles on delete cascade, amount numeric(20,8) not null check (amount >= 0), sort_order integer not null default 0,
  unique (transaction_id, member_id)
);
create table public.budgets (
  id uuid primary key default gen_random_uuid(), sheet_id uuid not null references public.sheets on delete cascade, owner_id uuid references public.profiles on delete cascade,
  name text not null, category_id uuid references public.categories on delete set null, amount numeric(20,8) not null check (amount > 0), currency char(3) not null,
  period jsonb not null, threshold_percent integer not null default 80 check (threshold_percent between 1 and 100), created_at timestamptz not null default now()
);
create table public.goals (
  id uuid primary key default gen_random_uuid(), sheet_id uuid references public.sheets on delete cascade, owner_id uuid references public.profiles on delete cascade,
  name text not null, target numeric(20,8) not null check (target > 0), currency char(3) not null, target_date date, created_at timestamptz not null default now()
);
create table public.goal_contributions (
  id uuid primary key default gen_random_uuid(), goal_id uuid not null references public.goals on delete cascade, contributor_id uuid not null references public.profiles,
  amount numeric(20,8) not null check (amount > 0), note text, transaction_id uuid references public.transactions on delete set null, created_at timestamptz not null default now()
);
create table public.bank_connections (
  id uuid primary key default gen_random_uuid(), household_id uuid not null references public.households on delete cascade, user_id uuid not null references public.profiles on delete cascade,
  provider text not null, provider_connection_id text not null, institution_name text not null, account_name text, account_mask text,
  access_token_ciphertext text, status text not null default 'pending' check (status in ('pending','connected','needs_attention','disconnected')), last_synced_at timestamptz, created_at timestamptz not null default now(),
  unique(provider, provider_connection_id)
);
create table public.google_sheet_connections (
  id uuid primary key default gen_random_uuid(), household_id uuid not null references public.households on delete cascade, owner_id uuid not null references public.profiles on delete cascade,
  spreadsheet_id text not null, refresh_token_ciphertext text not null, last_export_at timestamptz, created_at timestamptz not null default now(), unique(household_id)
);
create table public.push_subscriptions (
  id uuid primary key default gen_random_uuid(), user_id uuid not null references public.profiles on delete cascade, endpoint text not null unique, subscription jsonb not null, created_at timestamptz not null default now()
);
-- Per-record sync keeps large ledgers out of a single mutable JSON document.
create table public.app_sync_records (
  user_id uuid not null references auth.users on delete cascade,
  record_type text not null check (record_type in ('sheet', 'category', 'transaction', 'preferences')),
  record_id text not null,
  payload jsonb not null,
  version bigint not null default 1,
  deleted_at timestamptz,
  updated_at timestamptz not null default now(),
  primary key (user_id, record_type, record_id)
);

create or replace function public.can_read_sheet(requested_sheet_id uuid) returns boolean language sql stable security definer set search_path = public as $$
  select exists (select 1 from public.sheets s where s.id = requested_sheet_id and (s.owner_id = auth.uid() or exists (select 1 from public.household_members hm where hm.household_id = s.household_id and hm.user_id = auth.uid())))
$$;
alter table public.profiles enable row level security; alter table public.households enable row level security; alter table public.household_members enable row level security; alter table public.sheets enable row level security; alter table public.categories enable row level security; alter table public.transactions enable row level security; alter table public.transaction_splits enable row level security; alter table public.budgets enable row level security; alter table public.goals enable row level security; alter table public.goal_contributions enable row level security; alter table public.bank_connections enable row level security; alter table public.google_sheet_connections enable row level security; alter table public.push_subscriptions enable row level security;
alter table public.app_sync_records enable row level security;
create policy "read own profile" on public.profiles for select using (id = auth.uid()); create policy "update own profile" on public.profiles for update using (id = auth.uid());
create policy "read household membership" on public.household_members for select using (user_id = auth.uid() or exists (select 1 from public.household_members self where self.household_id = household_members.household_id and self.user_id = auth.uid()));
create policy "read available sheets" on public.sheets for select using (public.can_read_sheet(id)); create policy "manage available sheets" on public.sheets for all using (public.can_read_sheet(id));
create policy "read sheet categories" on public.categories for select using (sheet_id is null and owner_id = auth.uid() or public.can_read_sheet(sheet_id)); create policy "manage sheet categories" on public.categories for all using (owner_id = auth.uid() or public.can_read_sheet(sheet_id));
create policy "read sheet transactions" on public.transactions for select using (public.can_read_sheet(sheet_id)); create policy "manage sheet transactions" on public.transactions for all using (public.can_read_sheet(sheet_id));
create policy "read transaction splits" on public.transaction_splits for select using (exists (select 1 from public.transactions t where t.id = transaction_id and public.can_read_sheet(t.sheet_id))); create policy "manage transaction splits" on public.transaction_splits for all using (exists (select 1 from public.transactions t where t.id = transaction_id and public.can_read_sheet(t.sheet_id)));
create policy "read sheet budgets" on public.budgets for select using (public.can_read_sheet(sheet_id)); create policy "manage sheet budgets" on public.budgets for all using (public.can_read_sheet(sheet_id)); create policy "read available goals" on public.goals for select using (sheet_id is null and owner_id = auth.uid() or public.can_read_sheet(sheet_id)); create policy "manage available goals" on public.goals for all using (owner_id = auth.uid() or public.can_read_sheet(sheet_id));
create policy "read own bank connections" on public.bank_connections for select using (user_id = auth.uid()); create policy "manage own bank connections" on public.bank_connections for all using (user_id = auth.uid()); create policy "read own push subscriptions" on public.push_subscriptions for all using (user_id = auth.uid());
revoke all on public.app_sync_records from anon;
grant select, insert, update on public.app_sync_records to authenticated;
create policy "read own sync records" on public.app_sync_records for select to authenticated using ((select auth.uid()) is not null and user_id = (select auth.uid()));
create policy "insert own sync records" on public.app_sync_records for insert to authenticated with check ((select auth.uid()) is not null and user_id = (select auth.uid()));
create policy "update own sync records" on public.app_sync_records for update to authenticated using ((select auth.uid()) is not null and user_id = (select auth.uid())) with check ((select auth.uid()) is not null and user_id = (select auth.uid()));
