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

    func testDiagnosticsRouteReturnsPayload() async throws {
        let router = MobileGatewayRouter(
            tokenProvider: { "secret-token" },
            dataProvider: StubGatewayDataProvider()
        )

        let response = await router.route(
            method: "GET",
            path: "/api/v1/diagnostics",
            headers: ["Authorization": "Bearer secret-token"]
        )

        XCTAssertEqual(response.statusCode, 200)
        let payload = try JSONDateCodec.decoder.decode(DiagnosticsResponse.self, from: response.body)
        XCTAssertGreaterThanOrEqual(payload.storageLogCount, 0)
    }

    func testAuthorizationProviderFailureReturnsServerError() async {
        let router = MobileGatewayRouter(
            tokenProvider: {
                throw NSError(domain: "test", code: 500, userInfo: [NSLocalizedDescriptionKey: "token lookup failed"])
            },
            dataProvider: StubGatewayDataProvider()
        )

        let response = await router.route(method: "GET", path: "/api/v1/health")
        XCTAssertEqual(response.statusCode, 500)
    }

    func testBootstrapRouteReturnsRealtimePayload() async throws {
        let router = MobileGatewayRouter(
            tokenProvider: { "secret-token" },
            dataProvider: StubGatewayDataProvider()
        )

        let response = await router.route(
            method: "GET",
            path: "/api/v2/bootstrap",
            headers: ["Authorization": "Bearer secret-token"]
        )

        XCTAssertEqual(response.statusCode, 200)
        let payload = try JSONDateCodec.decoder.decode(GatewayBootstrapResponse.self, from: response.body)
        XCTAssertTrue(payload.featureFlags.remoteControlEnabled)
        XCTAssertEqual(payload.sessions.count, 1)
        XCTAssertEqual(payload.sessions.first?.id, "control-session-1")
    }

    func testBufferRouteReturnsCurrentSessionBuffer() async throws {
        let router = MobileGatewayRouter(
            tokenProvider: { "secret-token" },
            dataProvider: StubGatewayDataProvider()
        )

        let response = await router.route(
            method: "GET",
            path: "/api/v2/sessions/control-session-1/buffer",
            headers: ["Authorization": "Bearer secret-token"]
        )

        XCTAssertEqual(response.statusCode, 200)
        let payload = try JSONDateCodec.decoder.decode(SessionOutputBufferSnapshot.self, from: response.body)
        XCTAssertEqual(payload.session.id, "control-session-1")
        XCTAssertEqual(payload.chunks.count, 1)
        XCTAssertEqual(String(data: payload.chunks[0].data, encoding: .utf8), "npm test\n")
    }

    func testInputRouteRejectsMalformedPayloadAndAcceptsValidInput() async throws {
        let provider = StubGatewayDataProvider()
        let router = MobileGatewayRouter(
            tokenProvider: { "secret-token" },
            dataProvider: provider
        )

        let malformed = await router.route(
            method: "POST",
            path: "/api/v2/sessions/control-session-1/input",
            headers: ["Authorization": "Bearer secret-token"],
            body: Data("{}".utf8)
        )
        XCTAssertEqual(malformed.statusCode, 400)

        let validBody = try JSONDateCodec.encoder.encode(RemoteInputPayload.key(.enter))
        let accepted = await router.route(
            method: "POST",
            path: "/api/v2/sessions/control-session-1/input",
            headers: ["Authorization": "Bearer secret-token"],
            body: validBody
        )

        XCTAssertEqual(accepted.statusCode, 200)
        let payload = try JSONDateCodec.decoder.decode(RemoteInputAcceptance.self, from: accepted.body)
        XCTAssertEqual(payload.sessionId, "control-session-1")

        let lastInput = await provider.recordedInput()
        XCTAssertEqual(lastInput?.sessionId, "control-session-1")
        XCTAssertEqual(lastInput?.payload, .key(.enter))
    }

    func testProcessActionRouteAcceptsValidAction() async throws {
        let provider = StubGatewayDataProvider()
        let router = MobileGatewayRouter(
            tokenProvider: { "secret-token" },
            dataProvider: provider
        )

        let validBody = try JSONDateCodec.encoder.encode(RemoteProcessActionPayload(action: .interrupt))
        let accepted = await router.route(
            method: "POST",
            path: "/api/v2/sessions/control-session-1/process-action",
            headers: ["Authorization": "Bearer secret-token"],
            body: validBody
        )

        XCTAssertEqual(accepted.statusCode, 200)
        let payload = try JSONDateCodec.decoder.decode(RemoteProcessActionAcceptance.self, from: accepted.body)
        XCTAssertEqual(payload.sessionId, "control-session-1")
        XCTAssertEqual(payload.action, .interrupt)

        let lastAction = await provider.recordedProcessAction()
        XCTAssertEqual(lastAction?.sessionId, "control-session-1")
        XCTAssertEqual(lastAction?.payload, RemoteProcessActionPayload(action: .interrupt))
    }

    func testRateLimiterReturns429AfterRepeatedAuthFailures() async {
        let tracker = AuthFailureTracker(windowSeconds: 60, maxFailures: 3)
        let router = MobileGatewayRouter(
            tokenProvider: { "secret-token" },
            dataProvider: StubGatewayDataProvider(),
            failureTracker: tracker
        )

        // Three unauthorized requests from the same peer.
        for _ in 0..<3 {
            let response = await router.route(
                method: "GET",
                path: "/api/v1/health",
                headers: ["Authorization": "Bearer wrong"],
                peer: "1.2.3.4"
            )
            XCTAssertEqual(response.statusCode, 401)
        }

        // Fourth request should be blocked with a 429 and Retry-After.
        let blocked = await router.route(
            method: "GET",
            path: "/api/v1/health",
            headers: ["Authorization": "Bearer wrong"],
            peer: "1.2.3.4"
        )
        XCTAssertEqual(blocked.statusCode, 429)
        XCTAssertNotNil(blocked.headers["Retry-After"])

        // A different peer is not affected.
        let ok = await router.route(
            method: "GET",
            path: "/api/v1/health",
            headers: ["Authorization": "Bearer secret-token"],
            peer: "5.6.7.8"
        )
        XCTAssertEqual(ok.statusCode, 200)
    }

    func testRateLimiterResetsOnSuccessfulAuth() async {
        let tracker = AuthFailureTracker(windowSeconds: 60, maxFailures: 3)
        let router = MobileGatewayRouter(
            tokenProvider: { "secret-token" },
            dataProvider: StubGatewayDataProvider(),
            failureTracker: tracker
        )

        for _ in 0..<2 {
            _ = await router.route(
                method: "GET",
                path: "/api/v1/health",
                headers: ["Authorization": "Bearer wrong"],
                peer: "1.2.3.4"
            )
        }
        // Correct token resets.
        _ = await router.route(
            method: "GET",
            path: "/api/v1/health",
            headers: ["Authorization": "Bearer secret-token"],
            peer: "1.2.3.4"
        )
        // Five more wrong requests should still NOT be blocked at first —
        // bucket was cleared so the threshold starts over.
        for _ in 0..<2 {
            let r = await router.route(
                method: "GET",
                path: "/api/v1/health",
                headers: ["Authorization": "Bearer wrong"],
                peer: "1.2.3.4"
            )
            XCTAssertEqual(r.statusCode, 401, "expected 401 while under threshold after reset")
        }
    }
}

private actor StubGatewayDataProvider: MobileGatewayDataProvider {
    private(set) var lastCursor: Date?
    private(set) var lastLimit: Int?
    private(set) var lastInput: (sessionId: String, payload: RemoteInputPayload)?
    private(set) var lastProcessAction: (sessionId: String, payload: RemoteProcessActionPayload)?

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

    func diagnostics() async throws -> DiagnosticsResponse {
        DiagnosticsResponse(
            generatedAt: Date(),
            hostVersion: "test",
            diagnosticsEventCount: 5,
            warningCount: 1,
            errorCount: 0,
            criticalCount: 0,
            schedulerEventsProcessed: 23,
            storageLogCount: 12,
            storageBytes: 2048,
            tailscaleStatus: "not_serving",
            latestErrorTitle: nil,
            latestErrorTimestamp: nil,
            recentEvents: [
                DiagnosticsEventPreview(
                    timestamp: Date(),
                    component: "test",
                    severity: .info,
                    message: "ok"
                )
            ]
        )
    }

    func featureFlags() async throws -> FeatureFlagsResponse {
        FeatureFlagsResponse(
            legacyMonitoringEnabled: true,
            remoteControlEnabled: true,
            realtimeSessionsEnabled: true,
            websocketEnabled: false,
            fallbackInputEnabled: true
        )
    }

    func controllableSessions() async throws -> [ControllableSessionSnapshot] {
        [
            ControllableSessionSnapshot(
                id: "control-session-1",
                displayName: "Control Session",
                startedAt: Date(),
                lastActivityAt: Date(),
                status: .running,
                isControllable: true,
                workingDirectory: "/tmp/onibi",
                lastCommandPreview: "npm test",
                bufferCursor: "chunk-1"
            )
        ]
    }

    func sessionOutputBuffer(id: String) async throws -> SessionOutputBufferSnapshot? {
        guard let session = try await controllableSessions().first(where: { $0.id == id }) else {
            return nil
        }

        return SessionOutputBufferSnapshot(
            session: session,
            bufferCursor: "chunk-1",
            chunks: [
                SessionOutputChunk(
                    id: "chunk-1",
                    sessionId: id,
                    stream: .stdout,
                    timestamp: Date(),
                    data: Data("npm test\n".utf8)
                )
            ],
            truncated: false
        )
    }

    func sendInput(to sessionId: String, payload: RemoteInputPayload) async throws -> RemoteInputAcceptance? {
        guard sessionId == "control-session-1" else {
            throw RemoteControlError.sessionNotFound(sessionId)
        }

        lastInput = (sessionId, payload)
        return RemoteInputAcceptance(sessionId: sessionId, acceptedAt: Date())
    }

    func performProcessAction(
        id sessionId: String,
        payload: RemoteProcessActionPayload
    ) async throws -> RemoteProcessActionAcceptance? {
        guard sessionId == "control-session-1" else {
            throw RemoteControlError.sessionNotFound(sessionId)
        }

        lastProcessAction = (sessionId, payload)
        return RemoteProcessActionAcceptance(sessionId: sessionId, action: payload.action, acceptedAt: Date())
    }

    func recordedCursor() -> Date? {
        lastCursor
    }

    func recordedLimit() -> Int? {
        lastLimit
    }

    func recordedInput() -> (sessionId: String, payload: RemoteInputPayload)? {
        lastInput
    }

    func recordedProcessAction() -> (sessionId: String, payload: RemoteProcessActionPayload)? {
        lastProcessAction
    }
}
