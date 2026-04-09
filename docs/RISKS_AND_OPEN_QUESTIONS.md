# Risks and Open Questions

A frank assessment of what could go wrong with Piranesi v1. This document does not sugarcoat. Problems are real and mitigations are incomplete.

---

## 1. Scope Assessment -- Is v1 Too Ambitious?

**Yes.** The v1 spec combines five distinct hard problems into one tool:

1. **A research-grade inter-procedural taint analysis engine** with context sensitivity (k=2), field sensitivity, aliasing, and path condition tracking. This alone -- done properly for JavaScript/TypeScript -- is a multi-year PhD research project. Production implementations (CodeQL, Semgrep Pro, Joern) have teams of 5-20 engineers and years of development.

2. **An SMT-backed exploit generator** that translates path conditions into concrete payloads via Z3. This is a novel integration. There is no off-the-shelf library for "taint path conditions to Z3 constraints for web vulnerability payloads." This must be built from scratch.

3. **A regulatory rule engine** with formal legal reasoning across multiple frameworks (PDPA, MAS TRM, EU AI Act). This requires cross-domain expertise (security engineering + regulatory law) that is rare and expensive to validate.

4. **A calibrated LLM ensemble** with cost optimization, model routing, and confidence calibration. This is ML engineering that requires empirical tuning against ground truth data that won't exist until Phase 5.

5. **A comprehensive evaluation harness** with ground truth curation, multi-metric scoring, and Pareto frontier analysis.

### Effort Reality Check

**Total estimated effort: 660-950 ideal hours.**

For a solo developer working full-time (40h/week of actual focused engineering, which is optimistic):
- Best case: 16.5 weeks (~4 months)
- Worst case: 23.75 weeks (~6 months)
- Realistic case accounting for investigation spikes, debugging, design dead-ends, and the general friction of novel work: **7-9 months**.

The estimate assumes no significant blocking issues. For a project with this much novelty, that assumption is wrong. Budget for at least 2-3 "this approach doesn't work, redesign required" episodes, each costing 20-40 hours.

### Proposed Scope Reductions

If timelines slip, cut from the bottom of this list upward. Items at the top are lowest-impact cuts.

| Priority | Cut | Hours Saved | Impact |
|----------|-----|-------------|--------|
| 1 (cut first) | EU AI Act rules from Phase 3 | ~20h | Niche applicability. PDPA + MAS TRM are sufficient for v1. |
| 2 | Pareto frontier plotting from Phase 5 | ~20h | Nice-to-have visualization. Baseline comparison is enough. |
| 3 | Calibrated ensembling from Phase 4 | ~35h | Majority voting works. Calibration requires ground truth that won't exist early. |
| 4 | Path condition tracking from Phase 1 | ~90h | Big savings. Use LLM-based payload gen instead of SMT for v1. SMT approach becomes v1.1. This is the single biggest scope lever. |
| 5 | Cost-aware optimizer from Phase 4 | ~25h | Hardcoded model routing is fine for v1. Users can configure manually. |
| 6 (reluctant) | Docker sandbox from Phase 2 | ~60h | **Significant weakening.** Removes "verified" from "verified exploit." Payloads are generated but not confirmed. Strongly discourage this cut. |
| 7 (do not cut) | Regulatory engine (Phase 3) | -- | This is the differentiator. Without it, Piranesi is another static analysis tool. |
| 8 (do not cut) | Taint analysis (Phase 1) | -- | This is the core. Without it, there is no project. |

**If cuts 1-4 are applied:** ~165 hours saved, bringing total to 495-785h. This is a 3-5 month project. The tool still does real taint analysis with LLM-assisted payload generation, regulatory mapping for Singapore frameworks, and basic LLM orchestration. This is a credible v1.

**If cuts 1-5 are applied:** ~190 hours saved. Minimal viable product territory.

---

## 2. Technical Risks

### a) Taint Analysis Precision (Joern-dependent) -- HIGH RISK

**The problem:** Piranesi's taint analysis quality is bounded by Joern's data flow analysis precision. Joern is a general-purpose CPG platform, not specifically tuned for Node.js/Express security analysis. Its JS frontend may miss patterns that a custom engine could handle.

**Concrete concerns:**
- `eval()` and `new Function()` — Joern's JS frontend does not track taint through dynamically generated code. These are soundness holes.
- Dynamic property access (`obj[varName]`) — Joern may over-approximate (taint entire object) or under-approximate (miss taint on specific fields). Behavior depends on Joern's internal heuristics.
- `Reflect.apply`, `Proxy` — effectively unanalyzable statically. Joern will miss these.
- Express middleware chains — Joern must correctly model `app.use(middleware)` and `router.use()` to track taint through middleware. Not validated until the spike.
- Callback-heavy patterns (e.g., `array.map(fn).filter(fn2).reduce(fn3)`) — Joern's data flow may lose taint through higher-order function chains.
- Module resolution — Joern analyzes transpiled JS, so `tsc` handles TS module resolution. But Joern must still resolve JS `require()` and `import` to build the call graph across files.

**Mitigation:**
- Validation spike (Phase 1, Milestone 1.0) measures detection rate on real Express apps.
- Performance budget: 60 seconds for 500 files (Joern is JVM-fast, this should be achievable).
- Soundness holes documented. Piranesi reports what it cannot analyze.
- If specific patterns are systematically missed, add supplementary Python-based checks for those patterns only (doesn't require replacing Joern).

### b) Z3 Performance on String Constraints -- MEDIUM RISK

**The problem:** Z3's string theory solver (CVC5's is better for strings, but we're using Z3) is less mature than its bitvector and integer theories. Complex string constraints involving regex matching, multi-encoding awareness (URL encoding, HTML encoding, base64), and length-dependent operations may produce timeouts or `unknown` results.

**Concrete concern:** A SQL injection path condition like `contains(input, "'") AND length(input) < 100 AND NOT matches(input, sanitizer_regex)` involves string operations that Z3 can handle. But a more complex XSS path condition involving nested encoding (`encodeURIComponent(htmlEscape(input))`) may exceed Z3's capabilities.

**Mitigation:**
- 30-second timeout per Z3 query. Timeout -> fall back to template-based payload.
- v1 constraint classes (equality, contains, prefix/suffix, length bounds) are well within Z3's proven capabilities. Do not attempt regex constraint solving in v1.
- If Z3 string solving proves unreliable, the fallback is LLM-based payload generation with manual confirmation. This is less rigorous but pragmatically effective.

### c) Docker Sandbox Reliability -- MEDIUM RISK

**The problem:** The verify stage auto-generates a Dockerfile for the target Node.js application, builds the container, deploys the exploit payload, and observes the result. This requires:
- Correctly inferring `npm install` vs `yarn` vs `pnpm`
- Handling native modules that require system libraries
- Setting up databases (many Node.js apps require MongoDB/PostgreSQL)
- Configuring environment variables the app expects

Getting all of this right automatically for an arbitrary Node.js project is extremely fragile.

**Concrete concern:** A target app with `"postinstall": "node scripts/setup.js"` that expects a running MongoDB instance will fail to build in a basic Docker container. The sandbox will report "build failed" and the finding becomes "unverifiable." If this happens for most findings, the "verified exploit" claim is hollow.

**Mitigation:**
- Best-effort sandbox. "Unverifiable" is a valid status. The finding is still reported with the generated payload; it just isn't confirmed.
- Allow users to supply their own Dockerfile via config (`[sandbox] dockerfile = "path/to/Dockerfile"`).
- For the hand-crafted example app, the sandbox will work perfectly (we control the setup). For real-world projects, expect 30-50% sandbox failure rate.
- Document honestly: "Piranesi's sandbox works best with simple Express/Koa/Fastify apps. Complex applications may require user-provided Docker configuration."

### d) Joern Integration Risks -- MEDIUM RISK

**The problem:** Piranesi's taint analysis is delegated to Joern (JVM-based CPG engine). This introduces risks specific to depending on an external analysis engine.

**Specific concerns:**
- **TS transpilation fidelity:** TypeScript must be transpiled to JS before Joern can analyze it. Source map line-number mapping may be imprecise for complex TS constructs (decorators, enums, namespace merging, const enums that are inlined). Findings may point to wrong lines in the original TS.
- **Joern JS frontend coverage:** Joern's JavaScript frontend may not handle all modern JS patterns (optional chaining `?.`, nullish coalescing `??`, private class fields `#field`, top-level await). The validation spike will identify gaps.
- **Black box analysis:** If Joern misses a data flow path, Piranesi cannot fix it without modifying Joern's Scala source (~100K lines). Supplementary checks can be added in Python but cannot fix Joern's internal analysis.
- **JVM dependency:** Users must install JVM 11+ (~200MB). Adds friction to installation, especially on CI runners.
- **Joern version pinning:** Joern releases may change CPGQL semantics or output format. Must pin and test against specific versions.

**Mitigation:**
- Validation spike (Phase 1, Milestone 1.0) runs before any significant investment.
- Pin Joern version. CI tests run against the pinned version.
- For TS source map issues: verify line mappings in test suite, accept ±2 line drift for complex constructs.
- For Joern coverage gaps: maintain a list of unsupported patterns, add supplementary Python-based checks for critical gaps.
- For JVM dependency: document clearly, provide a Dockerfile with Joern pre-installed.

### e) Python Performance -- LOW RISK (reduced with Joern)

**The problem:** With Joern handling the heavy-lifting program analysis (CPG construction, data flow, call graph), Python is primarily doing: subprocess management, JSON parsing, CPGQL query construction, and Pydantic model mapping. These are I/O-bound tasks where Python performance is acceptable.

**Why it's low:** The CPU-intensive work (graph traversal, fixed-point iteration) runs in the JVM (Joern), not Python. Python's role is orchestration, not analysis. LLM API latency (triage, patch generation) will dominate wall-clock time, not Python CPU time.

**Remaining concern:** Joern server startup takes 3-5 seconds (JVM cold start). For small projects, this may be a noticeable fraction of total analysis time. Server mode amortizes this across queries.

---

## 3. Ethical and Legal Risks

### a) Misuse -- Unauthorized Target Scanning -- HIGH RISK

**The problem:** Piranesi generates working exploit payloads and reproducer scripts. If used against systems the user does not own or have authorization to test, this is a crime in most jurisdictions (CFAA in the US, CMA in Singapore, CFAA equivalents in EU member states).

**The uncomfortable truth:** No technical safeguard can prevent misuse. The `--authorized` flag and confirmation prompt are a liability shield, not a prevention mechanism. A malicious user will pass `--authorized --yes` without hesitation.

**This is not unique to Piranesi.** Metasploit, Burp Suite, sqlmap, Nuclei, and every other offensive security tool has this problem. The legal precedent is clear: tool authors are not liable for misuse when the tool includes reasonable safeguards and is designed for legitimate use.

**Mitigation (incomplete by design):**
- `--authorized` flag with confirmation prompt. Required for `verify` and `run` commands.
- Reproducer scripts include a header: `# WARNING: This script exploits a vulnerability. Use only against systems you own or have written authorization to test.`
- Documentation emphasizes authorized use only.
- Terms of use (in LICENSE or separate TERMS.md) disclaim liability for misuse.
- **No phone-home, no telemetry, no usage tracking.** This is a security tool. Users will not trust it if it calls home.

### b) Incorrect Legal Memos -- HIGH RISK

**The problem:** Piranesi's regulatory engine produces legal analysis: which laws apply, what obligations they impose, what penalties apply, and what deadlines exist. If a user relies on this analysis and it is incorrect -- missed an applicable regulation, cited the wrong section, stated an incorrect penalty range -- the user could suffer real regulatory consequences.

**This is arguably the highest-risk feature in the project.** A wrong technical finding is embarrassing. A wrong legal memo can lead to fines, regulatory enforcement actions, or legal liability.

**Specific failure modes:**
- **False negative (missed obligation):** The engine fails to identify that a finding triggers PDPA mandatory breach notification. The user does not notify PDPC within 3 days. Penalty: up to SGD 1,000,000.
- **False positive (spurious obligation):** The engine incorrectly states that a finding triggers MAS TRM incident reporting. The user wastes time on unnecessary compliance procedures. Less dangerous but erodes trust.
- **Stale rules:** PDPA is amended. The rule engine still has the old section numbers and penalty ranges. The analysis is wrong but appears authoritative.

**Mitigation:**
- Every legal memo includes: `"DISCLAIMER: This analysis is informational only. It is not legal advice. Consult qualified legal counsel for regulatory compliance decisions."`
- Rule sets are versioned with effective dates. The output shows: `"Rules current as of: 2026-01-15"`.
- If rules haven't been updated in > 6 months, log a warning: `"WARNING: Regulatory rules may be outdated. Last updated: 2025-07-01."`
- Consider: should legal memos be opt-in rather than default? An explicit `--legal` flag would reduce casual reliance. Counterargument: if legal mapping is the differentiator, hiding it behind a flag undermines the value proposition.

**Open question:** Should Piranesi's rule set be reviewed by a practicing Singapore technology lawyer before release? This would cost money and time but would significantly reduce the risk of embarrassing or harmful errors. Recommended for PDPA rules specifically.

### c) Liability for Generated Exploits -- MEDIUM RISK

**The problem:** Piranesi generates concrete exploit payloads (e.g., `' OR 1=1; --` for SQL injection) and reproducer scripts (runnable Python/bash scripts that exploit the vulnerability). If these artifacts leak or are misused by someone other than the intended user, is the project author liable?

**Legal analysis (not legal advice):** Exploit generation for defensive purposes is generally protected in jurisdictions with established cybersecurity case law. The Wassenaar Arrangement's "intrusion software" controls are the main regulatory risk, but open-source security research tools have historically been exempt from export controls. Metasploit, ExploitDB, and Nuclei have operated for years without legal challenge.

**Mitigation:**
- Reproducer scripts include safety warnings and authorization checks.
- Exploit payloads are stored in local files, never transmitted to external services (except the user's own LLM API calls, which are BYOK and under the user's control).
- Clear terms of use.

### d) Regulatory Accuracy Decay -- MEDIUM RISK

**The problem:** Laws change. Piranesi's regulatory rules are encoded at a point in time. Without ongoing maintenance:
- PDPA amendments (Singapore regularly updates the PDPA)
- New PDPC guidance notes and enforcement decisions
- MAS circular updates and new requirements
- EU AI Act delegated acts and implementing regulations (the EU AI Act is still being operationalized)

If the rule engine is not updated, it becomes silently inaccurate. "Silently" is the key word: there is no mechanism to detect that rules are stale (except the version date check mentioned above).

**Mitigation:**
- Version rule sets with effective dates.
- 6-month staleness warning.
- Make updating rules a documented, straightforward process (edit TOML/datalog files, no code changes required).
- In the long term, a community-maintained rule set (like Semgrep's community rules) would distribute the maintenance burden. But this requires a community, which Piranesi doesn't have yet.

---

## 4. Open Research Questions

### a) Path Condition Tracking Through Loops

**Question:** How should the analyzer handle data-dependent loop bounds?

A common pattern:
```javascript
for (let i = 0; i < input.length; i++) {
    output += sanitize(input[i]);
}
```

The path condition through this loop depends on `input.length`, which is data-dependent. Options:
- **Bounded unrolling (k=3):** Analyze the first k iterations. Sound for k iterations, unsound beyond. Simple to implement. May miss vulnerabilities that manifest only after many iterations (rare for security-relevant taint flows).
- **Widening:** Abstract the loop into a summary that captures all possible iterations. More general but harder to implement correctly, and may lose the precise constraints needed for Z3 exploit generation.
- **Loop invariant inference:** Infer what is true on every iteration. Theoretically ideal but practically very hard for JavaScript programs.

**Proposed approach for v1:** Bounded unrolling (k=3). Flag loops that exceed the unrolling bound for manual review. Accept that this is unsound for deep loops. In practice, security-relevant taint flows through loops are almost always capturable within 1-2 iterations (the taint enters the loop, propagates through one iteration, and reaches the sink).

### b) Calibration Across Heterogeneous Models

**Question:** How should confidence calibration work when LLM providers offer different output types?

| Provider | Logit Access | Token Probabilities | Structured Output |
|----------|-------------|--------------------|--------------------|
| OpenAI (GPT-4o) | No | Yes (top-5 logprobs) | Yes (function calling) |
| Anthropic (Claude) | No | No | Yes (tool use) |
| Local models (Ollama) | Yes (full) | Yes (full) | Varies |
| Google (Gemini) | No | No | Yes (function calling) |

Temperature scaling (Platt scaling) requires access to pre-softmax logits. Most API providers don't expose these.

**Proposed approach:** Use self-reported confidence as a proxy. Ask the model: "Rate your confidence in this assessment from 0.0 to 1.0." This is known to be poorly calibrated (LLMs tend to be overconfident), but it provides a signal. Apply a learned correction factor per model (e.g., "Claude reports 0.9 when empirical accuracy is 0.7, so apply a 0.78x correction factor"). The correction factor comes from Phase 5 ground truth evaluation.

**Honest assessment:** This is a hack. Proper calibration requires logit access. The correction factor approach is better than nothing but is model-version-specific (GPT-4o's calibration curve differs from GPT-4o-mini's) and will need re-estimation whenever models are updated.

### c) Regulatory Rule Composability

**Question:** What happens when rules from different frameworks interact?

Example: a confirmed SQL injection vulnerability with personal data exposure triggers:
- PDPA S24: mandatory breach notification within 3 days
- MAS TRM 11.2.3: incident reporting within 30 days (if the organization is an MAS-regulated financial institution)
- EU AI Act (if applicable): incident reporting for high-risk AI systems

Do penalties stack? Is the organization subject to all regulatory regimes simultaneously? If they notify PDPC under PDPA, does that satisfy MAS TRM reporting requirements?

**This is a genuine legal question** that varies by organization type, jurisdiction of incorporation, jurisdiction of data subjects, and the specific regulatory relationships between PDPC and MAS.

**Proposed approach for v1:** Present each framework's assessment independently. Do not attempt to synthesize cross-framework obligations. Do not attempt to determine which frameworks apply to the user's organization (that requires knowing their regulatory status, which Piranesi does not have). Let the user and their legal counsel determine applicability.

**Why not attempt synthesis:** Getting it wrong is worse than not attempting it. A wrong cross-framework analysis ("you only need to notify PDPC, not MAS") could lead to regulatory violations. Presenting each framework independently is safe and correct.

### d) Taint Analysis for Async/Await Patterns

**Question:** How should taint propagate through JavaScript's asynchronous execution model?

```javascript
// Case 1: simple await -- taint should propagate
const data = await fetchUserInput(); // tainted
db.query(data); // sink

// Case 2: Promise.then chain -- taint should propagate
fetchUserInput()
    .then(data => db.query(data)); // sink

// Case 3: Promise.all -- taint propagates through array
const [a, b] = await Promise.all([taintedPromise, cleanPromise]);
// 'a' is tainted, 'b' is not

// Case 4: event emitter -- taint propagation is unclear
emitter.on('data', (chunk) => {
    // is 'chunk' tainted if the event was emitted with tainted data?
    // this requires cross-function taint tracking through the event system
});

// Case 5: callback hell -- deep nesting
fs.readFile(taintedPath, (err, data) => {
    processData(data, (result) => {
        db.query(result); // is this tainted?
    });
});
```

**Proposed approach for v1:**
- `await`: model as simple value unwrapping. `const x = await p` propagates taint from `p` to `x`. This is correct for the common case.
- `Promise.then()`: propagate taint through callback parameters. `p.then(x => ...)` taints `x` if `p` is tainted. This is correct.
- `Promise.all()`: propagate taint element-wise. Each element in the result array inherits taint from the corresponding input promise. This is correct.
- `EventEmitter`: **soundness hole.** Flag as unanalyzable. The connection between `emitter.emit('data', taintedValue)` and `emitter.on('data', callback)` requires interprocedural analysis through the event system, which is essentially a dynamic dispatch problem. Do not attempt this in v1.
- Callbacks: model as normal function calls. `foo(callback)` where `foo` calls `callback(x)` propagates taint if the call graph connects `foo`'s parameter to the callback invocation. This works if the call graph is complete.

**Honest assessment:** Case 4 (event emitters) is a real soundness hole. Express middleware chains, Socket.io handlers, and stream processing all use event-based patterns. Vulnerabilities hidden behind event emitters will be missed. This is a known limitation, not a fixable bug.

### e) Ground Truth at Scale

**Question:** How many ground-truth vulnerability entries are needed for statistically meaningful evaluation?

The Phase 5 plan calls for 20 curated ground-truth entries (10 true vulnerabilities across CWE types + 10 negative examples). This is enough to demonstrate basic capability (does the tool find SQL injection? yes/no) but not enough for statistically significant per-CWE metrics.

**The math:** With 3 examples per CWE category, a single misclassification swings precision by 33%. Confidence intervals are enormous. You cannot claim "92% precision on CWE-89" when n=3.

**What would be sufficient:** 10-15 examples per CWE category, across 5-7 CWE types = 50-100 ground-truth entries. Each entry requires finding a real vulnerability in a real project, confirming it, documenting the taint path, and crafting the expected output. At ~2 hours per entry, that's 100-200 hours of curation work -- equivalent to an entire phase.

**Proposed approach:**
- v1: 20 entries. Accept that eval results are directional, not statistically robust. Report confidence intervals. Do not claim specific precision/recall numbers in marketing materials.
- v1.1: expand to 50 entries by incorporating findings from the example runs (NodeGoat, Juice Shop) as additional ground truth.
- Long-term: adopt a community-contributed ground truth dataset (similar to how SARD/Juliet test suites work for C/C++ analysis).

---

## 5. Dependency Risks

### a) Joern JS Frontend Maintenance

**Current status:** Joern is actively maintained by the ShiftLeft/Qwiet AI team and open source community. The JavaScript frontend (`jssrc2cpg`) is functional but receives less attention than the Java/C frontends.

**Risk:** If JavaScript/TypeScript evolves (new syntax like `using` declarations, decorator metadata, import attributes), Joern's JS frontend may lag behind. Since Piranesi transpiles TS→JS via `tsc`, the risk is mitigated (tsc handles syntax, Joern analyzes the output JS). But if `tsc` emits JS patterns that Joern's frontend doesn't model correctly, analysis quality degrades.

**Impact:** Moderate. Transpilation via `tsc` handles most syntax evolution. The risk is in semantic modeling (e.g., does Joern correctly model the output of `tsc`'s class field transforms? Its enum transforms?).

**Mitigation:**
- Pin Joern version. Test against a corpus of tsc-transpiled output.
- Monitor Joern releases and JS frontend changes.
- If Joern's JS frontend falls behind, consider contributing fixes upstream (Joern is Apache 2.0, contributions welcome).

### b) LiteLLM Stability

**Risk:** LiteLLM is a fast-moving project with frequent releases. API changes (renamed parameters, changed response formats, new provider integration methods) could break Piranesi's LLM abstraction layer.

**Impact:** Moderate. Breakage would affect triage and patch generation (Phases 4+), not the core taint analysis.

**Mitigation:**
- Pin LiteLLM version in `pyproject.toml`.
- Wrap LiteLLM behind Piranesi's own abstraction layer (`piranesi.llm.client`). If LiteLLM's API changes, only the wrapper needs updating.
- Integration tests that make actual LLM calls (gated behind an environment variable, not run in CI by default).

### c) Z3 Python Bindings Compatibility

**Risk:** Z3's Python bindings (`z3-solver` on PyPI) can be problematic:
- macOS ARM (Apple Silicon): the wheel may not be available for all Python versions, requiring a source build that needs CMake and a C++ compiler.
- Version conflicts: Z3 4.x vs 4.y may have API differences.
- Build time: source build takes 5-10 minutes.

**Impact:** Installation friction. A user running `pip install piranesi` on macOS ARM may hit a build failure at the Z3 step.

**Mitigation:**
- Document Z3 installation issues in getting-started.md.
- Test installation on macOS ARM, macOS Intel, Ubuntu 22.04 in CI.
- Provide a Dockerfile that includes Z3 pre-built, for users who can't install natively.
- If Z3 wheel availability improves (it has been improving), this risk decreases over time.

### d) Docker Availability

**Risk:** Piranesi requires Docker for the `verify` stage. Not all development environments have Docker available (corporate machines with restricted installs, CI environments without Docker-in-Docker, Windows without WSL2).

**Impact:** Without Docker, the `verify` stage cannot run. Findings are reported but not verified. The tool still works for scan/detect/triage/legal/patch, but the "verified exploit" claim is unavailable.

**Mitigation:**
- Make Docker optional. If Docker is not available, `verify` stage returns "skipped: Docker not available" and the pipeline continues.
- Document Docker requirement prominently.
- Consider: should `piranesi run` without Docker skip `verify` silently or require `--no-verify` flag? Recommendation: require `--no-verify` to make the limitation explicit.

---

## 6. What Should Be Cut First?

Explicit priority ordering for scope reduction. Start cutting from the top (lowest impact). Do not cut from the bottom unless the project is in crisis.

| Priority | Feature | Hours Saved | Risk of Cutting |
|----------|---------|-------------|-----------------|
| 1 (cut first) | EU AI Act rules | ~20h | Minimal. Niche applicability. PDPA + MAS TRM are sufficient for v1 and cover the primary target audience (Singapore-based organizations). |
| 2 | Pareto frontier plotting | ~20h | Low. Baseline comparison tables are sufficient. The plot is a nice visualization but adds little analytical value in v1. |
| 3 | Calibrated ensembling | ~35h | Low-medium. Majority voting is an adequate starting point. Calibration requires ground truth data from Phase 5 anyway, creating a circular dependency. |
| 4 | Path condition tracking | ~90h | **Medium. This is the biggest scope lever.** Removing SMT-backed path conditions means exploit payloads are generated by LLM heuristics rather than constraint solving. The payloads may be less precise, but for common vulnerability classes (SQLi, XSS, path traversal), template-based payloads work well enough. The SMT approach can be added in v1.1 without architectural changes (the constraint generation is a plugin point in the pipeline). |
| 5 | Cost-aware optimizer | ~25h | Low-medium. Users can manually configure model routing in `piranesi.toml`. Hardcoded defaults (e.g., "use Claude Haiku for triage, GPT-4o for patch gen") are fine for v1. |
| 6 (reluctant) | Docker sandbox | ~60h | **High. Significantly weakens the core thesis.** Without sandbox verification, "verified exploit" becomes "generated payload." The tool loses its key differentiator vs. existing static analysis tools that also generate findings but don't verify them. Cut this only if Docker integration proves technically infeasible (e.g., Docker-in-Docker issues in CI, overwhelming sandbox failure rates). |
| 7 (do not cut) | Regulatory engine | -- | **Unacceptable.** The regulatory mapping is Piranesi's moat. Without it, this is another static analysis tool competing with CodeQL, Semgrep, and Snyk -- all of which have more resources and broader language support. The regulatory angle is what makes Piranesi worth building. |
| 8 (do not cut) | Taint analysis | -- | **Unacceptable.** This is the core. Without inter-procedural taint analysis, there is no project. LLM-only vulnerability detection (send code to GPT and ask "are there vulnerabilities?") already exists in dozens of tools and is not worth building another one. |

### Summary

If cuts 1-3 are applied: ~75 hours saved. Minimal impact on the tool's value proposition.
If cuts 1-5 are applied: ~190 hours saved. The tool still works but relies more on LLMs and less on formal methods. This is a credible v1.
If cut 6 is applied: the "verified" claim is gone. Piranesi becomes "taint analysis + regulatory mapping" without the exploit verification that makes findings actionable. Avoid this.
