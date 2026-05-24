# Attack-Path Evidence Import

Date: 2026-05-20

Status: planned, gated by real authorized fixtures.

## Goal

Turn real graph and operator-tool exports into objective-focused attack-path
evidence. The output should explain how an objective became reachable and which
control gaps enabled the path.

## Approach

Build this only after a candidate adapter passes the fixture gate. Likely inputs
include BloodHound, NetExec, CrackMapExec, or similar operator exports. Piranesi
should preserve raw evidence, normalize relationship facts, and allow operators
to build attack-path narratives linked to objectives.

This should not become live enumeration or pathfinding against a target.

## Proposed Data Shape

An attack path should include:

- stable path ID;
- objective ID;
- source principal or starting condition;
- target asset, role, or objective;
- ordered path steps;
- linked evidence IDs per step;
- linked procedure IDs;
- ATT&CK technique IDs where applicable;
- control gaps;
- blocked or prevented steps;
- confidence and reviewer notes.

## User Flow

1. Operator imports real gated path-related evidence.
2. Piranesi preserves raw export and extracts conservative relationship facts.
3. Operator selects facts and links them into an attack path.
4. Report output explains the path and the control gaps.
5. Purple-team handoff maps each step to detection opportunities.

## Build Slices

1. Define attack-path model independent of any one adapter.
2. Add manual attack-path creation from existing evidence.
3. Add BloodHound import only after real sanitized fixture evidence exists.
4. Add NetExec/CrackMapExec support only after real fixtures exist.
5. Render attack paths in reports and handoff packs.

## Acceptance Criteria

- Manual attack paths can be created without any new adapter.
- Imported relationships preserve source digests and locators.
- Path steps require evidence links.
- Reports distinguish proven, inferred, and blocked path steps.
- Parser support is not claimed from synthetic fixtures.

## Non-Goals

- Running BloodHound, NetExec, CrackMapExec, or collectors.
- Live target interaction.
- Credential validation.
- Automated exploitation recommendations.
- Claiming complete graph coverage.
