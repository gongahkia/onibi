import Foundation

public enum LocalSessionProxyFrameType: String, Codable, Sendable {
    case register
    case metadata
    case output
    case commandStart = "command_start"
    case commandEnd = "command_end"
    case terminalEvent = "terminal_event"
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

public struct LocalSessionProxyMetadataMessage: Codable, Equatable, Sendable {
    public let type: LocalSessionProxyFrameType
    public let sessionId: String
    public let workingDirectory: String?
    public let terminalCols: Int?
    public let terminalRows: Int?
    public let terminalTitle: String?

    public init(
        sessionId: String,
        workingDirectory: String? = nil,
        terminalCols: Int? = nil,
        terminalRows: Int? = nil,
        terminalTitle: String? = nil
    ) {
        self.type = .metadata
        self.sessionId = sessionId
        self.workingDirectory = workingDirectory
        self.terminalCols = terminalCols
        self.terminalRows = terminalRows
        self.terminalTitle = terminalTitle
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

public enum TerminalEventKind: String, Codable, Sendable {
    case bell
    case workingDirectory = "working_directory"
}

public struct TerminalEventSnapshot: Codable, Equatable, Sendable {
    public let kind: TerminalEventKind
    public let value: String?
    public let timestamp: Date

    public init(kind: TerminalEventKind, value: String? = nil, timestamp: Date) {
        self.kind = kind
        self.value = value
        self.timestamp = timestamp
    }
}

public struct LocalSessionProxyTerminalEventMessage: Codable, Equatable, Sendable {
    public let type: LocalSessionProxyFrameType
    public let sessionId: String
    public let event: TerminalEventKind
    public let value: String?
    public let timestamp: Date

    public init(
        sessionId: String,
        event: TerminalEventKind,
        value: String? = nil,
        timestamp: Date
    ) {
        self.type = .terminalEvent
        self.sessionId = sessionId
        self.event = event
        self.value = value
        self.timestamp = timestamp
    }

    public var snapshot: TerminalEventSnapshot {
        TerminalEventSnapshot(kind: event, value: value, timestamp: timestamp)
    }
}

public struct LocalSessionProxyCommandStartMessage: Codable, Equatable, Sendable {
    public let type: LocalSessionProxyFrameType
    public let sessionId: String
    public let command: String
    public let workingDirectory: String?
    public let timestamp: Date

    public init(
        sessionId: String,
        command: String,
        workingDirectory: String? = nil,
        timestamp: Date
    ) {
        self.type = .commandStart
        self.sessionId = sessionId
        self.command = command
        self.workingDirectory = workingDirectory
        self.timestamp = timestamp
    }
}

public struct LocalSessionProxyCommandEndMessage: Codable, Equatable, Sendable {
    public let type: LocalSessionProxyFrameType
    public let sessionId: String
    public let exitCode: Int?
    public let workingDirectory: String?
    public let timestamp: Date

    public init(
        sessionId: String,
        exitCode: Int?,
        workingDirectory: String? = nil,
        timestamp: Date
    ) {
        self.type = .commandEnd
        self.sessionId = sessionId
        self.exitCode = exitCode
        self.workingDirectory = workingDirectory
        self.timestamp = timestamp
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
    public let data: String?
    public let fileName: String?

    public init(sessionId: String, payload: RemoteInputPayload) {
        self.type = .input
        self.sessionId = sessionId
        self.kind = payload.kind
        self.text = payload.text
        self.key = payload.key
        self.data = payload.data
        self.fileName = payload.fileName
    }

    public var payload: RemoteInputPayload {
        RemoteInputPayload(kind: kind, text: text, key: key, data: data, fileName: fileName)
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
