# Local HTTP API

Start the server with `kaypoh api serve`. Its default address is
`127.0.0.1:8373`; it never binds remotely unless `api.allow_remote = true` and
`api.auth_token` resolves to a non-empty secret. Remote requests use
`Authorization: Bearer <token>`.

The API accepts JSON bodies up to 1 MiB, rejects unknown fields for mutation and
search bodies, and returns JSON errors as `{"error":"..."}`.

| Method and path | Purpose |
| --- | --- |
| `GET /v1/health` | database, config, and source-health report |
| `GET /v1/sources` | source policies and health |
| `GET /v1/venues?search=&sport=&limit=` | discovered venues |
| `POST /v1/search` | search a `Query` object against local availability |
| `GET /v1/watches?enabled=true` | persistent watches |
| `POST /v1/watches` | create a local watch |
| `POST /v1/watches/evaluate` | evaluate enabled watches locally |
| `GET /v1/events?watch=&limit=` | idempotent watch events |
| `GET /v1/deliveries?event=&limit=` | delivery history |

The API does not expose arbitrary SQL, upstream HTTP, credential values, or any
booking action. `POST /v1/search` reads only the normalized SQLite state and
does not refresh sources.

Example search:

```sh
curl -sS http://127.0.0.1:8373/v1/search \
  -H 'content-type: application/json' \
  --data '{"sports":["badminton"],"minimum_duration":3600000000000,"ranking":"balanced"}'
```
