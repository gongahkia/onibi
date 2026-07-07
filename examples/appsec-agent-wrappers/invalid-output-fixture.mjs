#!/usr/bin/env node
import { writeFileSync } from "node:fs";

writeFileSync(process.env.KELPCLAW_APPSEC_OUTPUT, "not valid triage json\n");
