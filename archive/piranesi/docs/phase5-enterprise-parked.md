# Phase 5 Enterprise Parked Closeout

Date: 2026-05-20

Status: parked; requirements captured, implementation deferred.

Phase 5 does not ship enterprise functionality. It defines the gates and parked
requirements needed before future enterprise implementation can be responsibly
planned.

Captured planning artifacts:

- [`enterprise-demand-gate.md`](enterprise-demand-gate.md): customer evidence,
  ownership, and threat-model gate before enterprise work starts.
- [`enterprise-sso-rbac-requirements.md`](enterprise-sso-rbac-requirements.md):
  OIDC-first identity direction, candidate roles, permission boundaries, and
  audit requirements.
- [`enterprise-deployment-data-control-requirements.md`](enterprise-deployment-data-control-requirements.md):
  on-prem, Helm, air-gapped, residency, backup/restore, encryption, retention,
  and chain-of-custody constraints.
- [`enterprise-siem-support-requirements.md`](enterprise-siem-support-requirements.md):
  future SIEM event scope, support bundle redaction, customer-managed retention,
  and approval gates.

Current non-capabilities:

- no hosted SaaS or client portal;
- no multi-tenancy or organization isolation;
- no SSO, SAML, OIDC login, SCIM, RBAC, or roles;
- no on-prem package, Helm chart, air-gapped deployment, or managed backup;
- no SIEM export or automated support bundle generation;
- no SOC 2, ISO, SLA, residency, uptime, or operational compliance claim.

Issue #43 can close once #81 through #84 are closed. Future enterprise work should
open new implementation issues only after the demand gate is satisfied and a
specific deployment/identity/data-control shape is known.
