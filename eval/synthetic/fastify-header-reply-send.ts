const fastify = require("fastify")();

fastify.get("/headers", async function headers(request, reply) {
  const host = request.headers["x-forwarded-host"];
  reply.send(host);
});
