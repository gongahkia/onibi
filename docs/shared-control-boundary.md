# Deferred Shared-Control Authorization

Status: rejected for v1. This is a prerequisite security design for any future scope change, not an available command, profile, or promotion plan.

Onibi remains a personal, single-owner local cockpit. A shared-control feature must never convert an existing owner, browser session, device, local path, workspace, secret, or host into shared state by implication.

## Identities and invariants

- `tenant_id` is an opaque stable identifier. Every host, session, approval, invite, policy, audit event, and authorization decision belongs to exactly one tenant.
- `principal_id` is a human identity's public-key fingerprint; labels and email addresses are never authorization identities.
- `device_id` is a public-key fingerprint bound to one principal and proves possession for every remote action.
- `membership_version` increases on every membership, role, scope, invitation, device, or ownership change.
- `scope` is an allowlist of tenant resource IDs and operations. An absent scope grants nothing.

All future shared-control implementations must deny by default; authorize every request and again immediately before a persistent control action; bind grants to tenant, principal, device, scope, audience, expiry, nonce, and membership version; and reject unknown, stale, replayed, cross-tenant, or changed-identity requests. A grant may only reduce the grantor's authority.

No resource can move between tenants. Tenant identity cannot be inferred from a host name, workspace path, browser session, or caller-controlled value. Existing personal-owner data remains authoritative until an explicit reviewed migration completes.

## Roles

Each tenant has exactly one `primary_owner`, zero or more `owner`s, `operator`s, and `viewer`s. Roles apply only within their tenant.

| role | allowed | never allowed |
| --- | --- | --- |
| `primary_owner` | all tenant operations; owner management; ownership transfer; policy, paired-host, and identity-rekey changes | transfer authority without the target's device proof |
| `owner` | host/session operations; invite, modify, and revoke narrower memberships; issue narrower scopes | modify or revoke an owner; transfer primary ownership; alter tenant policy or rekey material |
| `operator` | explicitly scoped approve, deny, edit, and interrupt actions | invite, revoke, alter roles/policy, kill a host, access unscoped sessions, or transfer ownership |
| `viewer` | explicitly scoped read-only session and metadata observation | submit input, decide approvals, interrupt, export, alter policy, or access secret material |

Only a primary owner can create an owner, after direct invitation acceptance and target device proof. An owner can never create another owner. Product review must explicitly approve the operator decision matrix; unspecified actions are denied.

## Invitations and lifecycle

An invitation is a single-use, short-lived signed capability for one `principal_id` and pending `device_id`; maximum lifetime is 15 minutes. It binds tenant, role, scope, inviter, target, membership version, expiry, nonce, and Cockpit audience. It contains no bearer authority usable without the target device key.

Acceptance requires the target device to sign the exact invitation. The authority service atomically consumes the nonce, increments `membership_version`, records the membership, and appends an audit event. Expired, replayed, altered, cross-audience, wrong-device, cancelled, and revoked invitations fail closed. There is no email-link recovery, anonymous acceptance, or self-service escalation.

Revocation atomically increments `membership_version`, invalidates the member's device sessions and pending invitations, cancels queued controls, and appends an audit event. Connected clients reauthorize or disconnect; hosts reject controls bearing an older membership version. There is no grace period.

Primary ownership transfer targets an existing owner. The current primary initiates a short-lived, nonce-bound transfer to a specific target principal/device; the target accepts with that device key; then the current primary confirms from a current paired device. The authority service atomically promotes the target, demotes the former primary, increments `membership_version`, rotates tenant authority material, invalidates affected sessions, and records a signed audit event. Partial failure leaves the prior primary unchanged. No transfer occurs on inactivity, lost credentials, or another owner's request.

## Audit, protocol, and migration

Authority audit is immutable, append-only, and tamper-evident. It records tenant creation; invitation state; membership/scope/device/policy changes; authorization decisions; revocation; and ownership transfer. Each event includes event and request IDs, timestamp, tenant, result, policy/protocol version, actor and target fingerprints, role, hashed scope identifier, membership versions, previous-record hash, and authority signature.

Audit records never contain terminal output, approval/tool payloads, secrets, raw workspace paths, invite material, or free text. Audit viewers, if ever introduced, are read-only and scope-limited.

Shared control requires a new versioned protocol. Every authority-to-host control must be signed and must carry its complete authorization binding. Hosts validate signature, audience, tenant, resource scope, action, expiry, nonce, and membership version before execution. Browser tokens, if introduced, must be audience-restricted and sender-constrained; a bearer token alone cannot control a host.

A migration must use tenant-keyed persistent state, never silently create memberships, alter the current owner, share workspace bindings, or activate shared access. Failed migration or downgrade preserves the original personal-owner data untouched.

## Required implementation and review evidence

Before any experimental shared-control profile is exposed, implementation must test:

- every role/action/scope allow and deny path;
- cross-tenant and confused-deputy routing;
- invitation expiry, replay, cancellation, altered scope, wrong device, and concurrent acceptance;
- stale membership versions across HTTP, WebSocket, queued controls, and reconnects;
- revocation during active sessions and immediately before delivery;
- ownership-transfer concurrency, cancellation, partial failure, and authority rotation;
- audit-chain integrity and exclusion of terminal, approval, secret, path, and invite material; and
- migration, downgrade, and unchanged personal-owner defaults.

The implementation must pass `go test -race ./internal/web ./internal/store` plus its protocol and migration suites. Promotion requires recorded product approval of roles/recovery and security approval of the threat model, migration, token binding, audit handling, and revocation behavior.

## Sources

- [OWASP Authorization Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authorization_Cheat_Sheet.html)
- [NIST RBAC FAQ](https://csrc.nist.gov/projects/role-based-access-control/faqs)
- [RFC 9700: OAuth 2.0 Security BCP](https://www.rfc-editor.org/rfc/rfc9700.html)
