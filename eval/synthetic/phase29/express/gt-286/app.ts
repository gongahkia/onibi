import { captureNotice } from "./flash";
import { renderNotice } from "./view";

export function banner(
  req: { query: { notice: string } },
  res: { locals: Record<string, string>; send(value: string): string },
) {
  captureNotice(req, res);
  return res.send(renderNotice(res.locals.notice)); // SINK
}
