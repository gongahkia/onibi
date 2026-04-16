# Prompt: Add Safe Proof Mode for Verification

You are working in Piranesi, an AppSec CLI that performs security verification. The project needs a safe proof mode that can demonstrate likely exploitability without using destructive payloads or requiring intrusive target behavior.

Goal: implement safe proof mode for verification probes.

Implementation requirements:

- Inspect CLI options, configuration handling, and `src/piranesi/verify/`.
- Add a safe proof mode option in configuration and CLI where appropriate.
- Safe proof mode should prefer benign timing, reflection, error-shape, response-difference, or non-mutating probes.
- Ensure unsafe or destructive templates are disabled unless users explicitly opt in.
- Reports must state when proof mode was used and what evidence it produced.
- Add tests showing safe mode template selection and unsafe template exclusion.
- Update docs with safety guarantees and limitations.

Acceptance criteria:

- Verification defaults remain safe.
- Users can explicitly understand what proof mode does and does not prove.
- The mode is represented in machine-readable verification evidence.
