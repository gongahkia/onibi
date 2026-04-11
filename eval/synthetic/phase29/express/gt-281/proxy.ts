declare function fetch(url: string): Promise<unknown>;

export function requestRemote(endpoint: string) {
  return fetch(endpoint); // SINK
}
