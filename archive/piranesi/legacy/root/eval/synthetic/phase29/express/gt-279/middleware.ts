export function captureTarget(
  req: { headers: Record<string, string | undefined> },
  res: { locals: Record<string, string> },
) {
  res.locals.target = req.headers["x-target"] ?? "";
}
