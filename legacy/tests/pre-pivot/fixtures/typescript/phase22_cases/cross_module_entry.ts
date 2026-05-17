import { runCrossModuleLookup } from "./cross_module_helper";

export function crossModuleSummary(req: { body: { sql: string } }) {
  // @piranesi-expect: CWE-89, source=req.body.sql, sink=db.query
  return runCrossModuleLookup(req.body.sql);
}
