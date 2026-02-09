import Foundation

/// Protocol for parsing different event types from Ghostty logs
protocol EventParser {
    /// The type of events this parser handles
    var eventType: NotificationType { get }
    
    /// Check if the given log content matches this parser's pattern
    func matches(_ content: String) -> Bool
    
    /// Parse the content and extract relevant event data
    func parse(_ content: String, timestamp: Date) -> GhosttyEvent?
}

/// Base implementation helpers for event parsers
extension EventParser {
    /// Extract command from log line
    func extractCommand(from line: String) -> String? {
        // Default implementation - subclasses can override
        let trimmed = line.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        return trimmed
    }
}
