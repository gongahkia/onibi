import { captureTarget } from "./middleware";
import { launchProbe } from "./service";

export function runHeaderProbe(
  req: { headers: Record<string, string | undefined> },
  res: { locals: Record<string, string> },
) {
  captureTarget(req, res);
  return launchProbe(res.locals.target);
}
