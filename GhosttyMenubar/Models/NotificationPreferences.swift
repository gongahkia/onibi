import Foundation

/// Per-type notification preferences
struct NotificationPreferences: Codable, Equatable {
    var enableSystem: Bool
    var enableTaskCompletion: Bool
    var enableAIOutput: Bool
    var enableDevWorkflow: Bool
    var enableAutomation: Bool
    var soundName: String?
    var showBadge: Bool
    var autoExpireMinutes: Int?
    var useNativeNotifications: Bool
    
    init(
        enableSystem: Bool = true,
        enableTaskCompletion: Bool = true,
        enableAIOutput: Bool = true,
        enableDevWorkflow: Bool = true,
        enableAutomation: Bool = true,
        soundName: String? = nil,
        showBadge: Bool = true,
        autoExpireMinutes: Int? = nil,
        useNativeNotifications: Bool = true
    ) {
        self.enableSystem = enableSystem
        self.enableTaskCompletion = enableTaskCompletion
        self.enableAIOutput = enableAIOutput
        self.enableDevWorkflow = enableDevWorkflow
        self.enableAutomation = enableAutomation
        self.soundName = soundName
        self.showBadge = showBadge
        self.autoExpireMinutes = autoExpireMinutes
        self.useNativeNotifications = useNativeNotifications
    }
    
    /// Check if notifications are enabled for a specific type
    func isEnabled(for type: NotificationType) -> Bool {
        switch type {
        case .system: return enableSystem
        case .taskCompletion: return enableTaskCompletion
        case .aiOutput: return enableAIOutput
        case .devWorkflow: return enableDevWorkflow
        case .automation: return enableAutomation
        }
    }
}
