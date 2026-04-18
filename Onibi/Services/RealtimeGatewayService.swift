import CryptoKit
import Dispatch
import Foundation
import Network
import OnibiCore

protocol RealtimeClientTransport: AnyObject, Sendable {
    var id: UUID { get }
    func send(_ message: RealtimeServerMessage) async throws
    func close() async
}

actor RealtimeGatewayService {
    static let shared = RealtimeGatewayService(
        registry: .shared,
        tokenProvider: {
            try PairingTokenStore(
                service: "com.onibi.mobile.host",
                account: "pairing-token"
            ).ensureToken()
        },
        hostVersionProvider: {
            Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "unknown"
        }
    )

    private struct ClientState {
        let transport: any RealtimeClientTransport
        var isAuthenticated: Bool
        var subscriptions: Set<String>
        let connectedAt: Date
        let peer: String
    }

    struct ClientInfo: Identifiable, Equatable {
        let id: UUID
        let connectedAt: Date
        let peer: String
        let isAuthenticated: Bool
        let subscribedSessions: [String]
    }

    private let registry: ControllableSessionRegistry
    private let tokenProvider: @Sendable () throws -> String
    private let hostVersionProvider: @Sendable () -> String
    private var clients: [UUID: ClientState] = [:]
    private var registryObserverID: UUID?

    // Coalescing buffer: dozens of `session_updated` events can fire in the same ms when
    // a shell hook churns metadata. We dedupe by session id and flush once per window
    // to keep tunnels (Cloudflare Quick Tunnel in particular) from dropping under burst.
    private var pendingSessionUpdates: [String: ControllableSessionSnapshot] = [:]
    private var updateFlushTask: Task<Void, Never>?
    private let updateCoalesceWindowNanos: UInt64 = 150_000_000 // 150ms

    init(
        registry: ControllableSessionRegistry = .shared,
        tokenProvider: @escaping @Sendable () throws -> String,
        hostVersionProvider: @escaping @Sendable () -> String
    ) {
        self.registry = registry
        self.tokenProvider = tokenProvider
        self.hostVersionProvider = hostVersionProvider
    }

    func start() async {
        guard registryObserverID == nil else {
            return
        }

        let service = self
        registryObserverID = await registry.addObserver { event in
            Task {
                await service.handleRegistryEvent(event)
            }
        }
    }

    func stop() async {
        if let registryObserverID {
            await registry.removeObserver(id: registryObserverID)
            self.registryObserverID = nil
        }

        updateFlushTask?.cancel()
        updateFlushTask = nil
        pendingSessionUpdates.removeAll()

        let transports = clients.values.map(\.transport)
        clients.removeAll()
        for transport in transports {
            await transport.close()
        }
    }

    func connectedClientCount() -> Int {
        clients.count
    }

    func attach(_ transport: any RealtimeClientTransport, peer: String = "unknown") {
        clients[transport.id] = ClientState(
            transport: transport,
            isAuthenticated: false,
            subscriptions: [],
            connectedAt: Date(),
            peer: peer
        )
    }

    func disconnect(clientID: UUID) async {
        clients.removeValue(forKey: clientID)
    }

    /// Snapshot of all currently attached clients for the Settings "Connected Clients" view.
    func clientsSnapshot() -> [ClientInfo] {
        clients.values
            .map { state in
                ClientInfo(
                    id: state.transport.id,
                    connectedAt: state.connectedAt,
                    peer: state.peer,
                    isAuthenticated: state.isAuthenticated,
                    subscribedSessions: Array(state.subscriptions).sorted()
                )
            }
            .sorted { $0.connectedAt < $1.connectedAt }
    }

    /// Force-disconnect a client. Closes the WebSocket; client's reconnect logic may
    /// attempt to rejoin, but the next auth check re-evaluates the token.
    func kick(clientID: UUID) async {
        guard let state = clients.removeValue(forKey: clientID) else { return }
        await state.transport.close()
    }

    func receive(text: String, from clientID: UUID) async {
        guard var client = clients[clientID] else {
            return
        }

        let message: RealtimeClientMessage
        do {
            message = try JSONDateCodec.decoder.decode(RealtimeClientMessage.self, from: Data(text.utf8))
        } catch {
            try? await client.transport.send(.error(code: "invalid_message", message: "Invalid realtime JSON message"))
            return
        }

        if !client.isAuthenticated {
            guard message.type == .auth else {
                try? await client.transport.send(.error(code: "unauthorized", message: "Authenticate before sending realtime frames"))
                return
            }

            do {
                let expectedToken = try tokenProvider()
                guard let token = message.token, token == expectedToken else {
                    try? await client.transport.send(.error(code: "unauthorized", message: "Invalid pairing token"))
                    await client.transport.close()
                    clients.removeValue(forKey: clientID)
                    return
                }
            } catch {
                try? await client.transport.send(.error(code: "auth_provider_failure", message: error.localizedDescription))
                await client.transport.close()
                clients.removeValue(forKey: clientID)
                return
            }

            client.isAuthenticated = true
            clients[clientID] = client
            try? await client.transport.send(.authOK(hostVersion: hostVersionProvider()))
            let sessions = await registry.sessionsSnapshot()
            try? await client.transport.send(.sessionsSnapshot(sessions))
            return
        }

        switch message.type {
        case .auth:
            try? await client.transport.send(.error(code: "already_authenticated", message: "Client already authenticated"))
        case .subscribe:
            guard let sessionId = message.sessionId else {
                try? await client.transport.send(.error(code: "invalid_session_id", message: "Missing sessionId"))
                return
            }
            client.subscriptions.insert(sessionId)
            clients[clientID] = client
        case .unsubscribe:
            guard let sessionId = message.sessionId else {
                try? await client.transport.send(.error(code: "invalid_session_id", message: "Missing sessionId"))
                return
            }
            client.subscriptions.remove(sessionId)
            clients[clientID] = client
        case .requestBuffer:
            guard let sessionId = message.sessionId else {
                try? await client.transport.send(.error(code: "invalid_session_id", message: "Missing sessionId"))
                return
            }
            guard let snapshot = await registry.bufferSnapshot(for: sessionId) else {
                try? await client.transport.send(.error(code: "session_not_found", message: "Session not found"))
                return
            }
            try? await client.transport.send(.bufferSnapshot(snapshot))
        case .sendInput:
            guard let sessionId = message.sessionId else {
                try? await client.transport.send(.error(code: "invalid_session_id", message: "Missing sessionId"))
                return
            }
            guard let payload = message.inputPayload else {
                try? await client.transport.send(.error(code: "invalid_input_payload", message: "Invalid realtime input payload"))
                return
            }
            do {
                _ = try await registry.sendInput(payload, to: sessionId)
                try? await client.transport.send(
                    .inputAccepted(
                        sessionId: sessionId,
                        clientRequestId: message.clientRequestId
                    )
                )
            } catch let error as RemoteControlError {
                try? await client.transport.send(
                    .error(
                        code: errorCode(for: error),
                        message: error.localizedDescription
                    )
                )
            } catch {
                try? await client.transport.send(.error(code: "internal_error", message: error.localizedDescription))
            }
        case .resize:
            guard let sessionId = message.sessionId else {
                try? await client.transport.send(.error(code: "invalid_session_id", message: "Missing sessionId"))
                return
            }
            guard let payload = message.resizePayload else {
                try? await client.transport.send(.error(code: "invalid_resize_payload", message: "Invalid resize payload"))
                return
            }
            do {
                _ = try await registry.resizeTerminal(payload, for: sessionId)
            } catch let error as RemoteControlError {
                try? await client.transport.send(
                    .error(
                        code: errorCode(for: error),
                        message: error.localizedDescription
                    )
                )
            } catch {
                try? await client.transport.send(.error(code: "internal_error", message: error.localizedDescription))
            }
        }
    }

    private func handleRegistryEvent(_ event: ControllableSessionRegistryEvent) async {
        let authenticatedClients = clients.values.filter(\.isAuthenticated)
        guard !authenticatedClients.isEmpty else {
            return
        }

        switch event {
        case .sessionAdded(let session):
            // Add is latency-sensitive and rare — flush any pending updates for the same
            // id first so clients can't see an update for a session they don't know about yet.
            pendingSessionUpdates.removeValue(forKey: session.id)
            for client in authenticatedClients {
                try? await client.transport.send(.sessionAdded(session))
            }
        case .sessionUpdated(let session):
            pendingSessionUpdates[session.id] = session
            scheduleUpdateFlush()
        case .sessionRemoved(let sessionId):
            pendingSessionUpdates.removeValue(forKey: sessionId)
            for client in authenticatedClients {
                try? await client.transport.send(.sessionRemoved(sessionId))
            }
        case .output(let chunk):
            for client in authenticatedClients where client.subscriptions.contains(chunk.sessionId) {
                try? await client.transport.send(.output(sessionId: chunk.sessionId, chunk: chunk))
            }
        }
    }

    private func scheduleUpdateFlush() {
        guard updateFlushTask == nil else { return }
        updateFlushTask = Task { [weak self] in
            guard let self else { return }
            try? await Task.sleep(nanoseconds: await self.updateCoalesceWindowNanos)
            await self.flushPendingSessionUpdates()
        }
    }

    private func flushPendingSessionUpdates() async {
        updateFlushTask = nil
        guard !pendingSessionUpdates.isEmpty else { return }
        let snapshots = Array(pendingSessionUpdates.values)
        pendingSessionUpdates.removeAll()
        let authenticatedClients = clients.values.filter(\.isAuthenticated)
        guard !authenticatedClients.isEmpty else { return }
        for snapshot in snapshots {
            for client in authenticatedClients {
                try? await client.transport.send(.sessionUpdated(snapshot))
            }
        }
    }

    private func errorCode(for error: RemoteControlError) -> String {
        switch error {
        case .sessionNotFound:
            return "session_not_found"
        case .sessionNotControllable:
            return "session_not_controllable"
        case .inputUnavailable:
            return "input_unavailable"
        case .invalidInputPayload:
            return "invalid_input_payload"
        case .resizeUnavailable:
            return "resize_unavailable"
        case .invalidResizePayload:
            return "invalid_resize_payload"
        }
    }

}

final class GatewayWebSocketConnection: @unchecked Sendable, RealtimeClientTransport {
    static let websocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

    let id: UUID

    private let connection: NWConnection
    private let queue: DispatchQueue
    private let onText: @Sendable (String) -> Void
    private let onDisconnect: @Sendable (UUID) -> Void

    private var receiveBuffer = Data()
    private var isClosed = false

    init(
        id: UUID = UUID(),
        connection: NWConnection,
        queue: DispatchQueue,
        onText: @escaping @Sendable (String) -> Void,
        onDisconnect: @escaping @Sendable (UUID) -> Void
    ) {
        self.id = id
        self.connection = connection
        self.queue = queue
        self.onText = onText
        self.onDisconnect = onDisconnect
    }

    static func handshakeResponse(for secWebSocketKey: String) -> Data {
        let accept = websocketAcceptValue(for: secWebSocketKey)
        // Keep the HTTP status line at byte 0; leading whitespace/newlines can break
        // URLSession's websocket handshake parser and surface as request timeouts.
        let response =
            "HTTP/1.1 101 Switching Protocols\r\n" +
            "Upgrade: websocket\r\n" +
            "Connection: Upgrade\r\n" +
            "Sec-WebSocket-Accept: \(accept)\r\n" +
            "\r\n"
        return Data(response.utf8)
    }

    static func isUpgradeRequest(path: String, headers: [String: String]) -> Bool {
        guard path == "/api/v2/realtime" else {
            return false
        }

        let upgrade = header(named: "Upgrade", in: headers)?.lowercased()
        let connection = header(named: "Connection", in: headers)?.lowercased() ?? ""
        let version = header(named: "Sec-WebSocket-Version", in: headers)
        let key = header(named: "Sec-WebSocket-Key", in: headers)

        return upgrade == "websocket"
            && connection.contains("upgrade")
            && version == "13"
            && key?.isEmpty == false
    }

    func start() {
        receiveNextFrame()
    }

    func send(_ message: RealtimeServerMessage) async throws {
        let data = try GatewayWebSocketJSONCodec.encode(message)
        try await sendFrame(.text(data))
    }

    func close() async {
        guard !isClosed else {
            return
        }

        isClosed = true
        try? await sendFrame(.close(Data()))
        connection.cancel()
        onDisconnect(id)
    }

    private func receiveNextFrame() {
        guard !isClosed else {
            return
        }

        connection.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self] data, _, isComplete, error in
            guard let self else {
                return
            }

            if let error {
                DiagnosticsStore.shared.record(
                    component: "GatewayWebSocketConnection",
                    level: .warning,
                    message: "websocket receive failed",
                    metadata: ["reason": error.localizedDescription]
                )
                Task {
                    await self.close()
                }
                return
            }

            if let data, !data.isEmpty {
                self.receiveBuffer.append(data)
                do {
                    let frames = try WebSocketFrameCodec.extractFrames(from: &self.receiveBuffer)
                    for frame in frames {
                        self.handle(frame: frame)
                    }
                } catch {
                    Task {
                        await self.close()
                    }
                    return
                }
            }

            if isComplete {
                Task {
                    await self.close()
                }
                return
            }

            self.receiveNextFrame()
        }
    }

    private func handle(frame: WebSocketFrame) {
        switch frame.opcode {
        case .text:
            if let text = String(data: frame.payload, encoding: .utf8) {
                onText(text)
            }
        case .ping:
            Task {
                try? await sendFrame(.pong(frame.payload))
            }
        case .close:
            Task {
                await close()
            }
        case .pong:
            break
        }
    }

    private func sendFrame(_ frame: WebSocketOutboundFrame) async throws {
        guard !isClosed else {
            return
        }

        let data = try WebSocketFrameCodec.encode(frame)
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            connection.send(content: data, completion: .contentProcessed { error in
                if let error {
                    continuation.resume(throwing: error)
                } else {
                    continuation.resume()
                }
            })
        }
    }

    private static func websocketAcceptValue(for key: String) -> String {
        let combined = Data((key + websocketGUID).utf8)
        let digest = Insecure.SHA1.hash(data: combined)
        return Data(digest).base64EncodedString()
    }

    private static func header(named name: String, in headers: [String: String]) -> String? {
        headers.first { $0.key.caseInsensitiveCompare(name) == .orderedSame }?.value
    }
}

private enum GatewayWebSocketJSONCodec {
    private static let encoder: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }()

    static func encode(_ message: RealtimeServerMessage) throws -> Data {
        try encoder.encode(message)
    }
}

private struct WebSocketFrame {
    let opcode: WebSocketOpcode
    let payload: Data
}

private enum WebSocketOpcode: UInt8 {
    case text = 0x1
    case close = 0x8
    case ping = 0x9
    case pong = 0xA
}

private enum WebSocketOutboundFrame {
    case text(Data)
    case pong(Data)
    case close(Data)

    var opcode: WebSocketOpcode {
        switch self {
        case .text:
            return .text
        case .pong:
            return .pong
        case .close:
            return .close
        }
    }

    var payload: Data {
        switch self {
        case .text(let data), .pong(let data), .close(let data):
            return data
        }
    }
}

private enum WebSocketFrameCodecError: Error {
    case unsupportedOpcode
    case clientFrameNotMasked
}

private enum WebSocketFrameCodec {
    static func extractFrames(from buffer: inout Data) throws -> [WebSocketFrame] {
        var frames: [WebSocketFrame] = []
        var offset = 0

        while true {
            let remaining = buffer.count - offset
            guard remaining >= 2 else {
                break
            }

            let firstByte = buffer[offset]
            let secondByte = buffer[offset + 1]
            let opcodeRaw = firstByte & 0x0F
            guard let opcode = WebSocketOpcode(rawValue: opcodeRaw) else {
                throw WebSocketFrameCodecError.unsupportedOpcode
            }

            let isMasked = (secondByte & 0x80) != 0
            guard isMasked else {
                throw WebSocketFrameCodecError.clientFrameNotMasked
            }

            var payloadLength = Int(secondByte & 0x7F)
            var headerLength = 2

            if payloadLength == 126 {
                guard remaining >= headerLength + 2 else { break }
                payloadLength = Int(buffer[offset + 2]) << 8 | Int(buffer[offset + 3])
                headerLength += 2
            } else if payloadLength == 127 {
                guard remaining >= headerLength + 8 else { break }
                payloadLength = 0
                for index in 0..<8 {
                    payloadLength = (payloadLength << 8) | Int(buffer[offset + 2 + index])
                }
                headerLength += 8
            }

            guard remaining >= headerLength + 4 + payloadLength else {
                break
            }

            let maskStart = offset + headerLength
            let payloadStart = maskStart + 4
            let mask = Array(buffer[maskStart..<payloadStart])

            var payload = Data(buffer[payloadStart..<(payloadStart + payloadLength)])
            payload.withUnsafeMutableBytes { rawBuffer in
                guard let baseAddress = rawBuffer.baseAddress else {
                    return
                }
                let bytes = baseAddress.assumingMemoryBound(to: UInt8.self)
                for index in 0..<payloadLength {
                    bytes[index] ^= mask[index % 4]
                }
            }

            frames.append(WebSocketFrame(opcode: opcode, payload: payload))
            offset = payloadStart + payloadLength
        }

        if offset > 0 {
            buffer.removeSubrange(0..<offset)
        }

        return frames
    }

    static func encode(_ frame: WebSocketOutboundFrame) throws -> Data {
        let payload = frame.payload
        var data = Data()
        data.append(0x80 | frame.opcode.rawValue)

        if payload.count < 126 {
            data.append(UInt8(payload.count))
        } else if payload.count <= Int(UInt16.max) {
            data.append(126)
            data.append(UInt8((payload.count >> 8) & 0xFF))
            data.append(UInt8(payload.count & 0xFF))
        } else {
            data.append(127)
            for shift in stride(from: 56, through: 0, by: -8) {
                data.append(UInt8((UInt64(payload.count) >> UInt64(shift)) & 0xFF))
            }
        }

        data.append(payload)
        return data
    }
}
