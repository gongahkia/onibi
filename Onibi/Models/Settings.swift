import Foundation

/// Custom theme colors, e.g. from Ghostty sync
struct CustomTheme: Codable, Equatable {
    var backgroundColor: String
    var foregroundColor: String
    var accentColor: String?
}

/// Log resource usage profile
enum LogVolumeProfile: String, Codable, CaseIterable {
    case light
    case moderate
    case heavy
    
    var displayName: String {
        switch self {
        case .light: return "Light (Battery Saver)"
        case .moderate: return "Moderate (Balanced)"
        case .heavy: return "Heavy (Real-time)"
        }
    }
    
    var description: String {
        switch self {
        case .light: return "Polls less frequently, smaller log retention. Best for battery life."
        case .moderate: return "Balanced polling and retention. Recommended for most users."
        case .heavy: return "Frequent polling, larger log retention. Best for debugging or high-throughput."
        }
    }
    
    var debounceInterval: TimeInterval {
        switch self {
        case .light: return 2.0
        case .moderate: return 0.5
        case .heavy: return 0.1
        }
    }
    
    var maxFileSizeMB: Int {
        switch self {
        case .light: return 2
        case .moderate: return 10
        case .heavy: return 50
        }
    }
}

/// User preferences and app configuration
struct AppSettings: Codable, Equatable {
    var theme: Theme
    var syncThemeWithGhostty: Bool
    var customTheme: CustomTheme?
    var userPersona: UserPersona
    var logVolumeProfile: LogVolumeProfile
    var notifications: NotificationPreferences
    var logRetentionDays: Int
    var maxStorageMB: Int
    var maxLogFileSizeMB: Int
    var maxLogLines: Int
    var maxNotificationCount: Int
    var detectionThreshold: Double = 0.5
    var hasCompletedOnboarding: Bool
    var autoStartOnLogin: Bool
    var showInDock: Bool
    var playNotificationSounds: Bool
    var filterRules: [FilterRule]
    var logFilePath: String
    var ghosttyBundleId: String
    var ghosttyBinaryPath: String
    var maxErrorLogSizeBytes: Int64
    var errorLogMaxRotations: Int
    var notificationDeduplicationWindow: TimeInterval
    enum Defaults {
        static let logRetentionDays = 7
        static let maxStorageMB = 100
        static let maxLogFileSizeMB = 10
        static let maxLogLines = 10000
        static let logFilePath = NSHomeDirectory() + "/.config/onibi/terminal.log"
        static let ghosttyBundleId = "com.mitchellh.ghostty"
        static let ghosttyBinaryPath = "/usr/local/bin/ghostty"
        static let maxErrorLogSizeBytes: Int64 = 1_000_000
        static let errorLogMaxRotations = 3
        static let notificationDeduplicationWindow: TimeInterval = 5.0
        static func maxNotificationCount(for persona: UserPersona) -> Int {
            switch persona {
            case .casual: return 100
            case .powerUser: return 500
            }
        }
    }
    
    init(
        theme: Theme = .system,
        syncThemeWithGhostty: Bool = false,
        customTheme: CustomTheme? = nil,
        userPersona: UserPersona = .casual,
        logVolumeProfile: LogVolumeProfile = .moderate,
        notifications: NotificationPreferences = NotificationPreferences(),
        logRetentionDays: Int = Defaults.logRetentionDays,
        maxStorageMB: Int = Defaults.maxStorageMB,
        maxLogFileSizeMB: Int = Defaults.maxLogFileSizeMB,
        maxLogLines: Int = Defaults.maxLogLines,
        maxNotificationCount: Int? = nil,
        detectionThreshold: Double = 0.5,
        hasCompletedOnboarding: Bool = false,
        autoStartOnLogin: Bool = false,
        showInDock: Bool = false,
        playNotificationSounds: Bool = true,
        filterRules: [FilterRule] = [],
        logFilePath: String = Defaults.logFilePath
    ) {
        self.theme = theme
        self.syncThemeWithGhostty = syncThemeWithGhostty
        self.customTheme = customTheme
        self.userPersona = userPersona
        self.logVolumeProfile = logVolumeProfile
        self.notifications = notifications
        self.logRetentionDays = logRetentionDays
        self.maxStorageMB = maxStorageMB
        self.maxLogFileSizeMB = maxLogFileSizeMB
        self.maxLogLines = maxLogLines
        self.maxNotificationCount = maxNotificationCount ?? Defaults.maxNotificationCount(for: userPersona)
        self.detectionThreshold = detectionThreshold
        self.hasCompletedOnboarding = hasCompletedOnboarding
        self.autoStartOnLogin = autoStartOnLogin
        self.showInDock = showInDock
        self.playNotificationSounds = playNotificationSounds
        self.filterRules = filterRules
        self.logFilePath = logFilePath
    }
    
    static let `default` = AppSettings()
    
    /// Returns a validated copy with safe values
    func validated() -> AppSettings {
        var copy = self
        copy.logRetentionDays = max(1, copy.logRetentionDays)
        copy.maxStorageMB = max(10, copy.maxStorageMB)
        copy.maxLogFileSizeMB = max(1, copy.maxLogFileSizeMB)
        copy.maxLogLines = max(100, copy.maxLogLines)
        copy.detectionThreshold = min(max(0.0, copy.detectionThreshold), 1.0)
        if copy.logFilePath.isEmpty {
            copy.logFilePath = Defaults.logFilePath
        }
        return copy
    }
}
