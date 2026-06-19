# Kelp Pi Field Guide

## Identity and Purpose

Kelp Pi is a Raspberry Pi field data plane for scoped AppSec triage. It runs on-site beside a target, keeps evidence local, and exports signed audit bundles for reviewer handoff. The appliance is not a covert implant, not a general Wi-Fi attack toy, and not an internet-scale scanner. Its primary job is to preserve a reproducible chain from declared scope to scanner evidence, local retrieval, policy decisions, and final bundle verification.

## Scope Declaration

Every engagement starts with a signed scope declaration. The declaration names allowed CIDR ranges, hostnames, ports, and an expiration window. Scanner requests outside that scope are refused before any scanner process starts. Scope decisions are written to the audit log with the requested target, matched rule, and reason so a reviewer can reconstruct why an action was allowed or refused.

## Network Perimeter

The Pi defaults to a local operator access point and loopback services. Outbound network access is denied unless a control-plane endpoint appears in the configured allowlist. Captive-portal DNS names resolve to the local portal instead of upstream DNS. Client isolation prevents devices joined to the Pi access point from talking directly to each other.

## Retrieval and Ask

The local corpus is chunked deterministically, indexed in SQLite FTS5, and queried through `/ask`. Responses contain citations with path, heading path, byte range, and chunk ID. When retrieval has no useful match, the response is `no_answer` and contains no citations or generated prose. Default answers are extractive and citation-only.

## Policy Gates

Policy gates run before file mutation, outbound network access, and active scanner invocation. Active scanner invocations require operator approval. Mutating file operations require review. Outbound destinations outside the allowlist are denied. Dry-run mode records the decision that would have applied while returning a non-blocking review result.

## Evidence Bundle

A Pi-produced bundle mirrors the laptop evidence bundle layout. It includes an `audit-bundle/index.html`, `manifest.json`, signature material, attestation material, SARIF findings, normalized evidence rows, policy decisions, and the relevant hash-chained audit log slice. Verification must work on a laptop with no Pi-specific flags.

## Field Operations

Selfcheck reports data-directory health, stale index state, disk space, listening services, audit-log verification, and platform signals when available. Operators use recovery mode when a data directory is corrupted. Decommissioning wipes engagement data and forces a new bootstrap before the agent will start another run.

## Non-Goals

Kelp Pi does not promise legal admissibility by itself, does not replace independent evidence retention, and does not bypass target authorization. It does not execute exploit payloads by default, does not perform rogue access-point attacks, and does not synthesize answers unless an explicit synthesis mode and policy decision allow it.
