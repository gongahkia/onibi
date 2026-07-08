export type WorkspaceSummary = {
  name: string;
  path: string;
  last_seen: string;
  default_transport?: string;
  current: boolean;
};

export type WorkspaceState = {
  current: string;
  workspaces: WorkspaceSummary[];
};

type FetchJSON = <T>(path: string) => Promise<T>;
type PostJSON = (path: string, body: Record<string, unknown>) => Promise<Response>;

export class WorkspaceSwitcher {
  private state: WorkspaceState = { current: "", workspaces: [] };
  private open = false;

  constructor(
    private readonly root: HTMLElement,
    private readonly fetchJSON: FetchJSON,
    private readonly postJSON: PostJSON,
    private readonly onSwitch: (name: string) => void,
    private readonly showToast: (message: string) => void
  ) {}

  async load(): Promise<void> {
    try {
      this.state = await this.fetchJSON<WorkspaceState>("/workspaces");
    } catch {
      this.state = { current: "", workspaces: [] };
    }
    this.render();
  }

  current(): string {
    return this.state.current;
  }

  private render(): void {
    const shell = document.createElement("div");
    shell.className = "workspace-switcher";
    const button = document.createElement("button");
    button.type = "button";
    button.className = "workspace-pill";
    button.textContent = this.label();
    button.title = "Workspace";
    button.addEventListener("click", () => {
      this.open = !this.open;
      this.render();
    });
    shell.append(button);
    if (this.open) {
      shell.append(this.menu());
    }
    this.root.replaceChildren(shell);
  }

  private menu(): HTMLElement {
    const menu = document.createElement("div");
    menu.className = "workspace-menu";
    if (this.state.workspaces.length === 0) {
      const empty = document.createElement("div");
      empty.className = "workspace-empty";
      empty.textContent = "no workspaces";
      menu.append(empty);
      return menu;
    }
    for (const workspace of this.state.workspaces) {
      const item = document.createElement("button");
      item.type = "button";
      item.className =
        workspace.name === this.state.current ? "workspace-option active" : "workspace-option";
      item.title = workspace.path;
      const name = document.createElement("span");
      name.className = "workspace-option-name";
      name.textContent = workspace.name;
      const meta = document.createElement("span");
      meta.className = "workspace-option-meta";
      meta.textContent = workspace.default_transport ?? workspace.path;
      item.append(name, meta);
      item.addEventListener("click", () => void this.switchTo(workspace.name));
      menu.append(item);
    }
    return menu;
  }

  private async switchTo(name: string): Promise<void> {
    try {
      const response = await this.postJSON("/workspaces", { name });
      if (!response.ok) {
        throw new Error(`workspace ${response.status}`);
      }
      this.state = (await response.json()) as WorkspaceState;
      this.open = false;
      this.onSwitch(this.state.current);
      this.render();
    } catch (err) {
      this.showToast(err instanceof Error ? err.message : "workspace switch failed");
    }
  }

  private label(): string {
    const current = this.state.current.trim();
    return current === "" ? "workspace" : current;
  }
}
