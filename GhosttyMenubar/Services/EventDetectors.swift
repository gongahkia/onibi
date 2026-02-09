import Foundation

/// Detects AI assistant outputs (Claude, GPT, etc.) in terminal output
final class AIResponseDetector: EventParser {
    var eventType: NotificationType { .aiOutput }
    
    private let patterns: [NSRegularExpression]
    
    init() {
        self.patterns = CommandPatterns.compilePatterns(CommandPatterns.aiPatterns)
    }
    
    func matches(_ content: String) -> Bool {
        for pattern in patterns {
            let range = NSRange(content.startIndex..., in: content)
            if pattern.firstMatch(in: content, options: [], range: range) != nil {
                return true
            }
        }
        return false
    }
    
    func parse(_ content: String, timestamp: Date) -> GhosttyEvent? {
        guard matches(content) else { return nil }
        
        return GhosttyEvent(
            timestamp: timestamp,
            type: .output,
            command: nil,
            output: content,
            metadata: ["source": "ai"]
        )
    }
}

/// Detects task completion notifications
final class TaskCompletionDetector: EventParser {
    var eventType: NotificationType { .taskCompletion }
    
    private let patterns: [NSRegularExpression]
    
    init() {
        self.patterns = CommandPatterns.compilePatterns(CommandPatterns.taskPatterns)
    }
    
    func matches(_ content: String) -> Bool {
        for pattern in patterns {
            let range = NSRange(content.startIndex..., in: content)
            if pattern.firstMatch(in: content, options: [], range: range) != nil {
                return true
            }
        }
        return false
    }
    
    func parse(_ content: String, timestamp: Date) -> GhosttyEvent? {
        guard matches(content) else { return nil }
        
        return GhosttyEvent(
            timestamp: timestamp,
            type: .output,
            command: nil,
            output: content,
            metadata: ["source": "task"]
        )
    }
}

/// Detects build/test/deployment workflow events
final class DevWorkflowParser: EventParser {
    var eventType: NotificationType { .devWorkflow }
    
    private let buildPatterns: [NSRegularExpression]
    private let testPatterns: [NSRegularExpression]
    
    init() {
        self.buildPatterns = CommandPatterns.compilePatterns(CommandPatterns.buildPatterns)
        self.testPatterns = CommandPatterns.compilePatterns(CommandPatterns.testPatterns)
    }
    
    func matches(_ content: String) -> Bool {
        let range = NSRange(content.startIndex..., in: content)
        
        for pattern in buildPatterns {
            if pattern.firstMatch(in: content, options: [], range: range) != nil {
                return true
            }
        }
        
        for pattern in testPatterns {
            if pattern.firstMatch(in: content, options: [], range: range) != nil {
                return true
            }
        }
        
        return false
    }
    
    func parse(_ content: String, timestamp: Date) -> GhosttyEvent? {
        guard matches(content) else { return nil }
        
        let workflowType = detectWorkflowType(content)
        
        return GhosttyEvent(
            timestamp: timestamp,
            type: .output,
            command: nil,
            output: content,
            metadata: ["source": "workflow", "workflowType": workflowType]
        )
    }
    
    private func detectWorkflowType(_ content: String) -> String {
        let range = NSRange(content.startIndex..., in: content)
        
        for pattern in buildPatterns {
            if pattern.firstMatch(in: content, options: [], range: range) != nil {
                return "build"
            }
        }
        
        for pattern in testPatterns {
            if pattern.firstMatch(in: content, options: [], range: range) != nil {
                return "test"
            }
        }
        
        return "unknown"
    }
}

/// Detects custom automation script outputs
final class AutomationOutputParser: EventParser {
    var eventType: NotificationType { .automation }
    
    /// Custom patterns that users can configure
    var customPatterns: [String] = []
    
    private var compiledPatterns: [NSRegularExpression] = []
    
    init(patterns: [String] = []) {
        self.customPatterns = patterns
        recompilePatterns()
    }
    
    func updatePatterns(_ patterns: [String]) {
        self.customPatterns = patterns
        recompilePatterns()
    }
    
    private func recompilePatterns() {
        compiledPatterns = CommandPatterns.compilePatterns(customPatterns)
    }
    
    func matches(_ content: String) -> Bool {
        for pattern in compiledPatterns {
            let range = NSRange(content.startIndex..., in: content)
            if pattern.firstMatch(in: content, options: [], range: range) != nil {
                return true
            }
        }
        return false
    }
    
    func parse(_ content: String, timestamp: Date) -> GhosttyEvent? {
        guard matches(content) else { return nil }
        
        return GhosttyEvent(
            timestamp: timestamp,
            type: .output,
            command: nil,
            output: content,
            metadata: ["source": "automation"]
        )
    }
}
