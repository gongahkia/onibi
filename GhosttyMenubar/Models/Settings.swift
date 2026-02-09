import Foundation

/// User preferences and app configuration
struct Settings: Codable, Equatable {
    var theme: Theme
    var notifications: NotificationPreferences
    var logRetentionDays: Int
    var maxStorageMB: Int
    var autoStartOnLogin: Bool
    var showInDock: Bool
    var playNotificationSounds: Bool
    var filterRules: [FilterRule]
    
    init(
        theme: Theme = .system,
        notifications: NotificationPreferences = NotificationPreferences(),
        logRetentionDays: Int = 7,
        maxStorageMB: Int = 100,
        autoStartOnLogin: Bool = false,
        showInDock: Bool = false,
        playNotificationSounds: Bool = true,
        filterRules: [FilterRule] = []
    ) {
        self.theme = theme
        self.notifications = notifications
        self.logRetentionDays = logRetentionDays
        self.maxStorageMB = maxStorageMB
        self.autoStartOnLogin = autoStartOnLogin
        self.showInDock = showInDock
        self.playNotificationSounds = playNotificationSounds
        self.filterRules = filterRules
    }
    
    static let `default` = Settings()
}
