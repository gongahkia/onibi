import Foundation

public enum CommandSanitizer {
    public static func sanitize(command: String) -> String {
        let trimmed = command.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return "command" }

        let tokens = splitShellLike(trimmed)
        guard let firstToken = tokens.first else { return "command" }

        let commandName = URL(fileURLWithPath: firstToken).lastPathComponent
        var renderedTokens = [commandName]
        var hiddenCount = 0

        for token in tokens.dropFirst() {
            if renderedTokens.count >= 3 {
                hiddenCount += 1
                continue
            }

            if token.hasPrefix("-") {
                renderedTokens.append(token)
                continue
            }

            if let assignment = sanitizeAssignment(token) {
                renderedTokens.append(assignment)
                continue
            }

            if isVisibleSubcommand(token) {
                renderedTokens.append(token)
            } else {
                hiddenCount += 1
            }
        }

        if hiddenCount > 0 {
            renderedTokens.append("+\(hiddenCount)")
        }

        return renderedTokens.joined(separator: " ")
    }

    private static func splitShellLike(_ command: String) -> [String] {
        var tokens: [String] = []
        var current = ""
        var quote: Character?

        for character in command {
            if character == "\"" || character == "'" {
                if quote == character {
                    quote = nil
                } else if quote == nil {
                    quote = character
                } else {
                    current.append(character)
                }
                continue
            }

            if character.isWhitespace && quote == nil {
                if !current.isEmpty {
                    tokens.append(current)
                    current.removeAll(keepingCapacity: true)
                }
                continue
            }

            current.append(character)
        }

        if !current.isEmpty {
            tokens.append(current)
        }

        return tokens
    }

    private static func sanitizeAssignment(_ token: String) -> String? {
        guard let separatorIndex = token.firstIndex(of: "="), separatorIndex != token.startIndex else {
            return nil
        }

        let key = token[..<separatorIndex]
        return "\(key)=•••"
    }

    private static func isVisibleSubcommand(_ token: String) -> Bool {
        let allowedSubcommands: Set<String> = [
            "ask",
            "build",
            "chat",
            "commit",
            "diff",
            "exec",
            "run",
            "status",
            "test"
        ]

        return allowedSubcommands.contains(token.lowercased())
    }
}
