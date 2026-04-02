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

        let receivedOutput = await waitForCondition {
            let messages = await transport.messages()
            return messages.contains { $0.type == .output }
        }

        let messages = await transport.messages()
        XCTAssertTrue(receivedOutput)
        XCTAssertTrue(messages.contains { $0.type == .sessionUpdated })
        XCTAssertTrue(messages.contains { message in
            message.type == .output &&
            message.sessionId == "session-1" &&
            String(data: message.chunk?.data ?? Data(), encoding: .utf8) == "hello\n"
        })

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

private enum TestError: Error {
    case encodingFailed
}
