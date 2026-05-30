# Challenges

The main challenge was keeping the project honest. A polished demo could hide the exact failure mode we wanted to show, so the fixture intentionally includes 10 claims with supported evidence, weak indicators, contradictions, and hostile case-derived text.

A second challenge was separating evidence from instructions. DFIR data often contains filenames, ransom notes, scripts, log messages, and user documents. Some of that text looks like a command. The firewall had to treat those strings as evidence while preventing them from crossing into tool arguments.

The last challenge was keeping replay simple enough for judges. The current submission supports live SIFT mode, but it also keeps a deterministic offline trace in `fixtures/protocol-sift-baseline/` so the verifier, ATT&CK tagger, benchmark scorer, firewall, repair loop, spoliation check, and audit export can be rerun without requiring every judge to configure a live SIFT VM first.
