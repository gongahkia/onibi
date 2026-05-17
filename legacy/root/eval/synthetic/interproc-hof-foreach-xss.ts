const res = {
  send(body: string) {
    return body;
  },
};

export function hofForEachXss(req: { body: { messages: string[] } }) {
  req.body.messages.forEach((message) => {
    res.send(message);
  });
}
