import Foundation

public enum RealtimeClientMessageType: String, Codable, Sendable {
    case auth
    case subscribe
    case unsubscribe
    case requestBuffer = "request_buffer"
    case sendInput = "send_input"
    case resize
    case processAction = "process_action"
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
    case processActionAccepted = "process_action_accepted"
    case error
}

public enum RealtimeProtocolVersion {
    public static let current = 2
    public static let minimumSupported = 1
}

public struct RealtimeClientMessage: Codable, Equatable, Sendable {
    public let type: RealtimeClientMessageType
    public let token: String?
    public let sessionId: String?
    public let kind: RemoteInputKind?
    public let text: String?
    public let key: RemoteInputKey?
    public let data: String?
    public let fileName: String?
    public let cols: Int?
    public let rows: Int?
    public let action: RemoteProcessAction?
    public let bufferCursor: String?
    public let bufferLimit: Int?
    public let viewportCols: Int?
    public let viewportRows: Int?
    public let clientRequestId: String?

    public init(
        type: RealtimeClientMessageType,
        token: String? = nil,
        sessionId: String? = nil,
        kind: RemoteInputKind? = nil,
        text: String? = nil,
        key: RemoteInputKey? = nil,
        data: String? = nil,
        fileName: String? = nil,
        cols: Int? = nil,
        rows: Int? = nil,
        action: RemoteProcessAction? = nil,
        bufferCursor: String? = nil,
        bufferLimit: Int? = nil,
        viewportCols: Int? = nil,
        viewportRows: Int? = nil,
        clientRequestId: String? = nil
    ) {
        self.type = type
        self.token = token
        self.sessionId = sessionId
        self.kind = kind
        self.text = text
        self.key = key
        self.data = data
        self.fileName = fileName
        self.cols = cols
        self.rows = rows
        self.action = action
        self.bufferCursor = bufferCursor
        self.bufferLimit = bufferLimit
        self.viewportCols = viewportCols
        self.viewportRows = viewportRows
        self.clientRequestId = clientRequestId
    }

    public var inputPayload: RemoteInputPayload? {
        guard let kind else {
            return nil
        }
        let payload = RemoteInputPayload(kind: kind, text: text, key: key, data: data, fileName: fileName)
        return payload.isValid ? payload : nil
    }

    public var resizePayload: RemoteTerminalResizePayload? {
        guard let cols, let rows else {
            return nil
        }
        let payload = RemoteTerminalResizePayload(cols: cols, rows: rows)
        return payload.isValid ? payload : nil
    }

    public var processActionPayload: RemoteProcessActionPayload? {
        guard let action else {
            return nil
        }
        let payload = RemoteProcessActionPayload(action: action)
        return payload.isValid ? payload : nil
    }
}

public struct RealtimeServerMessage: Codable, Equatable, Sendable {
    public let type: RealtimeServerMessageType
    public let hostVersion: String?
    public let realtimeProtocolVersion: Int?
    public let minimumSupportedRealtimeProtocolVersion: Int?
    public let sessions: [ControllableSessionSnapshot]?
    public let session: ControllableSessionSnapshot?
    public let sessionId: String?
    public let chunks: [SessionOutputChunk]?
    public let bufferCursor: String?
    public let requestCursor: String?
    public let startCursor: String?
    public let endCursor: String?
    public let truncated: Bool?
    public let viewportCols: Int?
    public let viewportRows: Int?
    public let chunk: SessionOutputChunk?
    public let clientRequestId: String?
    public let action: RemoteProcessAction?
    public let code: String?
    public let message: String?

    public init(
        type: RealtimeServerMessageType,
        hostVersion: String? = nil,
        realtimeProtocolVersion: Int? = nil,
        minimumSupportedRealtimeProtocolVersion: Int? = nil,
        sessions: [ControllableSessionSnapshot]? = nil,
        session: ControllableSessionSnapshot? = nil,
        sessionId: String? = nil,
        chunks: [SessionOutputChunk]? = nil,
        bufferCursor: String? = nil,
        requestCursor: String? = nil,
        startCursor: String? = nil,
        endCursor: String? = nil,
        truncated: Bool? = nil,
        viewportCols: Int? = nil,
        viewportRows: Int? = nil,
        chunk: SessionOutputChunk? = nil,
        clientRequestId: String? = nil,
        action: RemoteProcessAction? = nil,
        code: String? = nil,
        message: String? = nil
    ) {
        self.type = type
        self.hostVersion = hostVersion
        self.realtimeProtocolVersion = realtimeProtocolVersion
        self.minimumSupportedRealtimeProtocolVersion = minimumSupportedRealtimeProtocolVersion
        self.sessions = sessions
        self.session = session
        self.sessionId = sessionId
        self.chunks = chunks
        self.bufferCursor = bufferCursor
        self.requestCursor = requestCursor
        self.startCursor = startCursor
        self.endCursor = endCursor
        self.truncated = truncated
        self.viewportCols = viewportCols
        self.viewportRows = viewportRows
        self.chunk = chunk
        self.clientRequestId = clientRequestId
        self.action = action
        self.code = code
        self.message = message
    }

    public static func authOK(hostVersion: String) -> RealtimeServerMessage {
        RealtimeServerMessage(
            type: .authOK,
            hostVersion: hostVersion,
            realtimeProtocolVersion: RealtimeProtocolVersion.current,
            minimumSupportedRealtimeProtocolVersion: RealtimeProtocolVersion.minimumSupported
        )
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

    public static func bufferSnapshot(
        _ snapshot: SessionOutputBufferSnapshot,
        requestCursor: String? = nil,
        viewportCols: Int? = nil,
        viewportRows: Int? = nil
    ) -> RealtimeServerMessage {
        RealtimeServerMessage(
            type: .bufferSnapshot,
            sessionId: snapshot.session.id,
            chunks: snapshot.chunks,
            bufferCursor: snapshot.bufferCursor,
            requestCursor: requestCursor,
            startCursor: snapshot.startCursor,
            endCursor: snapshot.endCursor,
            truncated: snapshot.truncated,
            viewportCols: viewportCols,
            viewportRows: viewportRows
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

    public static func processActionAccepted(
        sessionId: String,
        action: RemoteProcessAction,
        clientRequestId: String?
    ) -> RealtimeServerMessage {
        RealtimeServerMessage(
            type: .processActionAccepted,
            sessionId: sessionId,
            clientRequestId: clientRequestId,
            action: action
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
