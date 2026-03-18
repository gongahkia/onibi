import SwiftUI
import OnibiCore

struct PhoneRootView: View {
    @Environment(\.scenePhase) private var scenePhase
    @StateObject private var viewModel = MobileMonitorViewModel()
    @State private var showConnectionSheet = false

    var body: some View {
        NavigationStack {
            Group {
                if viewModel.hasConfiguration {
                    mainContent
                } else {
                    ConnectionSetupView(viewModel: viewModel)
                }
            }
            .navigationTitle("Onibi")
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    if viewModel.hasConfiguration {
                        Button("Connection") {
                            showConnectionSheet = true
                        }
                    }
                }
            }
        }
        .sheet(isPresented: $showConnectionSheet) {
            NavigationStack {
                ConnectionSetupView(viewModel: viewModel)
            }
        }
        .onChange(of: scenePhase) { _, newValue in
            viewModel.setSceneActive(newValue == .active)
        }
        .task {
            viewModel.setSceneActive(scenePhase == .active)
        }
    }

    private var mainContent: some View {
        TabView {
            DashboardView(viewModel: viewModel)
                .tabItem {
                    Label("Dashboard", systemImage: "gauge.with.dots.needle.50percent")
                }

            SessionListView(viewModel: viewModel)
                .tabItem {
                    Label("Sessions", systemImage: "rectangle.stack")
                }
        }
    }
}

private struct DashboardView: View {
    @ObservedObject var viewModel: MobileMonitorViewModel

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                statusBanner

                if let health = viewModel.health {
                    metricCard(
                        title: "Host Status",
                        values: [
                            "Ghostty": health.ghosttyRunning ? "Running" : "Offline",
                            "Scheduler": health.schedulerRunning ? "Running" : "Stopped",
                            "Gateway": health.gatewayRunning ? "Listening" : "Stopped"
                        ]
                    )
                }

                if let summary = viewModel.summary {
                    metricCard(
                        title: "Activity",
                        values: [
                            "Active Sessions": "\(summary.activeSessionCount)",
                            "Recent Activity": "\(summary.recentActivityCount)",
                            "Latest Event": summary.latestEventAt.map {
                                Self.relativeDateFormatter.localizedString(for: $0, relativeTo: Date())
                            } ?? "None"
                        ]
                    )
                }

                VStack(alignment: .leading, spacing: 12) {
                    HStack {
                        Text("Recent Events")
                            .font(.headline)
                        Spacer()
                        Button("Refresh") {
                            Task { await viewModel.refresh() }
                        }
                    }

                    if viewModel.recentEvents.isEmpty {
                        Text("No events yet.")
                            .foregroundStyle(.secondary)
                    } else {
                        ForEach(viewModel.recentEvents) { event in
                            VStack(alignment: .leading, spacing: 4) {
                                HStack {
                                    Text(event.title)
                                        .font(.subheadline.weight(.semibold))
                                    Spacer()
                                    Text(Self.relativeDateFormatter.localizedString(for: event.timestamp, relativeTo: Date()))
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                }
                                Text(event.message)
                                    .font(.callout)
                                    .foregroundStyle(.secondary)
                                Text(event.assistantKind.displayName)
                                    .font(.caption2)
                                    .padding(.horizontal, 6)
                                    .padding(.vertical, 2)
                                    .background(Color.accentColor.opacity(0.12))
                                    .clipShape(Capsule())
                            }
                            .padding()
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(Color(.secondarySystemGroupedBackground))
                            .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                        }
                    }
                }
            }
            .padding()
        }
        .refreshable {
            await viewModel.refresh()
        }
    }

    private var statusBanner: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(statusTitle)
                .font(.headline)
            Text(statusDescription)
                .font(.callout)
                .foregroundStyle(.secondary)
        }
        .padding()
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(statusColor.opacity(0.12))
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    private func metricCard(title: String, values: [String: String]) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title)
                .font(.headline)
            ForEach(values.sorted(by: { $0.key < $1.key }), id: \.key) { item in
                HStack {
                    Text(item.key)
                    Spacer()
                    Text(item.value)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding()
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemGroupedBackground))
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    private var statusTitle: String {
        switch viewModel.connectionState {
        case .idle, .loading:
            return "Connecting to host"
        case .online:
            return "Connected"
        case .notConfigured:
            return "Connection not configured"
        case .unauthorized:
            return "Pairing rejected"
        case .unreachable:
            return "Host unreachable"
        case .failed:
            return "Connection issue"
        }
    }

    private var statusDescription: String {
        switch viewModel.connectionState {
        case .idle, .loading:
            return "Onibi is polling your Mac for session updates."
        case .online:
            return "Live host, session, and command updates are available."
        case .notConfigured:
            return "Enter the Tailscale URL and pairing token from your Mac."
        case .unauthorized:
            return "The pairing token no longer matches the host settings."
        case .unreachable:
            return "The Mac host could not be reached over Tailscale."
        case .failed(let message):
            return message
        }
    }

    private var statusColor: Color {
        switch viewModel.connectionState {
        case .online:
            return .green
        case .unauthorized, .failed:
            return .orange
        case .unreachable:
            return .red
        default:
            return .blue
        }
    }

    private static let relativeDateFormatter: RelativeDateTimeFormatter = {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .short
        return formatter
    }()
}

private struct SessionListView: View {
    @ObservedObject var viewModel: MobileMonitorViewModel

    var body: some View {
        List(viewModel.sessions) { session in
            NavigationLink(value: session.id) {
                VStack(alignment: .leading, spacing: 6) {
                    HStack {
                        Text(session.displayName)
                            .font(.headline)
                        Spacer()
                        Text(session.isActive ? "Active" : "Idle")
                            .font(.caption)
                            .foregroundStyle(session.isActive ? .green : .secondary)
                    }

                    HStack {
                        Text(session.primaryAssistant.displayName)
                        Spacer()
                        Text("\(session.commandCount) commands")
                    }
                    .font(.subheadline)
                    .foregroundStyle(.secondary)

                    if let preview = session.lastCommandPreview {
                        Text(preview)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
                .padding(.vertical, 4)
            }
        }
        .navigationDestination(for: String.self) { sessionId in
            SessionDetailScreen(viewModel: viewModel, sessionId: sessionId)
        }
        .overlay {
            if viewModel.sessions.isEmpty {
                ContentUnavailableView(
                    "No Sessions",
                    systemImage: "rectangle.stack.badge.person.crop",
                    description: Text("Run commands in Ghostty on the Mac host to populate this list.")
                )
            }
        }
        .refreshable {
            await viewModel.refresh()
        }
    }
}

private struct SessionDetailScreen: View {
    @ObservedObject var viewModel: MobileMonitorViewModel
    let sessionId: String

    var body: some View {
        List {
            if let detail = detail {
                Section {
                    HStack {
                        Text("Assistant")
                        Spacer()
                        Text(detail.session.primaryAssistant.displayName)
                            .foregroundStyle(.secondary)
                    }
                    HStack {
                        Text("Commands")
                        Spacer()
                        Text("\(detail.session.commandCount)")
                            .foregroundStyle(.secondary)
                    }
                    HStack {
                        Text("Last Activity")
                        Spacer()
                        Text(detail.session.lastActivityAt.formatted(date: .abbreviated, time: .shortened))
                            .foregroundStyle(.secondary)
                    }
                }

                Section("Command Timeline") {
                    if detail.commands.isEmpty {
                        Text("No sanitized command history available.")
                            .foregroundStyle(.secondary)
                    } else {
                        ForEach(detail.commands) { command in
                            VStack(alignment: .leading, spacing: 4) {
                                Text(command.displayCommand)
                                    .font(.body.monospaced())
                                HStack {
                                    Text(command.assistantKind.displayName)
                                    Spacer()
                                    if let exitCode = command.exitCode {
                                        Text(exitCode == 0 ? "Success" : "Exit \(exitCode)")
                                    }
                                }
                                .font(.caption)
                                .foregroundStyle(.secondary)
                            }
                            .padding(.vertical, 4)
                        }
                    }
                }
            }
        }
        .navigationTitle(detail?.session.displayName ?? sessionId)
        .task {
            await viewModel.loadSessionDetail(id: sessionId)
        }
    }

    private var detail: SessionDetail? {
        guard viewModel.selectedSessionDetail?.session.id == sessionId else { return nil }
        return viewModel.selectedSessionDetail
    }
}
