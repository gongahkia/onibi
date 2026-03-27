import SwiftUI
import OnibiCore

struct PhoneSessionsView: View {
    let viewModel: MobileMonitorViewModel
    let openConnection: () -> Void

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    if viewModel.hasConfiguration {
                        SessionsOverviewCard(sessions: viewModel.sessions)

                        VStack(alignment: .leading, spacing: 14) {
                            PhoneSectionHeader(
                                title: "Tracked Sessions",
                                subtitle: "Jump into a Ghostty session to review its command timeline."
                            )

                            if viewModel.sessions.isEmpty {
                                PhoneEmptyStateCard(
                                    title: "No sessions yet",
                                    message: "Start working in Ghostty on your Mac and the session list will fill in here.",
                                    symbolName: "terminal"
                                )
                            } else {
                                LazyVStack(spacing: 14) {
                                    ForEach(viewModel.sessions) { session in
                                        NavigationLink(value: session.id) {
                                            SessionRowCard(session: session)
                                        }
                                        .buttonStyle(.plain)
                                    }
                                }
                            }
                        }
                    } else {
                        PhoneEmptyStateCard(
                            title: "Sessions unlock after pairing",
                            message: "Add the mobile host URL and pairing token from the Mac app before browsing Ghostty timelines.",
                            symbolName: "rectangle.stack.badge.plus"
                        ) {
                            Button("Set Up Connection", action: openConnection)
                                .buttonStyle(PhonePrimaryButtonStyle())
                        }
                    }
                }
                .padding(.horizontal, 20)
                .padding(.top, 18)
                .padding(.bottom, 32)
            }
            .background(PhoneBackground())
            .navigationTitle("Sessions")
            .toolbarTitleDisplayMode(.large)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        openConnection()
                    } label: {
                        Label("Connection", systemImage: "link")
                    }
                }
            }
            .navigationDestination(for: String.self) { sessionID in
                SessionDetailScreen(viewModel: viewModel, sessionID: sessionID)
            }
            .refreshable {
                await viewModel.refresh()
            }
        }
    }
}

private struct SessionsOverviewCard: View {
    let sessions: [SessionSnapshot]

    var body: some View {
        PhoneCard {
            VStack(alignment: .leading, spacing: 18) {
                PhoneSectionHeader(
                    title: "Session Pulse",
                    subtitle: "A snapshot of active and recently idle terminals."
                )

                HStack(spacing: 14) {
                    PhoneMetricTile(
                        title: "Active",
                        value: String(activeCount),
                        symbolName: "dot.radiowaves.left.and.right",
                        tint: PhonePalette.moss
                    )
                    PhoneMetricTile(
                        title: "Idle",
                        value: String(idleCount),
                        symbolName: "moon.zzz.fill",
                        tint: PhonePalette.cobalt
                    )
                }
            }
        }
    }

    private var activeCount: Int {
        sessions.filter(\.isActive).count
    }

    private var idleCount: Int {
        sessions.count - activeCount
    }
}

private struct SessionRowCard: View {
    let session: SessionSnapshot

    var body: some View {
        PhoneCard {
            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .top, spacing: 14) {
                    Image(systemName: session.primaryAssistant.symbolName)
                        .font(.headline.weight(.semibold))
                        .foregroundStyle(session.primaryAssistant.tintColor)
                        .frame(width: 42, height: 42)
                        .background(session.primaryAssistant.tintColor.opacity(0.14), in: RoundedRectangle(cornerRadius: 16, style: .continuous))

                    VStack(alignment: .leading, spacing: 6) {
                        Text(session.displayName)
                            .font(.headline)
                            .foregroundStyle(PhonePalette.charcoal)

                        Text("\(session.commandCount) commands • last active \(PhoneFormats.relativeString(for: session.lastActivityAt))")
                            .font(.subheadline)
                            .foregroundStyle(PhonePalette.smoke)
                    }

                    Spacer(minLength: 10)

                    PhoneBadge(
                        title: session.isActive ? "Active" : "Idle",
                        symbolName: session.isActive ? "waveform.path.ecg" : "pause.fill",
                        tint: session.isActive ? PhonePalette.moss : PhonePalette.cobalt
                    )
                }

                if let preview = session.lastCommandPreview {
                    Text(preview)
                        .font(.footnote.monospaced())
                        .foregroundStyle(PhonePalette.smoke)
                        .lineLimit(2)
                        .padding(14)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color.white.opacity(0.56), in: RoundedRectangle(cornerRadius: 18, style: .continuous))
                }
            }
        }
    }
}

private struct SessionDetailScreen: View {
    let viewModel: MobileMonitorViewModel
    let sessionID: String

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                if let detail {
                    SessionDetailSummaryCard(detail: detail)

                    VStack(alignment: .leading, spacing: 14) {
                        PhoneSectionHeader(
                            title: "Command Timeline",
                            subtitle: "Sanitized command history for this Ghostty session."
                        )

                        if detail.commands.isEmpty {
                            PhoneEmptyStateCard(
                                title: "No commands captured",
                                message: "This session has not produced sanitized command records yet.",
                                symbolName: "terminal"
                            )
                        } else {
                            LazyVStack(spacing: 14) {
                                ForEach(detail.commands) { command in
                                    CommandRecordCard(command: command)
                                }
                            }
                        }
                    }
                } else if viewModel.isLoadingSessionDetail(sessionID) {
                    PhoneEmptyStateCard(
                        title: "Loading session",
                        message: "Pulling the latest command timeline from your Mac.",
                        symbolName: "clock.arrow.circlepath"
                    ) {
                        ProgressView()
                            .tint(PhonePalette.ember)
                    }
                } else {
                    PhoneEmptyStateCard(
                        title: "Session unavailable",
                        message: "Pull to try loading this Ghostty session again.",
                        symbolName: "exclamationmark.triangle"
                    )
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 18)
            .padding(.bottom, 32)
        }
        .background(PhoneBackground())
        .navigationTitle(detail?.session.displayName ?? "Session")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: sessionID) {
            await viewModel.loadSessionDetail(id: sessionID)
        }
        .refreshable {
            await viewModel.loadSessionDetail(id: sessionID, forceRefresh: true)
        }
    }

    private var detail: SessionDetail? {
        viewModel.sessionDetail(for: sessionID)
    }
}

private struct SessionDetailSummaryCard: View {
    let detail: SessionDetail

    var body: some View {
        PhoneCard {
            VStack(alignment: .leading, spacing: 18) {
                HStack(alignment: .top) {
                    VStack(alignment: .leading, spacing: 8) {
                        Text(detail.session.displayName)
                            .font(.title3.weight(.bold))
                            .foregroundStyle(PhonePalette.charcoal)

                        Text("Started \(PhoneFormats.dateTimeString(for: detail.session.startedAt))")
                            .font(.subheadline)
                            .foregroundStyle(PhonePalette.smoke)
                    }

                    Spacer(minLength: 12)

                    PhoneBadge(
                        title: detail.session.primaryAssistant.displayName,
                        symbolName: detail.session.primaryAssistant.symbolName,
                        tint: detail.session.primaryAssistant.tintColor
                    )
                }

                HStack(spacing: 14) {
                    PhoneMetricTile(
                        title: "Commands",
                        value: String(detail.session.commandCount),
                        symbolName: "command",
                        tint: PhonePalette.ember
                    )
                    PhoneMetricTile(
                        title: "Last Activity",
                        value: PhoneFormats.relativeString(for: detail.session.lastActivityAt),
                        symbolName: "clock",
                        tint: detail.session.isActive ? PhonePalette.moss : PhonePalette.cobalt
                    )
                }
            }
        }
    }
}

private struct CommandRecordCard: View {
    let command: CommandRecordPreview

    var body: some View {
        PhoneCard {
            VStack(alignment: .leading, spacing: 14) {
                Text(command.displayCommand)
                    .font(.body.monospaced())
                    .foregroundStyle(PhonePalette.charcoal)
                    .fixedSize(horizontal: false, vertical: true)

                HStack(spacing: 10) {
                    PhoneBadge(
                        title: command.assistantKind.displayName,
                        symbolName: command.assistantKind.symbolName,
                        tint: command.assistantKind.tintColor
                    )

                    if let duration = PhoneFormats.durationString(for: command.duration) {
                        PhoneBadge(
                            title: duration,
                            symbolName: "timer",
                            tint: PhonePalette.cobalt
                        )
                    }

                    if let exitCode = command.exitCode {
                        PhoneBadge(
                            title: exitCode == 0 ? "Success" : "Exit \(exitCode)",
                            symbolName: exitCode == 0 ? "checkmark" : "xmark",
                            tint: exitCode == 0 ? PhonePalette.moss : PhonePalette.rose
                        )
                    }
                }

                Text("Started \(PhoneFormats.timeString(for: command.startedAt))")
                    .font(.footnote.weight(.medium))
                    .foregroundStyle(PhonePalette.smoke)
            }
        }
    }
}

#Preview("Session Row") {
    ZStack {
        PhoneBackground()
        SessionRowCard(
            session: SessionSnapshot(
                id: "session-1",
                displayName: "workspace-main",
                isActive: true,
                startedAt: .now.addingTimeInterval(-3_600),
                lastActivityAt: .now.addingTimeInterval(-90),
                commandCount: 14,
                primaryAssistant: .claudeCode,
                lastCommandPreview: "claude --print \"summarize the failing tests\""
            )
        )
        .padding(20)
    }
}

#Preview("Command Record") {
    ZStack {
        PhoneBackground()
        CommandRecordCard(
            command: CommandRecordPreview(
                id: UUID(),
                sessionId: "session-1",
                startedAt: .now.addingTimeInterval(-420),
                endedAt: .now.addingTimeInterval(-417),
                duration: 3.2,
                exitCode: 0,
                assistantKind: .codex,
                displayCommand: "codex exec --model gpt-5.4 --skip-approval"
            )
        )
        .padding(20)
    }
}
