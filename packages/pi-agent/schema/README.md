# Kelp Pi Protocol Schemas

`envelope.schema.json` defines the signed wire envelope. `payloads.schema.json`
defines one payload schema per v1 `kind`; each `$defs` entry includes an example.

| Kind               | Payload schema                                 |
| ------------------ | ---------------------------------------------- |
| `hello`            | `payloads.schema.json#/$defs/hello`            |
| `welcome`          | `payloads.schema.json#/$defs/welcome`          |
| `policy.pull`      | `payloads.schema.json#/$defs/policy.pull`      |
| `policy.push`      | `payloads.schema.json#/$defs/policy.push`      |
| `scope.set`        | `payloads.schema.json#/$defs/scope.set`        |
| `scan.request`     | `payloads.schema.json#/$defs/scan.request`     |
| `scan.event`       | `payloads.schema.json#/$defs/scan.event`       |
| `scan.complete`    | `payloads.schema.json#/$defs/scan.complete`    |
| `evidence.append`  | `payloads.schema.json#/$defs/evidence.append`  |
| `bundle.export`    | `payloads.schema.json#/$defs/bundle.export`    |
| `bundle.fetch`     | `payloads.schema.json#/$defs/bundle.fetch`     |
| `ask.query`        | `payloads.schema.json#/$defs/ask.query`        |
| `ask.result`       | `payloads.schema.json#/$defs/ask.result`       |
| `selfcheck.run`    | `payloads.schema.json#/$defs/selfcheck.run`    |
| `selfcheck.report` | `payloads.schema.json#/$defs/selfcheck.report` |
