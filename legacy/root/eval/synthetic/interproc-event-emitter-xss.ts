type Handler = (payload: string) => void;

class Emitter {
  on(_event: string, _handler: Handler) {}

  emit(_event: string, _payload: string) {}
}

const emitter = new Emitter();
const res = {
  send(body: string) {
    return body;
  },
};

emitter.on("preview", (payload) => {
  res.send(payload);
});

export function eventEmitterXss(req: { body: { preview: string } }) {
  emitter.emit("preview", req.body.preview);
}
