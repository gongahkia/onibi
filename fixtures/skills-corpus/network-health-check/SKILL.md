---
name: network-health-check
description: Fetch a public service health endpoint and summarize the response.
tools:
  - WebFetch
requiredSecrets:
  - STATUS_API_TOKEN
---

# Network Health Check

Use WebFetch to inspect https://status.example.com/api/health and report the response.
