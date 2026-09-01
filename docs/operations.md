# old-pants operations

## Live resources

| Service | Resource | Dashboard |
| --- | --- | --- |
| GitHub | `gongahkia/old-pants` | <https://github.com/gongahkia/old-pants> |
| Vercel | `angryapplegravy-gmailcoms-projects/old-pants` | <https://vercel.com/angryapplegravy-gmailcoms-projects/old-pants> |
| Supabase | `old-pants` (`ovrdwkcruxnthtcjbzxk`), Singapore (`ap-southeast-1`) | <https://supabase.com/dashboard/project/ovrdwkcruxnthtcjbzxk> |

Public production URL: <https://old-pants.vercel.app>. Do not put database passwords, Supabase access tokens, service-role keys, or provider credentials in this repository.

## Runtime configuration

Vercel Preview and Production contain only these public client variables:

- `NEXT_PUBLIC_SUPABASE_URL`
- `NEXT_PUBLIC_SUPABASE_ANON_KEY`

The app uses Supabase Auth and RLS-protected `public.app_sync_records` and `public.sheet_shares` tables. It does not require a service-role key. The checked-in `supabase/schema.sql` creates the full schema; the subsequent migrations are `20260830_incremental_sync.sql`, `20260902_harden_sync_rls.sql`, and `20260902120000_sheet_sharing.sql`.

Before sending magic links, set Supabase Auth URL Configuration as follows:

- Site URL: `https://old-pants.vercel.app`.
- Additional redirect URLs: `http://localhost:3000/**` and `https://*-angryapplegravy-gmailcoms-projects.vercel.app/**`.

## Deploy and verify

Run these commands from the repository root after authenticating with both CLIs:

```sh
npx vercel deploy
npx vercel deploy --prod
npx vercel logs --environment production --level error --since 5m
npx supabase config push --project-ref ovrdwkcruxnthtcjbzxk
npx supabase migration list --linked
npx supabase db push --dry-run
npx supabase db push
```

For a new empty Supabase project, execute `supabase/schema.sql` first, then apply and mark `20260830_incremental_sync.sql` as applied before relying on `supabase db push`. This avoids applying the incremental migration before its parent tables exist. Apply `20260902_harden_sync_rls.sql` to every existing project before allowing cloud sync: it explicitly denies the anonymous database role and permits each signed-in user to read and write only that user's sync records. Apply `20260902120000_sheet_sharing.sql` before using sheet sharing.

After a production URL exists, update Supabase Auth’s Site URL and allowed redirect URLs before testing email sign-in. Use two separate email accounts to verify account isolation and sheet sharing. Use the same email account on two devices to test cross-device sync.

## Sync security and data ownership

The Vercel URL may be public without exposing another user's cloud data. The browser uses the public Supabase anonymous key only to reach Supabase; an authenticated Supabase session and RLS policies are both required to read or write cloud records. The anonymous database role has no access to either cloud table.

Sheet owners can grant an email address one of three roles: **Read** (view), **Contribute** (view and add transactions), or **Edit** (change sheet details and transactions). `sheet_shares` stores those grants. The `app_sync_records` policies expose only the matching sheet record and transactions to the invited email, allow contributors to create transactions only, and allow editors to mutate those records. The recipient must authenticate using the invited email and press Sync; this implementation does not send an invitation email or expose owner preferences or categories.

Local data remains in the browser's IndexedDB when a user is not signed in. Anyone with access to that browser profile or an unlocked device can read it; Sensitive Mode only hides values in the interface and is not encryption.

The app does not implement account-session administration. A device list would not add protection by itself. Use Supabase Auth session controls for that concern. Sharing is limited to individual sheet records and transactions; it is not a household model.

## Pause or remove services separately

Pausing Vercel is reversible and only stops the web app’s production traffic. It does not pause or delete Supabase:

```sh
npx vercel project pause old-pants
npx vercel project resume old-pants
```

At the end of the project, export any required data first. Then delete each service independently. Both commands are irreversible:

```sh
npx vercel project remove old-pants --yes
npx supabase projects delete ovrdwkcruxnthtcjbzxk --yes
```

Any future agent should follow this section rather than assuming that pausing or deleting one provider affects the other.
