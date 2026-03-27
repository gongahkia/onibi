import SwiftUI
import OnibiCore

private enum PhoneTab: String, Hashable {
    case dashboard
    case sessions
}

private enum PhoneSheet: String, Identifiable {
    case connection

    var id: String {
        rawValue
    }
}

struct PhoneRootView: View {
    @Environment(\.scenePhase) private var scenePhase
    @SceneStorage("onibi.phone.selected-tab") private var selectedTabRawValue = PhoneTab.dashboard.rawValue

    @State private var viewModel = MobileMonitorViewModel()
    @State private var activeSheet: PhoneSheet?

    var body: some View {
        TabView(selection: selectedTab) {
            PhoneDashboardView(
                viewModel: viewModel,
                openConnection: presentConnectionSheet
            )
            .tabItem {
                Label("Overview", systemImage: "flame.fill")
            }
            .tag(PhoneTab.dashboard)

            PhoneSessionsView(
                viewModel: viewModel,
                openConnection: presentConnectionSheet
            )
            .tabItem {
                Label("Sessions", systemImage: "terminal")
            }
            .tag(PhoneTab.sessions)
        }
        .tint(PhonePalette.ember)
        .sheet(item: $activeSheet) { sheet in
            switch sheet {
            case .connection:
                NavigationStack {
                    ConnectionSetupView(viewModel: viewModel)
                }
                .presentationDetents([.large])
                .presentationDragIndicator(.visible)
            }
        }
        .task {
            updateSceneState(for: scenePhase)
        }
        .onChange(of: scenePhase) { _, newValue in
            updateSceneState(for: newValue)
        }
    }

    private var selectedTab: Binding<PhoneTab> {
        Binding(
            get: { PhoneTab(rawValue: selectedTabRawValue) ?? .dashboard },
            set: { selectedTabRawValue = $0.rawValue }
        )
    }

    private func presentConnectionSheet() {
        activeSheet = .connection
    }

    private func updateSceneState(for phase: ScenePhase) {
        viewModel.setSceneActive(phase == .active)
    }
}
