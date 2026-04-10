const fastify = require("fastify")();

fastify.get("/echo", async function echo(request, reply) {
  const name = request.query.name;
  reply.send(`<p>${name}</p>`);
});
