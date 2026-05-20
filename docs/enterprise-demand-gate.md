# Enterprise Demand Gate

Date: 2026-05-20

Status: parked future work.

Enterprise implementation must not start until product demand and threat-model
evidence justify the operational cost. Piranesi remains a local-first artifact
workspace unless this gate is explicitly passed.

## Go Criteria

Open implementation issues only after all of the following are true:

- At least one design partner or customer names a concrete enterprise deployment
  need that the local artifact workflow cannot satisfy.
- The requested deployment model is known: hosted SaaS, single-tenant hosted,
  customer-managed on-prem, air-gapped, or hybrid.
- The customer can describe identity, data residency, retention, support access,
  audit, and backup requirements.
- The team has an implementation owner for secure operation, not only feature
  development.
- A threat model is reviewed before code begins.
- Public docs can describe the feature accurately without implying unsupported
  compliance, uptime, residency, or access-control guarantees.

## No-Go Criteria

Keep enterprise work parked when:

- the request is speculative or copied from a generic enterprise checklist;
- the need can be handled by local exports, signed artifacts, or one-way handoff;
- no customer can provide realistic identity, deployment, or data-control shape;
- implementation would weaken local exportability or evidence chain preservation;
- nobody owns operational support, incident response, or trust posture.

## Threat Areas

Every enterprise implementation plan must cover:

- tenancy and project isolation;
- identity provider integration and session management;
- role boundaries and authorization checks;
- audit logs and tamper-evident event history;
- data residency, encryption, backup, restore, and retention;
- support access, break-glass access, and support-bundle redaction;
- external exports such as SIEM, ticketing, email, and webhooks;
- artifact exportability and chain-of-custody preservation;
- incident response ownership for a hosted or customer-managed deployment.

## Current Decision

Phase 5 remains parked. Issues #82, #83, and #84 capture future requirements, but
they must not start implementation work. Issue #43 should remain an umbrella until
the gate produces a concrete, demand-backed enterprise plan or the work is
explicitly deferred again.
