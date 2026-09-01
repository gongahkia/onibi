revoke all on public.app_sync_records from anon;
grant select, insert, update on public.app_sync_records to authenticated;

drop policy if exists "read own sync records" on public.app_sync_records;
drop policy if exists "insert own sync records" on public.app_sync_records;
drop policy if exists "update own sync records" on public.app_sync_records;

create policy "read own sync records" on public.app_sync_records for select to authenticated using ((select auth.uid()) is not null and user_id = (select auth.uid()));
create policy "insert own sync records" on public.app_sync_records for insert to authenticated with check ((select auth.uid()) is not null and user_id = (select auth.uid()));
create policy "update own sync records" on public.app_sync_records for update to authenticated using ((select auth.uid()) is not null and user_id = (select auth.uid())) with check ((select auth.uid()) is not null and user_id = (select auth.uid()));
