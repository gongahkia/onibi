#!/usr/bin/env node
import { runModelWrapper, tempResponsePath } from "./lib/appsec-wrapper.mjs";

const responsePath = tempResponsePath("kelpclaw-codex-response");
runModelWrapper({
  label: "Codex CLI",
  command: process.env.KELPCLAW_CODEX_COMMAND ?? "codex",
  args: [
    "exec",
    "--color",
    "never",
    "--output-last-message",
    responsePath,
    ...process.argv.slice(2),
    "-"
  ],
  responsePath
});
