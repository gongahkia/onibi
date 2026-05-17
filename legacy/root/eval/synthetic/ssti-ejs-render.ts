import ejs from "ejs";

export function handler(req) {
  return ejs.render(req.body.template, { name: "piranesi" });
}
