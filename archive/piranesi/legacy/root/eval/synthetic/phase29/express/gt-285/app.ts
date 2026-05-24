import { stashLookup } from "./middleware";
import { buildLookup } from "./service";

export function search(
  req: { body: { name: string } },
  res: { locals: Record<string, string> },
) {
  stashLookup(req, res);
  return buildLookup(res.locals.lookup);
}
