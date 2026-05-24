export const handler = (req, res) => {
  res.redirect(req.query.url);
};
