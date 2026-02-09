import Foundation

/// Represents a parsed terminal event from Ghostty logs
struct GhosttyEvent: Identifiable, Codable, Equatable {
    let id: UUID
    let timestamp: Date
    let type: EventType
    let command: String?
    let output: String?
    let sessionId: String?
    let metadata: [String: String]
    
    init(
        id: UUID = UUID(),
        timestamp: Date = Date(),
        type: EventType,
        command: String? = nil,
        output: String? = nil,
        sessionId: String? = nil,
        metadata: [String: String] = [:]
    ) {
        self.id = id
        self.timestamp = timestamp
        self.type = type
        self.command = command
        self.output = output
        self.sessionId = sessionId
        self.metadata = metadata
    }
}

/// Type of terminal event
enum EventType: String, Codable, CaseIterable {
    case command = "command"
    case output = "output"
    case error = "error"
    case system = "system"
}
