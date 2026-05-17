const fastify = require("fastify")();

fastify.post("/validated", {
  schema: {
    body: {
      type: "object",
      required: ["name"],
      properties: {
        name: { type: "string", maxLength: 50 },
      },
    },
  },
}, async function validated(request, reply) {
  const name = request.body.name;
  reply.send(name);
});
