# FindEvil Submission Runbook — KelpClaw SIFT Sentinel

Everything remaining between "code is done" and "submitted on Devpost."
Ordered by risk: the live run first (depends on external tooling), then the
artifacts that build on it.

**Deadline: 16 Jun 2026, 11:45am GMT+8.** Build/tests already green
(235 passing). Three priority fixes already landed (toolUseId threading,
hallucination metric, realpath containment).

**Eight required components — missing any one = elimination:**

- [ ] 1. Code repo (public, MIT/Apache)
- [ ] 2. Demo video (5 min, live terminal + self-correction shown)
- [ ] 3. Architecture diagram (patterns + trust boundaries)
- [ ] 4. Written project description (Devpost format)
- [ ] 5. Dataset documentation
- [ ] 6. Accuracy report (incl. evidence-integrity section)
- [ ] 7. Try-it-out instructions
- [ ] 8. Agent execution logs (traceable findings)

---

## Phase 0 — Pre-flight on the Fedora ThinkPad

You're on native x86-64 Linux, so no architecture friction. You still run
SIFT in an Ubuntu VM (SIFT's Cast/SaltStack installer targets Ubuntu, not
Fedora), but kelp-claw runs wherever the evidence is locally readable and
SIFT tools are on PATH. Per the repo: the CFReDS path uses `ewfmount` to
expose `ewf1`, then runs `mmls/fsstat/tsk_recover` against that raw view —
so kelp-claw needs Sleuth Kit + libewf available to its process.

**Decision — where does kelp-claw run?**
Simplest reliable setup: **run kelp-claw INSIDE the SIFT VM.** SIFT already
ships Sleuth Kit + libewf + ewfmount, the evidence is local, and you avoid
mount-sharing headaches. Install Node 20 + pnpm + the repo inside the VM.

### 0.1 Enable virtualization (Fedora host)

```bash
# Check VT-x/AMD-V is on (should print a number > 0)
egrep -c '(vmx|svm)' /proc/cpuinfo

# Install KVM + virt-manager (lower friction on Fedora than VirtualBox)
sudo dnf install -y @virtualization
sudo systemctl enable --now libvirtd
sudo usermod -aG libvirt $(whoami)
# log out / back in so the group takes effect
```

If `egrep -c` prints `0`, enable virtualization in BIOS/UEFI (ThinkPad:
Enter on boot → Security → Virtualization → enable VT-x/VT-d).

### 0.2 Get the SIFT VM

```bash
# Download the SIFT OVA from SANS (requires free SANS Portal login):
#   https://www.sans.org/tools/sift-workstation  → "Login to Download"
# Save the .ova to ~/Downloads/

# Import into virt-manager GUI:
#   File → Import existing disk image / or import the OVA
# Recommended VM specs: 4 vCPU, 8 GB RAM, 60+ GB disk
# SIFT default creds: sansforensics / forensics
```

Boot it, confirm network access (`ping -c1 github.com`), then snapshot the
clean VM (virt-manager → Snapshots → Take Snapshot, name it `clean-sift`)
so you can roll back if an install step goes sideways.

### 0.3 Install kelp-claw inside the SIFT VM

```bash
# Inside the SIFT VM terminal:
# Node 20 via nvm (SIFT's system node may be too old)
curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.7/install.sh | bash
source ~/.bashrc
nvm install 20 && nvm use 20

corepack enable

git clone <YOUR_REPO_URL> kelp-claw
cd kelp-claw
pnpm install --frozen-lockfile
pnpm -r build
pnpm -r test    # confirm 235 pass here too, in the VM
```

If `pnpm install` hits a network-allowlist wall, that's a VM networking
issue — confirm the VM has open egress, not the host.

---

## Phase 1 — Live CFReDS triage run

Goal: one real run against the CFReDS Hacking Case image, producing
toolUseId-traceable logs. Run everything below **inside the SIFT VM**, from
the repo root.

### 1.1 Fetch the dataset

```bash
node scripts/fetch-cfreds-hacking-case.mjs
# Pulls the CFReDS Hacking Case image into .kelpclaw/datasets/cfreds/hacking-case
```

### 1.2 Seed the extractor cache (offline-friendly)

The deterministic run refuses fresh LLM calls, so seed the cache first with
a non-deterministic run that emits `claim_extracted` events. Because the
CFReDS triage command emits claims, **this can seed without API creds.**

```bash
# Non-deterministic seeding run — same --out as the real run so cache lands right
node scripts/run-cfreds-hacking-case-triage.mjs \
  --dataset .kelpclaw/datasets/cfreds/hacking-case \
  --out .kelpclaw/findevil/cfreds-hacking-case/triage
```

This populates `$outDir/.extractor-cache/<sha256(reportText)>.json`
(a parsed ClaimLedger). If it instead demands a model, it means the command
isn't emitting claims in your environment — set `ANTHROPIC_API_KEY` and rerun
the seeding run once, then proceed; the deterministic run below still won't
call out.

### 1.3 The real run

```bash
node packages/cli/dist/index.js findevil sentinel \
  --case examples/findevil-cfreds-hacking-case/case.yml \
  --evidence-root .kelpclaw/findevil/cfreds-hacking-case/triage/evidence \
  --sift-command "node scripts/run-cfreds-hacking-case-triage.mjs --dataset .kelpclaw/datasets/cfreds/hacking-case --out .kelpclaw/findevil/cfreds-hacking-case/triage --mode emit-trace" \
  --max-iterations 3 \
  --repair-runner evidence-linked \
  --timestamp skip \
  --out .kelpclaw/findevil/cfreds-hacking-case/sentinel
```

Flag rationale:
- `--repair-runner evidence-linked` — no `claude -p` call, fully local, and
  required if you want determinism. Use `claude-code` only if you
  deliberately want to demo live-Claude repair (needs Claude CLI installed).
- `--timestamp skip` — offline; no RFC3161 TSA network call. Switch to
  `live` only for a final "real timestamp" run if you want one in the bundle.
- `--max-iterations 3` — the cap the repair loop honors; this is your
  self-correction budget for the demo.

**For a clean deterministic re-run** (after the cache is seeded), add
`--deterministic`. Do this for the *recorded* run so it's reproducible:

```bash
node packages/cli/dist/index.js findevil sentinel \
  --case examples/findevil-cfreds-hacking-case/case.yml \
  --evidence-root .kelpclaw/findevil/cfreds-hacking-case/triage/evidence \
  --sift-command "node scripts/run-cfreds-hacking-case-triage.mjs --dataset .kelpclaw/datasets/cfreds/hacking-case --out .kelpclaw/findevil/cfreds-hacking-case/triage --mode emit-trace" \
  --max-iterations 3 \
  --repair-runner evidence-linked \
  --timestamp skip \
  --deterministic \
  --out .kelpclaw/findevil/cfreds-hacking-case/sentinel
```

### 1.4 Confirm it worked

The CLI prints JSON with the output paths. Confirm:

```bash
cd .kelpclaw/findevil/cfreds-hacking-case/sentinel
ls -la

# verify the audit bundle
node ../../../../packages/cli/dist/index.js findevil verify-audit-bundle \
  --bundle ./audit-bundle    # adjust path to whatever the CLI printed
# expect: "ok": true, files checked, failed: []
```

---

## Phase 2 — Capture logs for the accuracy report & audit trail (components 6 & 8)

These files are your evidence. Capture and archive them; don't regenerate
later.

### 2.1 The file set to preserve

From the run's `--out` directory, the key files (per repo):

```
agent-execution.jsonl        # normalized tool events WITH toolUseId — component 8
claim-ledger.json            # claims + evidenceRefs[].toolUseId/toolName — the traceability win
repair-trace.jsonl           # self-correction iterations — the tiebreaker evidence
taint-ledger.jsonl
firewall-events.jsonl
spoliation-check.json        # evidence integrity before/after hashes
evidence-manifest.json
accuracy-report.md           # auto-generated starting point for component 6
attack-navigator-layer.json
committee-vote.jsonl
audit-bundle/                # signed bundle: manifest.json, manifest.sig, attestation.json
```

### 2.2 Prove the traceability claim (this is your headline)

Pick one finding and walk it end-to-end. This exact demonstration goes in
both the demo video and the accuracy report:

```bash
# 1. Pick a high/critical claim and read its evidence ref
jq '.claims[] | select(.severity=="high" or .severity=="critical")
    | {claim: .text, status: .status, refs: .evidenceRefs}' claim-ledger.json | head -40

# 2. Take a toolUseId from that claim's evidenceRefs and find the producing tool call
jq 'select(.toolUseId=="<PASTE_toolUseId_HERE>")' agent-execution.jsonl
```

If step 2 returns the `mmls`/`fls`/`icat` (or recover) call that produced the
evidence, you have a literal claim-to-tool-execution graph — exactly judging
criterion #5. Screenshot/record this.

### 2.3 Archive the run

```bash
# from repo root, inside the VM
tar czf cfreds-run-$(date +%Y%m%d).tar.gz \
  .kelpclaw/findevil/cfreds-hacking-case/sentinel
# copy this out of the VM to the host (shared folder or scp) and keep it
```

### 2.4 Finish the accuracy report (component 6)

Start from the auto-generated `accuracy-report.md`, then layer in (your
existing `CHANGES.md` already drafts most of the integrity section):

- Headline metrics from the DFIR-Metric benchmark: precision / recall / F1,
  TP/FP/FN, and the **new hallucination rate** (define it: confirmed claim
  with no ground-truth support; inferred claims excluded).
- Evidence-integrity section: realpath containment (architectural), command
  allowlist + typed MCP (architectural), spoliation hashing + firewall taint
  scan (**detective / post-exec, not a runtime sandbox** — state this plainly).
- **Documented residual gaps** — the rules reward this as signal, not weakness:
  - Firewall classification runs after `runAgent()` returns; not a hard
    pre-exec blocker.
  - `checkReadOnlyMount()` exists but is not wired into the live path.
  - `uncorrectedPolicyDenials` hardcoded to 0.
  - Live SIFT verified against CFReDS Hacking Case specifically (cite the run).

---

## Phase 3 — Architecture diagram (component 3)

The rules demand prompt-based vs. architectural guardrails be "clearly
distinguished." You're one of the few entrants who can honestly draw that
line — make it the centerpiece.

### What the diagram must show

- **Components:** Sentinel orchestrator → `--sift-command` runner (shell-exec
  + hook env) AND the typed MCP server (separate path) → SIFT tools
  (mmls/fls/icat via ewfmount→raw view) → linker/claim-extraction →
  ClaimLedger → repair loop → audit bundle.
- **Data sources:** CFReDS image → ewfmount → evidenceRoot.
- **Trust boundaries, color-coded into two classes:**
  - **Architectural (hard):** typed MCP tools (no `execute_shell`), command
    allowlist (`mmls|fls|icat` only, `shell:false`, args array, runtime/output
    caps), realpath containment on evidenceRoot.
  - **Detective / post-exec (soft):** spoliation before/after hashing,
    firewall taint scan over normalized events.
- **Which architectural pattern** you're claiming. You're primarily
  **#2 Custom MCP Server** (typed, allowlisted) layered with
  **#1 Direct Agent Extension** (Sentinel's agent loop + repair). Say so.

### How to make it

Quickest route that looks clean: draw in **Excalidraw** or **draw.io**
(both free, browser). Export PNG + the source file into the repo at
`docs/architecture.png` and `docs/architecture.excalidraw`. Reference it in
the README and the Devpost writeup.

If you'd rather generate it from text, Mermaid works and renders on GitHub:
```
docs/architecture.mmd   # commit the source
```
Tell me if you want a Mermaid draft — I can write the full graph from the
component list above.

---

## Phase 4 — Demo video (component 2)

5 min max, live terminal, audio narration, must show the agent on **real
case data** with **at least one self-correction sequence.**

### Recording setup (Fedora host or VM)

```bash
# OBS Studio is the reliable choice
sudo dnf install -y obs-studio
# Record the SIFT VM window + mic. 1080p, show the terminal large enough to read.
```

### Script (target ~4:30, leave buffer under the 5:00 cap)

1. **0:00–0:30 — The problem.** One line: AI adversaries reach domain control
   in minutes; defenders are still typing commands. KelpClaw closes that gap
   with a *constrained* autonomous DFIR agent.
2. **0:30–1:15 — Kick off the live run.** Show the `findevil sentinel` command
   against the CFReDS image. Narrate the two-path architecture (typed MCP +
   allowlisted SIFT tools).
3. **1:15–2:30 — Self-correction live.** Open `repair-trace.jsonl` (or show it
   streaming): a high/critical claim flagged `unsupported`/`contradicted`,
   the loop re-verifying, the corrected result. **This is the tiebreaker
   criterion — give it the most time.**
4. **2:30–3:30 — Traceability.** Run the two `jq` commands from Phase 2.2 live:
   pick a finding → jump to the exact `toolUseId` tool call that produced it.
   "Any finding traces to the specific tool execution."
5. **3:30–4:15 — Integrity + accuracy.** Show `spoliation-check.json`
   (before/after hashes unchanged), then the benchmark hallucination-rate
   metric. Be honest: architectural guardrails here, detective controls there.
6. **4:15–4:30 — Close.** Open-source, MIT/Apache, every submission becomes a
   community tool. Repo link.

Record 2–3 takes of the run beforehand so you have a clean one; you can do
the narration over the best take.

---

## Phase 5 — Written project description (component 4) + dataset docs (component 5)

### Devpost writeup (component 4)

Use the required format. Pull straight from `CHANGES.md` and the accuracy
report:

- **What it does:** Constrained autonomous DFIR triage on the SIFT
  Workstation. Typed read-only MCP tools, claim extraction with evidence
  linking, a verify→repair loop, spoliation/firewall detective controls,
  ATT&CK Navigator export, deterministic replay, RFC3161 timestamping, signed
  audit bundles.
- **How you built it:** Node 20 + pnpm 10 TS ESM monorepo; packages: `cli`,
  `findevil` (Sentinel/MCP/verifier/linkers/firewall/benchmark), `evidence`,
  `workflow-spec`, plus governance foundation. Sentinel orchestration in
  `findevil/src/sentinel`; typed MCP in `findevil/src/mcp/server.ts`.
- **Challenges:** Be specific — lexical vs. realpath containment (and how you
  hardened it), seeding the deterministic extractor cache, keeping firewall
  honest as a detective control rather than overselling it as a sandbox.
- **What you learned + What's next:** Wiring `checkReadOnlyMount()` into the
  live path; making firewall a true pre-exec blocker; per-tool-call claim
  graph. These are honest roadmap items, not apologies.
- **Which qualities of autonomous execution you address:** self-correction
  (repair loop), accuracy (hallucination metric), auditability (toolUseId
  graph), constraint enforcement (typed MCP + realpath).

### Dataset documentation (component 5)

- **Source:** CFReDS Hacking Case (cite the NIST/CFReDS origin and the
  `fetch-cfreds-hacking-case.mjs` script).
- **What the agent was tested against:** the disk image, exposed via
  `ewfmount` → raw view → `mmls/fsstat/tsk_recover`.
- **What it found:** summarize confirmed findings from `claim-ledger.json`
  and the benchmark scores.
- **Reproducibility:** the exact deterministic command from Phase 1.3 — this
  is what makes the run repeatable for judges.

---

## Phase 6 — Try-it-out instructions (component 7) + final repo check (component 1)

### Try-it-out (component 7)

Put a `JUDGES.md` (or a README section) with the **deterministic** path so a
judge reproduces your run exactly:

```bash
corepack enable
pnpm install --frozen-lockfile
pnpm -r build
node scripts/fetch-cfreds-hacking-case.mjs
# seed cache (offline if command emits claims):
node scripts/run-cfreds-hacking-case-triage.mjs \
  --dataset .kelpclaw/datasets/cfreds/hacking-case \
  --out .kelpclaw/findevil/cfreds-hacking-case/triage
# deterministic run:
node packages/cli/dist/index.js findevil sentinel \
  --case examples/findevil-cfreds-hacking-case/case.yml \
  --evidence-root .kelpclaw/findevil/cfreds-hacking-case/triage/evidence \
  --sift-command "node scripts/run-cfreds-hacking-case-triage.mjs --dataset .kelpclaw/datasets/cfreds/hacking-case --out .kelpclaw/findevil/cfreds-hacking-case/triage --mode emit-trace" \
  --max-iterations 3 --repair-runner evidence-linked --timestamp skip --deterministic \
  --out .kelpclaw/findevil/cfreds-hacking-case/sentinel
node packages/cli/dist/index.js findevil verify-audit-bundle --bundle <printed-path>
```

Document required host deps clearly: Node 20, pnpm 10, and for live SIFT —
Sleuth Kit + libewf (ewfmount). State that the SIFT VM ships these.

### Final repo check (component 1)

- [ ] Repo is **public** on GitHub.
- [ ] `LICENSE` file present and is **MIT or Apache 2.0** (verify the actual
      file, not just the README badge).
- [ ] README links: architecture diagram, JUDGES.md, demo video, accuracy
      report.
- [ ] Committed: `docs/architecture.*`, accuracy report, dataset docs,
      the archived run logs (or a link if too large).
- [ ] CI green on Node 20.

---

## Submission-day checklist (Devpost)

- [ ] Code repo URL (public)
- [ ] Demo video (≤5 min, uploaded/linked, shows real data + self-correction)
- [ ] Architecture diagram (in repo + embedded in writeup)
- [ ] Written project description (Devpost story format)
- [ ] Dataset documentation
- [ ] Accuracy report (with evidence-integrity section + residual gaps)
- [ ] Try-it-out instructions (JUDGES.md)
- [ ] Agent execution logs (committed/linked, toolUseId-traceable)

**Submit a few hours before 11:45am GMT+8 on 16 Jun.** Devpost lets you edit
after submitting as long as it's before the deadline — get a complete
draft in early, then refine.

---

## Risk notes

- **Highest remaining risk:** the live run depending on `ewfmount` + Sleuth
  Kit behaving on the CFReDS image inside the VM. Do Phase 1 first, with days
  of slack. If it fights you, the deterministic + benchmark path is still a
  complete submission — document live SIFT as "verified via CLI/hook
  integration" and lean on the synthetic + DFIR-Metric evidence.
- **Don't oversell the firewall.** A panel of DFIR people will test boundaries.
  Calling a detective control a sandbox is the exact thing they filter out;
  precise trust-boundary language reads as senior maturity.
- **Snapshot the VM** before each risky step so a broken install is a 30-second
  rollback, not a re-download.
