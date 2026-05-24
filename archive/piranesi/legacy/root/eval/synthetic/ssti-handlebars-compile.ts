import Handlebars from "handlebars";

export function handler(req) {
  return Handlebars.compile(req.body.tpl)({ name: "piranesi" });
}
