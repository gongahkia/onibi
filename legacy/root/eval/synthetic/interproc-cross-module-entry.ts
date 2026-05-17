import { runCrossModuleLookup } from "./interproc-cross-module-helper";

export function crossModuleSqli(req: { body: { sql: string } }) {
  return runCrossModuleLookup(req.body.sql);
}
