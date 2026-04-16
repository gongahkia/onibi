# Prompt: Clarify Compliance Claims and Add Framework Metadata

You are working in Piranesi, an AppSec CLI that includes compliance/reporting features. Compliance claims must be precise: the tool can provide supporting evidence, but it should not imply full compliance certification.

Goal: clarify compliance positioning and add last-reviewed framework metadata.

Implementation requirements:

- Inspect `src/piranesi/legal/`, report templates, and docs mentioning OWASP, SOC 2, ISO, PCI, HIPAA, GDPR, or similar frameworks.
- Add metadata for compliance mappings, such as framework name, version, control ID, mapping rationale, last reviewed date, reviewer/source, and confidence.
- Update reports to distinguish vulnerability evidence from compliance support evidence.
- Remove or soften any language that implies Piranesi certifies compliance by itself.
- Add tests for metadata serialization/rendering if compliance models exist.
- Update docs with a clear disclaimer and explanation of mapping maintenance.

Acceptance criteria:

- Compliance output is auditable and versioned.
- Users can see when a mapping was last reviewed.
- Claims are accurate and legally safer.
