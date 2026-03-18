import Foundation

public struct CompletedCommandRecord: Equatable, Sendable {
    public let id: UUID
    public let sessionId: String
    public let command: String
    public let displayCommand: String
    public let startedAt: Date
    public let endedAt: Date
    public let duration: TimeInterval
    public let exitCode: Int?
    public let workingDirectory: String?
    public let assistantKind: AssistantKind
    public let metadata: [String: String]

    public init(
        id: UUID = UUID(),
        sessionId: String,
        command: String,
        displayCommand: String,
        startedAt: Date,
        endedAt: Date,
        duration: TimeInterval,
        exitCode: Int?,
        workingDirectory: String? = nil,
        assistantKind: AssistantKind = .unknown,
        metadata: [String: String] = [:]
    ) {
        self.id = id
        self.sessionId = sessionId
        self.command = command
        self.displayCommand = displayCommand
        self.startedAt = startedAt
        self.endedAt = endedAt
        self.duration = duration
        self.exitCode = exitCode
        self.workingDirectory = workingDirectory
        self.assistantKind = assistantKind
        self.metadata = metadata
    }
}

public actor CommandLifecycleTracker {
    private struct PendingCommand: Sendable {
        let command: String
        let startedAt: Date
        let workingDirectory: String?
        let assistantKind: AssistantKind
        let metadata: [String: String]
    }

    private var pendingCommands: [String: PendingCommand] = [:]

    public init() {}

    public func recordStart(
        sessionId: String,
        command: String,
        timestamp: Date,
        workingDirectory: String? = nil,
        assistantKind: AssistantKind = .unknown,
        metadata: [String: String] = [:]
    ) {
        pendingCommands[sessionId] = PendingCommand(
            command: command,
            startedAt: timestamp,
            workingDirectory: workingDirectory,
            assistantKind: assistantKind,
            metadata: metadata
        )
    }

    public func complete(
        sessionId: String,
        exitCode: Int?,
        endedAt: Date
    ) -> CompletedCommandRecord? {
        guard let pending = pendingCommands.removeValue(forKey: sessionId) else {
            return nil
        }

        let duration = max(endedAt.timeIntervalSince(pending.startedAt), 0)
        let displayCommand = CommandSanitizer.sanitize(command: pending.command)

        var metadata = pending.metadata
        if metadata["assistantKind"] == nil {
            metadata["assistantKind"] = pending.assistantKind.rawValue
        }

        return CompletedCommandRecord(
            sessionId: sessionId,
            command: pending.command,
            displayCommand: displayCommand,
            startedAt: pending.startedAt,
            endedAt: endedAt,
            duration: duration,
            exitCode: exitCode,
            workingDirectory: pending.workingDirectory,
            assistantKind: pending.assistantKind,
            metadata: metadata
        )
    }

    public func clear() {
        pendingCommands.removeAll()
    }
}
