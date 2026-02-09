import Foundation

/// User preferences and app configuration
struct Settings: Codable, Equatable {
    var theme: Theme
    var notifications: NotificationPreferences
    var logRetentionDays: Int
    var maxStorageMB: Int
    var maxLogFileSizeMB: Int
    var maxLogLines: Int
    var autoStartOnLogin: Bool
    var showInDock: Bool
    var playNotificationSounds: Bool
    var filterRules: [FilterRule]
    
    init(
        theme: Theme = .system,
        notifications: NotificationPreferences = NotificationPreferences(),
        logRetentionDays: Int = 7,
        maxStorageMB: Int = 100,
        maxLogFileSizeMB: Int = 10,
        maxLogLines: Int = 10000,
        autoStartOnLogin: Bool = false,
        showInDock: Bool = false,
        playNotificationSounds: Bool = true,
        filterRules: [FilterRule] = []
    ) {
        self.theme = theme
        self.notifications = notifications
        self.logRetentionDays = logRetentionDays
        self.maxStorageMB = maxStorageMB
        self.maxLogFileSizeMB = maxLogFileSizeMB
        self.maxLogLines = maxLogLines
        self.autoStartOnLogin = autoStartOnLogin
        self.showInDock = showInDock
        self.playNotificationSounds = playNotificationSounds
        self.filterRules = filterRules
    }
    
    static let `default` = Settings()
    
    /// Returns a validated copy with safe values
    func validated() -> Settings {
        var copy = self
        copy.logRetentionDays = max(1, copy.logRetentionDays)
        copy.maxStorageMB = max(10, copy.maxStorageMB)
        copy.maxLogFileSizeMB = max(1, copy.maxLogFileSizeMB)
        copy.maxLogLines = max(100, copy.maxLogLines)
        return copy
    }
}
