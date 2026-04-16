import Foundation

public enum SessionTransportStatus: String, Codable, Sendable {
    case starting
    case running
    case exited
    case failed
}

public enum SessionOutputStream: String, Codable, Sendable {
    case stdout
    case stderr
}

public enum RemoteInputKind: String, Codable, Sendable {
    case text
    case key
}

public enum RemoteInputKey: String, Codable, Sendable, CaseIterable {
    case enter
    case ctrlC = "ctrl_c"
    case ctrlD = "ctrl_d"
    case tab
    case backspace
    case escape
    case delete
    case home
    case end
    case pageUp = "page_up"
    case pageDown = "page_down"
    case arrowUp = "arrow_up"
    case arrowDown = "arrow_down"
    case arrowLeft = "arrow_left"
    case arrowRight = "arrow_right"
    case space
}

public enum RemoteControlError: Error, Equatable, Sendable {
    case sessionNotFound(String)
    case sessionNotControllable(String)
    case inputUnavailable(String)
    case invalidInputPayload
    case resizeUnavailable(String)
    case invalidResizePayload
}

public struct ControllableSessionSnapshot: Codable, Equatable, Identifiable, Sendable {
    public let id: String
    public let displayName: String
    public let startedAt: Date
    public let lastActivityAt: Date
    public let status: SessionTransportStatus
    public let isControllable: Bool
    public let workingDirectory: String?
    public let lastCommandPreview: String?
    public let bufferCursor: String?

    public init(
        id: String,
        displayName: String,
        startedAt: Date,
        lastActivityAt: Date,
        status: SessionTransportStatus,
        isControllable: Bool,
        workingDirectory: String?,
        lastCommandPreview: String?,
        bufferCursor: String?
    ) {
        self.id = id
        self.displayName = displayName
        self.startedAt = startedAt
        self.lastActivityAt = lastActivityAt
        self.status = status
        self.isControllable = isControllable
        self.workingDirectory = workingDirectory
        self.lastCommandPreview = lastCommandPreview
        self.bufferCursor = bufferCursor
    }
}

public struct SessionOutputChunk: Codable, Equatable, Identifiable, Sendable {
    public let id: String
    public let sessionId: String
    public let stream: SessionOutputStream
    public let timestamp: Date
    public let data: Data

    public init(
        id: String = UUID().uuidString,
        sessionId: String,
        stream: SessionOutputStream,
        timestamp: Date,
        data: Data
    ) {
        self.id = id
        self.sessionId = sessionId
        self.stream = stream
        self.timestamp = timestamp
        self.data = data
    }
}

public struct SessionOutputBufferSnapshot: Codable, Equatable, Sendable {
    public let session: ControllableSessionSnapshot
    public let bufferCursor: String?
    public let chunks: [SessionOutputChunk]
    public let truncated: Bool

    public init(
        session: ControllableSessionSnapshot,
        bufferCursor: String?,
        chunks: [SessionOutputChunk],
        truncated: Bool
    ) {
        self.session = session
        self.bufferCursor = bufferCursor
        self.chunks = chunks
        self.truncated = truncated
    }
}

public struct RemoteInputPayload: Codable, Equatable, Sendable {
    public let kind: RemoteInputKind
    public let text: String?
    public let key: RemoteInputKey?

    public init(
        kind: RemoteInputKind,
        text: String? = nil,
        key: RemoteInputKey? = nil
    ) {
        self.kind = kind
        self.text = text
        self.key = key
    }

    public var isValid: Bool {
        switch kind {
        case .text:
            return text != nil && key == nil
        case .key:
            return key != nil && text == nil
        }
    }

    public static func text(_ text: String) -> RemoteInputPayload {
        RemoteInputPayload(kind: .text, text: text)
    }

    public static func key(_ key: RemoteInputKey) -> RemoteInputPayload {
        RemoteInputPayload(kind: .key, key: key)
    }
}

public struct RemoteInputAcceptance: Codable, Equatable, Sendable {
    public let sessionId: String
    public let acceptedAt: Date

    public init(sessionId: String, acceptedAt: Date) {
        self.sessionId = sessionId
        self.acceptedAt = acceptedAt
    }
}

public struct RemoteTerminalResizePayload: Codable, Equatable, Sendable {
    public let cols: Int
    public let rows: Int

    public init(cols: Int, rows: Int) {
        self.cols = cols
        self.rows = rows
    }

    public var isValid: Bool {
        cols > 0 && rows > 0 && cols <= 2000 && rows <= 2000
    }
}

public struct RemoteTerminalResizeAcceptance: Codable, Equatable, Sendable {
    public let sessionId: String
    public let cols: Int
    public let rows: Int
    public let acceptedAt: Date

    public init(sessionId: String, cols: Int, rows: Int, acceptedAt: Date) {
        self.sessionId = sessionId
        self.cols = cols
        self.rows = rows
        self.acceptedAt = acceptedAt
    }
}
