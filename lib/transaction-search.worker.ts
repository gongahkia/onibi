type SearchEntry = { id: string; text: string };

let entries: SearchEntry[] = [];

self.onmessage = (event: MessageEvent<{ type: "index"; entries: SearchEntry[] } | { type: "query"; query: string }>) => {
  if (event.data.type === "index") {
    entries = event.data.entries;
    return;
  }
  const terms = event.data.query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  const ids = !terms.length ? [] : entries.filter((entry) => terms.every((term) => entry.text.includes(term))).map((entry) => entry.id);
  self.postMessage({ query: event.data.query, ids });
};
