import XCTest
@testable import OnibiCore

@MainActor
final class MobileMonitorViewModelTests: XCTestCase {
    func testInitialStateReflectsMissingConfiguration() {
        let defaults = UserDefaults(suiteName: "MobileMonitorViewModelTests.missing")!
        defaults.removePersistentDomain(forName: "MobileMonitorViewModelTests.missing")
        let store = MobileConnectionStore(
            defaults: defaults,
            configurationKey: "config",
            tokenStore: PairingTokenStore(service: "test.mobile.missing", account: "token")
        )

        let viewModel = MobileMonitorViewModel(
            client: StubMobileClient(),
            connectionStore: store,
            pollInterval: 60
        )

        XCTAssertEqual(viewModel.connectionState, .notConfigured)
        XCTAssertFalse(viewModel.hasConfiguration)
        XCTAssertNil(viewModel.connectionDraft)
    }

    func testRefreshLoadsDataWhenConfigured() async throws {
        let defaults = UserDefaults(suiteName: "MobileMonitorViewModelTests.refresh")!
        defaults.removePersistentDomain(forName: "MobileMonitorViewModelTests.refresh")
        let store = MobileConnectionStore(
            defaults: defaults,
            configurationKey: "config",
            tokenStore: PairingTokenStore(service: "test.mobile.refresh", account: "token")
        )
        try store.saveConfiguration(baseURLString: "https://example.ts.net", token: "token")

        let viewModel = MobileMonitorViewModel(
            client: StubMobileClient(),
            connectionStore: store,
            pollInterval: 60
        )

        await viewModel.refresh()

        XCTAssertEqual(viewModel.connectionState, .online)
        XCTAssertEqual(viewModel.sessions.count, 1)
        XCTAssertEqual(viewModel.recentEvents.count, 1)
        XCTAssertNotNil(viewModel.lastRefreshAt)
        XCTAssertEqual(viewModel.connectionDraft?.baseURLString, "https://example.ts.net")
    }

    func testRefreshMarksUnauthorizedState() async throws {
        let defaults = UserDefaults(suiteName: "MobileMonitorViewModelTests.unauthorized")!
        defaults.removePersistentDomain(forName: "MobileMonitorViewModelTests.unauthorized")
        let store = MobileConnectionStore(
            defaults: defaults,
            configurationKey: "config",
            tokenStore: PairingTokenStore(service: "test.mobile.unauthorized", account: "token")
        )
        try store.saveConfiguration(baseURLString: "https://example.ts.net", token: "token")

        let viewModel = MobileMonitorViewModel(
            client: UnauthorizedMobileClient(),
            connectionStore: store,
            pollInterval: 60
        )

        await viewModel.refresh()

        XCTAssertEqual(viewModel.connectionState, .unauthorized)
    }

    func testLoadSessionDetailCachesValueBySessionID() async throws {
        let defaults = UserDefaults(suiteName: "MobileMonitorViewModelTests.detail")!
        defaults.removePersistentDomain(forName: "MobileMonitorViewModelTests.detail")
        let store = MobileConnectionStore(
            defaults: defaults,
            configurationKey: "config",
            tokenStore: PairingTokenStore(service: "test.mobile.detail", account: "token")
        )
        try store.saveConfiguration(baseURLString: "https://example.ts.net", token: "token")

        let viewModel = MobileMonitorViewModel(
            client: StubMobileClient(),
            connectionStore: store,
            pollInterval: 60
        )

        await viewModel.loadSessionDetail(id: "session-1")

        XCTAssertNotNil(viewModel.sessionDetail(for: "session-1"))
        XCTAssertFalse(viewModel.isLoadingSessionDetail("session-1"))
    }
}

private struct StubMobileClient: MobileAPIClientProtocol {
    func fetchHealth() async throws -> HostHealth {
        HostHealth(ghosttyRunning: true, schedulerRunning: true, lastIngestAt: Date(), activeSessionCount: 1, gatewayRunning: true)
    }

    func fetchSummary() async throws -> SummaryResponse {
        SummaryResponse(activeSessionCount: 1, recentActivityCount: 3, latestEventAt: Date())
    }

    func fetchSessions() async throws -> [SessionSnapshot] {
        [
            SessionSnapshot(
                id: "session-1",
                displayName: "session-1",
                isActive: true,
                startedAt: Date(),
                lastActivityAt: Date(),
                commandCount: 1,
                primaryAssistant: .claudeCode,
                lastCommandPreview: "claude --print +1"
            )
        ]
    }

    func fetchSessionDetail(id: String) async throws -> SessionDetail {
        SessionDetail(
            session: try await fetchSessions().first!,
            commands: []
        )
    }

    func fetchEvents(cursor: Date?, limit: Int) async throws -> [EventPreview] {
        [
            EventPreview(
                id: UUID(),
                timestamp: Date(),
                sessionId: "session-1",
                assistantKind: .claudeCode,
                kind: .assistantActivity,
                title: "Claude Code Activity",
                message: "claude --print +1",
                exitCode: 0
            )
        ]
    }
}

private struct UnauthorizedMobileClient: MobileAPIClientProtocol {
    func fetchHealth() async throws -> HostHealth {
        throw MobileClientError.unauthorized
    }

    func fetchSummary() async throws -> SummaryResponse {
        throw MobileClientError.unauthorized
    }

    func fetchSessions() async throws -> [SessionSnapshot] {
        throw MobileClientError.unauthorized
    }

    func fetchSessionDetail(id: String) async throws -> SessionDetail {
        throw MobileClientError.unauthorized
    }

    func fetchEvents(cursor: Date?, limit: Int) async throws -> [EventPreview] {
        throw MobileClientError.unauthorized
    }
}
