import Foundation

public enum RealtimeClientMessageType: String, Codable, Sendable {
    case auth
    case subscribe
    case unsubscribe
    case requestBuffer = "request_buffer"
    case sendInput = "send_input"
}

public enum RealtimeServerMessageType: String, Codable, Sendable {
    case authOK = "auth_ok"
    case sessionsSnapshot = "sessions_snapshot"
    case sessionAdded = "session_added"
    case sessionUpdated = "session_updated"
    case sessionRemoved = "session_removed"
    case bufferSnapshot = "buffer_snapshot"
    case output
    case inputAccepted = "input_accepted"
    case error
}

public struct RealtimeClientMessage: Codable, Equatable, Sendable {
    public let type: RealtimeClientMessageType
    public let token: String?
    public let sessionId: String?
    public let kind: RemoteInputKind?
    public let text: String?
    public let key: RemoteInputKey?
    public let clientRequestId: String?

    public init(
        type: RealtimeClientMessageType,
        token: String? = nil,
        sessionId: String? = nil,
        kind: RemoteInputKind? = nil,
        text: String? = nil,
        key: RemoteInputKey? = nil,
        clientRequestId: String? = nil
    ) {
        self.type = type
        self.token = token
        self.sessionId = sessionId
        self.kind = kind
        self.text = text
        self.key = key
        self.clientRequestId = clientRequestId
    }

    public var inputPayload: RemoteInputPayload? {
        guard let kind else {
            return nil
        }
        let payload = RemoteInputPayload(kind: kind, text: text, key: key)
        return payload.isValid ? payload : nil
    }
}

public struct RealtimeServerMessage: Codable, Equatable, Sendable {
    public let type: RealtimeServerMessageType
    public let hostVersion: String?
    public let sessions: [ControllableSessionSnapshot]?
    public let session: ControllableSessionSnapshot?
    public let sessionId: String?
    public let chunks: [SessionOutputChunk]?
    public let bufferCursor: String?
    public let truncated: Bool?
    public let chunk: SessionOutputChunk?
    public let clientRequestId: String?
    public let code: String?
    public let message: String?

    public init(
        type: RealtimeServerMessageType,
        hostVersion: String? = nil,
        sessions: [ControllableSessionSnapshot]? = nil,
        session: ControllableSessionSnapshot? = nil,
        sessionId: String? = nil,
        chunks: [SessionOutputChunk]? = nil,
        bufferCursor: String? = nil,
        truncated: Bool? = nil,
        chunk: SessionOutputChunk? = nil,
        clientRequestId: String? = nil,
        code: String? = nil,
        message: String? = nil
    ) {
        self.type = type
        self.hostVersion = hostVersion
        self.sessions = sessions
        self.session = session
        self.sessionId = sessionId
        self.chunks = chunks
        self.bufferCursor = bufferCursor
        self.truncated = truncated
        self.chunk = chunk
        self.clientRequestId = clientRequestId
        self.code = code
        self.message = message
    }

    public static func authOK(hostVersion: String) -> RealtimeServerMessage {
        RealtimeServerMessage(type: .authOK, hostVersion: hostVersion)
    }

    public static func sessionsSnapshot(_ sessions: [ControllableSessionSnapshot]) -> RealtimeServerMessage {
        RealtimeServerMessage(type: .sessionsSnapshot, sessions: sessions)
    }

    public static func sessionAdded(_ session: ControllableSessionSnapshot) -> RealtimeServerMessage {
        RealtimeServerMessage(type: .sessionAdded, session: session)
    }

    public static func sessionUpdated(_ session: ControllableSessionSnapshot) -> RealtimeServerMessage {
        RealtimeServerMessage(type: .sessionUpdated, session: session)
    }

    public static func sessionRemoved(_ sessionId: String) -> RealtimeServerMessage {
        RealtimeServerMessage(type: .sessionRemoved, sessionId: sessionId)
    }

    public static func bufferSnapshot(_ snapshot: SessionOutputBufferSnapshot) -> RealtimeServerMessage {
        RealtimeServerMessage(
            type: .bufferSnapshot,
            sessionId: snapshot.session.id,
            chunks: snapshot.chunks,
            bufferCursor: snapshot.bufferCursor,
            truncated: snapshot.truncated
        )
    }

    public static func output(sessionId: String, chunk: SessionOutputChunk) -> RealtimeServerMessage {
        RealtimeServerMessage(type: .output, sessionId: sessionId, chunk: chunk)
    }

    public static func inputAccepted(sessionId: String, clientRequestId: String?) -> RealtimeServerMessage {
        RealtimeServerMessage(
            type: .inputAccepted,
            sessionId: sessionId,
            clientRequestId: clientRequestId
        )
    }

    public static func error(code: String, message: String) -> RealtimeServerMessage {
        RealtimeServerMessage(type: .error, code: code, message: message)
    }
}

public struct FeatureFlagsResponse: Codable, Equatable, Sendable {
    public let legacyMonitoringEnabled: Bool
    public let remoteControlEnabled: Bool
    public let realtimeSessionsEnabled: Bool
    public let websocketEnabled: Bool
    public let fallbackInputEnabled: Bool

    public init(
        legacyMonitoringEnabled: Bool,
        remoteControlEnabled: Bool,
        realtimeSessionsEnabled: Bool,
        websocketEnabled: Bool,
        fallbackInputEnabled: Bool
    ) {
        self.legacyMonitoringEnabled = legacyMonitoringEnabled
        self.remoteControlEnabled = remoteControlEnabled
        self.realtimeSessionsEnabled = realtimeSessionsEnabled
        self.websocketEnabled = websocketEnabled
        self.fallbackInputEnabled = fallbackInputEnabled
    }
}

public struct GatewayBootstrapResponse: Codable, Equatable, Sendable {
    public let health: HostHealth
    public let featureFlags: FeatureFlagsResponse
    public let sessions: [ControllableSessionSnapshot]
    public let diagnostics: DiagnosticsResponse

    public init(
        health: HostHealth,
        featureFlags: FeatureFlagsResponse,
        sessions: [ControllableSessionSnapshot],
        diagnostics: DiagnosticsResponse
    ) {
        self.health = health
        self.featureFlags = featureFlags
        self.sessions = sessions
        self.diagnostics = diagnostics
    }
}
