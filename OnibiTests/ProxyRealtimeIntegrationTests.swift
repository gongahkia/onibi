import XCTest
import OnibiCore
@testable import Onibi
import Darwin

final class ProxyRealtimeIntegrationTests: XCTestCase {
    func testProxyRegisterAndOutputBroadcastToRealtimeSubscriber() async throws {
        let socketPath = temporarySocketPath()
        let originalSettings = try await prepareListener(socketPath: socketPath)
        let proxyClient = try UnixSocketTestClient(path: socketPath)
        let registry = ControllableSessionRegistry.shared
        await registry.clearAll()
        let service = makeRealtimeService(registry: registry)
        await service.start()
        defer {
            proxyClient.close()
        }

        do {
            let sessionId = "proxy-int-\(UUID().uuidString)"
            let transport = IntegrationRealtimeTransport()
            await service.attach(transport)
            await service.receive(
                text: try encode(
                    RealtimeClientMessage(type: .auth, token: "integration-token")
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

            try proxyClient.sendFrame(
                LocalSessionProxyRegisterMessage(
                    sessionId: sessionId,
                    shell: "/bin/zsh",
                    pid: 1234,
                    startedAt: Date(),
                    workingDirectory: "/tmp",
                    hostname: "localhost",
                    proxyVersion: "integration-proxy"
                )
            )

            let didRegister = await waitUntil(timeoutSeconds: 1.0) {
                await registry.session(id: sessionId) != nil
            }
            XCTAssertTrue(didRegister)
            let registeredSnapshot = await registry.session(id: sessionId)
            XCTAssertEqual(registeredSnapshot?.shell, "/bin/zsh")
            XCTAssertEqual(registeredSnapshot?.pid, 1234)
            XCTAssertEqual(registeredSnapshot?.hostname, "localhost")
            XCTAssertEqual(registeredSnapshot?.proxyVersion, "integration-proxy")

            try proxyClient.sendFrame(
                LocalSessionProxyMetadataMessage(
                    sessionId: sessionId,
                    workingDirectory: "/tmp/updated",
                    terminalCols: 132,
                    terminalRows: 38,
                    terminalTitle: "tail -f app.log"
                )
            )

            let didApplyMetadata = await waitUntil(timeoutSeconds: 1.0) {
                let snapshot = await registry.session(id: sessionId)
                return snapshot?.workingDirectory == "/tmp/updated" &&
                    snapshot?.terminalCols == 132 &&
                    snapshot?.terminalRows == 38 &&
                    snapshot?.terminalTitle == "tail -f app.log"
            }
            XCTAssertTrue(didApplyMetadata)

            try proxyClient.sendFrame(
                LocalSessionProxyTerminalEventMessage(
                    sessionId: sessionId,
                    event: .workingDirectory,
                    value: "/tmp/event-cwd",
                    timestamp: Date()
                )
            )

            let didApplyTerminalEvent = await waitUntil(timeoutSeconds: 1.0) {
                let snapshot = await registry.session(id: sessionId)
                return snapshot?.workingDirectory == "/tmp/event-cwd" &&
                    snapshot?.lastTerminalEvent?.kind == .workingDirectory &&
                    snapshot?.lastTerminalEvent?.value == "/tmp/event-cwd"
            }
            XCTAssertTrue(didApplyTerminalEvent)

            try proxyClient.sendFrame(
                LocalSessionProxyHealthMessage(
                    sessionId: sessionId,
                    timestamp: Date(),
                    canReceiveInput: true,
                    flowControl: .open,
                    inputByteCount: 42,
                    outputByteCount: 2048,
                    droppedOutputByteCount: 0,
                    lastInputAt: Date(),
                    lastOutputAt: Date()
                )
            )

            let didApplyHealth = await waitUntil(timeoutSeconds: 1.0) {
                let snapshot = await registry.session(id: sessionId)
                return snapshot?.health?.canReceiveInput == true &&
                    snapshot?.health?.flowControl == .open &&
                    snapshot?.health?.inputByteCount == 42 &&
                    snapshot?.health?.outputByteCount == 2048
            }
            XCTAssertTrue(didApplyHealth)

            try proxyClient.sendFrame(
                LocalSessionProxyCommandStartMessage(
                    sessionId: sessionId,
                    command: "npm run build -- --token sk-test",
                    workingDirectory: "/tmp/event-cwd",
                    timestamp: Date()
                )
            )

            let didApplyCommandStart = await waitUntil(timeoutSeconds: 1.0) {
                let snapshot = await registry.session(id: sessionId)
                return snapshot?.workingDirectory == "/tmp/event-cwd" &&
                    snapshot?.lastCommandPreview == "npm run build +3"
            }
            XCTAssertTrue(didApplyCommandStart)

            try proxyClient.sendFrame(
                LocalSessionProxyCommandEndMessage(
                    sessionId: sessionId,
                    exitCode: 0,
                    workingDirectory: "/tmp/event-cwd",
                    timestamp: Date()
                )
            )

            let didApplyCommandEnd = await waitUntil(timeoutSeconds: 1.0) {
                let snapshot = await registry.session(id: sessionId)
                return snapshot?.lastCommandPreview == "npm run build +3"
            }
            XCTAssertTrue(didApplyCommandEnd)

            try proxyClient.sendFrame(
                LocalSessionProxyOutputMessage(
                    sessionId: sessionId,
                    stream: .stdout,
                    timestamp: Date(),
                    outputData: Data("hello from proxy\n".utf8)
                )
            )

            let didReceiveOutput = await waitUntil(timeoutSeconds: 1.0) {
                let messages = await transport.messages()
                return messages.contains { message in
                    message.type == .output &&
                    message.sessionId == sessionId &&
                    String(data: message.chunk?.data ?? Data(), encoding: .utf8) == "hello from proxy\n"
                }
            }
            XCTAssertTrue(didReceiveOutput)
        } catch {
            await service.stop()
            await restoreListenerSettings(originalSettings)
            throw error
        }

        await service.stop()
        await restoreListenerSettings(originalSettings)
    }

    func testRealtimeSendInputRoutesToLocalProxyConnection() async throws {
        let socketPath = temporarySocketPath()
        let originalSettings = try await prepareListener(socketPath: socketPath)
        let proxyClient = try UnixSocketTestClient(path: socketPath)
        let registry = ControllableSessionRegistry.shared
        await registry.clearAll()
        let service = makeRealtimeService(registry: registry)
        await service.start()
        defer {
            proxyClient.close()
        }

        do {
            let sessionId = "proxy-input-\(UUID().uuidString)"
            try proxyClient.sendFrame(
                LocalSessionProxyRegisterMessage(
                    sessionId: sessionId,
                    shell: "/bin/zsh",
                    pid: 5678,
                    startedAt: Date(),
                    workingDirectory: "/tmp",
                    hostname: "localhost"
                )
            )

            let didRegister = await waitUntil(timeoutSeconds: 1.0) {
                await registry.session(id: sessionId) != nil
            }
            XCTAssertTrue(didRegister)

            let transport = IntegrationRealtimeTransport()
            await service.attach(transport)
            await service.receive(
                text: try encode(
                    RealtimeClientMessage(type: .auth, token: "integration-token")
                ),
                from: transport.id
            )
            await transport.clear()

            await service.receive(
                text: try encode(
                    RealtimeClientMessage(
                        type: .sendInput,
                        sessionId: sessionId,
                        kind: .key,
                        key: .enter,
                        clientRequestId: "req-1"
                    )
                ),
                from: transport.id
            )

            let websocketMessages = await transport.messages()
            XCTAssertEqual(websocketMessages.first?.type, .inputAccepted)
            XCTAssertEqual(websocketMessages.first?.clientRequestId, "req-1")

            let frame = try proxyClient.readFrame(timeoutSeconds: 1.0)
            XCTAssertNotNil(frame)
            if let frame {
                let inputMessage = try JSONDateCodec.decoder.decode(LocalSessionProxyInputMessage.self, from: frame)
                XCTAssertEqual(inputMessage.sessionId, sessionId)
                XCTAssertEqual(inputMessage.payload, .key(.enter))
            }

            await transport.clear()
            await service.receive(
                text: try encode(
                    RealtimeClientMessage(
                        type: .sendInput,
                        sessionId: sessionId,
                        kind: .paste,
                        text: "line 1\nline 2",
                        clientRequestId: "req-2"
                    )
                ),
                from: transport.id
            )

            let pasteFrame = try proxyClient.readFrame(timeoutSeconds: 1.0)
            XCTAssertNotNil(pasteFrame)
            if let pasteFrame {
                let inputMessage = try JSONDateCodec.decoder.decode(LocalSessionProxyInputMessage.self, from: pasteFrame)
                XCTAssertEqual(inputMessage.sessionId, sessionId)
                XCTAssertEqual(inputMessage.payload, .paste("line 1\nline 2"))
            }

            await transport.clear()
            await service.receive(
                text: try encode(
                    RealtimeClientMessage(
                        type: .sendInput,
                        sessionId: sessionId,
                        kind: .file,
                        data: Data("echo file\n".utf8).base64EncodedString(),
                        fileName: "script.sh",
                        clientRequestId: "req-3"
                    )
                ),
                from: transport.id
            )

            let fileFrame = try proxyClient.readFrame(timeoutSeconds: 1.0)
            XCTAssertNotNil(fileFrame)
            if let fileFrame {
                let inputMessage = try JSONDateCodec.decoder.decode(LocalSessionProxyInputMessage.self, from: fileFrame)
                XCTAssertEqual(inputMessage.sessionId, sessionId)
                XCTAssertEqual(inputMessage.payload, .file(name: "script.sh", data: Data("echo file\n".utf8)))
            }

            await transport.clear()
            await service.receive(
                text: try encode(
                    RealtimeClientMessage(
                        type: .processAction,
                        sessionId: sessionId,
                        action: .terminate,
                        clientRequestId: "action-1"
                    )
                ),
                from: transport.id
            )

            let processActionMessages = await transport.messages()
            XCTAssertEqual(processActionMessages.first?.type, .processActionAccepted)
            XCTAssertEqual(processActionMessages.first?.action, .terminate)
            XCTAssertEqual(processActionMessages.first?.clientRequestId, "action-1")

            let actionFrame = try proxyClient.readFrame(timeoutSeconds: 1.0)
            XCTAssertNotNil(actionFrame)
            if let actionFrame {
                let actionMessage = try JSONDateCodec.decoder.decode(LocalSessionProxyProcessActionMessage.self, from: actionFrame)
                XCTAssertEqual(actionMessage.sessionId, sessionId)
                XCTAssertEqual(actionMessage.payload, RemoteProcessActionPayload(action: .terminate))
            }
        } catch {
            await service.stop()
            await restoreListenerSettings(originalSettings)
            throw error
        }

        await service.stop()
        await restoreListenerSettings(originalSettings)
    }

    private func makeRealtimeService(registry: ControllableSessionRegistry) -> RealtimeGatewayService {
        RealtimeGatewayService(
            registry: registry,
            tokenProvider: { "integration-token" },
            hostVersionProvider: { "integration-host" }
        )
    }

    private func encode(_ message: RealtimeClientMessage) throws -> String {
        let data = try JSONEncoder().encode(message)
        guard let text = String(data: data, encoding: .utf8) else {
            throw TestError.encodingFailed
        }
        return text
    }

    private func prepareListener(socketPath: String) async throws -> AppSettings {
        let originalSettings = await MainActor.run { SettingsViewModel.shared.settings }

        var updatedSettings = originalSettings
        updatedSettings.remoteControlEnabled = true
        updatedSettings.sessionProxySocketPath = socketPath
        let nextSettings = updatedSettings

        await MainActor.run {
            SettingsViewModel.shared.settings = nextSettings
        }

        LocalSessionProxyListener.shared.stop()
        _ = await waitUntil(timeoutSeconds: 1.0) {
            await MainActor.run { !LocalSessionProxyListener.shared.isRunning }
        }

        LocalSessionProxyListener.shared.start()
        guard await waitUntil(timeoutSeconds: 2.0, condition: {
            await MainActor.run { LocalSessionProxyListener.shared.isRunning }
        }) else {
            throw TestError.listenerDidNotStart
        }

        return originalSettings
    }

    private func restoreListenerSettings(_ settings: AppSettings) async {
        LocalSessionProxyListener.shared.stop()
        _ = await waitUntil(timeoutSeconds: 1.0) {
            await MainActor.run { !LocalSessionProxyListener.shared.isRunning }
        }
        await MainActor.run {
            SettingsViewModel.shared.settings = settings
        }
    }

    private func waitUntil(
        timeoutSeconds: TimeInterval,
        intervalNanoseconds: UInt64 = 20_000_000,
        condition: @escaping @Sendable () async -> Bool
    ) async -> Bool {
        let deadline = Date().addingTimeInterval(timeoutSeconds)
        while Date() < deadline {
            if await condition() {
                return true
            }
            try? await Task.sleep(nanoseconds: intervalNanoseconds)
        }
        return await condition()
    }

    private func temporarySocketPath() -> String {
        "/tmp/onibi-\(UUID().uuidString.prefix(8)).sock"
    }
}

private actor IntegrationRealtimeTransport: RealtimeClientTransport {
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
}

private final class UnixSocketTestClient {
    private var fileDescriptor: Int32
    private var readBuffer = Data()

    init(path: String) throws {
        let descriptor = socket(AF_UNIX, SOCK_STREAM, 0)
        guard descriptor >= 0 else {
            throw TestError.socketCreateFailed(errno)
        }

        do {
            try setNonBlocking(descriptor)
            try withUnixSocketAddress(path: path) { pointer, length in
                guard connect(descriptor, pointer, length) == 0 else {
                    throw TestError.socketConnectFailed(errno)
                }
            }
            self.fileDescriptor = descriptor
        } catch {
            var closeFD = descriptor
            closeIfNeeded(&closeFD)
            throw error
        }
    }

    func sendFrame<T: Encodable>(_ frame: T) throws {
        let data = try RealtimeGatewayCodec.encodeFrame(frame)
        try writeAll(data, to: fileDescriptor)
    }

    func readFrame(timeoutSeconds: TimeInterval) throws -> Data? {
        let deadline = Date().addingTimeInterval(timeoutSeconds)
        while Date() < deadline {
            var bytes = [UInt8](repeating: 0, count: 4096)
            let byteCount = Darwin.read(fileDescriptor, &bytes, bytes.count)

            if byteCount > 0 {
                readBuffer.append(Data(bytes.prefix(Int(byteCount))))
            } else if byteCount == 0 {
                return nil
            } else if errno != EAGAIN && errno != EWOULDBLOCK {
                throw TestError.socketReadFailed(errno)
            }

            let frames = RealtimeGatewayCodec.extractFrames(from: &readBuffer)
            if let first = frames.first {
                return first
            }

            usleep(10_000)
        }
        return nil
    }

    func close() {
        closeIfNeeded(&fileDescriptor)
    }
}

private enum TestError: LocalizedError {
    case encodingFailed
    case listenerDidNotStart
    case socketCreateFailed(Int32)
    case socketConnectFailed(Int32)
    case socketReadFailed(Int32)

    var errorDescription: String? {
        switch self {
        case .encodingFailed:
            return "Failed to encode message to UTF-8 text"
        case .listenerDidNotStart:
            return "LocalSessionProxyListener did not report running state in time"
        case .socketCreateFailed(let code):
            return "socket() failed with errno \(code)"
        case .socketConnectFailed(let code):
            return "connect() failed with errno \(code)"
        case .socketReadFailed(let code):
            return "read() failed with errno \(code)"
        }
    }
}

private func writeAll(_ data: Data, to fileDescriptor: Int32) throws {
    try data.withUnsafeBytes { rawBuffer in
        guard let baseAddress = rawBuffer.baseAddress else {
            return
        }

        var offset = 0
        while offset < rawBuffer.count {
            let pointer = baseAddress.advanced(by: offset)
            let written = Darwin.write(fileDescriptor, pointer, rawBuffer.count - offset)
            if written > 0 {
                offset += written
            } else if written == -1 && (errno == EAGAIN || errno == EWOULDBLOCK) {
                continue
            } else {
                throw TestError.socketReadFailed(errno)
            }
        }
    }
}

private func setNonBlocking(_ fileDescriptor: Int32) throws {
    let currentFlags = fcntl(fileDescriptor, F_GETFL)
    guard currentFlags != -1 else {
        throw TestError.socketCreateFailed(errno)
    }
    guard fcntl(fileDescriptor, F_SETFL, currentFlags | O_NONBLOCK) == 0 else {
        throw TestError.socketCreateFailed(errno)
    }
}

private func withUnixSocketAddress<Result>(
    path: String,
    _ body: (UnsafePointer<sockaddr>, socklen_t) throws -> Result
) throws -> Result {
    var address = sockaddr_un()
    let pathLength = path.utf8.count
    let maxPathLength = MemoryLayout.size(ofValue: address.sun_path)
    guard pathLength < maxPathLength else {
        throw TestError.socketConnectFailed(ENAMETOOLONG)
    }

    address.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
    address.sun_family = sa_family_t(AF_UNIX)
    withUnsafeMutableBytes(of: &address.sun_path) { rawBuffer in
        guard let baseAddress = rawBuffer.baseAddress else {
            return
        }
        let buffer = baseAddress.assumingMemoryBound(to: CChar.self)
        path.withCString { source in
            strncpy(buffer, source, rawBuffer.count - 1)
            buffer[rawBuffer.count - 1] = 0
        }
    }

    let length = socklen_t(MemoryLayout<sockaddr_un>.size)
    return try withUnsafePointer(to: &address) { pointer in
        try pointer.withMemoryRebound(to: sockaddr.self, capacity: 1) { sockaddrPointer in
            try body(sockaddrPointer, length)
        }
    }
}

private func closeIfNeeded(_ fileDescriptor: inout Int32) {
    guard fileDescriptor >= 0 else {
        return
    }
    _ = Darwin.close(fileDescriptor)
    fileDescriptor = -1
}
