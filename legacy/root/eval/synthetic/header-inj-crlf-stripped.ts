export const handler = (req, res) => {
              const safe = req.query.val.replace(/[
]/g, "");
              res.setHeader("X-Custom", safe);
            };
