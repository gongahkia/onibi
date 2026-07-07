#!/usr/bin/env node
import { runModelWrapper } from "./lib/appsec-wrapper.mjs";

const [command, ...args] = process.argv.slice(2);
if (!command) {
  throw new Error("custom-command-wrapper requires a command followed by optional args.");
}

runModelWrapper({
  label: "custom command",
  command,
  args
});
