import Combine
import Dispatch
import Foundation
import OnibiCore
import Darwin

final class LocalSessionProxyListener: ObservableObject, @unchecked Sendable {
    static let shared = LocalSessionProxyListener()

    @Published private(set) var isRunning = false
    @Published private(set) var socketPath: String
    @Published private(set) var lastError: String?
    @Published private(set) var connectionCount: Int = 0

    private let queue = DispatchQueue(label: "com.onibi.local-session-proxy-listener", qos: .userInitiated)
    private let registry = ControllableSessionRegistry.shared

    private var settings: AppSettings
    private var serverFileDescriptor: Int32 = -1
    private var acceptSource: DispatchSourceRead?
    private var cleanupTimer: DispatchSourceTimer?
    private var connections: [UUID: LocalSessionProxyConnection] = [:]
    private var sessionConnectionIDs: [String: UUID] = [:]
    private var cancellables = Set<AnyCancellable>()

    private init() {
        let settings = SettingsViewModel.shared.settings
        self.settings = settings
        self.socketPath = settings.sessionProxySocketPath
        setupSubscriptions()
    }

    func bootstrap() {
        socketPath = settings.sessionProxySocketPath
        if settings.remoteControlEnabled {
            start()
        } else {
            stop()
        }
    }

    func start() {
        queue.async { [weak self] in
            self?.startLocked()
        }
    }

    func stop() {
        queue.async { [weak self] in
            self?.stopLocked()
        }
    }

    private func setupSubscriptions() {
        EventBus.shared.settingsPublisher
            .receive(on: DispatchQueue.main)
            .sink { [weak self] newSettings in
                guard let self else {
                    return
                }

                let previousEnabled = self.settings.remoteControlEnabled
                let previousSocketPath = self.settings.sessionProxySocketPath
                self.settings = newSettings
                self.socketPath = newSettings.sessionProxySocketPath

                if newSettings.remoteControlEnabled {
                    if !previousEnabled || previousSocketPath != newSettings.sessionProxySocketPath {
                        self.stop()
                        self.start()
                    }
                } else if previousEnabled {
                    self.stop()
                }
            }
            .store(in: &cancellables)
    }

    private func startLocked() {
        guard serverFileDescriptor < 0 else {
            return
        }

        do {
            try OnibiConfig.ensureDirectoryExists()
            try removeExistingSocketIfNeeded(at: settings.sessionProxySocketPath)

            let fileDescriptor = socket(AF_UNIX, SOCK_STREAM, 0)
            guard fileDescriptor >= 0 else {
                throw LocalSessionProxyListenerError.posixFailure(function: "socket", code: errno)
            }

            do {
                try setNoSigPipe(fileDescriptor)
                try setNonBlocking(fileDescriptor)
                try withUnixSocketAddress(path: settings.sessionProxySocketPath) { pointer, length in
                    guard bind(fileDescriptor, pointer, length) == 0 else {
                        throw LocalSessionProxyListenerError.posixFailure(function: "bind", code: errno)
                    }
                }

                guard listen(fileDescriptor, SOMAXCONN) == 0 else {
                    throw LocalSessionProxyListenerError.posixFailure(function: "listen", code: errno)
                }

                serverFileDescriptor = fileDescriptor
                let acceptSource = DispatchSource.makeReadSource(fileDescriptor: fileDescriptor, queue: queue)
                acceptSource.setEventHandler { [weak self] in
                    self?.acceptPendingConnections()
                }
                acceptSource.setCancelHandler {}
                self.acceptSource = acceptSource
                acceptSource.resume()

                let cleanupTimer = DispatchSource.makeTimerSource(queue: queue)
                cleanupTimer.schedule(deadline: .now() + .seconds(10), repeating: .seconds(10))
                cleanupTimer.setEventHandler { [weak self] in
                    guard let self else {
                        return
                    }
                    Task {
                        await self.registry.expireDisconnectedSessions()
                    }
                }
                self.cleanupTimer = cleanupTimer
                cleanupTimer.resume()

                DispatchQueue.main.async {
                    self.socketPath = self.settings.sessionProxySocketPath
                    self.isRunning = true
                    self.lastError = nil
                }
                DiagnosticsStore.shared.record(
                    component: "LocalSessionProxyListener",
                    level: .info,
                    message: "local session proxy listener started",
                    metadata: [
                        "socketPath": settings.sessionProxySocketPath
                    ]
                )
            } catch {
                var closeFD = fileDescriptor
                closeIfNeeded(&closeFD)
                throw error
            }
        } catch {
            DispatchQueue.main.async {
                self.isRunning = false
                self.lastError = error.localizedDescription
            }
            DiagnosticsStore.shared.record(
                component: "LocalSessionProxyListener",
                level: .error,
                message: "failed to start local session proxy listener",
                metadata: [
                    "socketPath": settings.sessionProxySocketPath,
                    "reason": error.localizedDescription
                ]
            )
            ErrorReporter.shared.report(
                error,
                context: "LocalSessionProxyListener.start",
                severity: .warning
            )
        }
    }

    private func stopLocked() {
        acceptSource?.cancel()
        acceptSource = nil

        cleanupTimer?.cancel()
        cleanupTimer = nil

        for (_, connection) in connections {
            connection.stop()
        }
        connections.removeAll()
        sessionConnectionIDs.removeAll()
        publishConnectionCount()

        closeIfNeeded(&serverFileDescriptor)
        removeSocketFile(path: settings.sessionProxySocketPath)

        DispatchQueue.main.async {
            self.isRunning = false
        }
        DiagnosticsStore.shared.record(
            component: "LocalSessionProxyListener",
            level: .info,
            message: "local session proxy listener stopped",
            metadata: [
                "socketPath": settings.sessionProxySocketPath
            ]
        )
    }

    private func acceptPendingConnections() {
        while true {
            let clientFileDescriptor = accept(serverFileDescriptor, nil, nil)
            if clientFileDescriptor < 0 {
                if errno == EAGAIN || errno == EWOULDBLOCK {
                    return
                }

                DiagnosticsStore.shared.record(
                    component: "LocalSessionProxyListener",
                    level: .warning,
                    message: "failed accepting local proxy connection",
                    metadata: [
                        "reason": String(cString: strerror(errno))
                    ]
                )
                return
            }

            do {
                try setNoSigPipe(clientFileDescriptor)
                try setNonBlocking(clientFileDescriptor)
                let connection = LocalSessionProxyConnection(
                    fileDescriptor: clientFileDescriptor,
                    queue: queue
                ) { [weak self] connectionID, frameData in
                    self?.handleFrame(connectionID: connectionID, frameData: frameData)
                } onClose: { [weak self] connectionID in
                    self?.handleDisconnect(connectionID: connectionID)
                }
                connections[connection.id] = connection
                publishConnectionCount()
                connection.start()
            } catch {
                DiagnosticsStore.shared.record(
                    component: "LocalSessionProxyListener",
                    level: .warning,
                    message: "failed preparing local proxy connection",
                    metadata: [
                        "reason": error.localizedDescription
                    ]
                )
                Task {
                    await registry.markProxyRegistrationFailure()
                }
                var closeFD = clientFileDescriptor
                closeIfNeeded(&closeFD)
            }
        }
    }

    private func handleFrame(connectionID: UUID, frameData: Data) {
        Task {
            do {
                let envelope = try RealtimeGatewayCodec.decodeEnvelope(from: frameData)
                switch envelope.type {
                case .register:
                    try await handleRegisterFrame(connectionID: connectionID, frameData: frameData)
                case .output:
                    try await handleOutputFrame(frameData: frameData)
                case .state:
                    try await handleStateFrame(frameData: frameData)
                case .exit:
                    try await handleExitFrame(frameData: frameData)
                case .heartbeat:
                    try await handleHeartbeatFrame(frameData: frameData)
                case .input, .resize:
                    throw LocalSessionProxyListenerError.invalidFrameType(envelope.type.rawValue)
                }
            } catch {
                DiagnosticsStore.shared.record(
                    component: "LocalSessionProxyListener",
                    level: .warning,
                    message: "failed handling proxy frame",
                    metadata: [
                        "reason": error.localizedDescription
                    ]
                )
                await registry.markProxyRegistrationFailure()
                queue.async { [weak self] in
                    self?.connections[connectionID]?.stop()
                }
            }
        }
    }

    private func publishConnectionCount() {
        let count = connections.count
        DispatchQueue.main.async {
            if self.connectionCount != count {
                self.connectionCount = count
            }
        }
    }

    private func handleRegisterFrame(connectionID: UUID, frameData: Data) async throws {
        let message = try JSONDateCodec.decoder.decode(LocalSessionProxyRegisterMessage.self, from: frameData)
        guard let connection = connections[connectionID] else {
            return
        }

        if let existingConnectionID = sessionConnectionIDs[message.sessionId], existingConnectionID != connectionID {
            connections[existingConnectionID]?.stop()
            connections.removeValue(forKey: existingConnectionID)
            publishConnectionCount()
        }

        sessionConnectionIDs[message.sessionId] = connectionID
        connection.sessionId = message.sessionId

        let writer = connection.writer
        await registry.register(
            ControllableSessionRegistration(
                id: message.sessionId,
                displayName: displayName(for: message),
                startedAt: message.startedAt,
                status: .starting,
                isControllable: true,
                workingDirectory: message.workingDirectory
            ),
            inputHandler: { payload in
                try await writer.sendInput(payload, sessionId: message.sessionId)
            },
            resizeHandler: { payload in
                try await writer.sendResize(payload, sessionId: message.sessionId)
            }
        )
        await registry.updateSession(
            id: message.sessionId,
            status: .running,
            workingDirectory: message.workingDirectory,
            displayName: displayName(for: message),
            isControllable: true,
            at: Date()
        )
    }

    private func handleOutputFrame(frameData: Data) async throws {
        let message = try JSONDateCodec.decoder.decode(LocalSessionProxyOutputMessage.self, from: frameData)
        guard let data = message.decodedData else {
            throw LocalSessionProxyListenerError.invalidOutputPayload
        }
        await registry.appendOutput(
            sessionId: message.sessionId,
            stream: message.stream,
            data: data,
            timestamp: message.timestamp
        )
    }

    private func handleStateFrame(frameData: Data) async throws {
        let message = try JSONDateCodec.decoder.decode(LocalSessionProxyStateMessage.self, from: frameData)
        await registry.updateSession(
            id: message.sessionId,
            status: message.status,
            isControllable: message.status == .running,
            at: Date()
        )
    }

    private func handleExitFrame(frameData: Data) async throws {
        let message = try JSONDateCodec.decoder.decode(LocalSessionProxyExitMessage.self, from: frameData)
        await registry.updateSession(
            id: message.sessionId,
            status: .exited,
            isControllable: false,
            at: message.timestamp
        )
        await registry.setInputHandler(nil, for: message.sessionId)
        await registry.setResizeHandler(nil, for: message.sessionId)
    }

    private func handleHeartbeatFrame(frameData: Data) async throws {
        let message = try JSONDateCodec.decoder.decode(LocalSessionProxyHeartbeatMessage.self, from: frameData)
        await registry.recordHeartbeat(for: message.sessionId, at: message.timestamp)
    }

    private func handleDisconnect(connectionID: UUID) {
        guard let connection = connections.removeValue(forKey: connectionID) else {
            return
        }
        publishConnectionCount()

        let sessionId = connection.sessionId
        if let sessionId, sessionConnectionIDs[sessionId] == connectionID {
            sessionConnectionIDs.removeValue(forKey: sessionId)
        }

        Task {
            guard let sessionId else {
                return
            }

            await registry.setInputHandler(nil, for: sessionId)
            await registry.setResizeHandler(nil, for: sessionId)
            if let snapshot = await registry.session(id: sessionId), snapshot.status != .exited {
                await registry.updateSession(
                    id: sessionId,
                    status: .failed,
                    isControllable: false,
                    at: Date()
                )
            }
        }
    }

    private func displayName(for message: LocalSessionProxyRegisterMessage) -> String {
        if
            let workingDirectory = message.workingDirectory,
            !workingDirectory.isEmpty
        {
            let lastPathComponent = URL(fileURLWithPath: workingDirectory).lastPathComponent
            if !lastPathComponent.isEmpty {
                return lastPathComponent
            }
        }

        let shellName = URL(fileURLWithPath: message.shell).lastPathComponent
        return "\(shellName) \(message.pid)"
    }

    private func removeExistingSocketIfNeeded(at path: String) throws {
        guard FileManager.default.fileExists(atPath: path) else {
            return
        }
        guard unlink(path) == 0 else {
            throw LocalSessionProxyListenerError.posixFailure(function: "unlink", code: errno)
        }
    }

    private func removeSocketFile(path: String) {
        guard FileManager.default.fileExists(atPath: path) else {
            return
        }
        _ = unlink(path)
    }
}

private enum LocalSessionProxyListenerError: LocalizedError {
    case posixFailure(function: String, code: Int32)
    case unixSocketPathTooLong(String)
    case invalidFrameType(String)
    case invalidOutputPayload

    var errorDescription: String? {
        switch self {
        case .posixFailure(let function, let code):
            return "\(function) failed with errno \(code): \(String(cString: strerror(code)))"
        case .unixSocketPathTooLong(let path):
            return "Unix socket path is too long: \(path)"
        case .invalidFrameType(let type):
            return "Unsupported proxy frame type: \(type)"
        case .invalidOutputPayload:
            return "Proxy output payload could not be decoded"
        }
    }
}

private final class LocalSessionProxyConnection {
    let id = UUID()
    let writer: LocalSessionProxyConnectionWriter
    var sessionId: String?

    private let fileDescriptor: Int32
    private let queue: DispatchQueue
    private let onFrame: (UUID, Data) -> Void
    private let onClose: (UUID) -> Void

    private var readSource: DispatchSourceRead?
    private var readBuffer = Data()
    private var isClosed = false

    init(
        fileDescriptor: Int32,
        queue: DispatchQueue,
        onFrame: @escaping (UUID, Data) -> Void,
        onClose: @escaping (UUID) -> Void
    ) {
        self.fileDescriptor = fileDescriptor
        self.queue = queue
        self.onFrame = onFrame
        self.onClose = onClose
        self.writer = LocalSessionProxyConnectionWriter(
            fileDescriptor: fileDescriptor,
            queue: queue
        )
    }

    func start() {
        let readSource = DispatchSource.makeReadSource(fileDescriptor: fileDescriptor, queue: queue)
        readSource.setEventHandler { [weak self] in
            self?.handleReadable()
        }
        readSource.setCancelHandler {}
        self.readSource = readSource
        readSource.resume()
    }

    func stop() {
        guard !isClosed else {
            return
        }
        isClosed = true
        readSource?.cancel()
        readSource = nil

        var descriptor = fileDescriptor
        closeIfNeeded(&descriptor)
        onClose(id)
    }

    private func handleReadable() {
        guard !isClosed else {
            return
        }

        do {
            let chunks = try readAvailable(from: fileDescriptor)
            if chunks.isEmpty {
                stop()
                return
            }

            for chunk in chunks {
                readBuffer.append(chunk)
                let frames = RealtimeGatewayCodec.extractFrames(from: &readBuffer)
                for frame in frames {
                    onFrame(id, frame)
                }
            }
        } catch {
            stop()
        }
    }
}

private final class LocalSessionProxyConnectionWriter: @unchecked Sendable {
    private let fileDescriptor: Int32
    private let queue: DispatchQueue

    init(fileDescriptor: Int32, queue: DispatchQueue) {
        self.fileDescriptor = fileDescriptor
        self.queue = queue
    }

    func sendInput(_ payload: RemoteInputPayload, sessionId: String) async throws {
        let frame = LocalSessionProxyInputMessage(sessionId: sessionId, payload: payload)
        let data = try RealtimeGatewayCodec.encodeFrame(frame)

        try await withCheckedThrowingContinuation { continuation in
            queue.async {
                do {
                    try writeAll(data, to: self.fileDescriptor)
                    continuation.resume()
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }

    func sendResize(_ payload: RemoteTerminalResizePayload, sessionId: String) async throws {
        let frame = LocalSessionProxyResizeMessage(sessionId: sessionId, payload: payload)
        let data = try RealtimeGatewayCodec.encodeFrame(frame)

        try await withCheckedThrowingContinuation { continuation in
            queue.async {
                do {
                    try writeAll(data, to: self.fileDescriptor)
                    continuation.resume()
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
    }
}

private func readAvailable(from fileDescriptor: Int32) throws -> [Data] {
    var chunks: [Data] = []

    while true {
        var buffer = [UInt8](repeating: 0, count: 4096)
        let byteCount = Darwin.read(fileDescriptor, &buffer, buffer.count)

        if byteCount > 0 {
            chunks.append(Data(buffer.prefix(Int(byteCount))))
            if byteCount < buffer.count {
                break
            }
        } else if byteCount == 0 {
            return chunks
        } else if errno == EAGAIN || errno == EWOULDBLOCK {
            return chunks
        } else {
            throw LocalSessionProxyListenerError.posixFailure(function: "read", code: errno)
        }
    }

    return chunks
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
                throw LocalSessionProxyListenerError.posixFailure(function: "write", code: errno)
            }
        }
    }
}

private func setNonBlocking(_ fileDescriptor: Int32) throws {
    let currentFlags = fcntl(fileDescriptor, F_GETFL)
    guard currentFlags != -1 else {
        throw LocalSessionProxyListenerError.posixFailure(function: "fcntl(F_GETFL)", code: errno)
    }
    guard fcntl(fileDescriptor, F_SETFL, currentFlags | O_NONBLOCK) == 0 else {
        throw LocalSessionProxyListenerError.posixFailure(function: "fcntl(F_SETFL)", code: errno)
    }
}

private func setNoSigPipe(_ fileDescriptor: Int32) throws {
    var value: Int32 = 1
    let result = withUnsafePointer(to: &value) {
        setsockopt(fileDescriptor, SOL_SOCKET, SO_NOSIGPIPE, $0, socklen_t(MemoryLayout<Int32>.size))
    }
    guard result == 0 else {
        throw LocalSessionProxyListenerError.posixFailure(function: "setsockopt(SO_NOSIGPIPE)", code: errno)
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
        throw LocalSessionProxyListenerError.unixSocketPathTooLong(path)
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
