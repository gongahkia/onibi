import { useEffect, useState } from "react";
import {
  Group as PanelGroup,
  Panel,
  Separator as PanelResizeHandle,
} from "react-resizable-panels";
import { ActivityBar } from "./components/ActivityBar";
import { AgentTabBar } from "./components/AgentTabBar";
import { ApprovalModal } from "./components/ApprovalModal";
import { CommandPalette } from "./components/CommandPalette";
import { MainPane } from "./components/MainPane";
import { StatusBar } from "./components/StatusBar";
import { TitleBar } from "./components/TitleBar";
import { WorkspaceSidebar } from "./components/WorkspaceSidebar";
import {
  applyDocumentSettings,
  hydrateSessionStore,
  useSessionStore,
} from "./lib/sessions";
import { startDesktopBridge } from "./lib/desktop-api";
import "./styles/layout.css";

function App() {
  const settings = useSessionStore((state) => state.settings);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);

  useEffect(() => {
    void hydrateSessionStore();
  }, []);

  useEffect(() => {
    let cleanup: (() => void) | undefined;
    let disposed = false;
    void startDesktopBridge().then((dispose) => {
      if (disposed) {
        dispose();
      } else {
        cleanup = dispose;
      }
    });
    return () => {
      disposed = true;
      cleanup?.();
    };
  }, []);

  useEffect(() => {
    applyDocumentSettings(settings);
  }, [settings]);

  const horizontalTabs = settings.tabBarOrientation === "horizontal";

  const body = (
    <main
      className="app-shell"
      data-tab-orientation={settings.tabBarOrientation}
    >
      {horizontalTabs ? (
        <PanelGroup orientation="vertical">
          {settings.tabBarPosition === "bottom" ? (
            <ContentPanels sidebarCollapsed={sidebarCollapsed} />
          ) : (
            <Panel defaultSize="7%" minSize="6%" maxSize="12%">
              <AgentTabBar orientation="horizontal" />
            </Panel>
          )}
          <PanelResizeHandle className="panel-resize-handle" />
          {settings.tabBarPosition === "bottom" ? (
            <Panel defaultSize="7%" minSize="6%" maxSize="12%">
              <AgentTabBar orientation="horizontal" />
            </Panel>
          ) : (
            <ContentPanels sidebarCollapsed={sidebarCollapsed} />
          )}
        </PanelGroup>
      ) : (
        <ContentPanels sidebarCollapsed={sidebarCollapsed} />
      )}
    </main>
  );

  return (
    <>
      <div className="app-frame">
        <TitleBar />
        <div className="app-body">
          <ActivityBar
            sidebarCollapsed={sidebarCollapsed}
            onToggleSidebar={() => setSidebarCollapsed((value) => !value)}
          />
          {body}
        </div>
        <StatusBar />
      </div>
      <ApprovalModal />
      <CommandPalette />
    </>
  );
}

interface ContentPanelsProps {
  sidebarCollapsed: boolean;
}

function ContentPanels({ sidebarCollapsed }: ContentPanelsProps) {
  return (
    <Panel defaultSize="100%" minSize="70%">
      <PanelGroup orientation="horizontal">
        {sidebarCollapsed ? null : (
          <>
            <Panel defaultSize="20%" minSize="12%" maxSize="40%" id="sidebar-panel">
              <WorkspaceSidebar />
            </Panel>
            <PanelResizeHandle className="panel-resize-handle" />
          </>
        )}
        <Panel defaultSize="80%" minSize="40%" id="main-panel">
          <MainPane />
        </Panel>
      </PanelGroup>
    </Panel>
  );
}

export default App;
