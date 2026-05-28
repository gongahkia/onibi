import { EmptyState } from "./EmptyState";
import { EditorBuffer } from "./EditorBuffer";
import { TerminalView } from "./TerminalView";
import { useSessionStore } from "../lib/sessions";

export function MainPane() {
  const selectedFile = useSessionStore((state) => state.selectedFile);
  const sessions = useSessionStore((state) => state.sessions);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const settings = useSessionStore((state) => state.settings);
  const session = sessions.find((item) => item.id === activeSessionId) ?? null;

  if (session) {
    const terminalVisible = selectedFile === null;
    return (
      <main
        className="main-pane main-pane-stacked"
        data-testid={selectedFile ? "main-pane-editor" : "main-pane-terminal"}
      >
        <section
          className={`main-pane-surface terminal-surface ${
            terminalVisible ? "is-active" : "is-background"
          }`}
          aria-hidden={!terminalVisible}
        >
          <TerminalView
            ptyId={session.id}
            fontFamily={settings.terminalFontFamily}
            fontSize={settings.fontSize}
            settings={settings}
            visible={terminalVisible}
          />
        </section>
        {selectedFile ? (
          <section className="main-pane-surface editor-surface">
            <EditorBuffer
              path={selectedFile.path}
              workspaceRoot={selectedFile.workspaceRoot}
              fontFamily={settings.editorFontFamily}
            />
          </section>
        ) : null}
      </main>
    );
  }

  if (selectedFile) {
    return (
      <main className="main-pane" data-testid="main-pane-editor">
        <EditorBuffer
          path={selectedFile.path}
          workspaceRoot={selectedFile.workspaceRoot}
          fontFamily={settings.editorFontFamily}
        />
      </main>
    );
  }

  return (
    <main className="main-pane" data-testid="main-pane-empty">
      <EmptyState />
    </main>
  );
}
