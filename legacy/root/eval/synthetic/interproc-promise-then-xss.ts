declare function renderPreview(markup: string): Promise<string>;

const res = {
  send(body: string) {
    return body;
  },
};

export function promiseThenXss(req: { body: { preview: string } }) {
  return renderPreview(req.body.preview).then((html) => {
    res.send(html);
    return html;
  });
}
