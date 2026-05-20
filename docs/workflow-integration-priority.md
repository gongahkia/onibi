# Workflow Integration Priority

Date: 2026-05-20

Status: accepted until design-partner evidence changes.

## Decision

Piranesi should keep GitHub Issues, Slack, and email as the first validated
integration paths. Linear is the next ticketing candidate, and Jira remains behind
Linear until a design partner demonstrates that Jira is required for delivery.

## Current Ranking

| Rank | Integration | Decision | Reason |
| ---: | --- | --- | --- |
| 1 | GitHub Issues | implemented | Useful for engineering-led remediation handoff and already has a one-way export threat model. |
| 2 | Slack | implemented | Useful for lightweight workflow notifications without moving evidence out of the workspace. |
| 3 | Email | implemented | Matches consultant delivery workflows and stays local by generating a draft instead of sending. |
| 4 | Linear | next candidate | Lower setup friction than Jira, strong fit for smaller product/security teams, and simpler issue payload shape. |
| 5 | Jira | defer | Common in larger enterprises, but higher configuration, auth, field-mapping, and data-leakage risk. |

## Evidence Required Before Linear

Create Linear implementation issues only after at least one of these is true:

- a design partner asks for Linear handoff for real remediation workflows;
- GitHub Issues export is too engineering-repository-specific for an active user;
- local email handoff is insufficient because a team needs issue ownership,
  status, and assignment in Linear.

Linear implementation must start with a threat model before API code. The threat
model must cover token handling, workspace/user/team identifiers, rate limits,
field mapping, dry-run behavior, and redaction defaults.

## Evidence Required Before Jira

Create Jira implementation issues only after at least one of these is true:

- an enterprise design partner requires Jira for report delivery or retest work;
- the partner can provide safe sample project configuration and field mappings;
- Linear or GitHub Issues cannot represent the required remediation workflow.

Jira implementation must wait for a concrete project shape. The first Jira issue
should document Cloud versus Data Center, authentication mode, required custom
fields, attachment policy, rate limits, and how assets are redacted by default.

## Non-Goals

- No bidirectional ticket sync.
- No raw evidence upload or attachment by default.
- No automatic finding status changes from external ticket state.
- No implementation issue without a separate threat model and dry-run contract.

