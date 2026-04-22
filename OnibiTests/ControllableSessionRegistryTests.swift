import XCTest
import OnibiCore
@testable import Onibi

final class ControllableSessionRegistryTests: XCTestCase {
    func testRegisterAndAppendOutputUpdatesSnapshotAndBuffer() async {
        let registry = ControllableSessionRegistry(
            defaultBufferLineLimit: 10,
            defaultBufferByteLimit: 1024,
            staleSessionGracePeriod: 30
        )

        await registry.register(
            ControllableSessionRegistration(
                id: "session-1",
                displayName: "Session 1",
                startedAt: Date().addingTimeInterval(-5),
                status: .starting
            )
        )
        await registry.appendOutput(
            sessionId: "session-1",
            data: Data("ready\n".utf8),
            timestamp: Date()
        )

        let snapshot = await registry.session(id: "session-1")
        let buffer = await registry.bufferSnapshot(for: "session-1")

        XCTAssertEqual(snapshot?.status, .running)
        XCTAssertNotNil(snapshot?.bufferCursor)
        XCTAssertEqual(buffer?.chunks.count, 1)
        XCTAssertEqual(String(data: buffer?.chunks[0].data ?? Data(), encoding: .utf8), "ready\n")
    }

    func testSendInputRoutesPayloadToRegisteredHandler() async throws {
        let registry = ControllableSessionRegistry(
            defaultBufferLineLimit: 10,
            defaultBufferByteLimit: 1024,
            staleSessionGracePeriod: 30
        )
        let recorder = InputRecorder()

        await registry.register(
            ControllableSessionRegistration(id: "session-1", status: .running),
            inputHandler: { payload in
                await recorder.record(payload)
            }
        )

        let acceptance = try await registry.sendInput(.key(.enter), to: "session-1")
        let recordedPayloads = await recorder.payloads()

        XCTAssertEqual(acceptance.sessionId, "session-1")
        XCTAssertEqual(recordedPayloads, [.key(.enter)])
    }

    func testSendInputFailsForNonControllableSession() async {
        let registry = ControllableSessionRegistry(
            defaultBufferLineLimit: 10,
            defaultBufferByteLimit: 1024,
            staleSessionGracePeriod: 30
        )

        await registry.register(
            ControllableSessionRegistration(
                id: "session-1",
                status: .running,
                isControllable: false
            ),
            inputHandler: { _ in }
        )

        do {
            _ = try await registry.sendInput(.text("ls"), to: "session-1")
            XCTFail("Expected non-controllable session to reject input")
        } catch let error as RemoteControlError {
            XCTAssertEqual(error, .sessionNotControllable("session-1"))
        } catch {
            XCTFail("Unexpected error: \(error)")
        }
    }

    func testExpireDisconnectedSessionsRemovesStaleExitedSessions() async {
        let registry = ControllableSessionRegistry(
            defaultBufferLineLimit: 10,
            defaultBufferByteLimit: 1024,
            staleSessionGracePeriod: 5
        )

        await registry.register(
            ControllableSessionRegistration(
                id: "session-1",
                startedAt: Date().addingTimeInterval(-30),
                status: .exited
            )
        )

        await registry.expireDisconnectedSessions(now: Date())
        let snapshot = await registry.session(id: "session-1")
        XCTAssertNil(snapshot)
    }

    func testDiagnosticsIncludesProxyDisconnectAndBufferTruncationCounts() async {
        let registry = ControllableSessionRegistry(
            defaultBufferLineLimit: 5,
            defaultBufferByteLimit: 8,
            staleSessionGracePeriod: 30
        )

        await registry.register(
            ControllableSessionRegistration(
                id: "session-1",
                status: .running
            )
        )
        await registry.appendOutput(
            sessionId: "session-1",
            data: Data("123456789012".utf8),
            timestamp: Date()
        )
        await registry.markProxyDisconnect()

        let diagnostics = await registry.diagnostics()
        XCTAssertEqual(diagnostics.proxyDisconnectCount, 1)
        XCTAssertEqual(diagnostics.bufferTruncationCount, 1)
    }

    func testDiagnosticsTracksLastInputRoutingError() async {
        let registry = ControllableSessionRegistry(
            defaultBufferLineLimit: 10,
            defaultBufferByteLimit: 1024,
            staleSessionGracePeriod: 30
        )

        await registry.register(
            ControllableSessionRegistration(id: "session-1", status: .running),
            inputHandler: { _ in
                throw StubInputError.routingFailure
            }
        )

        do {
            _ = try await registry.sendInput(.text("pwd"), to: "session-1")
            XCTFail("Expected input routing to fail")
        } catch {}

        let diagnostics = await registry.diagnostics()
        XCTAssertEqual(diagnostics.lastInputRoutingError, "send_input[session-1]: routing failed")
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

private enum StubInputError: LocalizedError {
    case routingFailure

    var errorDescription: String? {
        switch self {
        case .routingFailure:
            return "routing failed"
        }
    }
}
