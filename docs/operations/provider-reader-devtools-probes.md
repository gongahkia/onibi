# Provider reader DevTools probes

Use these probes to gather the structural evidence needed for a new
provider-specific, read-only reader. They do not click a time, add a court to a
cart, submit a booking, log in, export cookies, or copy request/response values.
They record only request method, origin/path (never query values), status,
content type, bounded JSON field shapes, and non-text DOM selector attributes.

Do not export a HAR or share cookies, authorization headers, session storage,
request bodies, or raw response bodies. If a provider page requires a session,
use an operator-authorized session and share only the sanitized probe report.

## Install the probe

Open the provider URL below in a normal browser tab. Open DevTools, choose the
Console panel, and paste this once. The probe applies only to the current page
until it is reloaded.

```js
(() => {
  if (window.__kaypohProbe) {
    console.info("Kaypoh probe is already installed.");
    return;
  }

  const records = [];
  let provider = "unconfigured";
  let pattern = new RegExp(
    "availability|appointment|booking|calendar|court|facility|resource|schedule|slot|timeslot|venue",
    "i"
  );
  const endpoint = (value) => {
    const url = new URL(value, location.href);
    return `${url.origin}${url.pathname}`;
  };
  const schema = (value, depth = 0) => {
    if (depth >= 4 || value === null) return value === null ? "null" : typeof value;
    if (Array.isArray(value)) return { type: "array", items: value.length ? schema(value[0], depth + 1) : "unknown" };
    if (typeof value !== "object") return typeof value;
    return Object.fromEntries(Object.keys(value).sort().slice(0, 60).map((key) => [key, schema(value[key], depth + 1)]));
  };
  const record = (method, rawURL, status, contentType, text) => {
    let url;
    try { url = endpoint(rawURL); } catch { return; }
    if (!pattern.test(url)) return;
    const item = { provider, method, url, status, content_type: contentType || "" };
    if (/json/i.test(contentType || "") && text.length <= 1_000_000) {
      try { item.json_schema = schema(JSON.parse(text)); } catch { item.body_kind = "invalid_json"; }
    } else {
      item.body_kind = text.length > 1_000_000 ? "body_over_1mb" : "non_json";
    }
    records.push(item);
    console.info("Kaypoh probe", item);
  };

  const originalFetch = window.fetch;
  window.fetch = async function (...args) {
    const response = await originalFetch.apply(this, args);
    void response.clone().text().then((text) => record(args[0]?.url || args[0], response.url, response.status, response.headers.get("content-type"), text));
    return response;
  };

  const originalOpen = XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open = function (method, url, ...rest) {
    this.__kaypohRequest = { method, url };
    this.addEventListener("loadend", () => record(this.__kaypohRequest.method, this.responseURL || this.__kaypohRequest.url, this.status, this.getResponseHeader("content-type"), typeof this.responseText === "string" ? this.responseText : ""));
    return originalOpen.call(this, method, url, ...rest);
  };

  const dom = () => [...document.querySelectorAll("a,button,input,select,[role=button],[aria-label],[data-]")].slice(0, 250).map((element) => ({
    tag: element.tagName.toLowerCase(), id: element.id || "", classes: [...element.classList].slice(0, 12), role: element.getAttribute("role") || "",
    aria_label: element.getAttribute("aria-label") || "", name: element.getAttribute("name") || "", type: element.getAttribute("type") || "",
    href: element.hasAttribute("href") ? endpoint(element.getAttribute("href")) : "",
    data_attributes: Object.fromEntries([...element.attributes].filter((attribute) => attribute.name.startsWith("data-")).map((attribute) => [attribute.name, attribute.value])),
  }));
  window.__kaypohProbe = {
    configure(nextProvider, nextPattern) { provider = nextProvider; pattern = nextPattern; console.info(`Kaypoh probe configured for ${provider}`); },
    report: () => ({ provider, page: endpoint(location.href), records, dom: dom() }),
    copy() { const value = JSON.stringify(this.report(), null, 2); if (typeof copy === "function") copy(value); else console.log(value); return this.report(); },
  };
  console.info("Kaypoh probe active.");
})();
```

After installing and configuring the probe, use only the site's normal venue
and date controls to display availability. Do not click a time, continue,
review ballot, cart, checkout, or payment control. Then run:

```js
__kaypohProbe.copy()
```

## Per-provider commands

Each entry gives the exact page to open and the corresponding filter command.
The filter names likely availability-related endpoints without retaining their
values. A copied report is evidence for choosing a JSON adapter, generic browser
reader, or a provider-specific DOM reader.

### onePA

onePA now has a dedicated anonymous reader. Configure it with the exact public
`facilityId` after selecting a community club and **Badminton Courts**. For
example, Woodlands CC uses:

```toml
[sources.onepa]
enabled = true

[sources.onepa.onepa]
enabled = true
facility_ids = ["WoodlandsCC_BADMINTONCOURTS"]
```

The reader opens the public availability page once per refresh, then sends only
read-only `selectedFacility` and `selectedDate` availability requests. It uses
non-overlapping three-day calendar windows, and does not log in, click a court,
or begin a booking. Use the generic probe only when that endpoint's contract
changes.

### The Kallang / OCBC Arena

```js
location.assign("https://thekallang.perfectgym.com/clientportal2/")
```

After the page loads and the probe is installed:

```js
__kaypohProbe.configure("the-kallang", /availability|facility|perfectgym|slot|timeslot|venue|calendar|schedule/i)
```

## Existing custom public readers

onePA, SBA, Singapore Badminton Hall, Smash Arena, and Wyse Active Hub already
have provider-specific, anonymous public readers. Use the following commands
only to investigate a contract change or to extend their coverage.

| Provider | Open | Configure |
| --- | --- | --- |
| onePA | `location.assign("https://www.onepa.gov.sg/facilities/availability")` | `__kaypohProbe.configure("onepa", /facility|slot|availability/i)` |
| SBA / KFF Guillemard | `location.assign("https://booking.singaporebadminton.org.sg/")` | `__kaypohProbe.configure("sba-stadium", /location|slot|court|booking/i)` |
| Singapore Badminton Hall | `location.assign("https://playtomic.com/clubs/sbh-sims")` | `__kaypohProbe.configure("singapore-badminton-hall", /availability|club|resource|slot|court/i)` |
| Smash Arena | `location.assign("https://booking.smasharena.sg/")` | `__kaypohProbe.configure("smash-arena", /getSmashSlot|getSmashCourt/i)` |
| Wyse Active Hub | `location.assign("https://wyseactivehub.rezerv.co/timetable")` | `__kaypohProbe.configure("wyse-active", /onboarding|get-session|appt-schedule|timeslot_calendar/i)` |

The Singapore Badminton Hall reader covers three club slugs. Repeat its probe
for `sbh-east-coast-expo` and `sph` if the contract differs by location.

## Turning a report into a reader

Use a generic API reader when the report shows a stable approved JSON endpoint
that can be mapped to Kaypoh's normalized slots shape. Use the generic browser
reader when a page exposes an approved JSON script/element. Add a dedicated
browser reader only when the page requires provider-specific UI interpretation,
as ActiveSG does with venue links and date cards, or The Kallang does with its
facility-type calendar response. Add a sanitized fixture and tests before
enabling a new reader.
