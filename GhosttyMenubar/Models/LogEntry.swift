import Foundation

/// Represents a log entry from Ghostty terminal
struct LogEntry: Identifiable, Codable, Equatable {
    let id: UUID
    let timestamp: Date
    let command: String
    let output: String
    let exitCode: Int?
    let duration: TimeInterval?
    let workingDirectory: String?
    let metadata: [String: String]
    
    init(
        id: UUID = UUID(),
        timestamp: Date = Date(),
        command: String,
        output: String,
        exitCode: Int? = nil,
        duration: TimeInterval? = nil,
        workingDirectory: String? = nil,
        metadata: [String: String] = [:]
    ) {
        self.id = id
        self.timestamp = timestamp
        self.command = command
        self.output = output
        self.exitCode = exitCode
        self.duration = duration
        self.workingDirectory = workingDirectory
        self.metadata = metadata
    }
}
