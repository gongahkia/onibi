import { runCrossModuleLookup } from "./cross_module_helper";

export function crossModuleFlow(req: { body: { userId: string } }) {
  const userId = req.body.userId;
  // @piranesi-expect-clean: cross-module taint not tracked by Joern
  return runCrossModuleLookup(userId);
}
