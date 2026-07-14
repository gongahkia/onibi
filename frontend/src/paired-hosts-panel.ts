import {
  type PairedHost,
  type PairedHostEndpointKind,
  PairedHostRegistry,
  pairedHostRegistryVersion
} from "./paired-hosts";

type Registry = Pick<
  PairedHostRegistry,
  "list" | "put" | "revoke" | "exportEncrypted" | "importEncrypted"
>;

export class PairedHostsPanel {
  readonly element = document.createElement("button");
  private modal: HTMLElement | undefined;
  private hosts: PairedHost[] = [];
  private status = "";
  private transfer = "";
  private revokeArmedID = "";

  constructor(
    private readonly root: HTMLElement,
    private readonly toast: (message: string) => void,
    private readonly registry: Registry = new PairedHostRegistry()
  ) {
    this.element.type = "button";
    this.element.className = "control-button";
    this.element.textContent = "Hosts";
    this.element.addEventListener("click", () => this.open());
  }

  open(): void {
    this.status = "";
    this.revokeArmedID = "";
    this.modal?.remove();
    this.modal = document.createElement("div");
    this.modal.className = "share-modal";
    this.modal.addEventListener("click", (event) => {
      if (event.target === this.modal) {
        this.close();
      }
    });
    this.root.append(this.modal);
    this.render();
    void this.refresh();
  }

  private close(): void {
    this.transfer = "";
    this.modal?.remove();
    this.modal = undefined;
  }

  private async refresh(): Promise<void> {
    try {
      this.hosts = await this.registry.list();
      this.status = "";
    } catch (error) {
      this.status = errorMessage(error, "paired hosts unavailable");
    }
    this.render();
  }

  private render(): void {
    if (this.modal === undefined) {
      return;
    }
    const shell = document.createElement("section");
    shell.className = "share-form paired-hosts-form";
    const header = document.createElement("div");
    header.className = "share-header";
    const title = document.createElement("div");
    title.className = "share-title";
    title.textContent = "paired hosts";
    const reload = button("Reload", "secondary");
    reload.addEventListener("click", () => void this.refresh());
    const close = button("Close", "secondary");
    close.addEventListener("click", () => this.close());
    header.append(title, reload, close);
    shell.append(
      header,
      this.hostList(),
      this.addForm(),
      this.transferForm(),
      statusLine(this.status)
    );
    this.modal.replaceChildren(shell);
  }

  private hostList(): HTMLElement {
    const wrap = document.createElement("div");
    wrap.className = "share-viewers";
    if (this.hosts.length === 0) {
      wrap.append(empty("no paired hosts"));
      return wrap;
    }
    for (const host of this.hosts) {
      const row = document.createElement("div");
      row.className = "share-viewer-row";
      const details = document.createElement("div");
      details.className = "share-viewer-main";
      const name = document.createElement("div");
      name.className = "share-viewer-id";
      name.textContent = host.displayName;
      name.title = host.id;
      const endpoint = document.createElement("div");
      endpoint.className = "share-viewer-meta";
      endpoint.textContent = `${host.state} / ${host.endpoint.kind} / ${host.endpoint.url}`;
      const identity = document.createElement("div");
      identity.className = "share-viewer-meta";
      identity.textContent = `identity ${host.identityPublic}`;
      details.append(name, endpoint, identity);
      const revoke = button(host.state === "revoked" ? "Revoked" : "Revoke", "danger");
      revoke.disabled = host.state === "revoked";
      revoke.addEventListener("click", () => void this.revoke(host.id));
      row.append(details, revoke);
      wrap.append(row);
    }
    return wrap;
  }

  private addForm(): HTMLFormElement {
    const form = document.createElement("form");
    form.className = "paired-hosts-fields";
    const id = input("host ID", "text");
    const name = input("display name", "text");
    const kind = select("endpoint type", ["mesh", "ssh", "relay"]);
    const endpoint = input("endpoint", "text");
    const identity = document.createElement("textarea");
    identity.className = "share-url paired-hosts-identity";
    identity.name = "identity public";
    identity.required = true;
    identity.autocomplete = "off";
    const save = button("Save host", "primary");
    save.type = "submit";
    form.append(
      field("host ID", id),
      field("display name", name),
      field("endpoint type", kind),
      field("endpoint", endpoint),
      field("identity public", identity),
      save
    );
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      void this.saveHost({
        version: pairedHostRegistryVersion,
        id: id.value,
        displayName: name.value,
        endpoint: { kind: kind.value as PairedHostEndpointKind, url: endpoint.value },
        identityPublic: identity.value,
        state: "active"
      });
    });
    return form;
  }

  private transferForm(): HTMLFormElement {
    const form = document.createElement("form");
    form.className = "paired-hosts-fields";
    const passphrase = input("transfer passphrase", "password");
    passphrase.minLength = 12;
    const payload = document.createElement("textarea");
    payload.className = "share-url paired-hosts-transfer";
    payload.name = "encrypted host registry";
    payload.value = this.transfer;
    payload.autocomplete = "off";
    const exportButton = button("Export", "secondary");
    exportButton.addEventListener("click", () => void this.exportRegistry(passphrase.value));
    const importButton = button("Import", "primary");
    importButton.addEventListener(
      "click",
      () => void this.importRegistry(payload.value, passphrase.value)
    );
    form.append(
      field("transfer passphrase", passphrase),
      field("encrypted registry", payload),
      exportButton,
      importButton
    );
    form.addEventListener("submit", (event) => event.preventDefault());
    return form;
  }

  private async saveHost(host: PairedHost): Promise<void> {
    this.status = "saving host";
    this.render();
    try {
      await this.registry.put(host);
      this.toast("Paired host saved.");
      await this.refresh();
    } catch (error) {
      this.status = errorMessage(error, "paired host save failed");
      this.render();
    }
  }

  private async revoke(id: string): Promise<void> {
    if (this.revokeArmedID !== id) {
      this.revokeArmedID = id;
      this.status = "tap Revoke again to revoke this local host record";
      this.render();
      return;
    }
    this.status = "revoking host";
    this.render();
    try {
      await this.registry.revoke(id);
      this.revokeArmedID = "";
      this.toast("Paired host revoked locally.");
      await this.refresh();
    } catch (error) {
      this.status = errorMessage(error, "paired host revoke failed");
      this.render();
    }
  }

  private async exportRegistry(passphrase: string): Promise<void> {
    try {
      this.transfer = await this.registry.exportEncrypted(passphrase);
      this.status = "encrypted registry ready to transfer";
      this.render();
    } catch (error) {
      this.status = errorMessage(error, "paired host export failed");
      this.render();
    }
  }

  private async importRegistry(serialized: string, passphrase: string): Promise<void> {
    try {
      const count = await this.registry.importEncrypted(serialized, passphrase);
      this.transfer = "";
      this.toast(
        count === 0 ? "Paired host registry already current." : `Imported ${count} paired host.`
      );
      await this.refresh();
    } catch (error) {
      this.status = errorMessage(error, "paired host import failed");
      this.render();
    }
  }
}

function field(label: string, control: HTMLElement): HTMLElement {
  const wrap = document.createElement("label");
  wrap.className = "share-field";
  const text = document.createElement("span");
  text.textContent = label;
  wrap.append(text, control);
  return wrap;
}

function input(label: string, type: string): HTMLInputElement {
  const el = document.createElement("input");
  el.className = "share-url";
  el.type = type;
  el.name = label;
  el.required = true;
  el.autocomplete = "off";
  return el;
}

function select(label: string, values: PairedHostEndpointKind[]): HTMLSelectElement {
  const el = document.createElement("select");
  el.className = "share-select";
  el.name = label;
  for (const value of values) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = value;
    el.append(option);
  }
  return el;
}

function button(label: string, variant: "primary" | "secondary" | "danger"): HTMLButtonElement {
  const el = document.createElement("button");
  el.type = "button";
  el.className = `approval-button ${variant === "danger" ? "danger" : variant}`;
  el.textContent = label;
  return el;
}

function empty(text: string): HTMLElement {
  const el = document.createElement("div");
  el.className = "share-empty";
  el.textContent = text;
  return el;
}

function statusLine(status: string): HTMLElement {
  const el = document.createElement("div");
  el.className = "share-status";
  el.textContent = status;
  return el;
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}
