import SwiftUI
import OnibiCore

struct PhoneDashboardView: View {
    let viewModel: MobileMonitorViewModel
    let openConnection: () -> Void

    private let metricColumns = [
        GridItem(.flexible(), spacing: 14),
        GridItem(.flexible(), spacing: 14)
    ]

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    StatusHeroCard(
                        state: viewModel.connectionState,
                        hostLabel: PhoneFormats.hostLabel(from: viewModel.connectionDraft?.baseURLString),
                        lastRefreshAt: viewModel.lastRefreshAt,
                        isRefreshing: viewModel.isRefreshing,
                        openConnection: openConnection
                    )

                    if viewModel.hasConfiguration {
                        summaryMetrics
                        hostServicesSection
                        recentEventsSection
                    } else {
                        onboardingSection
                    }
                }
                .padding(.horizontal, 20)
                .padding(.top, 18)
                .padding(.bottom, 32)
            }
            .background(PhoneBackground())
            .navigationTitle("Onibi")
            .toolbarTitleDisplayMode(.large)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    if viewModel.isRefreshing {
                        ProgressView()
                            .controlSize(.small)
                            .tint(PhonePalette.ember)
                    }
                }

                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        openConnection()
                    } label: {
                        Label("Connection", systemImage: "link")
                    }
                }
            }
            .refreshable {
                await viewModel.refresh()
            }
        }
    }

    private var summaryMetrics: some View {
        VStack(alignment: .leading, spacing: 14) {
            PhoneSectionHeader(
                title: "Live Snapshot",
                subtitle: "A quick read on host health and Ghostty activity."
            )

            LazyVGrid(columns: metricColumns, spacing: 14) {
                PhoneMetricTile(
                    title: "Active Sessions",
                    value: metricValue(for: viewModel.summary?.activeSessionCount),
                    symbolName: "rectangle.stack.fill",
                    tint: PhonePalette.ember
                )
                PhoneMetricTile(
                    title: "Recent Activity",
                    value: metricValue(for: viewModel.summary?.recentActivityCount),
                    symbolName: "waveform.path.ecg",
                    tint: PhonePalette.cobalt
                )
                PhoneMetricTile(
                    title: "Gateway",
                    value: gatewayValue,
                    symbolName: "antenna.radiowaves.left.and.right",
                    tint: gatewayTint
                )
                PhoneMetricTile(
                    title: "Latest Event",
                    value: PhoneFormats.relativeString(for: viewModel.summary?.latestEventAt),
                    symbolName: "clock.arrow.circlepath",
                    tint: PhonePalette.sunrise
                )
            }
        }
    }

    @ViewBuilder
    private var hostServicesSection: some View {
        if let health = viewModel.health {
            VStack(alignment: .leading, spacing: 14) {
                PhoneSectionHeader(
                    title: "Host Services",
                    subtitle: "These are the components the iPhone companion depends on."
                )

                PhoneCard {
                    VStack(alignment: .leading, spacing: 16) {
                        ServiceStatusRow(
                            title: "Ghostty",
                            message: "Terminal session tracking",
                            isOnline: health.ghosttyRunning
                        )
                        ServiceStatusRow(
                            title: "Scheduler",
                            message: "Background log processing",
                            isOnline: health.schedulerRunning
                        )
                        ServiceStatusRow(
                            title: "Gateway",
                            message: "Tailscale mobile bridge",
                            isOnline: health.gatewayRunning
                        )

                        Divider()

                        HStack {
                            Text("Last ingest")
                                .font(.subheadline.weight(.medium))
                                .foregroundStyle(PhonePalette.charcoal)

                            Spacer()

                            Text(PhoneFormats.dateTimeString(for: health.lastIngestAt))
                                .font(.subheadline)
                                .foregroundStyle(PhonePalette.smoke)
                        }
                    }
                }
            }
        }
    }

    private var recentEventsSection: some View {
        VStack(alignment: .leading, spacing: 14) {
            PhoneSectionHeader(
                title: "Recent Events",
                subtitle: "The latest command completions and assistant activity from your Mac."
            )

            if viewModel.recentEvents.isEmpty {
                PhoneEmptyStateCard(
                    title: "Nothing yet",
                    message: "Run a command in Ghostty to populate the live event feed.",
                    symbolName: "bolt.slash"
                )
            } else {
                LazyVStack(spacing: 14) {
                    ForEach(viewModel.recentEvents.prefix(8)) { event in
                        EventCard(event: event)
                    }
                }
            }
        }
    }

    private var onboardingSection: some View {
        VStack(alignment: .leading, spacing: 14) {
            PhoneSectionHeader(
                title: "Pairing",
                subtitle: "Link this iPhone to the macOS menu bar app to stream Ghostty activity."
            )

            PhoneEmptyStateCard(
                title: "Start with the Mac app",
                message: "Open Onibi on macOS, enable Mobile Access, then paste the host URL and pairing token here.",
                symbolName: "iphone.gen3.and.arrow.forward"
            ) {
                Button("Add Connection", action: openConnection)
                    .buttonStyle(PhonePrimaryButtonStyle())
            }
        }
    }

    private var gatewayValue: String {
        guard let health = viewModel.health else {
            return "Waiting"
        }

        return health.gatewayRunning ? "Live" : "Down"
    }

    private var gatewayTint: Color {
        guard let health = viewModel.health else {
            return PhonePalette.cobalt
        }

        return health.gatewayRunning ? PhonePalette.moss : PhonePalette.rose
    }

    private func metricValue(for value: Int?) -> String {
        guard let value else {
            return "--"
        }

        return String(value)
    }
}

private struct StatusHeroCard: View {
    let state: MobileMonitorViewModel.ConnectionState
    let hostLabel: String?
    let lastRefreshAt: Date?
    let isRefreshing: Bool
    let openConnection: () -> Void

    var body: some View {
        PhoneCard {
            VStack(alignment: .leading, spacing: 18) {
                HStack(alignment: .top) {
                    VStack(alignment: .leading, spacing: 8) {
                        PhoneBadge(
                            title: isRefreshing ? "Refreshing" : badgeTitle,
                            symbolName: isRefreshing ? "arrow.triangle.2.circlepath" : state.symbolName,
                            tint: state.tintColor
                        )

                        Text(state.message)
                            .font(.title3.weight(.semibold))
                            .foregroundStyle(PhonePalette.charcoal)
                            .fixedSize(horizontal: false, vertical: true)
                    }

                    Spacer(minLength: 12)

                    Image(systemName: "flame.fill")
                        .font(.system(size: 28, weight: .bold))
                        .foregroundStyle(PhonePalette.ember)
                        .padding(14)
                        .background(PhonePalette.sunrise.opacity(0.16), in: RoundedRectangle(cornerRadius: 18, style: .continuous))
                }

                if let hostLabel {
                    HStack(spacing: 10) {
                        Label(hostLabel, systemImage: "desktopcomputer")
                            .font(.subheadline.weight(.medium))
                            .foregroundStyle(PhonePalette.smoke)

                        Spacer()

                        Text(lastRefreshLabel)
                            .font(.footnote.weight(.medium))
                            .foregroundStyle(PhonePalette.smoke)
                    }
                }

                Button(buttonTitle, action: openConnection)
                    .buttonStyle(PhonePrimaryButtonStyle())
            }
        }
    }

    private var buttonTitle: String {
        switch state {
        case .notConfigured:
            return "Connect This iPhone"
        case .unauthorized, .unreachable, .failed:
            return "Update Connection"
        default:
            return "Manage Connection"
        }
    }

    private var badgeTitle: String {
        switch state {
        case .idle, .loading:
            return "Syncing"
        case .online:
            return "Live"
        case .notConfigured:
            return "Not Paired"
        case .unauthorized:
            return "Rejected"
        case .unreachable:
            return "Offline"
        case .failed:
            return "Issue"
        }
    }

    private var lastRefreshLabel: String {
        if isRefreshing {
            return "Syncing now"
        }

        guard let lastRefreshAt else {
            return "Waiting for first sync"
        }

        return "Updated \(PhoneFormats.relativeString(for: lastRefreshAt))"
    }
}

private struct ServiceStatusRow: View {
    let title: String
    let message: String
    let isOnline: Bool

    var body: some View {
        HStack(alignment: .center, spacing: 14) {
            Circle()
                .fill(isOnline ? PhonePalette.moss : PhonePalette.rose)
                .frame(width: 10, height: 10)

            VStack(alignment: .leading, spacing: 3) {
                Text(title)
                    .font(.headline)
                    .foregroundStyle(PhonePalette.charcoal)

                Text(message)
                    .font(.subheadline)
                    .foregroundStyle(PhonePalette.smoke)
            }

            Spacer()

            Text(isOnline ? "Online" : "Offline")
                .font(.subheadline.weight(.medium))
                .foregroundStyle(isOnline ? PhonePalette.moss : PhonePalette.rose)
        }
    }
}

private struct EventCard: View {
    let event: EventPreview

    var body: some View {
        PhoneCard {
            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .top) {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(event.title)
                            .font(.headline)
                            .foregroundStyle(PhonePalette.charcoal)

                        Text(event.message)
                            .font(.subheadline)
                            .foregroundStyle(PhonePalette.smoke)
                            .fixedSize(horizontal: false, vertical: true)
                    }

                    Spacer(minLength: 12)

                    PhoneBadge(
                        title: event.assistantKind.displayName,
                        symbolName: event.assistantKind.symbolName,
                        tint: event.assistantKind.tintColor
                    )
                }

                HStack(spacing: 10) {
                    Label(PhoneFormats.relativeString(for: event.timestamp), systemImage: "clock")
                        .font(.footnote.weight(.medium))
                        .foregroundStyle(PhonePalette.smoke)

                    if let exitCode = event.exitCode {
                        PhoneBadge(
                            title: exitCode == 0 ? "Success" : "Exit \(exitCode)",
                            symbolName: exitCode == 0 ? "checkmark" : "xmark",
                            tint: exitCode == 0 ? PhonePalette.moss : PhonePalette.rose
                        )
                    }
                }
            }
        }
    }
}

#Preview("Status Hero") {
    ZStack {
        PhoneBackground()
        StatusHeroCard(
            state: .online,
            hostLabel: "onibi-mac.tailnet.ts.net",
            lastRefreshAt: .now,
            isRefreshing: false,
            openConnection: {}
        )
        .padding(20)
    }
}

#Preview("Event Card") {
    ZStack {
        PhoneBackground()
        EventCard(
            event: EventPreview(
                id: UUID(),
                timestamp: .now.addingTimeInterval(-180),
                sessionId: "session-1",
                assistantKind: .codex,
                kind: .assistantActivity,
                title: "Codex completed a run",
                message: "codex exec --model gpt-5.4",
                exitCode: 0
            )
        )
        .padding(20)
    }
}
