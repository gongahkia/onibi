import { HttpClient } from "./client";

export class ProxyService {
  private readonly client = new HttpClient();

  proxy(url: string) {
    const endpoint = String(url.trim());
    return this.client.request(endpoint);
  }
}
