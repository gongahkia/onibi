# Solo Engagement Management

Piranesi models engagement management for a solo consultant or solo internal security operator.
The workspace owns local client/project metadata, scope, milestones, retest rounds, and delivery
state without introducing hosted teams, organizations, RBAC, or SaaS assumptions.

Engagement metadata lives in `workspace.json`:

```json
{
  "engagement": {
    "client": "Example Client",
    "project": "Loopback Lab",
    "scope": ["127.0.0.1"],
    "assessment_type": "web",
    "owner": "operator@example.test",
    "milestones": [
      {
        "id": "milestone:kickoff",
        "title": "Kickoff",
        "status": "complete",
        "due_date": "2026-05-20",
        "notes": "Scope confirmed."
      }
    ],
    "retest_rounds": [
      {
        "id": "retest:round-1",
        "title": "Round 1",
        "status": "planned",
        "baseline_workspace": "workspace-before",
        "current_workspace": null,
        "notes": null
      }
    ],
    "delivery": {
      "status": "draft",
      "reviewer": null,
      "reviewer_notes": [],
      "delivered_at": null
    }
  }
}
```

Existing Phase 1 workspaces continue to load because the new fields have defaults. Exported
reports and archives remain local artifacts, and external workflow integrations should map to this
schema instead of adding organization or team concepts to the core workspace.
