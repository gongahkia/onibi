import XCTest
import Dispatch
import OnibiCore
@testable import Onibi

final class RealtimeGatewayServiceTests: XCTestCase {
    func testRejectsFramesBeforeAuthentication() async throws {
        let service = makeService()
        let transport = MockRealtimeTransport()

        await service.attach(transport)
        await service.receive(
            text: try encode(
                RealtimeClientMessage(type: .subscribe, sessionId: "session-1")
            ),
            from: transport.id
        )

        let messages = await transport.messages()
        XCTAssertEqual(messages.count, 1)
        XCTAssertEqual(messages.first?.type, .error)
        XCTAssertEqual(messages.first?.code, "unauthorized")
    }

    func testAuthenticationSendsHostVersionAndSessionSnapshot() async throws {
        let registry = makeRegistry()
        let service = makeService(registry: registry)
        let transport = MockRealtimeTransport()

        await registry.register(
            ControllableSessionRegistration(
                id: "session-1",
                displayName: "Session 1",
                status: .running
            )
        )

        await service.attach(transport)
        await service.receive(
            text: try encode(
                RealtimeClientMessage(type: .auth, token: "test-token")
            ),
            from: transport.id
        )

        let messages = await transport.messages()
        XCTAssertEqual(messages.map(\.type), [.authOK, .sessionsSnapshot])
        XCTAssertEqual(messages.first?.hostVersion, "9.9.9-test")
        XCTAssertEqual(messages.first?.realtimeProtocolVersion, RealtimeProtocolVersion.current)
        XCTAssertEqual(
            messages.first?.minimumSupportedRealtimeProtocolVersion,
            RealtimeProtocolVersion.minimumSupported
        )
        XCTAssertEqual(messages.last?.sessions?.map(\.id), ["session-1"])
    }

    func testSendInputAcknowledgesClientRequestID() async throws {
        let registry = makeRegistry()
        let service = makeService(registry: registry)
        let transport = MockRealtimeTransport()
        let recorder = InputRecorder()

        await registry.register(
            ControllableSessionRegistration(
                id: "session-1",
                status: .running
            ),
            inputHandler: { payload in
                await recorder.record(payload)
            }
        )

        await service.attach(transport)
        await service.receive(
            text: try encode(
                RealtimeClientMessage(type: .auth, token: "test-token")
            ),
            from: transport.id
        )
        await transport.clear()

        await service.receive(
            text: try encode(
                RealtimeClientMessage(
                    type: .sendInput,
                    sessionId: "session-1",
                    kind: .key,
                    key: .enter,
                    clientRequestId: "req-1"
                )
            ),
            from: transport.id
        )

        let messages = await transport.messages()
        let payloads = await recorder.payloads()

        XCTAssertEqual(messages.map(\.type), [.inputAccepted])
        XCTAssertEqual(messages.first?.sessionId, "session-1")
        XCTAssertEqual(messages.first?.clientRequestId, "req-1")
        XCTAssertEqual(payloads, [.key(.enter)])
    }

    func testSubscribedClientReceivesLiveOutputEvents() async throws {
        let registry = makeRegistry()
        let service = makeService(registry: registry)
        let transport = MockRealtimeTransport()

        await service.start()
        await registry.register(
            ControllableSessionRegistration(
                id: "session-1",
                displayName: "Session 1",
                status: .running
            )
        )

        await service.attach(transport)
        await service.receive(
            text: try encode(
                RealtimeClientMessage(type: .auth, token: "test-token")
            ),
            from: transport.id
        )
        await transport.clear()

        await service.receive(
            text: try encode(
                RealtimeClientMessage(type: .subscribe, sessionId: "session-1")
            ),
            from: transport.id
        )

        await registry.appendOutput(
            sessionId: "session-1",
            data: Data("hello\n".utf8),
            timestamp: Date()
        )

        let receivedBoth = await waitForCondition {
            let messages = await transport.messages()
            return messages.contains { $0.type == .output }
                && messages.contains { $0.type == .sessionUpdated }
        }

        let messages = await transport.messages()
        XCTAssertTrue(receivedBoth)
        XCTAssertTrue(messages.contains { $0.type == .sessionUpdated })
        XCTAssertTrue(messages.contains { message in
            message.type == .output &&
            message.sessionId == "session-1" &&
            String(data: message.chunk?.data ?? Data(), encoding: .utf8) == "hello\n"
        })

        await service.stop()
    }

    func testResizeRoutesPayloadToRegisteredResizeHandler() async throws {
        let registry = makeRegistry()
        let service = makeService(registry: registry)
        let transport = MockRealtimeTransport()
        let recorder = ResizeRecorder()

        await registry.register(
            ControllableSessionRegistration(
                id: "session-1",
                status: .running
            ),
            resizeHandler: { payload in
                await recorder.record(payload)
            }
        )

        await service.attach(transport)
        await service.receive(
            text: try encode(
                RealtimeClientMessage(type: .auth, token: "test-token")
            ),
            from: transport.id
        )
        await transport.clear()

        await service.receive(
            text: try encode(
                RealtimeClientMessage(
                    type: .resize,
                    sessionId: "session-1",
                    cols: 120,
                    rows: 40
                )
            ),
            from: transport.id
        )

        let payloads = await recorder.payloads()
        let messages = await transport.messages()
        XCTAssertEqual(payloads, [RemoteTerminalResizePayload(cols: 120, rows: 40)])
        XCTAssertEqual(messages, [])
    }

    func testProcessActionAcknowledgesAndRoutesPayload() async throws {
        let registry = makeRegistry()
        let service = makeService(registry: registry)
        let transport = MockRealtimeTransport()
        let recorder = ProcessActionRecorder()

        await registry.register(
            ControllableSessionRegistration(
                id: "session-1",
                status: .running
            ),
            processActionHandler: { payload in
                await recorder.record(payload)
            }
        )

        await service.attach(transport)
        await service.receive(
            text: try encode(
                RealtimeClientMessage(type: .auth, token: "test-token")
            ),
            from: transport.id
        )
        await transport.clear()

        await service.receive(
            text: try encode(
                RealtimeClientMessage(
                    type: .processAction,
                    sessionId: "session-1",
                    action: .interrupt,
                    clientRequestId: "action-1"
                )
            ),
            from: transport.id
        )

        let payloads = await recorder.payloads()
        let messages = await transport.messages()
        XCTAssertEqual(payloads, [RemoteProcessActionPayload(action: .interrupt)])
        XCTAssertEqual(messages.map(\.type), [.processActionAccepted])
        XCTAssertEqual(messages.first?.sessionId, "session-1")
        XCTAssertEqual(messages.first?.action, .interrupt)
        XCTAssertEqual(messages.first?.clientRequestId, "action-1")
    }

    func testRequestBufferSupportsCursorLimitAndViewport() async throws {
        let registry = makeRegistry()
        let service = makeService(registry: registry)
        let transport = MockRealtimeTransport()

        await registry.register(
            ControllableSessionRegistration(
                id: "session-1",
                status: .running
            )
        )
        await registry.appendOutput(sessionId: "session-1", data: Data("one\n".utf8))
        let cursor = await registry.bufferSnapshot(for: "session-1")?.bufferCursor
        await registry.appendOutput(sessionId: "session-1", data: Data("two\n".utf8))
        await registry.appendOutput(sessionId: "session-1", data: Data("three\n".utf8))

        await service.attach(transport)
        await service.receive(
            text: try encode(
                RealtimeClientMessage(type: .auth, token: "test-token")
            ),
            from: transport.id
        )
        await transport.clear()

        await service.receive(
            text: try encode(
                RealtimeClientMessage(
                    type: .requestBuffer,
                    sessionId: "session-1",
                    bufferCursor: cursor,
                    bufferLimit: 1,
                    viewportCols: 120,
                    viewportRows: 40
                )
            ),
            from: transport.id
        )

        let messages = await transport.messages()
        XCTAssertEqual(messages.count, 1)
        XCTAssertEqual(messages.first?.type, .bufferSnapshot)
        XCTAssertEqual(messages.first?.requestCursor, cursor)
        XCTAssertEqual(messages.first?.chunks?.count, 1)
        XCTAssertEqual(String(data: messages.first?.chunks?.first?.data ?? Data(), encoding: .utf8), "three\n")
        XCTAssertEqual(messages.first?.startCursor, messages.first?.chunks?.first?.id)
        XCTAssertEqual(messages.first?.endCursor, messages.first?.chunks?.first?.id)
        XCTAssertEqual(messages.first?.viewportCols, 120)
        XCTAssertEqual(messages.first?.viewportRows, 40)
    }

    func testDisconnectAuthenticatedClientsClosesOnlyAuthenticatedSessions() async throws {
        let service = makeService()
        let authenticatedTransport = MockRealtimeTransport()
        let pendingTransport = MockRealtimeTransport()

        await service.attach(authenticatedTransport)
        await service.attach(pendingTransport)

        await service.receive(
            text: try encode(
                RealtimeClientMessage(type: .auth, token: "test-token")
            ),
            from: authenticatedTransport.id
        )
        await authenticatedTransport.clear()

        let disconnected = await service.disconnectAuthenticatedClients()
        let authenticatedCloseCount = await authenticatedTransport.closeCount()
        let pendingCloseCount = await pendingTransport.closeCount()
        let messages = await authenticatedTransport.messages()

        XCTAssertEqual(disconnected, 1)
        XCTAssertEqual(authenticatedCloseCount, 1)
        XCTAssertEqual(pendingCloseCount, 0)
        XCTAssertEqual(messages.map(\.type), [.error])
        XCTAssertEqual(messages.first?.code, "token_rotated")
    }

    func testDiagnosticsTracksWebsocketAuthFailures() async throws {
        let service = makeService()
        let preAuthTransport = MockRealtimeTransport()
        let badTokenTransport = MockRealtimeTransport()

        await service.attach(preAuthTransport)
        await service.receive(
            text: try encode(RealtimeClientMessage(type: .subscribe, sessionId: "session-1")),
            from: preAuthTransport.id
        )

        await service.attach(badTokenTransport)
        await service.receive(
            text: try encode(RealtimeClientMessage(type: .auth, token: "wrong-token")),
            from: badTokenTransport.id
        )

        let diagnostics = await service.diagnostics()
        XCTAssertEqual(diagnostics.websocketAuthFailureCount, 2)
        XCTAssertEqual(diagnostics.connectedClientCount, 1)
    }

    func testSessionRemovalClearsSubscriptionsBeforeSessionIDReuse() async throws {
        let registry = ControllableSessionRegistry(
            defaultBufferLineLimit: 10,
            defaultBufferByteLimit: 1024,
            staleSessionGracePeriod: 1
        )
        let service = makeService(registry: registry)
        let transport = MockRealtimeTransport()
        let sessionId = "session-1"

        await service.start()
        await registry.register(
            ControllableSessionRegistration(
                id: sessionId,
                startedAt: Date().addingTimeInterval(-20),
                status: .exited
            )
        )
        await service.attach(transport)
        await service.receive(
            text: try encode(
                RealtimeClientMessage(type: .auth, token: "test-token")
            ),
            from: transport.id
        )
        await service.receive(
            text: try encode(
                RealtimeClientMessage(type: .subscribe, sessionId: sessionId)
            ),
            from: transport.id
        )
        await transport.clear()

        await registry.expireDisconnectedSessions(now: Date())
        let didReceiveRemoved = await waitForCondition {
            let messages = await transport.messages()
            return messages.contains { $0.type == .sessionRemoved && $0.sessionId == sessionId }
        }
        XCTAssertTrue(didReceiveRemoved)

        await transport.clear()
        await registry.register(
            ControllableSessionRegistration(
                id: sessionId,
                status: .running
            )
        )
        await registry.appendOutput(
            sessionId: sessionId,
            data: Data("new output\n".utf8),
            timestamp: Date()
        )
        try? await Task.sleep(nanoseconds: 120_000_000)

        let messagesAfterReuse = await transport.messages()
        XCTAssertFalse(messagesAfterReuse.contains { $0.type == .output })

        await service.stop()
    }

    private func makeRegistry() -> ControllableSessionRegistry {
        ControllableSessionRegistry(
            defaultBufferLineLimit: 10,
            defaultBufferByteLimit: 1024,
            staleSessionGracePeriod: 30
        )
    }

    private func makeService(
        registry: ControllableSessionRegistry? = nil
    ) -> RealtimeGatewayService {
        RealtimeGatewayService(
            registry: registry ?? makeRegistry(),
            tokenProvider: { "test-token" },
            hostVersionProvider: { "9.9.9-test" }
        )
    }

    private func encode(_ message: RealtimeClientMessage) throws -> String {
        let data = try JSONEncoder().encode(message)
        guard let text = String(data: data, encoding: .utf8) else {
            throw TestError.encodingFailed
        }
        return text
    }

    private func waitForCondition(
        timeoutNanoseconds: UInt64 = 500_000_000,
        intervalNanoseconds: UInt64 = 20_000_000,
        condition: @escaping @Sendable () async -> Bool
    ) async -> Bool {
        let deadline = DispatchTime.now().uptimeNanoseconds + timeoutNanoseconds
        while DispatchTime.now().uptimeNanoseconds < deadline {
            if await condition() {
                return true
            }
            try? await Task.sleep(nanoseconds: intervalNanoseconds)
        }
        return await condition()
    }
}

private actor MockRealtimeTransport: RealtimeClientTransport {
    nonisolated let id: UUID

    private var sentMessages: [RealtimeServerMessage] = []
    private var closeInvocations = 0

    init(id: UUID = UUID()) {
        self.id = id
    }

    func send(_ message: RealtimeServerMessage) async throws {
        sentMessages.append(message)
    }

    func close() async {
        closeInvocations += 1
    }

    func messages() -> [RealtimeServerMessage] {
        sentMessages
    }

    func clear() {
        sentMessages.removeAll()
    }

    func closeCount() -> Int {
        closeInvocations
    }
}

private actor InputRecorder {
    private var values: [RemoteInputPayload] = []

    func record(_ payload: RemoteInputPayload) {
        values.append(payload)
    }

    func payloads() -> [RemoteInputPayload] {
        values
    }
}

private actor ResizeRecorder {
    private var values: [RemoteTerminalResizePayload] = []

    func record(_ payload: RemoteTerminalResizePayload) {
        values.append(payload)
    }

    func payloads() -> [RemoteTerminalResizePayload] {
        values
    }
}

private actor ProcessActionRecorder {
    private var values: [RemoteProcessActionPayload] = []

    func record(_ payload: RemoteProcessActionPayload) {
        values.append(payload)
    }

    func payloads() -> [RemoteProcessActionPayload] {
        values
    }
}

private enum TestError: Error {
    case encodingFailed
}
