#!/usr/bin/env node
import { deterministicTriage, readAppsecInput, writeAppsecOutput } from "./lib/appsec-wrapper.mjs";

writeAppsecOutput(deterministicTriage(readAppsecInput(), "deterministic wrapper fixture"));
