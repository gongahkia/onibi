# BloodHound Import Gate

Date: 2026-05-20

Status: parked behind sanitized authorized collection exports.

BloodHound is the highest-ranked long-tail offensive adapter candidate, but
Piranesi must not claim support until a real authorized export is available.
The adapter would import collection evidence for local reporting and handoff; it
would not run collectors, query directory services, validate credentials, or
operate against a live domain.

## Accepted Evidence

An implementation issue may start only with one of these sanitized exports:

- BloodHound CE JSON files produced by an authorized lab collection;
- SharpHound ZIP output with JSON members preserved in the original archive;
- a documented subset export that still contains node and edge coverage notes.

The intake record must identify collector version, BloodHound version if known,
collection method, target lab scope, sanitization steps, fixture digests, and
secret-scan results.

## Mapping Expectations

The first adapter should preserve raw collection evidence and normalize only
report-relevant observations. Expected mappings include:

- users, computers, groups, domains, and high-value nodes as evidence context;
- privilege, session, membership, delegation, and control edges as relationship
  evidence;
- path-style findings only when the export itself supports the relationship and
  the report wording can avoid overstating exploitability;
- warnings for unsupported node or edge types.

## Redaction Requirements

Fixtures and reports must redact or replace real domains, hostnames, usernames,
SIDs where sensitive, object IDs, descriptions, and free-text notes. Relationship shape should be preserved so parser behavior remains testable after sanitization.

## Out of Scope

- Running SharpHound or any collector.
- LDAP, SMB, WinRM, Kerberos, or Graph API interaction.
- Credential validation, password spraying, or target reachability checks.
- Automated attack-path recommendations without operator approval.
