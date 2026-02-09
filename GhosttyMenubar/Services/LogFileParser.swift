import Foundation

/// Parses log entries from the terminal log file
final class LogFileParser {
    
    /// Parse a single line from the log file
    func parseLine(_ line: String) -> ParsedLogLine? {
        let components = line.split(separator: "|", maxSplits: 2).map(String.init)
        guard components.count >= 2 else { return nil }
        
        let timestampStr = components[0]
        let eventType = components[1]
        let payload = components.count > 2 ? components[2] : nil
        
        guard let timestamp = parseTimestamp(timestampStr) else { return nil }
        
        switch eventType {
        case "CMD_START":
            return ParsedLogLine(
                timestamp: timestamp,
                type: .commandStart,
                command: payload,
                exitCode: nil
            )
        case "CMD_END":
            let exitCode = payload.flatMap { Int($0) }
            return ParsedLogLine(
                timestamp: timestamp,
                type: .commandEnd,
                command: nil,
                exitCode: exitCode
            )
        case "OUTPUT":
            return ParsedLogLine(
                timestamp: timestamp,
                type: .output,
                command: payload,
                exitCode: nil
            )
        case "AI_RESPONSE":
            return ParsedLogLine(
                timestamp: timestamp,
                type: .aiResponse,
                command: payload,
                exitCode: nil
            )
        case "TASK_COMPLETE":
            return ParsedLogLine(
                timestamp: timestamp,
                type: .taskComplete,
                command: payload,
                exitCode: nil
            )
        case "BUILD":
            return ParsedLogLine(
                timestamp: timestamp,
                type: .build,
                command: payload,
                exitCode: nil
            )
        case "TEST":
            return ParsedLogLine(
                timestamp: timestamp,
                type: .test,
                command: payload,
                exitCode: nil
            )
        default:
            return nil
        }
    }
    
    /// Parse multiple lines
    func parseLines(_ content: String) -> [ParsedLogLine] {
        content.split(separator: "\n")
            .compactMap { parseLine(String($0)) }
    }
    
    /// Parse ISO8601 timestamp
    private func parseTimestamp(_ str: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = formatter.date(from: str) {
            return date
        }
        // Try without fractional seconds
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.date(from: str)
    }
}

/// Represents a parsed log line
struct ParsedLogLine {
    let timestamp: Date
    let type: LogLineType
    let command: String?
    let exitCode: Int?
}

/// Types of log lines
enum LogLineType {
    case commandStart
    case commandEnd
    case output
    case aiResponse
    case taskComplete
    case build
    case test
}

// MARK: - Regex Patterns

/// Regex patterns for detecting various command types
struct CommandPatterns {
    /// AI assistant output patterns (Claude, GPT, etc.)
    static let aiPatterns: [String] = [
        "^\\[Claude\\]",
        "^\\[GPT\\]",
        "^\\[AI\\]",
        "claude:",
        "assistant:",
        "^> .*thinking\\.\\.\\."
    ]
    
    /// Task completion patterns
    static let taskPatterns: [String] = [
        "✓",
        "✔",
        "\\[DONE\\]",
        "\\[COMPLETE\\]",
        "Task completed",
        "Successfully"
    ]
    
    /// Build/compile patterns
    static let buildPatterns: [String] = [
        "^Build succeeded",
        "^Compilation finished",
        "^npm run build",
        "^cargo build",
        "^swift build",
        "^make",
        "^cmake"
    ]
    
    /// Test patterns
    static let testPatterns: [String] = [
        "^Test.*passed",
        "^\\d+ tests.*\\d+ passed",
        "npm test",
        "cargo test",
        "swift test",
        "pytest",
        "jest"
    ]
    
    /// Error patterns
    static let errorPatterns: [String] = [
        "^error:",
        "^Error:",
        "^ERROR",
        "^fatal:",
        "^FATAL",
        "\\[ERR\\]",
        "failed",
        "FAILED"
    ]
    
    /// Compile all patterns into regex objects
    static func compilePatterns(_ patterns: [String]) -> [NSRegularExpression] {
        patterns.compactMap { pattern in
            try? NSRegularExpression(pattern: pattern, options: [.caseInsensitive])
        }
    }
}
