# Governed Web Intelligence

KelpClaw can run web search and browsing providers behind the same policy, audit, replay, and governance surface used for SKILL.md runs.

## CLI

```console
$ kelp-claw web search "Singapore agentic AI governance" --provider exa --policy sg-web-research --out .kelpclaw/web-evidence/sg-ai
$ kelp-claw web fetch https://example.com/source --provider tinyfish --policy web-search-safe --out .kelpclaw/web-evidence/source
$ kelp-claw web answer "What changed in MAS AI governance?" --provider exa --policy sg-web-research
$ kelp-claw web research "APAC agentic AI controls" --providers exa,tinyfish --policy sg-web-research
```

Set `EXA_API_KEY` for Exa and `TINYFISH_API_KEY` for TinyFish. The command evaluates policy before making the provider call. If the selected pack returns `deny` or `require-approval`, KelpClaw blocks the call.

## Evidence Files

`--out <dir>` writes a portable evidence set:

- `web-evidence.json`: normalized request, events, sources, hashes, and summary.
- `web-events.jsonl`: JSONL event trail for replay and audit.
- `web-bom.json`: source and event bill of materials.
- `web-evidence.html`: static review page.

Attach evidence to other reports with:

```console
$ kelp-claw governance report ./SKILL.md --include-web-evidence .kelpclaw/web-evidence/sg-ai
$ kelp-claw export-audit-bundle <runId> --include-web-evidence .kelpclaw/web-evidence/sg-ai --include-governance
```

## Policy Packs

- `web-search-safe`: default web research pack; gates full-content storage and browser/agent escalation.
- `sg-web-research`: Singapore-oriented pack for PDPA, finance, MAS, CPF, IRAS, and regulated-source research.
- `browser-automation-strict`: approval-first TinyFish browser/web-agent pack with denials for login, payment, credential, and account flows.

## MCP Gateway

```console
$ kelp-claw mcp web-gateway --policy sg-web-research
$ kelp-claw mcp web-gateway --policy browser-automation-strict --allow-browser-tools
```

The MCP gateway exposes `kelp.web_search`, `kelp.web_fetch`, `kelp.web_answer`, and `kelp.web_research` by default. `kelp.web_browser_session`, `kelp.web_browser_action`, and `kelp.web_agent_task` require `--allow-browser-tools`.
