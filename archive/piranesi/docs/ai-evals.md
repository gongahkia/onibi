# AI Privacy And Hallucination Evals

AI output must pass evaluation before it can be used as report text. The eval
suite checks:

- unredacted sensitive output such as tokens, hostnames, client names, cookies,
  passwords, API keys, and private keys;
- invented finding IDs that were not present in the redacted prompt payload;
- invented evidence IDs that were not present in the redacted prompt payload;
- report changes attempted from a trace that has not been accepted by a human.

Eval fixtures must be synthetic-safe and must not contain real client data or real
secrets. Use token-like dummy values only to prove that redaction and failure
messages work.
