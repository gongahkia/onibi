# Enterprise Demand Intake Template

Date: 2026-05-20

Status: template only; enterprise implementation remains parked.

Use this template when a design partner or customer asks for enterprise
deployment, identity, support, SIEM, or data-control features. Do not open implementation issues until the demand gate in [`enterprise-demand-gate.md`](enterprise-demand-gate.md) is satisfied.

## Request Summary

- Customer or design partner:
- Request owner:
- Date received:
- Requested deployment model:
- Current local-first workflow that fails:
- Why signed local exports or one-way handoff are insufficient:

## Deployment Shape

Choose one and describe operational ownership:

- hosted SaaS;
- single-tenant hosted;
- customer-managed on-prem;
- air-gapped;
- hybrid.

Required details:

- tenant/project isolation expectations;
- network boundaries and allowed egress;
- backup, restore, and disaster-recovery expectations;
- support ownership and incident-response owner;
- upgrade and rollback expectations.

## Identity and Access

- Identity provider:
- Required protocol: OIDC, SAML, SCIM, local users, or other.
- Required roles:
- Permission boundaries:
- Session lifetime and device constraints:
- Audit events required for access decisions:

## Data Control

- Data residency:
- Retention period:
- Deletion requirements:
- Encryption requirements:
- Customer-managed key requirements:
- Raw evidence export requirements:
- Chain-of-custody requirements:

## Support and Integrations

- SIEM destination and event types:
- Ticketing or notification destination:
- Support bundle contents:
- Redaction rules:
- Approval flow before support access or export:

## Threat Model Checklist

Before code begins, document:

- trust boundaries;
- abuse cases;
- administrator and support-user privileges;
- break-glass access behavior;
- audit-log tamper resistance;
- evidence exportability;
- failure modes that could leak client evidence.

## Decision

- Gate decision: pass, park, or reject.
- Decision date:
- Decider:
- Follow-up GitHub issues:
- Reason implementation is safe to start, or reason it remains parked:
