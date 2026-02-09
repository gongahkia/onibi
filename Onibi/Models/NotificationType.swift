import Foundation

/// Types of notifications displayed in the menubar
enum NotificationType: String, Codable, CaseIterable {
    case system = "system"
    case taskCompletion = "taskCompletion"
    case aiOutput = "aiOutput"
    case devWorkflow = "devWorkflow"
    case automation = "automation"
    case terminalNotification = "terminalNotification"
    
    var displayName: String {
        switch self {
        case .system: return "System"
        case .taskCompletion: return "Task Completion"
        case .aiOutput: return "AI Output"
        case .devWorkflow: return "Dev Workflow"
        case .automation: return "Automation"
        case .terminalNotification: return "Terminal Notification"
        }
    }
    
    var iconName: String {
        switch self {
        case .system: return "gear"
        case .taskCompletion: return "checkmark.circle.fill"
        case .aiOutput: return "sparkles"
        case .devWorkflow: return "hammer.fill"
        case .automation: return "bolt.fill"
        case .terminalNotification: return "bell.badge.fill"
        }
    }
}
