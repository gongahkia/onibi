import { useEffect, useState } from "react";
import {
  Group as PanelGroup,
  Panel,
  Separator as PanelResizeHandle,
} from "react-resizable-panels";
import { AgentTabBar } from "./components/AgentTabBar";
import { CommandPalette } from "./components/CommandPalette";
import { FocusHud } from "./components/FocusHud";
import { MainPane } from "./components/MainPane";
import { NotificationToastHost } from "./components/NotificationToastHost";
import { OnboardingDialog } from "./components/OnboardingDialog";
import { StatusBar } from "./components/StatusBar";
import { TitleBar } from "./components/TitleBar";
import { TransportWatcher } from "./components/TransportWatcher";
import { UpdateDialog } from "./components/UpdateDialog";
import { WorkspaceRightDock } from "./components/WorkspaceRightDock";
import {
  applyDocumentSettings,
  hydrateSessionStore,
  themeFollowsSystem,
  useSessionStore,
} from "./lib/sessions";
import { startDesktopBridge } from "./lib/desktop-api";
import { AppQueryProvider } from "./lib/query-client";
import "./styles/layout.css";

function App() {
  const settings = useSessionStore((state) => state.settings);
  const [onboardingOpen, setOnboardingOpen] = useState(
    () => window.localStorage.getItem("onibi.onboarding.dismissed") !== "1",
  );
  const [mobileLayout, setMobileLayout] = useState(
    () => window.innerWidth <= settings.mobileLayoutThresholdPx,
  );

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

  useEffect(() => {
    const updateMobileLayout = () => {
      setMobileLayout(window.innerWidth <= settings.mobileLayoutThresholdPx);
    };
    updateMobileLayout();
    window.addEventListener("resize", updateMobileLayout);
    return () => window.removeEventListener("resize", updateMobileLayout);
  }, [settings.mobileLayoutThresholdPx]);

  useEffect(() => {
    if (!themeFollowsSystem(settings.theme) || typeof window.matchMedia !== "function") {
      return;
    }
    const query = window.matchMedia("(prefers-color-scheme: light)");
    const applySystemTheme = () => applyDocumentSettings(useSessionStore.getState().settings);
    if (query.addEventListener) {
      query.addEventListener("change", applySystemTheme);
    } else {
      query.addListener?.(applySystemTheme);
    }
    return () => {
      if (query.removeEventListener) {
        query.removeEventListener("change", applySystemTheme);
      } else {
        query.removeListener?.(applySystemTheme);
      }
    };
  }, [settings.theme]);

  const horizontalTabs = settings.tabBarOrientation === "horizontal";

  const body = (
    <main
      className="app-shell"
      data-tab-orientation={settings.tabBarOrientation}
    >
      {horizontalTabs ? (
        <PanelGroup orientation="vertical">
          {settings.tabBarPosition === "bottom" ? (
            <ContentPanels nestedInPanelGroup />
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
            <ContentPanels nestedInPanelGroup />
          )}
        </PanelGroup>
      ) : (
        <ContentPanels />
      )}
    </main>
  );

  return (
    <AppQueryProvider>
      <div className="app-frame" data-mobile-layout={mobileLayout || undefined}>
        <TitleBar />
        <div className="app-body">
          {horizontalTabs ? null : (
            <aside
              className="agent-rail-shell"
              aria-label="Session rail"
            >
              <AgentTabBar orientation="vertical" />
            </aside>
          )}
          {body}
          <WorkspaceRightDock />
        </div>
        <StatusBar />
        <FocusHud />
      </div>
      <NotificationToastHost />
      <TransportWatcher />
      <CommandPalette />
      <UpdateDialog />
      <OnboardingDialog open={onboardingOpen} onClose={() => setOnboardingOpen(false)} />
    </AppQueryProvider>
  );
}

interface ContentPanelsProps {
  nestedInPanelGroup?: boolean;
}

function ContentPanels({ nestedInPanelGroup = false }: ContentPanelsProps) {
  const panels = (
    <PanelGroup orientation="horizontal">
      <Panel defaultSize="100%" minSize="40%" id="main-panel">
        <MainPane />
      </Panel>
    </PanelGroup>
  );

  if (nestedInPanelGroup) {
    return (
      <Panel defaultSize="93%" minSize="70%" id="content-panel">
        {panels}
      </Panel>
    );
  }

  return panels;
}

export default App;
