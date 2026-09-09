# Badminton availability source audit

Last reviewed: 2026-08-22 (Asia/Singapore)

## Runtime decision

This prototype is restricted to **read-only badminton availability**. The
operator has confirmed written partner approval for the integrations below; the
approval material is intentionally not stored in this repository because it
contains personal data. That approval supersedes the earlier public-terms-only
assessment for these integrations, but does not authorize booking, payments,
account changes, CAPTCHA/OTP bypass, or collection beyond the configured
availability window.

The runtime selection order is partner API, approved authenticated Playwright
browser session, then a partner-approved public page, followed by a dedicated
reader when configured. Four verified public readers are enabled without
credentials. onePA's verified anonymous reader requires explicit public facility
IDs and is disabled by default. Configured API/browser/public access takes
precedence over each built-in reader. ActiveSG, onePA, and The Kallang can
additionally enable dedicated readers after their enabled generic modes. Source
health records each failed access mode in attempt order; an empty successful
snapshot is distinct from a failed request.

| Source ID | Operator / badminton surface | Read access | Horizon default |
| --- | --- | --- | --- |
| `myactivesg` | ActiveSG | dedicated imported-session typed badminton reader, or partner API/browser/public reader | 15 days |
| `onepa` | People's Association / onePA | dedicated anonymous reader for configured public facility IDs, or partner API/browser/public reader | 10 days |
| `the-kallang` | The Kallang / OCBC Arena | dedicated imported-session PerfectGym badminton reader, or partner API/browser/public reader | 30 days |
| `sba-stadium` | Singapore Badminton Association / KFF Badminton Arena @ Guillemard | built-in public anonymous reader; API/browser/public mapper override supported | 7 days |
| `singapore-badminton-hall` | Singapore Badminton Hall | built-in Playtomic public reader for confirmed SBH/TSA locations; API/browser/public mapper override supported | 7 days |
| `smash-arena` | Smash Arena | built-in public anonymous reader; API/browser/public mapper override supported | 1 day |
| `wyse-active` | Wyse Active Hub / Rezerv | built-in public anonymous reader; API/browser/public mapper override supported | 7 days |
| `trusmash` | TruSmash / AFA | partner API or approved browser/public reader | 14 days |

The horizon is an operator-configurable maximum; set
`availability_max_days` to a positive value to choose a different bound.
The public readers use a one-hour poll floor. Smash's public interface requires
a request per available hour, so its default is one day; a longer configured
window deliberately increases read volume.

## Runtime audit: 2026-08-23 (Asia/Singapore)

The documented Docker audit was run against the enabled anonymous readers:

```sh
docker compose build
docker compose run --rm daemon config validate
docker compose run --rm daemon refresh --json
docker compose run --rm daemon sources doctor
docker compose run --rm daemon search --date 2026-08-23 --duration 1h
docker compose up -d daemon
```

All five enabled public contracts completed without a failure: SBA returned 257
availability records, Singapore Badminton Hall 2,553, Smash Arena 7, Wyse
Active 1,304, and SportSG discovered 45 venues. Those counts are point-in-time
observations rather than a coverage guarantee. OneMap correctly reported
`credentials_required`; it was not treated as a reader failure. `myactivesg`,
onePA, The Kallang, and TruSmash were disabled and were therefore intentionally
outside this live audit.

Use `refresh --json` as the live contract check. `sources doctor` reports the
last persisted health and does not send a new upstream request. See the
[device migration and provider onboarding runbook](../operations/device-migration-and-provider-onboarding.md)
for the repeatable audit procedure and next-provider handoff.

## Supporting official surfaces

- [ActiveSG badminton facilities](https://www.activesgcircle.gov.sg/facilities/badminton)
- [ActiveSG badminton facility-booking venue list](https://activesg.gov.sg/facility-bookings/activities/YLONatwvqJfikKOmB5N9U/venues)
- [onePA availability](https://www.onepa.gov.sg/facilities/availability)
- [The Kallang badminton](https://change.sportshub.com.sg/sport-fitness/badminton)
- [SBA playing and KFF booking](https://singaporebadminton.org.sg/playing/)
- [Singapore Badminton Hall](https://singaporebadmintonhall.com/book-now/)
- [Smash Arena](https://booking.smasharena.sg/)
- [Wyse Active Hub](https://www.wyseactivehub.com/)
- [TruSmash](https://trusmash.com.sg/)

SportSG's [data.gov.sg facility dataset](https://data.gov.sg/datasets/d_9b87bab59d036a60fad2a91530e10773/view)
is separately used for venue discovery. OneMap is separately used for optional
geocoding and routing when configured.

## ActiveSG typed-reader contract

The dedicated `myactivesg` reader is restricted to the verified **badminton**
venue-list URL above. It requires a user- or partner-imported Playwright
storage-state value and is disabled by default. The reader has no login,
credential, booking, ballot-review, checkout, payment, CAPTCHA, or OTP code.

The September 2026 live capture established this read-only interaction contract:

1. Read typed venue records from `venue.listByActivity`.
2. Select configured venue names, or every current venue only when `scan_all = true`.
3. Read typed date and time ranges from `schedule.listAvailable`.
4. Publish only entries whose provider type is `instant`; retain ballot dates as non-bookable observations.

The mapper preserves the endpoint's explicit epoch-millisecond start and end;
it does not infer durations from page text. The endpoint supplies subvenue IDs
but not court names, so the current normalized records remain venue-level.
Ballot availability and dates with no instant ranges are parsed as non-bookable results and do not become
bookable records. A venue page that cannot be read fails the entire snapshot;
Kaypoh does not persist a partial scan and accidentally reconcile unseen slots
as unavailable.

## The Kallang / OCBC Arena PerfectGym contract

The dedicated `the-kallang` reader accepts only the PerfectGym client portal
URL and a user- or partner-imported Playwright storage-state value. It is
disabled by default and has no login, credential, booking, cart, checkout,
payment, CAPTCHA, or OTP code.

The August 2026 browser capture established this read-only interaction
contract:

1. Load `https://thekallang.perfectgym.com/clientportal2/`.
2. Read the `GetWeeklySchedule` calendar response.
3. Select the **Badminton Courts** facility type only when it is not selected.
4. Click only `Next week` to cover the configured horizon, reading each
   resulting calendar response.

The response contains `StartTime`, `EndTime`, `Status`, and duration metadata.
[Inference] The response is venue-level when “Any facility” is selected: the
captured bookable entries have no stable court name or ID. The reader filters
`Bookable` entries, deduplicates identical time ranges, and publishes
venue-level availability only. A changed response shape, missing requested
facility type, or failed week navigation fails the complete snapshot rather
than reconciling incomplete data as unavailable.

## Operator hand-off checklist

For each provider, place only the following in the local `config.toml` and
environment secret store:

1. API base URL, availability endpoint, token, and a fixture or payload mapper.
2. Or a service-account username/password, login selectors, availability URL,
   and a JSON selector that yields the documented availability payload.
3. Or a base64 imported Playwright storage state plus the availability URL and
   JSON selector.

Never commit any credentials, cookies, raw provider payloads containing personal
data, or approval emails. The unit fixtures are sanitized and the persistence
layer stores normalized slots and a slot-identity digest only.
