# Deferred Adapter Expansion

Date: 2026-05-20

Status: parked behind real authorized fixture evidence.

## Decision

Phase 1.2 adapter work should treat host posture and long-tail offensive tool
outputs as optional evidence imports, not as a return to pre-pivot host scanning.
No candidate below is accepted for implementation until a real export fixture,
authorization note, and sanitization record exist.

## Candidate Ranking

| Rank | Candidate | Current decision | Evidence needed |
| ---: | --- | --- | --- |
| 1 | BloodHound collection export | likely first long-tail offensive import | Sanitized JSON/ZIP export from an authorized lab, with object and relationship coverage notes. |
| 2 | CrackMapExec or NetExec output | candidate | Real JSON/log export showing hosts, protocols, and credential-status evidence without secrets. |
| 3 | Trivy filesystem or container JSON | candidate | Real JSON export where package/CVE evidence improves a red-team or assessment report. |
| 4 | Lynis report output | candidate | Real host hardening output where the report needs selected posture findings. |
| 5 | osquery JSON results | candidate | Small authorized table-export bundle with clear query provenance. |
| 6 | OpenSCAP ARF/XML | defer | Useful for compliance posture, but risks pulling Piranesi toward compliance-engine scope. |

## Implementation Gate

Each accepted adapter must get its own GitHub issue and must include:

- real exported tool output, never a hand-authored support fixture;
- authorization and target-context notes;
- sanitization notes and secret-scan confirmation;
- raw evidence preservation under `raw/<tool>/`;
- lossy-field warnings instead of silent omission;
- stable finding or evidence identifiers;
- redaction behavior for hosts, users, credentials, tokens, payloads, and client
  names;
- documentation that the adapter imports evidence only.

## Out Of Scope

- Live SSH probing or fleet scanning.
- Active exploitation, payload generation, credential validation, or target
  interaction.
- Reintroducing legacy host-posture workflows wholesale.
- Claiming adapter support from synthetic fixtures.

## Closure Criteria

This Phase 1.2 planning item is complete when the candidate list, evidence gate,
and non-goals are documented. Future implementation work must start from a
separate adapter issue with real fixtures attached or referenced.

