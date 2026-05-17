export const handler = (req) => {
  return new RegExp(req.query.q);
};
