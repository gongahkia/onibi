# Multi-owner authorization (experimental design)

Status: v1.1 design. This document does not enable, describe an available command for, or authorize promotion of shared-fleet access. v1 remains a personal fleet with one owner and optional read-only session viewers.

## Goals and non-goals

This design defines the authorization boundary required before an experimental shared fleet exists. It must preserve the existing personal-fleet isolation model: an owner identity, device binding, paired-host trust, and private local configuration must never become tenant-shared by implication.

This design does not grant filesystem, SSH, secret-store, local-path, or local-workspace access to another principal. A tenant membership grants only the explicitly authorized Cockpit operation on a tenant resource.

## Terms and identities

- `tenant_id`: an opaque, randomly generated stable identifier. Every host, session, approval, invite, policy, audit event, and authorization decision belongs to exactly one tenant.
- `principal_id`: the public-key fingerprint of a human identity. Email addresses, display names, and invite URLs are labels, never authorization identities.
- `device_id`: a public-key fingerprint bound to one principal. A device proves possession for every online action.
- `membership_version`: a monotonically increasing tenant value. It changes on every membership, role, scope, invite, or ownership change.
- `scope`: an allowlist of tenant resource IDs and operation names. An absent scope grants nothing.

No resource may be moved between tenants. No request may infer a tenant from a host name, workspace path, browser session, or caller-supplied display value.

## Authorization invariants

1. Deny by default. Unknown roles, operations, scopes, versions, principals, devices, protocol versions, or policy fields fail closed.
2. Check authorization at every externally reachable operation and again immediately before a persistent control action is sent to a host. Do not rely on route-level checks, cached UI state, or a prior WebSocket decision.
3. Bind every grant and control to `tenant_id`, `principal_id`, `device_id`, `membership_version`, operation, resource scope, audience, expiry, and a request nonce. Hosts reject an expired, replayed, cross-tenant, or stale-version control.
4. A principal cannot grant authority it does not possess. Scope and role delegation can only reduce authority.
5. Existing viewer links stay read-only and session-scoped. They do not create tenant membership, may not be promoted, and cannot be used to accept an invite or recover an identity.
6. The current one-owner schema and default flows remain authoritative until an explicit experimental migration is reviewed and released. There is no silent conversion to a tenant.

## Roles

Each tenant has exactly one `primary_owner`, zero or more `owner`s, zero or more `operator`s, and zero or more `viewer`s. Roles are capabilities, not titles; a role applies only within its tenant.

| Role | Allowed | Never allowed |
| --- | --- | --- |
| `primary_owner` | all tenant operations; owner management; ownership transfer; tenant policy, budget, paired-host, and identity-rekey changes | transfer authority without the target's proof of possession |
| `owner` | all host and session operations; invite, modify, and revoke `operator` and `viewer` memberships; issue narrower scopes | modify or revoke any owner; transfer primary ownership; change rekey, paired-host, budget, or tenant policy |
| `operator` | explicitly scoped approve, deny, edit, and interrupt actions | invite, revoke, change roles, modify trust, kill a host, change budgets, view unscoped sessions, or transfer ownership |
| `viewer` | explicitly scoped read-only session and metadata observation | submit input, approve, deny, edit, interrupt, export, change policy, or access stored secret material |

`primary_owner` may create an `owner` only after the target accepts a direct invitation. An owner can never create another owner. All authority changes require a fresh device proof from the affected principal.

The product review must approve this role matrix, especially whether an operator may edit an approval. An unapproved operation is denied.

## Invitations

An invitation is a single-use, short-lived signed capability directed to one `principal_id` and one pending `device_id`; its maximum lifetime is 15 minutes. It contains the tenant, proposed role, scope, inviter, target, `membership_version`, expiry, nonce, and intended Cockpit audience. It contains no bearer authorization usable without the target device key.

Acceptance requires the target device to prove possession and sign the exact invitation. The authority service serializes acceptance, consumes the nonce, increments `membership_version`, creates the membership, and records an audit event atomically. Expired, replayed, altered, cross-audience, or wrong-key invitations fail closed. A cancelled or revoked invitation cannot be resumed; a new invitation is required.

There is no email-link recovery, anonymous invitation acceptance, or self-service role escalation. Identity recovery is deferred to product and security review; it must not bypass device proof or tenant authorization.

## Audit record

The authority service records immutable, append-only, tamper-evident events for tenant creation, invite creation/cancellation/expiry/acceptance/replay rejection, membership and scope changes, authorization allow/deny, device changes, revocation, ownership-transfer state changes, and policy changes. Each record contains:

- event ID, timestamp, tenant ID, request ID, result, and policy/protocol version;
- actor and target principal/device fingerprints;
- role and a hashed resource-scope identifier;
- current and resulting `membership_version`; and
- previous-record hash plus an authority signature.

Audit records must not contain terminal output, tool input, approval payloads, secrets, raw workspace paths, invite material, or user-controlled free text. Audit viewers are read-only and receive only events for scopes they may observe; no separate auditor role exists in this phase.

## Revocation

The `primary_owner` may revoke any non-primary membership. An `owner` may revoke only `operator` and `viewer` memberships it is authorized to manage. Revocation atomically increments `membership_version`, invalidates the member's device sessions and pending invitations, and appends an audit record.

All connected clients reauthorize on the new version or disconnect. Queued controls created by the revoked principal are cancelled before delivery; hosts reject a queued control carrying an older membership version. A principal whose device is revoked must obtain a fresh, explicitly authorized membership and invitation. There is no grace period.

## Ownership transfer

Primary ownership can move only to an existing `owner` membership. The current primary initiates a transfer bound to the target principal/device, tenant, current `membership_version`, a short expiry, and a nonce. The target accepts with its device key; the current primary confirms the accepted transfer from a current paired device. The authority service then atomically makes the target primary, demotes the former primary to `owner`, increments `membership_version`, rotates tenant authority material, invalidates affected sessions, and writes a signed audit event.

Only the initiating primary may cancel a pending transfer. There is no automatic transfer on inactivity, unavailable devices, or lost credentials, and another owner cannot force a transfer. Failure at any step leaves the prior primary unchanged. A recovery path, if any, requires separate product and security approval.

## Protocol and storage boundary

Shared-fleet authorization requires a new, versioned protocol capability; it must not be smuggled into existing personal-fleet frames. Each authority-to-host control must be signed by tenant authority and carry the complete binding in the authorization invariants above. A host validates the signature, audience, tenant, host/session scope, action, expiry, nonce, and membership version before executing it.

If a browser-facing access token is introduced, it must be audience restricted and sender constrained to the authenticated device. A bearer token alone is insufficient for host control. Persistent authorization state must be tenant-keyed; no lookup may use a global `owner_id` as a cross-tenant capability.

An eventual migration may add tenant-aware tables, but must not create memberships, alter the current owner, share workspace bindings, or activate shared access without an explicit primary-device confirmation. Downgrade or failed migration must retain the original personal-fleet data untouched.

## Required implementation evidence

Before any experimental profile is exposed, implementation must add tests covering:

- complete role/action/scope matrix, including every denied action;
- cross-tenant IDs, forged tenant values, and confused-deputy host/session routing;
- invite expiry, replay, cancellation, wrong device, altered scope, and concurrent acceptance;
- stale membership versions across HTTP, WebSocket, queued control, and host reconnect paths;
- revocation during an active session and immediately before control delivery;
- ownership-transfer concurrency, cancellation, partial failure, and authority-material rotation;
- audit-chain verification and absence of terminal, approval, secret, path, and invite material; and
- migration, downgrade, and default personal-fleet behavior.

The implementation must pass `go test -race ./internal/web ./internal/store` plus protocol and migration tests before review. Promotion requires recorded product approval of role and recovery semantics and security approval of the threat model, migration, token binding, audit handling, and revocation behavior.

## Sources

- [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)
- [NIST RBAC FAQ](https://csrc.nist.gov/Projects/role-based-access-control/faqs)
- [RFC 9700: OAuth 2.0 Security Best Current Practice](https://www.rfc-editor.org/rfc/rfc9700.html)
