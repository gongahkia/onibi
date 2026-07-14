import { expect, test } from "vitest";

import type { PairedHost } from "../paired-hosts";
import { PairedHostsPanel } from "../paired-hosts-panel";

class FakeRegistry {
  hosts: PairedHost[] = [];

  async list(): Promise<PairedHost[]> {
    return [...this.hosts];
  }

  async put(host: PairedHost): Promise<void> {
    this.hosts = [...this.hosts.filter((item) => item.id !== host.id), host];
  }

  async revoke(id: string): Promise<void> {
    this.hosts = this.hosts.map((host) => (host.id === id ? { ...host, state: "revoked" } : host));
  }

  async exportEncrypted(passphrase: string): Promise<string> {
    if (passphrase.length < 12) {
      throw new Error("passphrase too short");
    }
    return "encrypted-registry";
  }

  async importEncrypted(serialized: string, passphrase: string): Promise<number> {
    if (serialized !== "encrypted-registry" || passphrase.length < 12) {
      throw new Error("import failed");
    }
    return 1;
  }
}

test("paired hosts panel saves, inspects, revokes, and transfers local records", async () => {
  document.body.replaceChildren();
  const root = document.createElement("main");
  document.body.append(root);
  const registry = new FakeRegistry();
  const toast: string[] = [];
  const panel = new PairedHostsPanel(root, (message) => toast.push(message), registry);
  root.append(panel.element);

  panel.element.click();
  await settle();
  setInput("host ID", "host-mini");
  setInput("display name", "Mac mini");
  setInput("endpoint", "https://mini.tailnet.ts.net");
  const identity = document.querySelector<HTMLTextAreaElement>('textarea[name="identity public"]');
  if (identity === null) {
    throw new Error("identity field missing");
  }
  identity.value = "public-key";
  submit(document.querySelector("form.paired-hosts-fields")!);
  await settle();

  expect(root.textContent).toContain("Mac mini");
  expect(root.textContent).toContain("identity public-key");
  expect(registry.hosts).toHaveLength(1);
  clickButton("Revoke");
  clickButton("Revoke");
  await settle();
  expect(root.textContent).toContain("revoked / mesh");
  expect(toast).toContain("Paired host revoked locally.");

  setInput("transfer passphrase", "transfer passphrase");
  clickButton("Export");
  await settle();
  const transfer = document.querySelector<HTMLTextAreaElement>(
    'textarea[name="encrypted host registry"]'
  );
  expect(transfer?.value).toBe("encrypted-registry");
  setInput("transfer passphrase", "transfer passphrase");
  clickButton("Import");
  await settle();
  expect(toast).toContain("Imported 1 paired host.");
});

function setInput(name: string, value: string): void {
  const input = document.querySelector<HTMLInputElement>(`input[name="${name}"]`);
  if (input === null) {
    throw new Error(`missing ${name}`);
  }
  input.value = value;
}

function submit(form: Element): void {
  form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
}

function clickButton(label: string): void {
  const button = Array.from(document.querySelectorAll("button")).find(
    (element) => element.textContent === label
  );
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error(`missing ${label} button`);
  }
  button.click();
}

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
  await Promise.resolve();
}
