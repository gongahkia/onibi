#!/usr/bin/env node
import { runModelWrapper } from "./lib/appsec-wrapper.mjs";

runModelWrapper({
  label: "Goose",
  command: process.env.KELPCLAW_GOOSE_COMMAND ?? "goose",
  args: [
    "run",
    "--instructions",
    "-",
    "--no-session",
    "--quiet",
    "--output-format",
    "json",
    ...process.argv.slice(2)
  ]
});
