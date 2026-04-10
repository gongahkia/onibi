declare function fetchMarkup(
  html: string,
  cb: (err: Error | null, data: string) => void,
): void;

const res = {
  send(body: string) {
    return body;
  },
};

export function callbackXss(req: { body: { preview: string } }) {
  fetchMarkup(req.body.preview, (_err, html) => {
    res.send(html);
  });
}
