import Foundation

public enum AssistantKind: String, Codable, CaseIterable, Sendable {
    case claudeCode
    case codex
    case gemini
    case copilot
    case otherAI
    case unknown

    public var displayName: String {
        switch self {
        case .claudeCode:
            return "Claude Code"
        case .codex:
            return "Codex"
        case .gemini:
            return "Gemini"
        case .copilot:
            return "Copilot"
        case .otherAI:
            return "Other AI"
        case .unknown:
            return "Unknown"
        }
    }

    public var symbolName: String {
        switch self {
        case .claudeCode:
            return "text.bubble"
        case .codex:
            return "sparkles"
        case .gemini:
            return "diamond"
        case .copilot:
            return "paperplane"
        case .otherAI:
            return "cpu"
        case .unknown:
            return "questionmark.circle"
        }
    }
}
