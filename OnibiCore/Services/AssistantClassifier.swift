import Foundation

public enum AssistantClassifier {
    public static func classify(command: String?, metadata: [String: String] = [:]) -> AssistantKind {
        if let rawKind = metadata["assistantKind"], let assistantKind = AssistantKind(rawValue: rawKind) {
            return assistantKind
        }

        guard let command else {
            return metadata["source"] == "ai" ? .otherAI : .unknown
        }

        let normalized = command.lowercased()
        if normalized.contains("claude") {
            return .claudeCode
        }
        if normalized.contains("codex") {
            return .codex
        }
        if normalized.contains("gemini") {
            return .gemini
        }
        if normalized.contains("copilot") || normalized.contains("github-copilot") {
            return .copilot
        }

        let aiHints = [
            "anthropic",
            "chatgpt",
            "openai",
            "ollama",
            "aider",
            "llm",
            "assistant"
        ]

        if aiHints.contains(where: normalized.contains) || metadata["source"] == "ai" {
            return .otherAI
        }

        return .unknown
    }
}
