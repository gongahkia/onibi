# Together Budget

An original, mobile-first budgeting PWA for a couple: shared and personal ledgers, budgets, savings goals, multi-currency tracking, custom splits/settlements, receipt attachments, CSV migration, and optional bank feeds.

## Run locally

```sh
cp .env.example .env.local
npm install
npm run dev
```

The app is immediately usable as a local demo and persists its sample data to browser storage. Use the Settings screen to import a compatible CSV or export your current demo data.

## Deploy the real application

1. Create a Supabase project, run [`supabase/schema.sql`](supabase/schema.sql), enable magic-link email auth, and create a private `attachments` storage bucket.
2. Add `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_ANON_KEY`, and server-only secrets to Vercel. Never expose `SUPABASE_SERVICE_ROLE_KEY`, `OPENAI_API_KEY`, Google secrets, or a bank provider secret in the browser.
3. Configure Google OAuth only for the household-owner one-way Sheets export. Configure VAPID keys and a scheduled job for recurring entries, rate refresh, and notification reminders.
4. Configure `OPENAI_API_KEY` only if receipt suggestions are desired. The endpoint uses the Responses API with `store: false`; attachments still work if it is unset.

## Bank and card transactions

This repository intentionally does not collect card numbers, passwords, PINs, or banking logins. A regulated/open-finance provider must host customer consent and return read-only, signed transaction data to the server.

- Plaid production transaction coverage is documented for North America and Europe, not Singapore.
- Salt Edge's current Singapore coverage page reports zero connections.
- Brankas is a Southeast Asia Open Finance provider and is the configured provider boundary, but a live connection requires its commercial approval, confirmed bank coverage, credentials, consent callback specification, and webhook signing secret.

Until those prerequisites are available, use bank-statement CSV import; the app includes the normalized transaction and connection schema needed to turn the provider callback/webhook into deduplicated `source = 'bank'` ledger entries.

## Verification

```sh
npm run build
```

Production acceptance must additionally test magic-link authentication, RLS policies, two-account sharing, offline reconciliation, iPhone Home Screen install, push permissions, CSV migration fixtures, receipt analysis with and without credentials, and a provider sandbox webhook before enabling live bank data.
