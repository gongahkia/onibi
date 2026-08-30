# Together Budget

An original, mobile-first budgeting PWA for a couple: shared and personal ledgers, budgets, savings goals, multi-currency tracking, custom splits/settlements, on-device receipt OCR, CSV migration, and optional bank feeds.

## Run locally

```sh
cp .env.example .env.local
npm install
npm run dev
```

The app is immediately usable as a local demo and persists its sample data to browser storage. Use the Settings screen to import a compatible CSV or export your current demo data.

## Deploy the real application

1. Create a Supabase project, run [`supabase/schema.sql`](supabase/schema.sql), and enable magic-link email auth.
2. Add `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_ANON_KEY`, and server-only Google or bank-provider secrets to Vercel. Never expose service-role, Google, or bank-provider secrets in the browser.
3. Configure Google OAuth only for the household-owner one-way Sheets export. Configure VAPID keys and a scheduled job for recurring entries, rate refresh, and notification reminders.
4. Receipt OCR runs locally in a lazily loaded WebAssembly worker. Receipt images are discarded after text extraction; only the reviewed transaction values and extracted text are saved.

## Bank and card transactions

This repository intentionally does not collect card numbers, passwords, PINs, or banking logins. A regulated/open-finance provider must host customer consent and return read-only, signed transaction data to the server.

- **Preferred in Singapore:** SGFinDex offers user-consented financial-data retrieval through Singpass. It needs GovTech approval, a registered redirect URI/JWKS endpoint, PKCE, DPoP, and server-side signed client assertions before production use.
- Plaid production transaction coverage is documented for North America and Europe, not Singapore. Salt Edge's current Singapore coverage page reports zero connections.
- Brankas remains the commercial Southeast Asia Open Finance alternative, but it also needs commercial approval, confirmed bank coverage, credentials, consent callback specification, and webhook signing secret.

Set `BANK_FEED_PROVIDER=sgfindex` or `brankas` and supply the matching server-only configuration. Until those prerequisites are available, use bank-statement CSV import; the app includes the normalized transaction and connection schema needed to turn a provider callback/webhook into deduplicated `source = 'bank'` ledger entries.

## Verification

```sh
npm run build
```

Production acceptance must additionally test magic-link authentication, RLS policies, two-account sharing, offline reconciliation, iPhone Home Screen install, reduced-motion behavior, CSV migration fixtures, on-device receipt OCR, and a provider sandbox webhook before enabling live bank data.
