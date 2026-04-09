# Piranesi: Vision and Intellectual Foundation

This document lays out the thesis behind Piranesi -- why it exists, what it does differently, and why the approach is architecturally sound. It is intended for anyone evaluating the project: potential contributors, hiring managers reviewing the portfolio, grant committees, or security engineers deciding whether to invest time in adoption.

---

## 1. The Jagged Frontier Thesis

The prevailing assumption in AI cybersecurity tooling is straightforward: better models produce better security analysis. Scale the model, scale the capability. This assumption is wrong, or at best, incomplete.

The AISLE article "AI Cybersecurity After Mythos: The Jagged Frontier" presents empirical evidence that AI cybersecurity capability does not scale smoothly with model size, cost, or benchmark performance. Instead, it exhibits what Ethan Mollick calls a "jagged frontier" -- a capability boundary that is irregular, task-dependent, and frequently counterintuitive.

### The Empirical Evidence

Three experiments from the article illustrate the jaggedness:

**FreeBSD NFS Vulnerability (CVE-2023-3326).** A null-pointer dereference in the NFS server. Every model tested -- from GPT-4o at $2.50/M tokens down to GPT-OSS-20b at $0.11/M tokens -- successfully identified the vulnerability when given the relevant source context. This is a detection task with clear structural signals. Model capability does not meaningfully differentiate here.

**OpenBSD SACK Vulnerability (CVE-2023-2156).** An integer underflow in TCP SACK processing. A harder bug requiring understanding of protocol state machines. Even here, smaller open-weights models successfully identified the issue, though with less precise explanations. The gap between frontier and commodity models was measurable but not decisive -- roughly the difference between a senior and junior analyst, not between capability and incapability.

**OWASP False Positive Discrimination.** When presented with code snippets containing both real vulnerabilities and benign patterns that superficially resemble vulnerabilities, the results showed near-inverse scaling on some tasks. Smaller models, potentially because they rely more on structural pattern matching and less on "creative" interpretation, outperformed frontier models on specific false positive discrimination tasks. Model rankings completely reshuffled compared to the detection experiments.

### The Conclusion

No single model dominates all stages of the security analysis pipeline. Model rankings reshuffle across tasks. A system that routes tasks to the empirically best model per stage will outperform a system that uses a single frontier model for everything -- and will do so at lower cost.

This is the core architectural insight that Piranesi is built on. The moat is not access to a better model. The moat is the system that decomposes the problem correctly, routes each subtask to the appropriate capability tier, and integrates the results with formal verification.

---

## 2. The Five-Stage Decomposition

Piranesi decomposes cybersecurity analysis into five stages. This decomposition is not merely organizational -- it is architecturally load-bearing. Each stage has fundamentally different scaling properties, error modes, and cost profiles. Treating them as a monolithic "analyze this code" prompt to a single model is engineering malpractice, equivalent to using the same database for OLTP and OLAP because "they both store data."

### Stage 1: Broad-Spectrum Scanning

**What it does:** Parse source code, build a call graph, enumerate the attack surface (HTTP handlers, SQL queries, file operations, exec calls, deserialization points).

**Scaling properties:** This is the cheapest stage. It is largely deterministic -- tree-sitter parsing produces an AST, call graph construction is a fixed-point computation. LLM involvement is limited to heuristic classification of framework-specific patterns (e.g., identifying Express.js route handlers). Small models handle this adequately.

**Key insight from the article:** "A thousand adequate detectives searching everywhere will find more bugs than one brilliant detective who has to guess where to look." Coverage beats depth at this stage. The correct strategy is to scan everything cheaply, not to scan selectively with an expensive model.

### Stage 2: Vulnerability Detection

**What it does:** Inter-procedural taint analysis. Track data flow from user-controlled inputs (sources) through the call graph to security-sensitive operations (sinks). Generate candidate findings with taint paths.

**Scaling properties:** This is primarily a program analysis task, not an LLM task. The taint analysis engine is deterministic. LLMs assist with source/sink classification in unfamiliar frameworks and with contextual understanding of sanitization routines. Domain knowledge matters here but frontier reasoning does not -- a model needs to know that `req.body` is user-controlled and `db.query()` is a SQL sink, not to solve novel logical puzzles.

**Error mode:** False negatives from incomplete source/sink specification or imprecise taint propagation through higher-order functions and callbacks.

### Stage 3: Triage and Verification

**What it does:** Skeptic review of candidate findings. Ensemble voting across multiple models to discriminate true positives from false positives. Confidence scoring. Contextual analysis of whether sanitization is actually effective.

**Scaling properties:** This is where ensemble approaches provide the most value. The article's OWASP experiment showed that model rankings reshuffle on FP discrimination tasks -- a model that excels at detection may be mediocre at distinguishing real findings from noise. Using majority vote across 3+ models with different capability profiles produces more reliable FP discrimination than any single model.

**Why this matters:** False positives are not merely annoying. They are operationally destructive. The curl project terminated its bug bounty program specifically because AI-generated false positive submissions overwhelmed human reviewers. A security tool that generates 50 findings of which 40 are false positives is worse than useless -- it actively consumes reviewer time that could be spent on real vulnerabilities.

### Stage 4: Exploit Construction

**What it does:** Generate concrete exploit payloads via SMT constraint solving (Z3), then execute them in a sandboxed Docker container to confirm exploitability.

**Scaling properties:** This is the one stage that genuinely benefits from frontier model capability. Constructing a working exploit requires reasoning about multiple interacting constraints -- input validation logic, encoding requirements, payload structure, runtime behavior. The article's data shows the widest capability gap between frontier and commodity models at this stage.

**Piranesi's approach:** Even here, the LLM is not working alone. Z3 constraint solving handles the formal aspects of payload generation (satisfying input constraints, finding values that reach the target sink with the right properties). The LLM's role is to understand the high-level exploit strategy and translate it into constraint specifications. Sandboxed execution provides ground truth -- either the payload triggers the vulnerability or it does not.

### Stage 5: Patch Generation

**What it does:** Generate a minimal code fix for the confirmed vulnerability, then verify the patch by running the exploit reproducer script against the patched code.

**Scaling properties:** Intermediate. Patch generation requires understanding code semantics but not frontier-level reasoning for most common vulnerability patterns (parameterized queries for SQLi, output encoding for XSS, path canonicalization for traversal). The verification step -- running the reproducer against the patched code -- is deterministic and provides a hard pass/fail signal.

### Why Decomposition Matters

A monolithic system that prompts a single model with "find and fix security vulnerabilities in this code" collapses all five stages into one, losing the ability to:

- Route each stage to the cost-appropriate capability tier
- Apply different verification strategies per stage (ensemble voting for triage, formal verification for exploits)
- Provide intermediate outputs that are independently auditable
- Fail gracefully -- a scanning failure does not prevent previously detected findings from being triaged
- Iterate on individual stages without rerunning the entire pipeline

Piranesi's pipeline architecture is a direct consequence of the jagged frontier thesis. If capability scaled smoothly, a monolithic approach would be defensible. It does not, so it is not.

---

## 3. What Piranesi Does Differently

### Analysis Depth

Most AI security tools operate at one of two extremes: pure LLM pattern matching (send code to a model, get findings back) or thin wrappers around existing SAST tools (run Semgrep/CodeQL, pipe results through an LLM for triage).

Piranesi does neither. It builds a real call graph from tree-sitter ASTs, performs inter-procedural taint analysis that tracks data flow across function boundaries, through callbacks, and across module imports. The taint analysis engine is deterministic and auditable -- every finding includes a concrete taint path from source to sink, with intermediate nodes and transformations.

Whether the taint analysis engine can achieve sufficient precision on real-world TypeScript/JavaScript code is an open empirical question. JavaScript's dynamic dispatch, higher-order functions, and prototype-based inheritance present genuine challenges for static analysis. Piranesi's approach is to use LLM-assisted type inference and framework-specific modeling to handle the cases that defeat purely static analysis, but the effectiveness of this hybrid approach at scale is unproven.

### Verification

Existing tools report findings with confidence scores. Piranesi reports findings with exploits. The difference is categorical: a confidence score is a model's self-assessment of its own output; an exploit is a concrete demonstration that the vulnerability exists and can be triggered.

The verification pipeline works as follows:
1. Z3 constraint solver generates input values that satisfy the path constraints to reach the vulnerable sink
2. These values are assembled into a concrete exploit payload (HTTP request, CLI input, file content)
3. The payload is executed against the target code in a sandboxed Docker container
4. The sandbox monitors for the expected effect (SQL query modification, file system access, command execution)
5. Only findings where the exploit succeeds are reported as "confirmed"

Findings where the exploit fails are reported separately as "unconfirmed" with the constraint solver's output for manual review. This is an honest representation -- the failure might indicate a false positive, or it might indicate that the exploit generation was insufficiently creative.

### Legal Integration

This is covered in depth in the next section, but the key contrast is: no existing AI security tool -- open source or commercial -- maps findings to specific statutory obligations with section-level precision. Piranesi does this as a first-class pipeline stage, not a bolt-on.

---

## 4. The Legal Mapping Moat

### The Gap

Security tools find vulnerabilities. Compliance tools audit controls. These are separate products, purchased by separate teams, producing separate reports that someone has to manually reconcile. A security engineer finds a SQL injection in a payment processing endpoint. A compliance officer needs to know: does this trigger a mandatory breach notification? Under which statute? What is the notification timeline? What are the penalty ranges? What enforcement precedent exists?

Today, answering these questions requires a human lawyer or compliance specialist to manually cross-reference the technical finding against the applicable regulatory framework. This is slow, expensive, and often does not happen until after an incident -- which is exactly when you can least afford slow and expensive.

### Piranesi's Approach

Piranesi includes a datalog-style rule engine that formally encodes regulatory obligations. This is not keyword matching ("SQL injection" -> "bad"). It is a formal representation of legal rules that takes structured facts about a confirmed vulnerability and derives specific legal conclusions.

Example of the reasoning chain:

```
% Facts derived from the confirmed finding
vulnerability(finding_001, sqli).
affected_data(finding_001, personal_data).
data_subjects(finding_001, singapore_residents).
organization_type(org, private_sector).
annual_turnover(org, above_10m_sgd).

% PDPA rules (simplified)
pdpa_applies(Org) :- organization_type(Org, private_sector),
                      data_subjects(_, singapore_residents).

protection_obligation(Finding) :- vulnerability(Finding, Type),
                                   affected_data(Finding, personal_data),
                                   member(Type, [sqli, xss, path_traversal, ssrf]).

breach_notification_required(Finding) :- protection_obligation(Finding),
                                          exploitable(Finding, confirmed).

notification_timeline(Finding, 3, days) :- breach_notification_required(Finding),
                                            pdpa_applies(_).

max_penalty(Finding, 1000000, sgd) :- breach_notification_required(Finding),
                                       pdpa_applies(Org),
                                       annual_turnover(Org, above_10m_sgd).
```

The output for a confirmed SQL injection affecting personal data of Singapore residents would include:

- **Statute:** Personal Data Protection Act 2012 (PDPA)
- **Obligation:** Section 24 (Protection Obligation) -- reasonable security arrangements
- **Notification:** Section 26D -- mandatory notification to PDPC within 3 calendar days of assessment
- **Penalty:** Section 48J -- financial penalty up to SGD 1,000,000 or 10% of annual turnover
- **Precedent:** PDPC enforcement decision [2023-XX] -- similar SQLi finding resulted in [specific outcome]

### Why This Is a Moat

**It changes the buyer conversation.** A security tool that says "you have 11 confirmed vulnerabilities" competes on detection quality against every other scanner. A security tool that says "you have 11 confirmed vulnerabilities, 4 of which trigger mandatory breach notification under PDPA with a 3-day timeline and up to SGD 1M in penalties" competes on a dimension where no other tool operates.

**The content is laborious to create.** Encoding regulatory frameworks as formal rules requires reading statutes, understanding their structure, tracking amendments, and cross-referencing enforcement decisions. This is not work that an LLM can do unsupervised -- legal rules require precision that current models cannot guarantee. Every statute encoded, every enforcement decision catalogued, every amendment tracked is accumulated domain-specific work that compounds over time.

**The approach has precedent.** Piranesi's regulatory engine is inspired by Yuho, a domain-specific language for representing Singapore's criminal statutes as executable formal logic. Yuho demonstrated that statutes can be faithfully represented as logical rules -- the challenge is coverage, not feasibility. Piranesi applies the same principle to regulatory compliance frameworks relevant to cybersecurity.

**Singapore's regulatory landscape is well-suited to this approach.** The Personal Data Protection Commission (PDPC) publishes detailed enforcement decisions with specific factual findings, statutory references, and penalty calculations. This creates a structured corpus that can be encoded as rules and precedent. The MAS Technology Risk Management Guidelines are similarly prescriptive. These are not vague "best practice" documents -- they specify concrete obligations with concrete consequences.

**It extends naturally.** Once the rule engine exists and the encoding methodology is established, adding new regulatory frameworks (EU GDPR, CCPA/CPRA, HIPAA, PCI-DSS, NIS2, EU AI Act) is incremental effort per framework, not architectural rework.

### Honest Caveats

The regulatory engine produces legal information, not legal advice. Its output requires review by qualified legal counsel before being relied upon for compliance decisions. The formal rules are an approximation of legal reasoning -- law involves interpretation, context, and judgment that formal logic captures imperfectly. Piranesi's legal output is a starting point for legal analysis, not a substitute for it.

Whether organizations will trust automated regulatory mapping enough to integrate it into their compliance workflows is a market question, not a technical one. The hypothesis is that imperfect-but-structured legal analysis delivered at the point of vulnerability discovery is more valuable than perfect legal analysis delivered weeks later by an external law firm. This hypothesis is untested.

---

## 5. Who This Is For

### Primary Users

**Security engineers at regulated companies.** Fintech, healthtech, and govtech organizations in Singapore and ASEAN that operate under PDPA, MAS TRM, and sector-specific regulations. These engineers need security scanning that goes beyond "here are some findings" to "here are confirmed, exploitable vulnerabilities with their regulatory implications."

**Compliance teams.** Teams responsible for demonstrating security posture to regulators (MAS, PDPC, sector-specific authorities). Piranesi's legal output provides structured evidence of security analysis tied to specific regulatory obligations -- useful for regulatory examinations, audit responses, and incident response.

**Open source maintainers.** Maintainers of TypeScript/JavaScript projects who want actionable security scanning without the false positive noise that makes AI security tools unusable. The verification stage (SMT + sandbox) means Piranesi's confirmed findings are high-signal by construction.

### Secondary Users

**Penetration testing firms.** As a pre-engagement reconnaissance tool. Piranesi's scan and detect stages can rapidly map the attack surface of a TypeScript/JavaScript application, providing a structured starting point for manual testing.

**Security researchers.** The pipeline's intermediate outputs (call graphs, taint paths, constraint specifications) are useful artifacts for security research, independent of the end-to-end workflow.

---

## 6. What This Is Not

**Not a replacement for manual penetration testing.** Piranesi analyzes source code statically (with dynamic verification of specific findings). It does not test running applications, assess network configurations, evaluate authentication flows end-to-end, or test business logic vulnerabilities that require domain knowledge beyond what is present in the code.

**Not a general-purpose SAST tool.** Piranesi targets TypeScript/JavaScript in v1. It is not competing with Semgrep, CodeQL, or SonarQube on breadth of language support. Its value proposition is depth of analysis and verification on a focused scope, not breadth across all languages and frameworks.

**Not an LLM wrapper.** Piranesi uses LLMs -- extensively -- but the LLMs are components in a pipeline that includes deterministic program analysis, formal constraint solving, and sandboxed execution. A finding is not reported as confirmed because a model said it was a vulnerability. It is reported as confirmed because a generated exploit succeeded in a sandbox. The distinction matters.

**Not a product.** Piranesi is an open source tool built by one developer. It does not have a support team, an SLA, or a roadmap driven by customer requests. It is opinionated software that solves a specific problem in a specific way. Use it if the approach fits your needs. Do not use it if you need enterprise support, broad language coverage, or a web dashboard.
