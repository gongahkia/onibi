# Singapore sports-facility source audit

Last reviewed: 2026-08-10 (Asia/Singapore)

## Decision

`courtsg` automates only documented, officially supported data/API surfaces.
SportSG's open data.gov.sg facility GeoJSON is enabled for live venue discovery.
OneMap's documented APIs are enabled when the user supplies registered
credentials. Geographic ranking falls back to a local Haversine calculation.

No booking-system scraper is enabled initially. The remaining providers either
expressly prohibit extraction, require partner/venue credentials, require an
account, or do not publish an automation policy/API that permits a reliable
read-only integration. They remain curated link/metadata records where
appropriate. This is a fail-closed compliance decision, not a statement that
their sites cannot technically be queried.

## Method and policy

This review used official operator documentation, terms, API documents and
`robots.txt` responses on 2026-08-10. Public visibility is not permission to
extract. A missing or permissive robots file is not permission where terms or
API documentation are absent. A source may only be promoted after a fresh review
of terms, robots, APIs, authentication and parser fixtures.

Statuses: `enabled_public_data` (documented open data), `enabled_official_api`
(documented API with required user credential), `requires_permission` (official
partner integration), `manual_only` (curated metadata/link only), `link_only`,
`disabled_by_terms`, and `disabled_unknown_policy`.

All web-source policies default to a 10-minute floor only after an adapter is
approved. The enabled venue dataset refreshes once per day by default; OneMap is
on-demand and cached. The central client enforces host allowlists, one in-flight
request per key, timeouts, bounded retries, `Retry-After`, backoff/jitter,
conditional requests, bounded bodies and circuit degradation.

## Source matrix

`metadata` includes name/address/coordinates and known sports. `price` means a
normalizable published rate; `availability` means normalized current slots.
`—` means not claimed. Every row was reviewed on 2026-08-10.

`local-manual` is an application-owned input source, not an upstream operator:
it accepts user-entered slots into the local database and makes no network
requests. It exists so a user can safely search, rank, and watch availability
that they are authorized to supply themselves.

| ID / name / operator | Sports and coverage | metadata / price / availability / booking link | transport and auth | robots / terms / API or integration | poll floor / concurrency | status / confidence / evidence |
| --- | --- | --- | --- | --- | --- |
| `sportsg-facilities` / SportSG facilities / Sport Singapore | SportSG-managed facilities nationwide; venue-level data, court inventory not guaranteed | yes / — / — / official facilities link | data.gov.sg public dataset API; optional production key; documented API returns a time-limited direct `s3.ap-southeast-1.amazonaws.com` download URL | robots permits; [dataset][sportsg-data] is Open Data Licence; [dataset API][data-api] documented | 24 h / 1 host request | `enabled_public_data` / high / [dataset][sportsg-data] |
| `onemap` / OneMap / Singapore Land Authority | Singapore geocoding and walk, cycle, drive and public-transport routing | geocode / — / — / — | documented Search and Routing APIs; registered token required | robots permits; [API terms][onemap-terms] and [routing docs][onemap-routing] govern credentials and limits | on demand, 30 d cache / 2 requests | `enabled_official_api` / high / [docs][onemap-routing] |
| `myactivesg` / MyActiveSG+ / SportSG | badminton, basketball, football, hockey, pickleball, table tennis, tennis, volleyball and other SportSG facilities | public metadata / variable public rates / no approved automated read / official link | public JavaScript site; booking requires ActiveSG/Singpass | `/robots.txt` served an app 404/noindex rather than policy; [terms][activesg-terms] limit automated devices and prohibit reproduction without permission; no public availability API found | n/a / 0 | `disabled_unknown_policy` (link only) / medium / [terms][activesg-terms] |
| `onepa` / onePA / People's Association | CC/RN badminton, basketball, futsal, football, table tennis; some CC courts permit pickleball | public catalogue / rendered labels / public calendar but no approved automated read / official link | JavaScript application with account/payment booking | robots allows facilities; [PA terms][pa-terms] restrict reproduction and caching/linking without permission; no developer API; undocumented prior endpoints are not used | n/a / 0 | `disabled_unknown_policy` (link only) / high / [availability][onepa-availability] |
| `safra` / SAFRA / SAFRA | member/public-adjacent badminton, tennis, squash, futsal and other clubs | public venue pages / variable / — / official link | consumer/member booking site | [terms][safra-terms] expressly exclude data mining, robots and similar extraction; robots disallows terms path | n/a / 0 | `disabled_by_terms` (link only) / high / [terms][safra-terms] |
| `playtomic` / Playtomic / venue partners | padel, tennis, pickleball and venue-defined sports; Singapore coverage varies by partner | partner API data / partner-defined / only with authorization / official link | [Third Party API][playtomic-api] credentials issued in venue manager; [Connect][playtomic-connect] is official program | [robots][playtomic-robots] disallows `/api`, `/search`, sport/date query paths; partner terms govern use | partner policy / configured cap | `requires_permission` / high / [Connect][playtomic-connect] |
| `the-kallang` / The Kallang / The Kallang Group | pickleball, tennis, badminton, basketball, netball, volleyball, table tennis, beach volleyball, lawn bowls, water sports | public descriptions/rates / some published / no API / official link | public booking flow and payment | [booking T&Cs][kallang-terms] found; no developer read API/extraction authorization found | n/a / 0 | `manual_only` / medium / [pickleball page][kallang-pickleball] |
| `sba-stadium` / Singapore Badminton Stadium / Singapore Badminton Association | badminton | public location / booking FAQ / no approved automated slots / official link | own booking portal; no documented developer API | general robots permit crawl but no terms/API authorizing extraction located | n/a / 0 | `manual_only` / medium / [booking page][sba-booking] |
| `singapore-badminton-hall` / Singapore Badminton Hall / SBH | badminton | public address / no verified structured price / no slots / telephone/manual link | telephone/manual booking | no official automation/API policy found in review | n/a / 0 | `manual_only` / low / [SBA directory][sba-booking] |
| `smash-arena` / Smash Arena / Smash Arena | badminton | public location/release window / variable public pricing / no approved slots / official link | consumer booking platform; 7-day rolling availability | [terms][smash-terms] state booking conditions; robots has no restriction but no developer API/permission found | n/a / 0 | `disabled_unknown_policy` (link only) / medium / [terms][smash-terms] |
| `wyse-active` / Wyse Active Hub / Rezerv | badminton; 32-court venue | public metadata / platform price where shown / no approved slots / official link | Rezerv booking service; account/OTP may apply | [Rezerv terms][rezerv-terms] and permissive robots reviewed; no consumer developer API/permission found | n/a / 0 | `disabled_unknown_policy` (link only) / medium / [venue][wyse] |
| `trusmash` / TruSmash / Viva Capital and AFA | badminton | public location and price table / static published price / no approved slots / official link | AFA web/app booking service | [TruSmash][trusmash] documents AFA; no read API/policy authorizing extraction found | n/a / 0 | `disabled_unknown_policy` (link only) / medium / [operator][trusmash] |
| `oba` / Optimum Badminton Academy / OBA | badminton training and selected court booking | public listing / variable / app-only availability / official link | OBA app | [app listing][oba-app] confirms booking; no public integration program found | n/a / 0 | `manual_only` / medium / [app][oba-app] |
| `performance-pickleball` / Performance Pickleball / Umeus | pickleball: Boathouse, Beach Club, pop-ups | public location/price / published peak/off-peak / real-time in account system only / official link | proprietary app and membership system | [official page][performance] says availability/bookings are exclusively through its app/system; no automation program found | n/a / 0 | `manual_only` / high / [booking][performance] |
| `play-pickle` / Play! Pickle / operator unverified | pickleball | public address / advertised price may vary / no approved slots / official link | booking application | public [operator page][play-pickle] found; terms/API insufficient for scraper | n/a / 0 | `disabled_unknown_policy` (link only) / low / [page][play-pickle] |
| `matchpoint-inc` / Matchpoint Inc / Matchpoint Inc | indoor pickleball and virtual tennis | public location/rates / static peak/off-peak / no approved slots / official link | consumer booking link | [services page][matchpoint-inc] describes rates; no API/policy permitting extraction found | n/a / 0 | `manual_only` / medium / [services][matchpoint-inc] |
| `kings-pickleball` / Kings Pickleball Arena / Kings | pickleball | public venue metadata / variable / no approved slots / official link | customer booking flow | [operator page][kings] found; no published API/automation policy located | n/a / 0 | `manual_only` / low / [page][kings] |
| `mbp-sports` / MBP Sports / MBP Sports | pickleball, padel, tennis at selected sites | public locations / app/membership price / app-only slots / official link | MBP Sports app; membership may apply | [directory][mbp] says app booking; no developer integration or permission found | n/a / 0 | `manual_only` / medium / [directory][mbp] |
| `picklechoo` and `pickle-lize` / current operators unverified | reported pickleball listings | no verified official data / — / — / — | unknown | no verified official operator policy/API found | n/a / 0 | `manual_only` (not seeded until verified) / low / no authoritative source |

## Existing project audit

The deprecated [onePA badminton finder][prior-onepa] called undocumented onePA
endpoints and created a single-sport local map. Its stated rate-limiting intent
does not substitute for current operator permission, so this project does not
copy its technique. `badmintoncourts.sg` is the named successor. This project
instead differentiates by source policy, multi-sport normalization, persistence,
watches, notifications, commute ranking, TUI/CLI and MCP—not by a novelty claim.

ActiveSG Court Map is location-oriented. Sportify is a 2026 hack project for
player-organised games. Neither is a documented compliant availability API.

## Implementation consequences

1. Sources advertise only capabilities they can execute; a link-only source never
   claims availability.
2. SportSG data.gov.sg is a real end-to-end discovery source: it fetches,
   validates and normalizes official GeoJSON into the local catalogue.
3. OneMap is a real credentialed source. Missing credentials cause a labelled
   Haversine fallback, not loss of search/watches/TUI.
4. Curated records retain only minimal operator/sport/policy/link data. They do
   not copy protected rich content.
5. Empty availability never masks transport/parser/policy failure. Source doctor,
   CLI JSON, HTTP and MCP show disabled/credential/stale state explicitly.

## MCP and routing audit

OneMap Search now requires a token; its documented routing supports `walk`,
`cycle`, `drive` and `pt`, returns `401`/`429`, and is cached by `courtsg`.

The [official Go MCP SDK][mcp-go-sdk] is Tier 1, supports stdio and Streamable
HTTP, and its v1.7.0 release supports MCP 2026-07-28 while preserving older
protocols. `courtsg` uses this SDK rather than hand-rolled JSON-RPC. Stdio is the
primary transport. Streamable HTTP is loopback-only with origin validation and
write tools disabled by default.

Current [Hermes docs][hermes-mcp] use `~/.hermes/config.yaml` `mcp_servers` with
stdio `command`/`args`. Current [OpenClaw docs][openclaw-mcp] provide
`openclaw mcp add` and `openclaw mcp probe`. Neither executable was installed for
a live probe during this audit; setup and probes are documented in `docs/mcp.md`.

## Evidence

[sportsg-data]: https://data.gov.sg/datasets/d_9b87bab59d036a60fad2a91530e10773/view
[data-api]: https://guide.data.gov.sg/developer-guide/dataset-apis/download-dataset
[onemap-terms]: https://www.onemap.gov.sg/legal/apitermsofservice.html
[onemap-routing]: https://www.onemap.gov.sg/apidocs/routing
[activesg-terms]: https://file.go.gov.sg/activesg-terms-of-use.pdf
[pa-terms]: https://www.pa.gov.sg/terms-of-use/
[onepa-availability]: https://www.onepa.gov.sg/facilities/availability
[safra-terms]: https://www.safra.sg/terms-of-use
[playtomic-robots]: https://playtomic.com/robots.txt
[playtomic-connect]: https://playtomic.com/connect
[playtomic-api]: https://third-party.playtomic.io/
[kallang-terms]: https://change.sportshub.com.sg/facility-booking-terms-and-conditions
[kallang-pickleball]: https://www.thekallang.com.sg/en/things-to-do/sports/pickleball.html
[sba-booking]: https://staging.singaporebadminton.org.sg/book-a-badminton-court/
[smash-terms]: https://smasharena.sg/terms-%26-conditions
[rezerv-terms]: https://www.rezerv.co/terms-and-conditions
[wyse]: https://www.wyseactivehub.com/
[trusmash]: https://trusmash.com.sg/
[oba-app]: https://play.google.com/store/apps/details?id=com.zencloud.oba
[performance]: https://www.performancepickleball.org/court-booking
[play-pickle]: https://www.playpickle.sg/cny-2026
[matchpoint-inc]: https://matchpointinc.com.sg/services/
[kings]: https://kingspickleballarena.com/
[mbp]: https://www.pickleball.sg/
[prior-onepa]: https://github.com/Jarrettgohxz/onepa-badminton-courts-finder
[mcp-go-sdk]: https://github.com/modelcontextprotocol/go-sdk
[hermes-mcp]: https://github.com/NousResearch/hermes-agent/blob/main/website/docs/user-guide/features/mcp.md
[openclaw-mcp]: https://docs.openclaw.ai/cli/mcp
