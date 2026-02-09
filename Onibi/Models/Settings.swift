import Foundation

/// User preferences and app configuration
struct Settings: Codable, Equatable {
    var theme: Theme
    var userPersona: UserPersona
    var notifications: NotificationPreferences
    var logRetentionDays: Int
    var maxStorageMB: Int
    var maxLogFileSizeMB: Int
    var maxLogLines: Int
    var autoStartOnLogin: Bool
    var showInDock: Bool
    var playNotificationSounds: Bool
    var filterRules: [FilterRule]
    var logFilePath: String
    
    enum Defaults {
        static let logRetentionDays = 7
        static let maxStorageMB = 100
        static let maxLogFileSizeMB = 10
        static let maxLogLines = 10000
        static let logFilePath = NSHomeDirectory() + "/.config/onibi/terminal.log"
    }
    
    init(
        theme: Theme = .system,
        userPersona: UserPersona = .casual,
        notifications: NotificationPreferences = NotificationPreferences(),
        logRetentionDays: Int = Defaults.logRetentionDays,
        maxStorageMB: Int = Defaults.maxStorageMB,
        maxLogFileSizeMB: Int = Defaults.maxLogFileSizeMB,
        maxLogLines: Int = Defaults.maxLogLines,
        autoStartOnLogin: Bool = false,
        showInDock: Bool = false,
        playNotificationSounds: Bool = true,
        filterRules: [FilterRule] = [],
        logFilePath: String = Defaults.logFilePath
    ) {
        self.theme = theme
        self.userPersona = userPersona
        self.notifications = notifications
        self.logRetentionDays = logRetentionDays
        self.maxStorageMB = maxStorageMB
        self.maxLogFileSizeMB = maxLogFileSizeMB
        self.maxLogLines = maxLogLines
        self.autoStartOnLogin = autoStartOnLogin
        self.showInDock = showInDock
        self.playNotificationSounds = playNotificationSounds
        self.filterRules = filterRules
        self.logFilePath = logFilePath
    }
    
    static let `default` = Settings()
    
    /// Returns a validated copy with safe values
    func validated() -> Settings {
        var copy = self
        copy.logRetentionDays = max(1, copy.logRetentionDays)
        copy.maxStorageMB = max(10, copy.maxStorageMB)
        copy.maxLogFileSizeMB = max(1, copy.maxLogFileSizeMB)
        copy.maxLogLines = max(100, copy.maxLogLines)
        if copy.logFilePath.isEmpty {
            copy.logFilePath = Defaults.logFilePath
        }
        return copy
    }
}
