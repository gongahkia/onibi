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

  if (selectedFile) {
    return (
      <main className="main-pane" data-testid="main-pane-editor">
        <EditorBuffer
          path={selectedFile.path}
          workspaceRoot={selectedFile.workspaceRoot}
        />
      </main>
    );
  }

  if (session) {
    return (
      <main className="main-pane" data-testid="main-pane-terminal">
        <TerminalView
          ptyId={session.id}
          fontFamily={settings.fontFamily}
          fontSize={settings.fontSize}
          settings={settings}
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
