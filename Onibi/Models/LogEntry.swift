import Foundation
import OnibiCore

/// Represents a log entry from Ghostty terminal
struct LogEntry: Identifiable, Codable, Equatable {
    let id: UUID
    let timestamp: Date
    let startedAt: Date
    let endedAt: Date?
    let command: String
    let displayCommand: String
    let output: String
    let exitCode: Int?
    let duration: TimeInterval?
    let workingDirectory: String?
    let sessionId: String?
    let assistantKind: AssistantKind
    let metadata: [String: String]
    
    init(
        id: UUID = UUID(),
        timestamp: Date? = nil,
        startedAt: Date? = nil,
        endedAt: Date? = nil,
        command: String,
        displayCommand: String? = nil,
        output: String,
        exitCode: Int? = nil,
        duration: TimeInterval? = nil,
        workingDirectory: String? = nil,
        sessionId: String? = nil,
        assistantKind: AssistantKind = .unknown,
        metadata: [String: String] = [:]
    ) {
        let resolvedStartedAt = startedAt ?? timestamp ?? Date()
        let resolvedTimestamp = timestamp ?? endedAt ?? resolvedStartedAt
        var resolvedMetadata = metadata
        if resolvedMetadata["assistantKind"] == nil {
            resolvedMetadata["assistantKind"] = assistantKind.rawValue
        }

        self.id = id
        self.timestamp = resolvedTimestamp
        self.startedAt = resolvedStartedAt
        self.endedAt = endedAt
        self.command = command
        self.displayCommand = displayCommand ?? CommandSanitizer.sanitize(command: command)
        self.output = output
        self.exitCode = exitCode
        self.duration = duration
        self.workingDirectory = workingDirectory
        self.sessionId = sessionId
        self.assistantKind = assistantKind
        self.metadata = resolvedMetadata
    }

    var sortTimestamp: Date {
        endedAt ?? timestamp
    }

    var isAssistantCommand: Bool {
        assistantKind != .unknown
    }
}

extension LogEntry {
    enum CodingKeys: String, CodingKey {
        case id
        case timestamp
        case startedAt
        case endedAt
        case command
        case displayCommand
        case output
        case exitCode
        case duration
        case workingDirectory
        case sessionId
        case assistantKind
        case metadata
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        let id = try container.decodeIfPresent(UUID.self, forKey: .id) ?? UUID()
        let timestamp = try container.decodeIfPresent(Date.self, forKey: .timestamp)
        let startedAt = try container.decodeIfPresent(Date.self, forKey: .startedAt)
        let endedAt = try container.decodeIfPresent(Date.self, forKey: .endedAt)
        let command = try container.decode(String.self, forKey: .command)
        let displayCommand = try container.decodeIfPresent(String.self, forKey: .displayCommand)
        let output = try container.decodeIfPresent(String.self, forKey: .output) ?? ""
        let exitCode = try container.decodeIfPresent(Int.self, forKey: .exitCode)
        let duration = try container.decodeIfPresent(TimeInterval.self, forKey: .duration)
        let workingDirectory = try container.decodeIfPresent(String.self, forKey: .workingDirectory)
        let sessionId = try container.decodeIfPresent(String.self, forKey: .sessionId)
        let metadata = try container.decodeIfPresent([String: String].self, forKey: .metadata) ?? [:]
        let assistantKind = try container.decodeIfPresent(AssistantKind.self, forKey: .assistantKind)
            ?? metadata["assistantKind"].flatMap(AssistantKind.init(rawValue:))
            ?? AssistantClassifier.classify(command: command, metadata: metadata)

        self.init(
            id: id,
            timestamp: timestamp,
            startedAt: startedAt ?? timestamp,
            endedAt: endedAt,
            command: command,
            displayCommand: displayCommand,
            output: output,
            exitCode: exitCode,
            duration: duration,
            workingDirectory: workingDirectory,
            sessionId: sessionId,
            assistantKind: assistantKind,
            metadata: metadata
        )
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(timestamp, forKey: .timestamp)
        try container.encode(startedAt, forKey: .startedAt)
        try container.encodeIfPresent(endedAt, forKey: .endedAt)
        try container.encode(command, forKey: .command)
        try container.encode(displayCommand, forKey: .displayCommand)
        try container.encode(output, forKey: .output)
        try container.encodeIfPresent(exitCode, forKey: .exitCode)
        try container.encodeIfPresent(duration, forKey: .duration)
        try container.encodeIfPresent(workingDirectory, forKey: .workingDirectory)
        try container.encodeIfPresent(sessionId, forKey: .sessionId)
        try container.encode(assistantKind, forKey: .assistantKind)
        try container.encode(metadata, forKey: .metadata)
    }
}
