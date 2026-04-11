export function captureNotice(
  req: { query: { notice: string } },
  res: { locals: Record<string, string> },
) {
  res.locals.notice = req.query.notice;
}
