import nunjucks from "nunjucks";

export function handler(req) {
  return nunjucks.renderString(req.body.tpl, {});
}
