create table public.sheet_shares (
  id uuid primary key default gen_random_uuid(),
  owner_id uuid not null references auth.users on delete cascade,
  sheet_id text not null,
  recipient_email text not null check (recipient_email = lower(recipient_email)),
  access_level text not null check (access_level in ('read', 'contribute', 'edit')),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (owner_id, sheet_id, recipient_email)
);

create index sheet_shares_recipient_email_idx on public.sheet_shares (recipient_email);
create index sheet_shares_owner_sheet_idx on public.sheet_shares (owner_id, sheet_id);

alter table public.sheet_shares enable row level security;
revoke all on public.sheet_shares from anon, authenticated;
grant select, insert, update, delete on public.sheet_shares to authenticated;

create policy "sheet share owners manage their shares"
on public.sheet_shares for all to authenticated
using ((select auth.uid()) is not null and owner_id = (select auth.uid()))
with check ((select auth.uid()) is not null and owner_id = (select auth.uid()));

create policy "sheet share recipients can read their access"
on public.sheet_shares for select to authenticated
using (
  (select auth.uid()) is not null
  and recipient_email = lower(coalesce((select auth.jwt() ->> 'email'), ''))
);

create or replace function public.can_read_shared_sync_record(
  requested_owner_id uuid,
  requested_record_type text,
  requested_record_id text,
  requested_payload jsonb
) returns boolean
language sql stable security definer set search_path = public as $$
  select exists (
    select 1
    from public.sheet_shares share
    where share.owner_id = requested_owner_id
      and share.recipient_email = lower(coalesce(auth.jwt() ->> 'email', ''))
      and (
        (requested_record_type = 'sheet' and requested_record_id = share.sheet_id)
        or (requested_record_type = 'transaction' and requested_payload ->> 'sheetId' = share.sheet_id)
      )
  )
$$;

create or replace function public.can_contribute_shared_sync_record(
  requested_owner_id uuid,
  requested_record_type text,
  requested_payload jsonb
) returns boolean
language sql stable security definer set search_path = public as $$
  select exists (
    select 1
    from public.sheet_shares share
    where share.owner_id = requested_owner_id
      and share.recipient_email = lower(coalesce(auth.jwt() ->> 'email', ''))
      and share.access_level in ('contribute', 'edit')
      and requested_record_type = 'transaction'
      and requested_payload ->> 'sheetId' = share.sheet_id
  )
$$;

create or replace function public.can_edit_shared_sync_record(
  requested_owner_id uuid,
  requested_record_type text,
  requested_record_id text,
  requested_payload jsonb
) returns boolean
language sql stable security definer set search_path = public as $$
  select exists (
    select 1
    from public.sheet_shares share
    where share.owner_id = requested_owner_id
      and share.recipient_email = lower(coalesce(auth.jwt() ->> 'email', ''))
      and share.access_level = 'edit'
      and (
        (requested_record_type = 'sheet' and requested_record_id = share.sheet_id)
        or (requested_record_type = 'transaction' and requested_payload ->> 'sheetId' = share.sheet_id)
      )
  )
$$;

drop policy if exists "read own sync records" on public.app_sync_records;
drop policy if exists "insert own sync records" on public.app_sync_records;
drop policy if exists "update own sync records" on public.app_sync_records;

create policy "read own or shared sync records"
on public.app_sync_records for select to authenticated
using (
  ((select auth.uid()) is not null and user_id = (select auth.uid()))
  or public.can_read_shared_sync_record(user_id, record_type, record_id, payload)
);

create policy "insert own or contributed sync records"
on public.app_sync_records for insert to authenticated
with check (
  ((select auth.uid()) is not null and user_id = (select auth.uid()))
  or public.can_contribute_shared_sync_record(user_id, record_type, payload)
  or public.can_edit_shared_sync_record(user_id, record_type, record_id, payload)
);

create policy "update own or edited shared sync records"
on public.app_sync_records for update to authenticated
using (
  ((select auth.uid()) is not null and user_id = (select auth.uid()))
  or public.can_edit_shared_sync_record(user_id, record_type, record_id, payload)
)
with check (
  ((select auth.uid()) is not null and user_id = (select auth.uid()))
  or public.can_edit_shared_sync_record(user_id, record_type, record_id, payload)
);
