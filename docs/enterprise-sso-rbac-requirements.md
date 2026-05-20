# Enterprise SSO And RBAC Requirements

Date: 2026-05-20

Status: parked future work behind the enterprise demand gate.

Piranesi does not currently implement hosted authentication, SSO, organizations,
teams, or RBAC. These requirements define the minimum shape for future enterprise
planning only.

## Identity Direction

- Prefer OIDC first because it is the most common modern SSO path for hosted and
  customer-managed web applications.
- Defer SAML until a design partner explicitly requires it.
- Defer SCIM until there is a real multi-user organization model.
- Keep local CLI/workspace use independent from hosted identity.

## Candidate Roles

Future RBAC should start with five roles:

- `owner`: manages organization settings, identity configuration, retention,
  project membership, and export policy.
- `operator`: imports evidence, updates workspace data, drafts reports, runs
  retests, and prepares handoff artifacts.
- `reviewer`: reviews findings, report drafts, retest state, and delivery
  readiness without changing raw evidence.
- `viewer`: reads reports, timelines, findings, and approved artifacts.
- `auditor`: reads audit logs, chain-of-custody records, export history, and
  retention metadata.

## Permission Boundaries

An implementation plan must define permissions for:

- workspace creation, archive import, and archive export;
- evidence upload, redaction updates, and evidence deletion;
- finding import, edit, close, accept-risk, and retest annotation;
- report generation, AI draft acceptance, and delivery approval;
- external handoffs to GitHub, Slack, email, Jira, Linear, SIEM, or support;
- signing, verification, manifest export, and audit-log inspection;
- retention policy changes and backup/restore actions;
- support access and break-glass access.

## Audit Requirements

SSO/RBAC events must be audit logged with actor, role, target, action, timestamp,
result, and request source where available. Required events include:

- login, logout, session refresh, and failed login;
- identity-provider configuration changes;
- role or membership changes;
- permission-denied outcomes;
- export, handoff, support access, and retention policy changes.

## Deferral Rule

Do not create implementation issues until
[`enterprise-demand-gate.md`](enterprise-demand-gate.md) is satisfied. Any future
SSO/RBAC implementation must start with a threat model that covers account
takeover, tenant isolation, audit integrity, support access, and local artifact
export.
