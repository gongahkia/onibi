import XCTest
@testable import OnibiCore

final class MobileGatewayRouterTests: XCTestCase {
    func testHealthRouteRequiresAuthorization() async throws {
        let router = MobileGatewayRouter(
            tokenProvider: { "secret-token" },
            dataProvider: StubGatewayDataProvider()
        )

        let unauthorized = await router.route(method: "GET", path: "/api/v1/health")
        XCTAssertEqual(unauthorized.statusCode, 401)

        let authorized = await router.route(
            method: "GET",
            path: "/api/v1/health",
            headers: ["Authorization": "Bearer secret-token"]
        )
        XCTAssertEqual(authorized.statusCode, 200)
    }

    func testEventsRouteUsesCursorAndLimit() async throws {
        let provider = StubGatewayDataProvider()
        let router = MobileGatewayRouter(
            tokenProvider: { "secret-token" },
            dataProvider: provider
        )

        let response = await router.route(
            method: "GET",
            path: "/api/v1/events",
            queryItems: [
                URLQueryItem(name: "cursor", value: "2026-03-18T00:00:00Z"),
                URLQueryItem(name: "limit", value: "5")
            ],
            headers: ["Authorization": "Bearer secret-token"]
        )

        XCTAssertEqual(response.statusCode, 200)
        let lastLimit = await provider.recordedLimit()
        let lastCursor = await provider.recordedCursor()
        XCTAssertEqual(lastLimit, 5)
        XCTAssertNotNil(lastCursor)
    }
}

private actor StubGatewayDataProvider: MobileGatewayDataProvider {
    private(set) var lastCursor: Date?
    private(set) var lastLimit: Int?

    func health() async throws -> HostHealth {
        HostHealth(
            ghosttyRunning: true,
            schedulerRunning: true,
            lastIngestAt: Date(),
            activeSessionCount: 1,
            gatewayRunning: true
        )
    }

    func summary() async throws -> SummaryResponse {
        SummaryResponse(activeSessionCount: 1, recentActivityCount: 2, latestEventAt: Date())
    }

    func sessions() async throws -> [SessionSnapshot] {
        [
            SessionSnapshot(
                id: "session-1",
                displayName: "session-1",
                isActive: true,
                startedAt: Date(),
                lastActivityAt: Date(),
                commandCount: 2,
                primaryAssistant: .codex,
                lastCommandPreview: "codex exec"
            )
        ]
    }

    func session(id: String) async throws -> SessionDetail? {
        let snapshot = try await sessions().first!
        return SessionDetail(
            session: snapshot,
            commands: [
                CommandRecordPreview(
                    id: UUID(),
                    sessionId: id,
                    startedAt: Date(),
                    endedAt: Date(),
                    duration: 1,
                    exitCode: 0,
                    assistantKind: .codex,
                    displayCommand: "codex exec"
                )
            ]
        )
    }

    func events(after cursor: Date?, limit: Int) async throws -> [EventPreview] {
        lastCursor = cursor
        lastLimit = limit
        return [
            EventPreview(
                id: UUID(),
                timestamp: Date(),
                sessionId: "session-1",
                assistantKind: .codex,
                kind: .assistantActivity,
                title: "Codex Activity",
                message: "codex exec",
                exitCode: 0
            )
        ]
    }

    func recordedCursor() -> Date? {
        lastCursor
    }

    func recordedLimit() -> Int? {
        lastLimit
    }
}
