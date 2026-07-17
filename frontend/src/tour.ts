const doneKey = "onibi-tour-done";
const maxDurationMs = 60_000;

type TourStep = {
  title: string;
  body: string;
  targets: string[];
};

const steps: TourStep[] = [
  {
    title: "Pair flow",
    body: "Start from the paired session list, then open a session.",
    targets: ["#session-list"]
  },
  {
    title: "Terminal mount",
    body: "The live session terminal mounts here.",
    targets: ["#term"]
  },
  {
    title: "Intervene",
    body: "ACT opens acknowledged interrupt, input, handoff, and kill controls.",
    targets: ['[data-tour="intervention"]', "#toolbar"]
  },
  {
    title: "Approval card",
    body: "Tool approval cards appear above the soft-key bar.",
    targets: [".approval-card", "#approval-overlay"]
  },
  {
    title: "Soft-key bar",
    body: "Terminal keys stay pinned for mobile input.",
    targets: ["#softkeys"]
  },
  {
    title: "Done",
    body: "Tour complete.",
    targets: ["#toolbar", "#softkeys", "#term"]
  }
];

export function startFirstRunTour(): void {
  if (isDone() || document.querySelector(".tour-root") !== null) {
    return;
  }
  new FirstRunTour().start();
}

class FirstRunTour {
  private index = 0;
  private readonly root = document.createElement("div");
  private readonly highlight = document.createElement("div");
  private readonly card = document.createElement("section");
  private readonly count = document.createElement("div");
  private readonly title = document.createElement("div");
  private readonly body = document.createElement("div");
  private readonly back = document.createElement("button");
  private readonly next = document.createElement("button");
  private readonly skip = document.createElement("button");
  private doneTimer = 0;

  start(): void {
    this.root.className = "tour-root";
    this.highlight.className = "tour-highlight";
    this.card.className = "tour-card";
    this.card.setAttribute("role", "dialog");
    this.card.setAttribute("aria-label", "Onibi first-run tour");
    this.count.className = "tour-count";
    this.title.className = "tour-title";
    this.body.className = "tour-body";
    const actions = document.createElement("div");
    actions.className = "tour-actions";
    this.back.type = "button";
    this.back.className = "tour-button";
    this.back.textContent = "Back";
    this.next.type = "button";
    this.next.className = "tour-button primary";
    this.skip.type = "button";
    this.skip.className = "tour-skip";
    this.skip.textContent = "Skip";
    actions.append(this.back, this.next);
    this.card.append(this.count, this.title, this.body, actions);
    this.root.append(this.highlight, this.card, this.skip);
    document.body.append(this.root);
    this.back.addEventListener("click", () => this.previous());
    this.next.addEventListener("click", () => this.advance());
    this.skip.addEventListener("click", () => this.finish());
    window.addEventListener("resize", this.reposition);
    window.addEventListener("scroll", this.reposition, true);
    window.addEventListener("keydown", this.keydown);
    this.doneTimer = window.setTimeout(() => this.finish(), maxDurationMs);
    this.render();
  }

  private readonly reposition = (): void => {
    this.place(findTarget(steps[this.index].targets));
  };

  private readonly keydown = (event: KeyboardEvent): void => {
    if (event.key === "Escape") {
      event.preventDefault();
      this.finish();
    }
  };

  private previous(): void {
    if (this.index > 0) {
      this.index -= 1;
      this.render();
    }
  }

  private advance(): void {
    if (this.index >= steps.length - 1) {
      this.finish();
      return;
    }
    this.index += 1;
    this.render();
  }

  private finish(): void {
    markDone();
    window.clearTimeout(this.doneTimer);
    window.removeEventListener("resize", this.reposition);
    window.removeEventListener("scroll", this.reposition, true);
    window.removeEventListener("keydown", this.keydown);
    this.root.remove();
  }

  private render(): void {
    const step = steps[this.index];
    this.count.textContent = `${this.index + 1}/${steps.length}`;
    this.title.textContent = step.title;
    this.body.textContent = step.body;
    this.back.disabled = this.index === 0;
    this.next.textContent = this.index === steps.length - 1 ? "Done" : "Next";
    window.requestAnimationFrame(() => this.place(findTarget(step.targets)));
  }

  private place(target: HTMLElement | undefined): void {
    const viewport = window.visualViewport;
    const leftEdge = viewport?.offsetLeft ?? 0;
    const topEdge = viewport?.offsetTop ?? 0;
    const width = viewport?.width ?? window.innerWidth;
    const height = viewport?.height ?? window.innerHeight;
    const margin = 14;
    const cardRect = this.card.getBoundingClientRect();
    const targetRect = target?.getBoundingClientRect();
    if (targetRect === undefined) {
      this.highlight.hidden = true;
      this.card.style.left = `${leftEdge + Math.max(margin, (width - cardRect.width) / 2)}px`;
      this.card.style.top = `${topEdge + Math.max(margin, (height - cardRect.height) / 2)}px`;
      return;
    }
    this.highlight.hidden = false;
    const pad = 5;
    const highLeft = clamp(targetRect.left - pad, leftEdge + margin, leftEdge + width - margin);
    const highTop = clamp(targetRect.top - pad, topEdge + margin, topEdge + height - margin);
    const highWidth = Math.max(
      0,
      Math.min(targetRect.width + pad * 2, leftEdge + width - margin - highLeft)
    );
    const highHeight = Math.max(
      0,
      Math.min(targetRect.height + pad * 2, topEdge + height - margin - highTop)
    );
    this.highlight.style.left = `${highLeft}px`;
    this.highlight.style.top = `${highTop}px`;
    this.highlight.style.width = `${highWidth}px`;
    this.highlight.style.height = `${highHeight}px`;
    let top = targetRect.bottom + margin;
    if (top + cardRect.height > topEdge + height - margin) {
      top = targetRect.top - cardRect.height - margin;
    }
    if (top < topEdge + margin) {
      top = topEdge + margin;
    }
    const left = clamp(
      targetRect.left,
      leftEdge + margin,
      leftEdge + width - cardRect.width - margin
    );
    this.card.style.left = `${left}px`;
    this.card.style.top = `${top}px`;
  }
}

function findTarget(selectors: string[]): HTMLElement | undefined {
  for (const selector of selectors) {
    const matches = document.querySelectorAll<HTMLElement>(selector);
    for (let i = 0; i < matches.length; i += 1) {
      const el = matches[i];
      if (isVisible(el)) {
        return el;
      }
    }
  }
  return undefined;
}

function isVisible(el: HTMLElement): boolean {
  const style = window.getComputedStyle(el);
  if (el.hidden || style.display === "none" || style.visibility === "hidden") {
    return false;
  }
  const rect = el.getBoundingClientRect();
  return rect.width > 1 && rect.height > 1;
}

function clamp(value: number, min: number, max: number): number {
  if (max < min) {
    return min;
  }
  return Math.min(Math.max(value, min), max);
}

function isDone(): boolean {
  try {
    return window.localStorage.getItem(doneKey) !== null;
  } catch {
    return true;
  }
}

function markDone(): void {
  try {
    window.localStorage.setItem(doneKey, "1");
  } catch {
    return;
  }
}
