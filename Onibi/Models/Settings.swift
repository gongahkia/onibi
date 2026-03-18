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
    var menubarIconStyle: String  // SF Symbol name for menubar icon
    var mobileAccessEnabled: Bool
    var mobileAccessPort: Int
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
        static let mobileAccessPort = 8787
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
        logFilePath: String = Defaults.logFilePath,
        ghosttyBundleId: String = Defaults.ghosttyBundleId,
        ghosttyBinaryPath: String = Defaults.ghosttyBinaryPath,
        maxErrorLogSizeBytes: Int64 = Defaults.maxErrorLogSizeBytes,
        errorLogMaxRotations: Int = Defaults.errorLogMaxRotations,
        notificationDeduplicationWindow: TimeInterval = Defaults.notificationDeduplicationWindow,
        menubarIconStyle: String = "terminal",
        mobileAccessEnabled: Bool = false,
        mobileAccessPort: Int = Defaults.mobileAccessPort
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
        self.ghosttyBundleId = ghosttyBundleId
        self.ghosttyBinaryPath = ghosttyBinaryPath
        self.maxErrorLogSizeBytes = maxErrorLogSizeBytes
        self.errorLogMaxRotations = errorLogMaxRotations
        self.notificationDeduplicationWindow = notificationDeduplicationWindow
        self.menubarIconStyle = menubarIconStyle
        self.mobileAccessEnabled = mobileAccessEnabled
        self.mobileAccessPort = mobileAccessPort
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
        copy.mobileAccessPort = min(max(1, copy.mobileAccessPort), 65535)
        return copy
    }
}

extension AppSettings {
    enum CodingKeys: String, CodingKey {
        case theme
        case syncThemeWithGhostty
        case customTheme
        case userPersona
        case logVolumeProfile
        case notifications
        case logRetentionDays
        case maxStorageMB
        case maxLogFileSizeMB
        case maxLogLines
        case maxNotificationCount
        case detectionThreshold
        case hasCompletedOnboarding
        case autoStartOnLogin
        case showInDock
        case playNotificationSounds
        case filterRules
        case logFilePath
        case ghosttyBundleId
        case ghosttyBinaryPath
        case maxErrorLogSizeBytes
        case errorLogMaxRotations
        case notificationDeduplicationWindow
        case menubarIconStyle
        case mobileAccessEnabled
        case mobileAccessPort
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            theme: try container.decodeIfPresent(Theme.self, forKey: .theme) ?? .system,
            syncThemeWithGhostty: try container.decodeIfPresent(Bool.self, forKey: .syncThemeWithGhostty) ?? false,
            customTheme: try container.decodeIfPresent(CustomTheme.self, forKey: .customTheme),
            userPersona: try container.decodeIfPresent(UserPersona.self, forKey: .userPersona) ?? .casual,
            logVolumeProfile: try container.decodeIfPresent(LogVolumeProfile.self, forKey: .logVolumeProfile) ?? .moderate,
            notifications: try container.decodeIfPresent(NotificationPreferences.self, forKey: .notifications) ?? NotificationPreferences(),
            logRetentionDays: try container.decodeIfPresent(Int.self, forKey: .logRetentionDays) ?? Defaults.logRetentionDays,
            maxStorageMB: try container.decodeIfPresent(Int.self, forKey: .maxStorageMB) ?? Defaults.maxStorageMB,
            maxLogFileSizeMB: try container.decodeIfPresent(Int.self, forKey: .maxLogFileSizeMB) ?? Defaults.maxLogFileSizeMB,
            maxLogLines: try container.decodeIfPresent(Int.self, forKey: .maxLogLines) ?? Defaults.maxLogLines,
            maxNotificationCount: try container.decodeIfPresent(Int.self, forKey: .maxNotificationCount),
            detectionThreshold: try container.decodeIfPresent(Double.self, forKey: .detectionThreshold) ?? 0.5,
            hasCompletedOnboarding: try container.decodeIfPresent(Bool.self, forKey: .hasCompletedOnboarding) ?? false,
            autoStartOnLogin: try container.decodeIfPresent(Bool.self, forKey: .autoStartOnLogin) ?? false,
            showInDock: try container.decodeIfPresent(Bool.self, forKey: .showInDock) ?? false,
            playNotificationSounds: try container.decodeIfPresent(Bool.self, forKey: .playNotificationSounds) ?? true,
            filterRules: try container.decodeIfPresent([FilterRule].self, forKey: .filterRules) ?? [],
            logFilePath: try container.decodeIfPresent(String.self, forKey: .logFilePath) ?? Defaults.logFilePath,
            ghosttyBundleId: try container.decodeIfPresent(String.self, forKey: .ghosttyBundleId) ?? Defaults.ghosttyBundleId,
            ghosttyBinaryPath: try container.decodeIfPresent(String.self, forKey: .ghosttyBinaryPath) ?? Defaults.ghosttyBinaryPath,
            maxErrorLogSizeBytes: try container.decodeIfPresent(Int64.self, forKey: .maxErrorLogSizeBytes) ?? Defaults.maxErrorLogSizeBytes,
            errorLogMaxRotations: try container.decodeIfPresent(Int.self, forKey: .errorLogMaxRotations) ?? Defaults.errorLogMaxRotations,
            notificationDeduplicationWindow: try container.decodeIfPresent(TimeInterval.self, forKey: .notificationDeduplicationWindow) ?? Defaults.notificationDeduplicationWindow,
            menubarIconStyle: try container.decodeIfPresent(String.self, forKey: .menubarIconStyle) ?? "terminal",
            mobileAccessEnabled: try container.decodeIfPresent(Bool.self, forKey: .mobileAccessEnabled) ?? false,
            mobileAccessPort: try container.decodeIfPresent(Int.self, forKey: .mobileAccessPort) ?? Defaults.mobileAccessPort
        )
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(theme, forKey: .theme)
        try container.encode(syncThemeWithGhostty, forKey: .syncThemeWithGhostty)
        try container.encodeIfPresent(customTheme, forKey: .customTheme)
        try container.encode(userPersona, forKey: .userPersona)
        try container.encode(logVolumeProfile, forKey: .logVolumeProfile)
        try container.encode(notifications, forKey: .notifications)
        try container.encode(logRetentionDays, forKey: .logRetentionDays)
        try container.encode(maxStorageMB, forKey: .maxStorageMB)
        try container.encode(maxLogFileSizeMB, forKey: .maxLogFileSizeMB)
        try container.encode(maxLogLines, forKey: .maxLogLines)
        try container.encode(maxNotificationCount, forKey: .maxNotificationCount)
        try container.encode(detectionThreshold, forKey: .detectionThreshold)
        try container.encode(hasCompletedOnboarding, forKey: .hasCompletedOnboarding)
        try container.encode(autoStartOnLogin, forKey: .autoStartOnLogin)
        try container.encode(showInDock, forKey: .showInDock)
        try container.encode(playNotificationSounds, forKey: .playNotificationSounds)
        try container.encode(filterRules, forKey: .filterRules)
        try container.encode(logFilePath, forKey: .logFilePath)
        try container.encode(ghosttyBundleId, forKey: .ghosttyBundleId)
        try container.encode(ghosttyBinaryPath, forKey: .ghosttyBinaryPath)
        try container.encode(maxErrorLogSizeBytes, forKey: .maxErrorLogSizeBytes)
        try container.encode(errorLogMaxRotations, forKey: .errorLogMaxRotations)
        try container.encode(notificationDeduplicationWindow, forKey: .notificationDeduplicationWindow)
        try container.encode(menubarIconStyle, forKey: .menubarIconStyle)
        try container.encode(mobileAccessEnabled, forKey: .mobileAccessEnabled)
        try container.encode(mobileAccessPort, forKey: .mobileAccessPort)
    }
}
