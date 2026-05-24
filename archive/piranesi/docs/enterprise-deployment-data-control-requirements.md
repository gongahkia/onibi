# Enterprise Deployment And Data-Control Requirements

Date: 2026-05-20

Status: parked future work behind the enterprise demand gate.

Piranesi does not currently provide hosted SaaS, Helm charts, on-prem packages,
air-gapped deployment, managed backup, or enterprise retention controls. These
requirements capture future planning boundaries only.

## Deployment Models To Decide

A future enterprise plan must choose one supported deployment model before
implementation:

- hosted multi-tenant SaaS;
- hosted single-tenant environment;
- customer-managed on-prem deployment;
- Kubernetes/Helm deployment;
- air-gapped deployment;
- hybrid local workspace plus hosted coordination service.

The plan must document operator responsibilities, upgrade path, incident response,
backup ownership, log access, and support boundaries for the selected model.

## On-Prem And Air-Gapped Constraints

Future on-prem or air-gapped work must define:

- installation artifact format and signing;
- offline license or entitlement behavior if any;
- offline documentation and schema availability;
- image/package provenance and vulnerability update process;
- no required external model, telemetry, support, or update calls;
- deterministic export/import path for local workspaces and signed artifacts;
- disaster recovery procedure without vendor access.

## Data Residency

An implementation plan must identify where each data class lives:

- workspace metadata;
- raw evidence and operator artifacts;
- normalized findings;
- AI prompts, traces, drafts, and suggestions;
- report artifacts and handoff archives;
- audit logs and chain-of-custody manifests;
- provider credentials, webhook URLs, and integration tokens;
- support bundles and diagnostic logs.

The plan must also define whether cross-region replication, support access, or
external model calls are allowed, and how operators can prove where data was
stored.

## Backup, Restore, And Retention

Future enterprise controls must define:

- backup frequency, scope, encryption, and restore testing;
- retention periods per data class;
- legal hold behavior;
- deletion workflow and audit record;
- export-before-delete option for local artifact ownership;
- signed chain-of-custody preservation across backup and restore;
- retention behavior for AI traces and external handoff history.

## Encryption And Key Ownership

Future work must distinguish:

- local filesystem encryption outside Piranesi;
- application-level encryption for hosted storage;
- customer-managed keys;
- key rotation;
- backup key handling;
- failure behavior when keys are unavailable.

## Deferral Rule

Do not create implementation issues until
[`enterprise-demand-gate.md`](enterprise-demand-gate.md) is satisfied. Any future
deployment issue must name the deployment model, data residency requirement,
backup/restore owner, retention model, and chain-of-custody preservation strategy.
