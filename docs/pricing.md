# Pricing

Onibi budget math uses the standard Claude API token prices published by Anthropic.
Source: <https://platform.claude.com/docs/en/about-claude/pricing>. Checked 2026-06-30.

| Model | Input | Output |
| --- | ---: | ---: |
| `claude-sonnet-4-6` | $3 / MTok | $15 / MTok |
| `claude-opus-4-7` | $5 / MTok | $25 / MTok |
| `claude-haiku-4-5` | $1 / MTok | $5 / MTok |

The code table lives in `internal/budget/policy.go` as cents per million tokens. Cost estimates use integer micro-cents internally, so one input token on `claude-sonnet-4-6` is `300` micro-cents and one output token is `1500` micro-cents.

Not yet modeled: prompt caching, batch discounts, priority tier, regional pricing, marketplace markups, or non-Claude agents.
