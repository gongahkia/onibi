export const handler = (req, escapeStringRegexp) => {
  return new RegExp(escapeStringRegexp(req.query.q));
};
