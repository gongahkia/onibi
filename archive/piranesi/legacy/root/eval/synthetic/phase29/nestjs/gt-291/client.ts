declare function fetch(url: string): Promise<unknown>;

export class HttpClient {
  request(endpoint: string) {
    return fetch(endpoint); // SINK
  }
}
