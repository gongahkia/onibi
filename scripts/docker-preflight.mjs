#!/usr/bin/env node
import { spawn } from "node:child_process";
import { constants } from "node:fs";
import { access, mkdir } from "node:fs/promises";
import { dirname } from "node:path";

const [, , service, separator, ...command] = process.argv;

if (!["api", "kelpclaw"].includes(service) || separator !== "--" || command.length === 0) {
  console.error("Usage: docker-preflight.mjs <api|kelpclaw> -- <command...>");
  process.exit(64);
}

if (process.env.KELPCLAW_PREFLIGHT === "0") {
  execCommand(command);
} else {
  const errors = [];
  const warnings = [];

  if (service === "api") {
    await validateApi(errors, warnings);
  } else {
    validateKelpClaw(errors);
  }

  if (warnings.length > 0) {
    for (const warning of warnings) {
      console.warn(`[preflight:${service}] warning: ${warning}`);
    }
  }

  if (errors.length > 0) {
    console.error(`[preflight:${service}] blocked startup:`);
    for (const error of errors) {
      console.error(`- ${error}`);
    }
    console.error("Fix .env or docker-compose.yml, then run docker compose up again.");
    process.exit(78);
  }

  console.log(`[preflight:${service}] ok`);
  execCommand(command);
}

async function validateApi(errors, warnings) {
  requireSecret(errors, "KELPCLAW_ADMIN_TOKEN", {
    placeholder: "change-me-admin-token",
    reason: "KelpClaw and API calls require a Bearer token."
  });

  const secretStore = stringValue("KELPCLAW_SECRET_STORE", "sqlite");
  if (secretStore !== "memory") {
    requireSecret(errors, "KELPCLAW_SECRET_MASTER_KEY", {
      placeholder: "change-me-32-byte-minimum-master-key",
      minLength: 32,
      reason: "encrypted local secrets need a stable AES-256-GCM master key."
    });
  }

  requireEnum(errors, "KELPCLAW_PLANNER_MODE", ["deterministic", "live"], "live");
  const plannerMode = stringValue("KELPCLAW_PLANNER_MODE", "live");
  const plannerProvider = stringValue("KELPCLAW_PLANNER_PROVIDER", "anthropic");
  requireEnum(errors, "KELPCLAW_PLANNER_PROVIDER", ["anthropic", "openai"], "anthropic");

  const explicitCodegenProvider = optionalString("KELPCLAW_CODEGEN_PROVIDER");
  if (explicitCodegenProvider) {
    requireEnum(errors, "KELPCLAW_CODEGEN_PROVIDER", ["anthropic", "openai"]);
  }
  const codegenProvider = explicitCodegenProvider || plannerProvider;
  const explicitAgenticProvider = optionalString("KELPCLAW_AGENTIC_PROVIDER");
  if (explicitAgenticProvider) {
    requireEnum(errors, "KELPCLAW_AGENTIC_PROVIDER", ["anthropic", "openai"]);
  }
  const agenticProvider = explicitAgenticProvider || plannerProvider;

  if (plannerMode === "live") {
    requireProviderKey(errors, plannerProvider, "KELPCLAW_PLANNER_PROVIDER");
    if (codegenProvider !== plannerProvider) {
      requireProviderKey(errors, codegenProvider, "KELPCLAW_CODEGEN_PROVIDER");
    }
    if (agenticProvider !== plannerProvider && agenticProvider !== codegenProvider) {
      requireProviderKey(errors, agenticProvider, "KELPCLAW_AGENTIC_PROVIDER");
    }
  }

  requireEnum(errors, "NANOCLAW_RUNNER", ["production", "mock"], "production");
  const runner = stringValue("NANOCLAW_RUNNER", "production");
  if (runner === "production") {
    await requireExecutable(errors, stringValue("NANOCLAW_DOCKER_BIN", "docker"));
    await requirePath(
      errors,
      "/var/run/docker.sock",
      "Docker socket mount is required for NanoClaw Docker nodes."
    );
  }

  await requireWritableDirectoryForFile(
    errors,
    stringValue("KELPCLAW_WORKFLOW_DB", "/data/workflow.sqlite"),
    "KELPCLAW_WORKFLOW_DB"
  );
  await requireWritableDirectoryForFile(
    errors,
    stringValue("KELPCLAW_SECRET_DB", stringValue("KELPCLAW_WORKFLOW_DB", "/data/workflow.sqlite")),
    "KELPCLAW_SECRET_DB"
  );
  await requireWritableDirectory(
    errors,
    stringValue("KELPCLAW_ARTIFACT_STORE", "/data/artifacts"),
    "KELPCLAW_ARTIFACT_STORE"
  );
  await requireWritableDirectory(
    errors,
    stringValue("NANOCLAW_HOST_WORKSPACE", "/workspace"),
    "NANOCLAW_HOST_WORKSPACE"
  );

  requireUrl(errors, "KELPCLAW_PUBLIC_BASE_URL", false);

  if (plannerMode === "deterministic" && plannerProvider !== "anthropic") {
    warnings.push("KELPCLAW_PLANNER_PROVIDER is ignored when KELPCLAW_PLANNER_MODE=deterministic.");
  }
}

function validateKelpClaw(errors) {
  requireUrl(errors, "KELPCLAW_API_TARGET", true);
  requireSecret(errors, "VITE_KELPCLAW_ADMIN_TOKEN", {
    placeholder: "change-me-admin-token",
    reason: "the browser client must send the same Bearer token expected by the API."
  });

  const apiToken = optionalString("KELPCLAW_ADMIN_TOKEN");
  const uiToken = optionalString("VITE_KELPCLAW_ADMIN_TOKEN");
  if (apiToken && uiToken && apiToken !== uiToken) {
    errors.push("KELPCLAW_ADMIN_TOKEN and VITE_KELPCLAW_ADMIN_TOKEN must match.");
  }
}

function requireProviderKey(errors, provider, providerVariable) {
  if (provider === "openai") {
    requireSecret(errors, "OPENAI_API_KEY", {
      reason: `${providerVariable}=openai requires OpenAI credentials.`
    });
  } else if (provider === "anthropic") {
    requireSecret(errors, "ANTHROPIC_API_KEY", {
      reason: `${providerVariable}=anthropic requires Anthropic credentials.`
    });
  }
}

function requireSecret(errors, name, options = {}) {
  const value = optionalString(name);
  if (!value) {
    errors.push(`${name} is required${options.reason ? `: ${options.reason}` : "."}`);
    return;
  }
  if (options.placeholder && value === options.placeholder) {
    errors.push(`${name} is still set to the example placeholder '${options.placeholder}'.`);
  }
  if (options.minLength && value.length < options.minLength) {
    errors.push(`${name} must be at least ${options.minLength} characters.`);
  }
}

function requireEnum(errors, name, allowed, defaultValue) {
  const value = stringValue(name, defaultValue);
  if (!allowed.includes(value)) {
    errors.push(`${name} must be one of: ${allowed.join(", ")}.`);
  }
}

function requireUrl(errors, name, required) {
  const value = optionalString(name);
  if (!value) {
    if (required) {
      errors.push(`${name} is required.`);
    }
    return;
  }
  try {
    new URL(value);
  } catch {
    errors.push(`${name} must be a valid URL.`);
  }
}

async function requireExecutable(errors, executable) {
  const paths = (process.env.PATH ?? "").split(":").filter(Boolean);
  for (const path of paths) {
    try {
      await access(`${path}/${executable}`, constants.X_OK);
      return;
    } catch {
      // Keep searching PATH.
    }
  }
  errors.push(`NANOCLAW_DOCKER_BIN '${executable}' was not found in PATH.`);
}

async function requirePath(errors, path, message) {
  try {
    await access(path, constants.R_OK | constants.W_OK);
  } catch {
    errors.push(message);
  }
}

async function requireWritableDirectoryForFile(errors, filePath, name) {
  await requireWritableDirectory(errors, dirname(filePath), `${name} parent directory`);
}

async function requireWritableDirectory(errors, directory, name) {
  try {
    await mkdir(directory, { recursive: true });
    await access(directory, constants.R_OK | constants.W_OK);
  } catch {
    errors.push(`${name} '${directory}' must be writable by the container.`);
  }
}

function optionalString(name) {
  const value = process.env[name]?.trim();
  return value && value.length > 0 ? value : undefined;
}

function stringValue(name, fallback) {
  return optionalString(name) ?? fallback;
}

function execCommand(commandParts) {
  const child = spawn(commandParts[0], commandParts.slice(1), {
    stdio: "inherit",
    env: process.env
  });

  const forward = (signal) => {
    child.kill(signal);
  };
  process.on("SIGINT", forward);
  process.on("SIGTERM", forward);

  child.on("exit", (code, signal) => {
    if (signal) {
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code ?? 1);
  });
}
