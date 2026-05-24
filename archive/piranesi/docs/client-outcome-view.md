# Client Outcome View

Date: 2026-05-20

Status: planned supplementary feature.

## Goal

Create a client-facing outcome view that summarizes what the engagement proved,
what was prevented, what gaps remain, and what should be prioritized next. This
should be understandable without reading raw tool output.

## Approach

Build an outcome model and renderer on top of existing objectives, findings,
timeline, evidence, detection opportunities, measurable events, and debrief
items. The view should be short, defensible, and linked to evidence.

The client outcome view should not replace detailed technical appendices.

## Proposed Sections

- engagement objectives and status;
- business-relevant impact statements;
- confirmed paths or blocked paths to objectives;
- top control gaps;
- strengths observed;
- detection and response outcomes;
- prioritized remediation themes;
- retest plan;
- evidence and report artifact references.

## User Flow

1. Operator completes technical records.
2. Operator marks client-safe debrief items and outcome notes.
3. Piranesi generates an outcome view as Markdown, PDF, and archive content.
4. Client receives a concise outcome summary with evidence-backed detail.

## Build Slices

1. Add outcome summary model.
2. Add renderer for Markdown and PDF.
3. Link outcome sections to objectives and findings.
4. Add report QA checks for unsupported claims.
5. Add local web UI preview.

## Acceptance Criteria

- Outcome view can be generated from an existing workspace.
- Every impact claim can link to evidence, findings, objectives, or timeline.
- Strengths and blocked paths can be represented, not only failures.
- Output excludes internal-only debrief notes.
- Tests cover sparse and populated workspaces.

## Non-Goals

- Marketing-style dashboards.
- Unsupported compliance claims.
- Replacing detailed technical report sections.
- Auto-generating business impact without operator approval.
