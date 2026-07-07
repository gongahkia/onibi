#!/usr/bin/env node
import { runModelWrapper } from "./lib/appsec-wrapper.mjs";

runModelWrapper({
  label: "Claude Code",
  command: process.env.KELPCLAW_CLAUDE_COMMAND ?? "claude",
  args: ["-p", "--output-format", "json", "--tools", "", ...process.argv.slice(2)]
});
