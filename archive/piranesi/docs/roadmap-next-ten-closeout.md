# Roadmap Next-Ten Closeout

Date: 2026-05-20

Status: second queue implemented; trackers ready to close after verification and push.

The first next-ten queue was handled as individual rollback-friendly commits:

| Issue | Outcome |
| ---: | --- |
| #92 | AI dedupe, severity-rationale, and retest-checklist suggestion mode implemented. |
| #44 | Phase 6 AI co-pilot closeout documented. |
| #42 | Phase 4 PFF platform closeout documented. |
| #41 | Phase 3 workflow closeout documented. |
| #81 | Enterprise demand and threat-model gate documented. |
| #82 | Parked SSO/RBAC requirements documented. |
| #83 | Parked deployment, residency, retention, and data-control requirements documented. |
| #84 | Parked SIEM and support bundle requirements documented. |
| #43 | Phase 5 enterprise umbrella parked-state closeout documented. |
| #125 | This tracker closeout note records completion of the queue. |

After CI-equivalent local verification passes and `main` is pushed, the GitHub
issues can be closed. Future roadmap queues should be opened only when there is a
new actionable ordering decision to preserve.

## 2026-05-20 Follow-Up Queue

The follow-up queue from #125 was turned into explicit GitHub issues and handled
as separate rollback-friendly commits:

| Issue | Outcome |
| ---: | --- |
| #126 | Adapter intake gate documented and tested. |
| #127 | BloodHound import parked behind sanitized authorized collection exports. |
| #128 | NetExec and CrackMapExec import parked behind redacted real output fixtures. |
| #129 | Trivy import parked behind real JSON evidence and report-value proof. |
| #130 | Local web app health route added for smoke checks. |
| #131 | Email handoff draft now writes a local delivery manifest. |
| #132 | External install validation checklist documented and tested. |
| #133 | Known limitations review metadata refreshed and tested. |
| #134 | Enterprise demand intake template documented and tested. |
| #135 | This closeout section records completion of the follow-up queue. |

The adapter and enterprise items that lacked real fixtures or customer evidence
were resolved by adding explicit gates, not by pretending unsupported features exist. Future implementation work should open a new issue only when the relevant gate has concrete evidence.
