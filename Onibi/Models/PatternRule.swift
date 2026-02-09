import Foundation

/// A user-defined pattern rule for custom notification triggers
struct PatternRule: Identifiable, Codable, Equatable {
    let id: UUID
    var name: String
    var pattern: String
    var isRegex: Bool
    var notificationType: NotificationType
    var isEnabled: Bool
    var priority: Int
    
    init(
        id: UUID = UUID(),
        name: String,
        pattern: String,
        isRegex: Bool = false,
        notificationType: NotificationType = .system,
        isEnabled: Bool = true,
        priority: Int = 0
    ) {
        self.id = id
        self.name = name
        self.pattern = pattern
        self.isRegex = isRegex
        self.notificationType = notificationType
        self.isEnabled = isEnabled
        self.priority = priority
    }
    
    /// Check if content matches this rule
    func matches(_ content: String) -> Bool {
        guard isEnabled else { return false }
        
        if isRegex {
            do {
                let regex = try NSRegularExpression(pattern: pattern, options: [.caseInsensitive])
                let range = NSRange(content.startIndex..., in: content)
                return regex.firstMatch(in: content, options: [], range: range) != nil
            } catch {
                return false
            }
        } else {
            return content.localizedCaseInsensitiveContains(pattern)
        }
    }
}

// MARK: - Built-in Presets

extension PatternRule {
    /// Pre-defined pattern presets for common dev workflows
    static let presets: [PatternRule] = [
        // npm
        PatternRule(
            name: "npm scripts complete",
            pattern: "npm (run|start|test|build).*done",
            isRegex: true,
            notificationType: .taskCompletion,
            priority: 10
        ),
        PatternRule(
            name: "npm install complete",
            pattern: "added \\d+ packages",
            isRegex: true,
            notificationType: .taskCompletion,
            priority: 10
        ),
        
        // pytest
        PatternRule(
            name: "pytest passed",
            pattern: "passed|failed|error",
            isRegex: false,
            notificationType: .taskCompletion,
            priority: 10
        ),
        PatternRule(
            name: "pytest summary",
            pattern: "\\d+ passed",
            isRegex: true,
            notificationType: .taskCompletion,
            priority: 10
        ),
        
        // cargo (Rust)
        PatternRule(
            name: "cargo build success",
            pattern: "Compiling.*Finished",
            isRegex: true,
            notificationType: .devWorkflow,
            priority: 10
        ),
        PatternRule(
            name: "cargo test complete",
            pattern: "test result: (ok|FAILED)",
            isRegex: true,
            notificationType: .taskCompletion,
            priority: 10
        ),
        
        // go test
        PatternRule(
            name: "go test pass",
            pattern: "^(ok|PASS|FAIL)",
            isRegex: true,
            notificationType: .taskCompletion,
            priority: 10
        ),
        PatternRule(
            name: "go build complete",
            pattern: "go: building",
            isRegex: false,
            notificationType: .devWorkflow,
            priority: 10
        ),
        
        // Docker
        PatternRule(
            name: "docker build complete",
            pattern: "Successfully built|Successfully tagged",
            isRegex: true,
            notificationType: .devWorkflow,
            priority: 10
        ),
        
        // Git
        PatternRule(
            name: "git push complete",
            pattern: "\\[\\w+\\s+\\w+\\]",
            isRegex: true,
            notificationType: .system,
            priority: 5
        )
    ]
}

// MARK: - Custom Pattern Detector

/// Applies user-defined patterns alongside built-in detectors
final class CustomPatternDetector: ObservableObject {
    static let shared = CustomPatternDetector()
    
    @Published var customPatterns: [PatternRule] = []
    
    private init() {
        loadPatterns()
    }
    
    /// Check content against all custom patterns
    func check(_ content: String) -> PatternRule? {
        // Sort by priority (higher first)
        let sorted = customPatterns.sorted { $0.priority > $1.priority }
        
        for pattern in sorted where pattern.isEnabled {
            if pattern.matches(content) {
                return pattern
            }
        }
        
        return nil
    }
    
    /// Add a new pattern
    func addPattern(_ pattern: PatternRule) {
        customPatterns.append(pattern)
        savePatterns()
    }
    
    /// Update an existing pattern
    func updatePattern(_ pattern: PatternRule) {
        if let index = customPatterns.firstIndex(where: { $0.id == pattern.id }) {
            customPatterns[index] = pattern
            savePatterns()
        }
    }
    
    /// Delete a pattern
    func deletePattern(id: UUID) {
        customPatterns.removeAll { $0.id == id }
        savePatterns()
    }
    
    /// Add preset patterns
    func addPreset(_ preset: PatternRule) {
        // Don't add duplicates
        guard !customPatterns.contains(where: { $0.name == preset.name }) else { return }
        addPattern(preset)
    }
    
    /// Import patterns from JSON
    func importPatterns(from data: Data) throws {
        let patterns = try JSONDecoder().decode([PatternRule].self, from: data)
        customPatterns.append(contentsOf: patterns)
        savePatterns()
    }
    
    /// Export patterns to JSON
    func exportPatterns() throws -> Data {
        try JSONEncoder().encode(customPatterns)
    }
    
    // MARK: - Persistence
    
    private let patternsKey = "customPatterns"
    
    private func loadPatterns() {
        if let data = UserDefaults.standard.data(forKey: patternsKey),
           let patterns = try? JSONDecoder().decode([PatternRule].self, from: data) {
            customPatterns = patterns
        }
    }
    
    private func savePatterns() {
        if let data = try? JSONEncoder().encode(customPatterns) {
            UserDefaults.standard.set(data, forKey: patternsKey)
        }
    }
}
