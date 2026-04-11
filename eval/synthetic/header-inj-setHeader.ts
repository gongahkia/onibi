export const handler = (req, res) => {
  res.setHeader("X-Custom", req.query.val);
};
