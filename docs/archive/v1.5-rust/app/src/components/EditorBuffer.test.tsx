import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { EditorView } from "@codemirror/view";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { EditorBuffer } from "./EditorBuffer";
import { DEFAULT_SETTINGS, useSessionStore } from "../lib/sessions";

vi.mock("mermaid", () => ({
  default: {
    initialize: vi.fn(),
    render: vi.fn(async (_id: string, chart: string) => ({
      svg: `<svg data-testid="mermaid-svg"><text>${chart}</text></svg>`,
    })),
  },
}));

function resetStore() {
  useSessionStore.setState({
    hydrated: true,
    sessions: [],
    activeSessionId: null,
    workspaceTabs: [],
    activeWorkspaceId: null,
    activeWorkspaceTabId: null,
    workspaces: [],
    selectedFile: null,
    settings: DEFAULT_SETTINGS,
  });
  globalThis.__TAURI_MOCKS__.invoke.mockReset();
}

describe("EditorBuffer", () => {
  beforeEach(() => {
    resetStore();
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  test("marks dirty and saves edited text", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce([104, 105]);
    render(<EditorBuffer path="/repo/a.txt" workspaceRoot="/repo" />);

    const editor = await screen.findByLabelText("Editor buffer");
    expect(document.querySelector(".cm-lineNumbers")).toBeTruthy();
    const view = EditorView.findFromDOM(editor as HTMLElement);
    expect(view).toBeTruthy();
    act(() => {
      view?.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: "hello" },
      });
    });
    expect(screen.getByLabelText("dirty")).toBeTruthy();

    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => {
      expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
        "fs_write_file",
        {
          root: "/repo",
          path: "/repo/a.txt",
          data: [104, 101, 108, 108, 111],
        },
      );
    });
  });

  test("remeasures CodeMirror when a hidden buffer becomes active", async () => {
    const measureSpy = vi.spyOn(EditorView.prototype, "requestMeasure");
    const key = "file:workspace:/repo:/repo/a.txt";
    useSessionStore.setState({ activeBufferKey: "other-buffer" });
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce([104, 105]);

    render(<EditorBuffer path="/repo/a.txt" workspaceRoot="/repo" bufferKey={key} />);
    await screen.findByLabelText("Editor buffer");
    measureSpy.mockClear();

    act(() => {
      useSessionStore.setState({ activeBufferKey: key });
    });

    await waitFor(() => expect(measureSpy).toHaveBeenCalled());
    measureSpy.mockRestore();
  });

  test("refuses binary files", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce([0, 1, 2]);
    render(<EditorBuffer path="/repo/blob.bin" workspaceRoot="/repo" />);

    expect(await screen.findByText("Cannot edit binary file.")).toBeTruthy();
  });

  test("renders markdown files with a preview pane", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce(
      Array.from(
        new TextEncoder().encode(
          "# Title\n\n- [x] item\n\n[OpenAI](https://openai.com)\n\n<strong>raw html</strong>",
        ),
      ),
    );
    render(<EditorBuffer path="/repo/README.md" workspaceRoot="/repo" />);

    const preview = await screen.findByLabelText("Markdown preview");
    expect(within(preview).getByText("Title").tagName).toBe("H1");
    expect(
      within(preview).getByRole("link", { name: "OpenAI" }).getAttribute("href"),
    ).toBe("https://openai.com");
    expect(within(preview).getByText("raw html").tagName).toBe("STRONG");
  });

  test("renders mermaid fenced code blocks as diagrams", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce(
      Array.from(
        new TextEncoder().encode(
          "```mermaid\nflowchart TD\n  A --> B\n```\n\n```ts\nconst value = 1\n```",
        ),
      ),
    );
    render(<EditorBuffer path="/repo/README.md" workspaceRoot="/repo" />);

    const preview = await screen.findByLabelText("Markdown preview");
    const diagram = await within(preview).findByLabelText("Mermaid diagram");

    await waitFor(() => expect(diagram.innerHTML).toContain("data-testid=\"mermaid-svg\""));
    expect(preview.querySelector("pre code.language-mermaid")).toBeNull();
    expect(preview.querySelector("pre code.language-ts")).toBeTruthy();
  });

  test("resolves local markdown images through workspace previews", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockImplementation(async (command: string) => {
      if (command === "fs_read_file") {
        return Array.from(
          new TextEncoder().encode(
            '# Title\n\n<img src="assets/logo.png" alt="Local logo" />',
          ),
        );
      }
      if (command === "fs_read_preview_file") {
        return [137, 80, 78, 71];
      }
      return null;
    });
    render(<EditorBuffer path="/repo/docs/README.md" workspaceRoot="/repo" />);

    const image = await screen.findByAltText("Local logo");

    await waitFor(() => expect(image.getAttribute("src")).toContain("blob:"));
    expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
      "fs_read_preview_file",
      { root: "/repo", path: "/repo/docs/assets/logo.png" },
    );
  });

  test("syncs markdown editor and preview scrolling", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce(
      Array.from(new TextEncoder().encode("# Title\n\n".repeat(80))),
    );
    render(<EditorBuffer path="/repo/README.md" workspaceRoot="/repo" />);

    const preview = await screen.findByLabelText("Markdown preview");
    await waitFor(() => expect(document.querySelector(".cm-scroller")).toBeTruthy());
    const scroller = document.querySelector(".cm-scroller") as HTMLElement;
    Object.defineProperty(scroller, "scrollHeight", { value: 1000, configurable: true });
    Object.defineProperty(scroller, "clientHeight", { value: 500, configurable: true });
    Object.defineProperty(preview, "scrollHeight", { value: 2000, configurable: true });
    Object.defineProperty(preview, "clientHeight", { value: 500, configurable: true });

    scroller.scrollTop = 250;
    fireEvent.scroll(scroller);

    await waitFor(() => expect(preview.scrollTop).toBe(750));

    preview.scrollTop = 375;
    fireEvent.scroll(preview);

    await waitFor(() => expect(scroller.scrollTop).toBe(125));
  });

  test("renders image files as previews, including ico", async () => {
    globalThis.__TAURI_MOCKS__.invoke.mockResolvedValueOnce([137, 80, 78, 71]);
    render(<EditorBuffer path="/repo/favicon.ico" workspaceRoot="/repo" />);

    const image = await screen.findByAltText("/repo/favicon.ico");
    expect(image.getAttribute("src")).toContain("blob:");
    expect(globalThis.__TAURI_MOCKS__.invoke).toHaveBeenCalledWith(
      "fs_read_preview_file",
      { root: "/repo", path: "/repo/favicon.ico" },
    );
    expect((vi.mocked(URL.createObjectURL).mock.calls[0][0] as Blob).type).toBe(
      "image/x-icon",
    );
  });
});
