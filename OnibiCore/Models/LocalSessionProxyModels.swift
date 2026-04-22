import Foundation

public enum LocalSessionProxyFrameType: String, Codable, Sendable {
    case register
    case output
    case state
    case exit
    case heartbeat
    case input
    case resize
}

public struct LocalSessionProxyEnvelope: Codable, Equatable, Sendable {
    public let type: LocalSessionProxyFrameType

    public init(type: LocalSessionProxyFrameType) {
        self.type = type
    }
}

public struct LocalSessionProxyRegisterMessage: Codable, Equatable, Sendable {
    public let type: LocalSessionProxyFrameType
    public let sessionId: String
    public let shell: String
    public let pid: Int32
    public let startedAt: Date
    public let workingDirectory: String?
    public let hostname: String
    public let proxyVersion: String?

    public init(
        sessionId: String,
        shell: String,
        pid: Int32,
        startedAt: Date,
        workingDirectory: String?,
        hostname: String,
        proxyVersion: String? = nil
    ) {
        self.type = .register
        self.sessionId = sessionId
        self.shell = shell
        self.pid = pid
        self.startedAt = startedAt
        self.workingDirectory = workingDirectory
        self.hostname = hostname
        self.proxyVersion = proxyVersion
    }
}

public struct LocalSessionProxyOutputMessage: Codable, Equatable, Sendable {
    public let type: LocalSessionProxyFrameType
    public let sessionId: String
    public let stream: SessionOutputStream
    public let timestamp: Date
    public let data: String

    public init(
        sessionId: String,
        stream: SessionOutputStream,
        timestamp: Date,
        data: String
    ) {
        self.type = .output
        self.sessionId = sessionId
        self.stream = stream
        self.timestamp = timestamp
        self.data = data
    }

    public init(
        sessionId: String,
        stream: SessionOutputStream,
        timestamp: Date,
        outputData: Data
    ) {
        self.init(
            sessionId: sessionId,
            stream: stream,
            timestamp: timestamp,
            data: outputData.base64EncodedString()
        )
    }

    public var decodedData: Data? {
        Data(base64Encoded: data)
    }
}

public struct LocalSessionProxyStateMessage: Codable, Equatable, Sendable {
    public let type: LocalSessionProxyFrameType
    public let sessionId: String
    public let status: SessionTransportStatus

    public init(sessionId: String, status: SessionTransportStatus) {
        self.type = .state
        self.sessionId = sessionId
        self.status = status
    }
}

public struct LocalSessionProxyExitMessage: Codable, Equatable, Sendable {
    public let type: LocalSessionProxyFrameType
    public let sessionId: String
    public let exitCode: Int32
    public let timestamp: Date

    public init(sessionId: String, exitCode: Int32, timestamp: Date) {
        self.type = .exit
        self.sessionId = sessionId
        self.exitCode = exitCode
        self.timestamp = timestamp
    }
}

public struct LocalSessionProxyHeartbeatMessage: Codable, Equatable, Sendable {
    public let type: LocalSessionProxyFrameType
    public let sessionId: String
    public let timestamp: Date

    public init(sessionId: String, timestamp: Date) {
        self.type = .heartbeat
        self.sessionId = sessionId
        self.timestamp = timestamp
    }
}

public struct LocalSessionProxyInputMessage: Codable, Equatable, Sendable {
    public let type: LocalSessionProxyFrameType
    public let sessionId: String
    public let kind: RemoteInputKind
    public let text: String?
    public let key: RemoteInputKey?

    public init(sessionId: String, payload: RemoteInputPayload) {
        self.type = .input
        self.sessionId = sessionId
        self.kind = payload.kind
        self.text = payload.text
        self.key = payload.key
    }

    public var payload: RemoteInputPayload {
        RemoteInputPayload(kind: kind, text: text, key: key)
    }
}

public struct LocalSessionProxyResizeMessage: Codable, Equatable, Sendable {
    public let type: LocalSessionProxyFrameType
    public let sessionId: String
    public let cols: Int
    public let rows: Int

    public init(sessionId: String, payload: RemoteTerminalResizePayload) {
        self.type = .resize
        self.sessionId = sessionId
        self.cols = payload.cols
        self.rows = payload.rows
    }

    public var payload: RemoteTerminalResizePayload {
        RemoteTerminalResizePayload(cols: cols, rows: rows)
    }
}
