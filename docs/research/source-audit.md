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
browser session, then a partner-approved public page. Every source is disabled
until its operator configuration is present. A failed source is reported through
source health; an empty successful snapshot is distinct from a failed request.

| Source ID | Operator / badminton surface | Read access | Horizon default |
| --- | --- | --- | --- |
| `myactivesg` | ActiveSG | partner API or approved ActiveSG browser/public reader | 15 days |
| `onepa` | People's Association / onePA | partner API or approved browser/public reader | 15 days |
| `the-kallang` | The Kallang / OCBC Arena | PerfectGym/partner API or approved browser/public reader | 30 days |
| `sba-stadium` | Singapore Badminton Association / KFF Badminton Arena @ Guillemard | SBA/Quape partner API or approved browser/public reader | 14 days |
| `singapore-badminton-hall` | Singapore Badminton Hall | partner API or approved browser/public reader | 30 days |
| `smash-arena` | Smash Arena | partner API or approved browser/public reader | 14 days |
| `wyse-active` | Wyse Active Hub / Rezerv | partner API or approved browser/public reader | 30 days |
| `trusmash` | TruSmash / AFA | partner API or approved browser/public reader | 14 days |

The horizon is an operator-configurable maximum; set
`availability_max_days` to a positive value to impose a smaller global limit.
The listed defaults are implementation defaults and should be updated if a
partner contract specifies a different booking window.

## Supporting official surfaces

- [ActiveSG badminton facilities](https://www.activesgcircle.gov.sg/facilities/badminton)
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
