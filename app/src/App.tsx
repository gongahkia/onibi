import { useEffect } from "react";
import { Panel, PanelGroup, PanelResizeHandle } from "react-resizable-panels";
import { AgentTabBar } from "./components/AgentTabBar";
import { FileTree } from "./components/FileTree";
import { MainPane } from "./components/MainPane";
import {
  applyDocumentSettings,
  hydrateSessionStore,
  useSessionStore,
} from "./lib/sessions";
import "./styles/layout.css";

function App() {
  const settings = useSessionStore((state) => state.settings);

  useEffect(() => {
    void hydrateSessionStore();
  }, []);

  useEffect(() => {
    applyDocumentSettings(settings);
  }, [settings]);

  const content = <ContentPanels />;

  if (settings.tabBarOrientation === "horizontal") {
    const tabs = (
      <Panel defaultSize={7} minSize={6} maxSize={12}>
        <AgentTabBar orientation="horizontal" />
      </Panel>
    );
    return (
      <main className="app-shell" data-tab-orientation="horizontal">
        <PanelGroup direction="vertical">
          {settings.tabBarPosition === "bottom" ? content : tabs}
          <PanelResizeHandle className="panel-resize-handle" />
          {settings.tabBarPosition === "bottom" ? tabs : content}
        </PanelGroup>
      </main>
    );
  }

  const tabs = (
    <Panel defaultSize={6} minSize={4} maxSize={10}>
      <AgentTabBar orientation="vertical" />
    </Panel>
  );

  return (
    <main className="app-shell" data-tab-orientation="vertical">
      <PanelGroup direction="horizontal">
        {settings.tabBarPosition === "right" ? content : tabs}
        <PanelResizeHandle className="panel-resize-handle" />
        {settings.tabBarPosition === "right" ? tabs : content}
      </PanelGroup>
    </main>
  );
}

function ContentPanels() {
  return (
    <Panel defaultSize={94} minSize={70}>
      <PanelGroup direction="horizontal">
        <Panel defaultSize={18} minSize={10} maxSize={34}>
          <FileTree />
        </Panel>
        <PanelResizeHandle className="panel-resize-handle" />
        <Panel defaultSize={76} minSize={40}>
          <MainPane />
        </Panel>
      </PanelGroup>
    </Panel>
  );
}

export default App;
