import ejs from "ejs";

export function handler(req) {
  return ejs.render("<h1><%= name %></h1>", { name: req.body.name });
}
