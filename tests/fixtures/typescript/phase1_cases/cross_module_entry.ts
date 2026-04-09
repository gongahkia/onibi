import { runCrossModuleLookup } from "./cross_module_helper";

export function crossModuleFlow(req: { body: { userId: string } }) {
  const userId = req.body.userId;
  // @piranesi-expect: CWE-89, source=req.body.userId, sink=db.query
  return runCrossModuleLookup(userId);
}
